package shipment

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Transitioner applies shipment state changes.
//
// Every Release 2 module — pickup, scanning, bagging, manifest, line haul, hub
// operations, delivery, NDR, RTO — changes shipment state through this one
// type. That is deliberate: the permission check, the custody check, the
// compare-and-swap, the append-only event and the audit record are five things
// that must happen together every time, and five modules each remembering to do
// all five is five chances to forget one.
type Transitioner struct {
	q       *dbgen.Queries
	audit   *audit.Recorder
	log     *slog.Logger
	metrics *telemetry.Metrics
	// maxClockSkew bounds how far a client-reported occurrence time may sit in
	// the future before it is clamped.
	maxClockSkew time.Duration
	// maxBackdate bounds how far in the past a client may claim an event
	// happened. A field app that was offline for a shift is legitimate; one
	// claiming last month is not.
	maxBackdate time.Duration

	// observers run in the transition's transaction. See Observer.
	observers []Observer
}

// NewTransitioner builds the engine.
func NewTransitioner(q *dbgen.Queries, rec *audit.Recorder, log *slog.Logger, m *telemetry.Metrics) *Transitioner {
	return &Transitioner{
		q: q, audit: rec, log: log, metrics: m,
		maxClockSkew: 5 * time.Minute,
		maxBackdate:  7 * 24 * time.Hour,
	}
}

// Actor identifies who is making a change and from where.
//
// FacilityID is the operating unit the actor is physically standing in, which
// is what custody is checked against. It is resolved server-side from the
// authenticated principal's scope plus the unit named in the request; a client
// cannot simply assert it.
type Actor struct {
	Principal  *tenant.Principal
	FacilityID *int64
	// Source and device describe where the event came from, for forensics.
	Source      string
	DeviceID    string
	DeviceModel string
	Latitude    *float64
	Longitude   *float64
}

// Request is one state change.
type Request struct {
	// Shipment must already be locked FOR UPDATE by the caller. Passing the
	// locked row rather than an id makes it impossible to apply a transition
	// against a state nobody has serialised on.
	Shipment dbgen.Shipment
	To       Status

	Reason      string
	ReasonCode  string
	Description string
	// InternalRemarks never reach public tracking.
	InternalRemarks string

	// OccurredAt is the client's claim about when this happened. Nil means now.
	OccurredAt *time.Time

	// Custody changes applied alongside the state change. A nil pointer with
	// the matching Set flag false leaves the column untouched; the Set flag
	// with a nil pointer clears it.
	SetCustodyUnit bool
	CustodyUnitID  *int64
	SetCustodyUser bool
	CustodyUserID  *int64
	SetBag         bool
	BagID          *int64
	SetTrip        bool
	TripID         *int64

	Direction Direction

	IncrementDeliveryAttempt bool
	IncrementPickupAttempt   bool

	// IdempotencyKey deduplicates the event at the database level via the
	// partial unique index on shipment_events.
	IdempotencyKey string

	Metadata map[string]any

	// AuditAction, when set, writes an audit record in the same transaction.
	AuditAction string

	// AllowHeld overrides the hold block. Used by the RELEASE scan itself and
	// by privileged custody overrides, both of which are audited.
	AllowHeld bool

	// SkipCustodyCheck is the privileged override. It requires
	// shipment.override_custody and always writes an audit record naming the
	// rule that was bypassed.
	SkipCustodyCheck bool

	// SystemInitiated marks a transition the platform made by policy rather
	// than one a person asked for — an automatic RTO once the NDR attempt
	// budget is exhausted, for example.
	//
	// It skips the actor's permission and custody checks, because the actor did
	// not choose this: a delivery agent recording their third failed attempt
	// must not need rto.manage for the configured policy to fire. The event is
	// attributed to SYSTEM so the history says who really decided, and the
	// caller is expected to have already checked whatever permission governs
	// the operation that triggered it.
	SystemInitiated bool
}

// Result is what the transition produced.
type Result struct {
	Shipment dbgen.Shipment
	Event    dbgen.ShipmentEvent
	From     Status
	To       Status
}

// Apply performs one state change inside the caller's transaction.
//
// Order matters and is fixed: validate the transition, check the actor's
// permission, check custody, then compare-and-swap the row, append the event,
// and record the audit. Nothing is written until every check has passed, and
// everything written commits together with the caller's other work.
func (t *Transitioner) Apply(ctx context.Context, tx pgx.Tx, actor Actor, req Request) (*Result, error) {
	p := actor.Principal
	from := Status(req.Shipment.CurrentStatus)

	direction := req.Direction
	if direction == "" {
		direction = Direction(req.Shipment.MovementDirection)
	}

	// 1. Is this transition legal at all, in this release, in this direction?
	def, err := ValidateDirection(from, req.To, direction)
	if err != nil {
		return nil, err
	}

	// 2. Does the actor hold the permission this transition demands?
	if !req.SystemInitiated {
		if err := p.Require(def.Permission); err != nil {
			return nil, err
		}
	}

	// 3. Reason, where the transition demands one.
	if def.RequiresReason && req.Reason == "" {
		return nil, apierr.Validation("This transition requires a reason.",
			map[string]any{"field": "reason", "fromStatus": string(from), "toStatus": string(req.To)})
	}

	// 4. Is the parcel on hold? A held parcel does not move.
	if req.Shipment.IsHeld && def.BlockedByHold && !req.AllowHeld {
		reason := ""
		if req.Shipment.HoldReason != nil {
			reason = *req.Shipment.HoldReason
		}
		return nil, apierr.Conflict("SHIPMENT_ON_HOLD",
			"This shipment is on hold and cannot be moved until it is released.").
			WithDetail("holdReason", reason)
	}

	// 5. Custody.
	if req.SkipCustodyCheck {
		if err := p.Require("shipment.override_custody"); err != nil {
			return nil, err
		}
		if req.Reason == "" {
			return nil, apierr.Validation("A custody override requires a reason.",
				map[string]any{"field": "reason"})
		}
	} else if !req.SystemInitiated {
		if err := t.checkCustody(def, actor, req.Shipment); err != nil {
			return nil, err
		}
	}

	// 6. Occurrence time. The client's claim is recorded verbatim but the value
	// used for ordering is clamped, so a wrong device clock cannot reorder
	// history or backdate an event out of an audit window.
	occurred, clientClaim := t.resolveOccurredAt(req.OccurredAt)

	q := t.q.WithTx(tx)

	// 7. Compare-and-swap. A concurrent transition on the same shipment updates
	// zero rows here rather than overwriting the other actor's change.
	updated, err := q.ApplyOperationalTransition(ctx, dbgen.ApplyOperationalTransitionParams{
		ID:                       req.Shipment.ID,
		OrganizationID:           req.Shipment.OrganizationID,
		ExpectedStatus:           string(from),
		ToStatus:                 string(req.To),
		SetCustodyUnit:           req.SetCustodyUnit,
		CustodyUnitID:            req.CustodyUnitID,
		SetCustodyUser:           req.SetCustodyUser,
		CustodyUserID:            req.CustodyUserID,
		SetBag:                   req.SetBag,
		BagID:                    req.BagID,
		SetTrip:                  req.SetTrip,
		TripID:                   req.TripID,
		MovementDirection:        directionArg(req.Direction),
		DeliveryAttemptIncrement: boolToInt(req.IncrementDeliveryAttempt),
		PickupAttemptIncrement:   boolToInt(req.IncrementPickupAttempt),
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.Conflict(apierr.CodeConcurrentModification,
				"This shipment changed state while the request was being processed. Reload and try again.").
				WithDetail("expectedStatus", string(from)).
				WithDetail("attemptedStatus", string(req.To))
		}
		return nil, apierr.Internal(fmt.Errorf("apply transition %s->%s: %w", from, req.To, err))
	}

	// 8. Append-only history.
	description := req.Description
	if description == "" {
		description = def.Description
	}
	event, err := q.AppendShipmentEventFull(ctx, dbgen.AppendShipmentEventFullParams{
		PublicID:         publicid.New(publicid.PrefixShipmentEvent),
		OrganizationID:   req.Shipment.OrganizationID,
		ShipmentID:       updated.ID,
		Sequence:         updated.EventSequence,
		EventType:        def.EventType,
		FromStatus:       strPtr(string(from)),
		ToStatus:         string(req.To),
		OccurredAt:       &occurred,
		ClientOccurredAt: clientClaim,
		ActorUserID:      actorUserID(p),
		ActorType:        actorTypeFor(p, req.SystemInitiated),
		OperatingUnitID:  actor.FacilityID,
		LocationPincode:  nil,
		Description:      description,
		InternalRemarks:  optional(req.InternalRemarks),
		ReasonCode:       optional(req.ReasonCode),
		RequestID:        optional(httpx.RequestID(ctx)),
		IdempotencyKey:   optional(req.IdempotencyKey),
		Source:           sourceOrDefault(actor.Source),
		DeviceID:         optional(actor.DeviceID),
		DeviceModel:      optional(actor.DeviceModel),
		Latitude:         actor.Latitude,
		Longitude:        actor.Longitude,
		Metadata:         encodeJSON(req.Metadata),
	})
	if err != nil {
		if database.IsUniqueViolation(err, "shipment_events_idempotency_idx") {
			return nil, apierr.Conflict(apierr.CodeDuplicate,
				"This operation was already recorded for this shipment.").
				WithDetail("idempotencyKey", req.IdempotencyKey)
		}
		return nil, apierr.Internal(fmt.Errorf("append shipment event: %w", err))
	}

	// 9. Audit, in the same transaction so it cannot be lost.
	if req.AuditAction != "" {
		after := map[string]any{
			"status": string(req.To), "awb": updated.Awb,
			"custodyUnitId": updated.CurrentCustodyUnitID,
		}
		if req.SkipCustodyCheck {
			after["custodyOverride"] = true
			after["bypassedCustodyRule"] = string(def.Custody)
		}
		entry := audit.Entry{
			Action: req.AuditAction, ResourceType: "shipment",
			ResourceID: &updated.ID, ResourcePublicID: updated.PublicID,
			OperatingUnitID: actor.FacilityID, Reason: req.Reason,
			Before: map[string]any{"status": string(from)},
			After:  after,
		}
		if err := t.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, entry)); err != nil {
			return nil, apierr.Internal(fmt.Errorf("record transition audit: %w", err))
		}
	}

	t.metrics.RecordBusiness("shipment_transition", string(req.To))
	result := &Result{Shipment: updated, Event: event, From: from, To: req.To}

	// 10. Observers: customer notifications (M26) and partner webhooks (M32).
	if err := t.runObservers(ctx, tx, p, result); err != nil {
		return nil, err
	}
	return result, nil
}

// Observer is called after a transition's writes, inside the same transaction.
//
// This is the outbox point of the platform. An observer may only *enqueue* work
// — insert a notification row, insert a webhook delivery row — never contact an
// external system. That is what makes a shipment's success independent of any
// provider's availability: the state change and the intent to tell someone
// commit together, and the telling happens later on a worker that can retry.
//
// An observer that fails aborts the transition, which sounds severe but is the
// only honest option: it runs inside the caller's transaction, so a failed
// statement has already poisoned it. The only failures possible are database
// failures, at which point the business write was not going to commit either.
type Observer func(ctx context.Context, tx pgx.Tx, p *tenant.Principal, res *Result) error

// Observe registers an observer. Called once at wiring time, never concurrently
// with request handling.
func (t *Transitioner) Observe(o Observer) { t.observers = append(t.observers, o) }

func (t *Transitioner) runObservers(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, res *Result,
) error {
	for _, o := range t.observers {
		if err := o(ctx, tx, p, res); err != nil {
			return err
		}
	}
	return nil
}

// RecordEvent appends history without changing state.
//
// Reverse movement uses this: an RTO parcel scanned into a hub stays
// RTO_IN_TRANSIT but its passage through that facility is a fact the timeline
// must carry. So does a remark, and a location ping from a trip.
func (t *Transitioner) RecordEvent(
	ctx context.Context, tx pgx.Tx, actor Actor, s dbgen.Shipment,
	eventType, description string, req Request,
) (*dbgen.ShipmentEvent, error) {
	q := t.q.WithTx(tx)
	seq, err := q.ReserveShipmentEventSequence(ctx, dbgen.ReserveShipmentEventSequenceParams{
		ID: s.ID, OrganizationID: s.OrganizationID,
	})
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("reserve event sequence: %w", err))
	}
	occurred, clientClaim := t.resolveOccurredAt(req.OccurredAt)

	event, err := q.AppendShipmentEventFull(ctx, dbgen.AppendShipmentEventFullParams{
		PublicID:       publicid.New(publicid.PrefixShipmentEvent),
		OrganizationID: s.OrganizationID, ShipmentID: s.ID, Sequence: seq.EventSequence,
		EventType: eventType,
		// from and to are the same: nothing moved in the lifecycle.
		FromStatus: strPtr(seq.CurrentStatus), ToStatus: seq.CurrentStatus,
		OccurredAt: &occurred, ClientOccurredAt: clientClaim,
		ActorUserID: actorUserID(actor.Principal), ActorType: actorType(actor.Principal),
		OperatingUnitID: actor.FacilityID,
		Description:     description,
		InternalRemarks: optional(req.InternalRemarks),
		ReasonCode:      optional(req.ReasonCode),
		RequestID:       optional(httpx.RequestID(ctx)),
		IdempotencyKey:  optional(req.IdempotencyKey),
		Source:          sourceOrDefault(actor.Source),
		DeviceID:        optional(actor.DeviceID),
		DeviceModel:     optional(actor.DeviceModel),
		Latitude:        actor.Latitude, Longitude: actor.Longitude,
		Metadata: encodeJSON(req.Metadata),
	})
	if err != nil {
		if database.IsUniqueViolation(err, "shipment_events_idempotency_idx") {
			return nil, apierr.Conflict(apierr.CodeDuplicate,
				"This operation was already recorded for this shipment.")
		}
		return nil, apierr.Internal(fmt.Errorf("append shipment event: %w", err))
	}
	return &event, nil
}

// checkCustody enforces the facility rule for a transition.
//
// The failures here are the ones that separate a real courier system from a
// status field: delivering from the wrong branch, dispatching a parcel that is
// physically somewhere else, receiving into a facility that never had it.
func (t *Transitioner) checkCustody(def *Transition, actor Actor, s dbgen.Shipment) error {
	switch def.Custody {
	case CustodyNone, "":
		return nil

	case CustodyHolder:
		if s.CurrentCustodyUnitID == nil {
			return custodyError(def, "This shipment is not currently held by any facility.", nil, actor.FacilityID)
		}
		if actor.FacilityID == nil {
			return missingFacility(def)
		}
		if *actor.FacilityID != *s.CurrentCustodyUnitID {
			return custodyError(def,
				"This shipment is held by another facility and cannot be processed here.",
				s.CurrentCustodyUnitID, actor.FacilityID)
		}
		return nil

	case CustodyOriginBranch:
		return requireUnit(def, actor.FacilityID, s.OriginBranchID,
			"This action must be performed at the shipment's origin branch.")

	case CustodyDestinationBranch:
		// The delivering branch must be the routed destination branch. Without
		// this, any branch in the network could mark a parcel out for delivery.
		if err := requireUnit(def, actor.FacilityID, s.DestinationBranchID,
			"This action must be performed at the shipment's destination branch."); err != nil {
			return err
		}
		// It must also physically be there.
		if s.CurrentCustodyUnitID == nil || *s.CurrentCustodyUnitID != *s.DestinationBranchID {
			return custodyError(def,
				"This shipment has not been received at the destination branch yet.",
				s.CurrentCustodyUnitID, actor.FacilityID)
		}
		return nil

	case CustodyAgent:
		// The acting user must be the one holding the parcel. This is what stops
		// a second agent completing a delivery for a parcel in someone else's bag.
		if s.CurrentCustodyUserID == nil {
			return custodyError(def,
				"This shipment is not out with a delivery agent.", nil, actor.FacilityID)
		}
		if actor.Principal.UserID != *s.CurrentCustodyUserID {
			// A supervisor with the override permission can still act, but only
			// through the explicit override path.
			return apierr.Forbidden(
				"This shipment is out with another agent. Only that agent can complete it.").
				WithDetail("code", "CUSTODY_VIOLATION").
				WithDetail("custodyRule", string(def.Custody))
		}
		return nil

	case CustodyAnyFacility:
		if actor.FacilityID == nil {
			return missingFacility(def)
		}
		return nil
	}
	return nil
}

func requireUnit(def *Transition, actual, expected *int64, message string) error {
	if expected == nil {
		return custodyError(def, "This shipment has no routed facility for this action.", nil, actual)
	}
	if actual == nil {
		return missingFacility(def)
	}
	if *actual != *expected {
		return custodyError(def, message, expected, actual)
	}
	return nil
}

func custodyError(def *Transition, message string, expected, actual *int64) error {
	e := apierr.Conflict("CUSTODY_VIOLATION", message).
		WithDetail("custodyRule", string(def.Custody)).
		WithDetail("fromStatus", string(def.From)).
		WithDetail("toStatus", string(def.To))
	// Internal ids are never exposed; the detail says only whether the facility
	// matched, which is all a client can act on.
	if expected != nil && actual != nil {
		e = e.WithDetail("facilityMatches", false)
	}
	return e
}

func missingFacility(def *Transition) error {
	return apierr.Validation("This action requires the operating unit you are working at.",
		map[string]any{"field": "operatingUnitId", "custodyRule": string(def.Custody)})
}

// resolveOccurredAt clamps a client-supplied timestamp and preserves the claim.
//
// Constitution: "Distinguish client occurrence time from server-recorded time"
// and "Never trust arbitrary client timestamps for financial/security
// decisions". A device with a wrong clock is common; a device deliberately
// backdating a delivery is fraud. Both are handled the same way — record what
// was claimed, order by what is plausible.
func (t *Transitioner) resolveOccurredAt(claim *time.Time) (time.Time, *time.Time) {
	now := time.Now()
	if claim == nil {
		return now, nil
	}
	c := *claim
	switch {
	case c.After(now.Add(t.maxClockSkew)):
		// A future timestamp is never accepted for ordering.
		return now, &c
	case c.Before(now.Add(-t.maxBackdate)):
		return now, &c
	default:
		return c, &c
	}
}

func actorUserID(p *tenant.Principal) *int64 {
	if p == nil {
		return nil
	}
	id := p.UserID
	return &id
}

func actorType(p *tenant.Principal) string { return actorTypeFor(p, false) }

// actorTypeFor attributes an event to whoever really caused it.
// actorTypeFor classifies who produced a shipment event.
//
// The vocabulary is the one shipment_events allows, which is not the one
// audit_events allows: a partner is PARTNER in operational history and API_KEY
// in the audit trail. They are different records with different readers, and
// collapsing the two would mean widening one CHECK for the other's benefit.
func actorTypeFor(p *tenant.Principal, systemInitiated bool) string {
	switch {
	case systemInitiated, p == nil:
		return "SYSTEM"
	case p.IsPartner:
		return "PARTNER"
	case p.IsPortalUser:
		return "CUSTOMER"
	default:
		return "USER"
	}
}

func sourceOrDefault(s string) string {
	if s == "" {
		return "API"
	}
	return s
}

func directionArg(d Direction) *string {
	if d == "" {
		return nil
	}
	s := string(d)
	return &s
}

func boolToInt(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// eventMetadataJSON is a small helper for modules building event metadata.
func eventMetadataJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return raw
}
