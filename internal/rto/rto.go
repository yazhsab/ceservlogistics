// Package rto implements M18: return to sender as reverse courier movement.
//
// The Constitution is explicit that RTO "must be modelled as reverse courier
// movement" and "not as a single status update". This module does that by
// reusing the forward network rather than duplicating it:
//
//   - The reverse route is resolved by the same routing engine, with origin and
//     destination swapped, so a return follows the lanes that actually exist.
//   - The parcel travels in the same bags, manifests and trips as forward
//     traffic, filtered by direction so the two never mix in one container.
//   - Its position on the return journey is tracked by rto_legs plus the
//     append-only event stream, so RTO_IN_TRANSIT is one status rather than a
//     mirrored copy of the whole forward ladder. See ADR 0009.
//
// The alternative — inventing RTO_ORIGIN_HUB_RECEIVED and friends — would have
// doubled the state machine to express a journey that uses identical facilities
// and identical scans.
package rto

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/serviceability"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Case states.
const (
	StatusInitiated      = "INITIATED"
	StatusInTransit      = "IN_TRANSIT"
	StatusAtOriginBranch = "AT_ORIGIN_BRANCH"
	StatusOutForReturn   = "OUT_FOR_RETURN"
	StatusReturned       = "RETURNED"
	StatusReturnFailed   = "RETURN_FAILED"
	StatusDisposed       = "DISPOSED"
	StatusCancelled      = "CANCELLED"
)

// AllStatuses is the published RTO lifecycle.
var AllStatuses = []string{
	StatusInitiated, StatusInTransit, StatusAtOriginBranch, StatusOutForReturn,
	StatusReturned, StatusReturnFailed, StatusDisposed, StatusCancelled,
}

// ChargeBearers say who pays for the return.
var ChargeBearers = []string{"CUSTOMER", "FRANCHISE", "ORGANIZATION", "WAIVED"}

// Service implements the RTO workflow.
type Service struct {
	db      *database.DB
	q       *dbgen.Queries
	units   *ops.Resolver
	codes   *ops.CodeAllocator
	routing *serviceability.Resolver
	trans   *shipment.Transitioner
	audit   *audit.Recorder
	log     *slog.Logger
}

// NewService builds the RTO service.
func NewService(
	db *database.DB, q *dbgen.Queries, units *ops.Resolver, codes *ops.CodeAllocator,
	routing *serviceability.Resolver, trans *shipment.Transitioner,
	rec *audit.Recorder, log *slog.Logger,
) *Service {
	return &Service{
		db: db, q: q, units: units, codes: codes, routing: routing,
		trans: trans, audit: rec, log: log,
	}
}

// Initiate starts a return, inside the caller's transaction.
//
// It is called from the NDR workflow and from the standalone endpoint, and both
// paths must produce the same reverse route, so the routing work lives here
// rather than in either caller.
func (s *Service) Initiate(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal,
	sh dbgen.Shipment, ndrCaseID *int64, reasonCode, notes string,
) (string, error) {
	return s.initiate(ctx, tx, p, sh, ndrCaseID, reasonCode, notes, false)
}

// InitiateByPolicy starts a return the NDR configuration demanded rather than
// one a person asked for.
//
// The distinction matters at the permission boundary: a delivery agent
// recording their last permitted attempt triggers this, and they hold neither
// rto.manage nor any custody claim on the reverse journey.
func (s *Service) InitiateByPolicy(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal,
	sh dbgen.Shipment, ndrCaseID *int64, reasonCode, notes string,
) (string, error) {
	return s.initiate(ctx, tx, p, sh, ndrCaseID, reasonCode, notes, true)
}

func (s *Service) initiate(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal,
	sh dbgen.Shipment, ndrCaseID *int64, reasonCode, notes string, byPolicy bool,
) (string, error) {
	q := s.q.WithTx(tx)

	if existing, err := q.GetActiveRTOCase(ctx, sh.ID); err == nil {
		return existing.PublicID, nil
	} else if !ops.IsNoRows(err) {
		return "", apierr.Internal(err)
	}

	// The return address is the sender's booking snapshot. Using the current
	// customer address would send the parcel wherever they have since moved,
	// which is not where it came from.
	sender, err := q.GetShipmentAddressSnapshot(ctx, dbgen.GetShipmentAddressSnapshotParams{
		ShipmentID: sh.ID, Role: "SENDER",
	})
	if err != nil {
		return "", ops.NotFoundOr(err, "Sender address")
	}

	route, err := s.resolveReversePath(ctx, p, sh, sender.Pincode)
	if err != nil {
		return "", err
	}

	code, err := s.codes.Allocate(ctx, p.OrganizationID, ops.KindRTOCase, "RTO", time.Now())
	if err != nil {
		return "", err
	}

	legsJSON, mErr := json.Marshal(route.legs)
	if mErr != nil {
		return "", apierr.Internal(mErr)
	}
	explainJSON, eErr := json.Marshal(route.explanation)
	if eErr != nil {
		return "", apierr.Internal(eErr)
	}

	params := dbgen.CreateRTOCaseParams{
		PublicID: publicid.New(publicid.PrefixRTOCase), OrganizationID: p.OrganizationID,
		CaseCode: code, ShipmentID: sh.ID, NdrCaseID: ndrCaseID,
		ReasonCode:  orDefault(reasonCode, "RTO_REQUESTED"),
		ReasonNotes: ops.Optional(notes), InitiatedByUserID: &p.UserID,
		ReturnBranchID: route.returnBranchID, ReturnHubID: route.returnHubID,
		OriginHubID:           sh.OriginHubID,
		RouteResolutionSource: &route.source,
		RouteLegs:             legsJSON, RouteExplanation: explainJSON,
		ReturnContactName: &sender.ContactName, ReturnPhone: &sender.Phone,
		ReturnLine1: &sender.Line1, ReturnLine2: sender.Line2,
		ReturnCity: &sender.CityName, ReturnState: &sender.StateName,
		ReturnPincode: &sender.Pincode,
		// Charging is computed here but settled in Release 3; the bearer is the
		// customer by default because a return normally follows a delivery
		// failure the customer caused.
		RtoChargeMinor: 0, Currency: sh.Currency, ChargeBearer: "CUSTOMER",
		ChargeRuleNotes: ops.Optional("RTO charge calculation lands with commission and settlement in Release 3"),
		Metadata:        []byte("{}"),
	}
	created, cErr := q.CreateRTOCase(ctx, params)
	if cErr != nil {
		if ops.IsUnique(cErr, "rto_cases_active_idx") {
			return "", apierr.Conflict(apierr.CodeConcurrentModification,
				"A return was started for this shipment a moment ago.")
		}
		return "", apierr.Internal(fmt.Errorf("create rto case: %w", cErr))
	}

	for i, leg := range route.legs {
		if _, lErr := q.CreateRTOLeg(ctx, dbgen.CreateRTOLegParams{
			OrganizationID: p.OrganizationID, RtoCaseID: created.ID, Sequence: int32(i + 1),
			FromUnitID: leg.fromID, ToUnitID: leg.toID, LegType: leg.legType,
			Status: "PENDING",
		}); lErr != nil {
			return "", apierr.Internal(fmt.Errorf("create rto leg %d: %w", i+1, lErr))
		}
	}

	// The shipment flips to reverse movement. From here the state machine
	// refuses forward transitions, which is what stops an RTO parcel being
	// accidentally delivered to the original consignee.
	if _, tErr := s.trans.Apply(ctx, tx, ops.Device{Source: "API"}.Actor(p, nil), shipment.Request{
		Shipment: sh, To: shipment.StatusRTOInitiated,
		Reason: orDefault(notes, "Return to sender"), ReasonCode: reasonCode,
		Description:     "Return to sender started",
		AuditAction:     audit.ActionRTOInitiated,
		SystemInitiated: byPolicy,
		Metadata: map[string]any{
			"rtoCase": code, "returnPincode": sender.Pincode,
			"resolutionSource": route.source, "legCount": len(route.legs),
		},
	}); tErr != nil {
		return "", tErr
	}
	return created.PublicID, nil
}

// InitiateStandalone starts a return outside the NDR workflow.
func (s *Service) InitiateStandalone(
	ctx context.Context, p *tenant.Principal, barcode, reasonCode, notes string,
) (*CaseDetail, error) {
	var detail *CaseDetail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		resolved, rErr := q.ResolveScanBarcode(ctx, dbgen.ResolveScanBarcodeParams{
			Barcode: barcode, OrganizationID: p.OrganizationID,
		})
		if rErr != nil {
			return ops.NotFoundOr(rErr, "Shipment")
		}
		sh, lErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
			ID: resolved.ID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return ops.NotFoundOr(lErr, "Shipment")
		}
		publicID, iErr := s.Initiate(ctx, tx, p, sh, nil, reasonCode, notes)
		if iErr != nil {
			return iErr
		}
		var dErr error
		detail, dErr = s.loadCase(ctx, q, p, publicID)
		return dErr
	})
	return detail, err
}

type reverseLeg struct {
	fromID  *int64
	toID    *int64
	legType string
}

type reversePath struct {
	source         string
	returnBranchID *int64
	returnHubID    *int64
	legs           []reverseLeg
	explanation    map[string]any
}

// resolveReversePath works out how the parcel gets home.
//
// The general case mirrors the forward path: destination branch → destination
// hub → origin hub → origin branch → sender. Two shortcuts matter operationally:
// a parcel that never left its origin branch is simply handed back, and one
// whose sender is served by the branch currently holding it needs no line haul
// at all. Both avoid moving a parcel across the country to bring it back to the
// street it started on.
func (s *Service) resolveReversePath(
	ctx context.Context, p *tenant.Principal, sh dbgen.Shipment, senderPincode string,
) (*reversePath, error) {
	path := &reversePath{explanation: map[string]any{}}

	// Which branch serves the sender's address for delivery today?
	branchID, err := s.routing.ResolveDeliveryBranch(ctx, p.OrganizationID, senderPincode, time.Now())
	if err != nil {
		return nil, err
	}
	if branchID == nil {
		// Fall back to the branch that booked it. A return must always have
		// somewhere to go, even if service areas have since changed.
		branchID = sh.OriginBranchID
		path.explanation["fallback"] = "no delivery service area for the sender pincode; using the booking branch"
	}
	if branchID == nil {
		return nil, apierr.Conflict("RTO_NOT_ROUTABLE",
			"No branch can return this shipment to the sender.").
			WithDetail("senderPincode", senderPincode)
	}
	path.returnBranchID = branchID
	path.explanation["senderPincode"] = senderPincode
	path.explanation["returnBranchResolved"] = true

	holder := sh.CurrentCustodyUnitID
	if holder == nil {
		holder = sh.DestinationBranchID
	}

	switch {
	case holder != nil && *holder == *branchID:
		// Already at the branch that will hand it back.
		path.source = "SAME_BRANCH"
		path.legs = []reverseLeg{{fromID: branchID, toID: nil, legType: "BRANCH_TO_SENDER"}}
		path.explanation["shortcut"] = "the returning branch already holds the parcel"
	default:
		path.source = "REVERSE_ROUTING"
		path.returnHubID = sh.OriginHubID
		legs := make([]reverseLeg, 0, 4)
		if holder != nil && sh.DestinationHubID != nil && *holder != *sh.DestinationHubID {
			legs = append(legs, reverseLeg{fromID: holder, toID: sh.DestinationHubID, legType: "BRANCH_TO_HUB"})
		}
		if sh.DestinationHubID != nil && sh.OriginHubID != nil &&
			*sh.DestinationHubID != *sh.OriginHubID {
			legs = append(legs, reverseLeg{
				fromID: sh.DestinationHubID, toID: sh.OriginHubID, legType: "HUB_TO_HUB",
			})
		}
		if sh.OriginHubID != nil {
			legs = append(legs, reverseLeg{fromID: sh.OriginHubID, toID: branchID, legType: "HUB_TO_BRANCH"})
		} else if holder != nil {
			legs = append(legs, reverseLeg{fromID: holder, toID: branchID, legType: "DIRECT"})
		}
		legs = append(legs, reverseLeg{fromID: branchID, toID: nil, legType: "BRANCH_TO_SENDER"})
		path.legs = legs
		path.explanation["mirroredForwardPath"] = true
	}
	path.explanation["legCount"] = len(path.legs)
	return path, nil
}

// Dispatch puts an initiated return into reverse transit.
func (s *Service) Dispatch(
	ctx context.Context, p *tenant.Principal, caseID string, facility *ops.Facility, device ops.Device,
) (*CaseDetail, error) {
	var detail *CaseDetail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		c, cErr := q.LockRTOCaseForUpdate(ctx, dbgen.LockRTOCaseForUpdateParams{
			PublicID: caseID, OrganizationID: p.OrganizationID,
		})
		if cErr != nil {
			return ops.NotFoundOr(cErr, "RTO case")
		}
		if c.Status != StatusInitiated {
			return apierr.Conflict("RTO_INVALID_STATE",
				"Only an initiated return can be dispatched.").
				WithDetail("currentStatus", c.Status)
		}
		sh, sErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
			ID: c.ShipmentID, OrganizationID: p.OrganizationID,
		})
		if sErr != nil {
			return ops.NotFoundOr(sErr, "Shipment")
		}
		if _, uErr := q.UpdateRTOCaseStatus(ctx, dbgen.UpdateRTOCaseStatusParams{
			ID: c.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: c.Status, ToStatus: StatusInTransit,
		}); uErr != nil {
			return ops.ConflictOr(uErr, "This return changed state while it was being dispatched.")
		}
		if _, lErr := q.AdvanceRTOLeg(ctx, dbgen.AdvanceRTOLegParams{
			RtoCaseID: c.ID, Sequence: 1, ToStatus: "IN_PROGRESS",
		}); lErr != nil && !ops.IsNoRows(lErr) {
			return apierr.Internal(lErr)
		}
		if _, tErr := s.trans.Apply(ctx, tx, device.Actor(p, facility), shipment.Request{
			Shipment: sh, To: shipment.StatusRTOInTransit,
			Description: "Return in transit",
			Direction:   shipment.DirectionReverse,
			AuditAction: audit.ActionRTOProgress,
			Metadata:    map[string]any{"rtoCase": c.CaseCode},
		}); tErr != nil {
			return tErr
		}
		var dErr error
		detail, dErr = s.loadCase(ctx, q, p, c.PublicID)
		return dErr
	})
	return detail, err
}

// ReceiveAtFacility records a returning parcel arriving somewhere on its way
// back.
//
// The shipment's status stays RTO_IN_TRANSIT — that is the point of the design.
// What changes is the leg, the custody, and a location event on the timeline,
// which together answer "where is my return" without a parallel status ladder.
func (s *Service) ReceiveAtFacility(
	ctx context.Context, p *tenant.Principal, barcode string, facility *ops.Facility, device ops.Device,
) (*CaseDetail, error) {
	if facility == nil {
		return nil, apierr.Validation("Receiving a return requires the operating unit you are working at.",
			map[string]any{"field": "operatingUnitId"})
	}
	var detail *CaseDetail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		resolved, rErr := q.ResolveScanBarcode(ctx, dbgen.ResolveScanBarcodeParams{
			Barcode: barcode, OrganizationID: p.OrganizationID,
		})
		if rErr != nil {
			return ops.NotFoundOr(rErr, "Shipment")
		}
		sh, lErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
			ID: resolved.ID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return ops.NotFoundOr(lErr, "Shipment")
		}
		c, cErr := q.GetActiveRTOCase(ctx, sh.ID)
		if cErr != nil {
			return ops.NotFoundOr(cErr, "RTO case")
		}
		if err := p.RequireUnitInScope(facility.ID); err != nil {
			return err
		}

		// Custody moves to this facility; the parcel leaves whatever bag or trip
		// brought it here.
		unitID := facility.ID
		if _, sErr := q.SetShipmentCustody(ctx, dbgen.SetShipmentCustodyParams{
			ID: sh.ID, OrganizationID: p.OrganizationID,
			SetCustodyUnit: true, CustodyUnitID: &unitID,
			SetCustodyUser: true, CustodyUserID: nil,
			SetBag: true, BagID: nil, SetTrip: true, TripID: nil,
		}); sErr != nil {
			return apierr.Internal(sErr)
		}

		if _, eErr := s.trans.RecordEvent(ctx, tx, device.Actor(p, facility), sh, "RTO",
			"Return received at "+facility.Code, shipment.Request{
				Metadata: map[string]any{"rtoCase": c.CaseCode, "facility": facility.Code},
			}); eErr != nil {
			return eErr
		}

		// Complete the leg that ends here and start the next.
		if _, mErr := q.MarkRTOLegForUnit(ctx, dbgen.MarkRTOLegForUnitParams{
			RtoCaseID: c.ID, UnitID: &unitID,
		}); mErr != nil && !ops.IsNoRows(mErr) {
			return apierr.Internal(mErr)
		}

		// Reaching the returning branch is the one milestone that changes the
		// case status: from here the parcel goes out to the sender.
		if c.ReturnBranchID != nil && *c.ReturnBranchID == facility.ID {
			if _, uErr := q.UpdateRTOCaseStatus(ctx, dbgen.UpdateRTOCaseStatusParams{
				ID: c.ID, OrganizationID: p.OrganizationID,
				ExpectedStatus: c.Status, ToStatus: StatusAtOriginBranch,
			}); uErr != nil && !ops.IsNoRows(uErr) {
				return apierr.Internal(uErr)
			}
		}

		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionRTOProgress, ResourceType: "rto_case",
			ResourceID: &c.ID, ResourcePublicID: c.PublicID,
			OperatingUnitID: &facility.ID,
			After: map[string]any{
				"caseCode": c.CaseCode, "awb": sh.Awb, "facility": facility.Code,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var dErr error
		detail, dErr = s.loadCase(ctx, q, p, c.PublicID)
		return dErr
	})
	return detail, err
}

// CompleteInput records the return being handed back.
type CompleteInput struct {
	CaseID     string
	Outcome    string
	ReceivedBy string
	Remarks    string
	Facility   *ops.Facility
	Device     ops.Device
}

// Complete hands the parcel back to the sender, or records that it could not be.
func (s *Service) Complete(ctx context.Context, p *tenant.Principal, in CompleteInput) (*CaseDetail, error) {
	var detail *CaseDetail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		c, cErr := q.LockRTOCaseForUpdate(ctx, dbgen.LockRTOCaseForUpdateParams{
			PublicID: in.CaseID, OrganizationID: p.OrganizationID,
		})
		if cErr != nil {
			return ops.NotFoundOr(cErr, "RTO case")
		}
		if c.Status == StatusReturned || c.Status == StatusDisposed || c.Status == StatusCancelled {
			return apierr.Conflict("RTO_CLOSED",
				"This return is already closed.").WithDetail("currentStatus", c.Status)
		}
		if c.ReturnBranchID != nil {
			if err := p.RequireUnitInScope(*c.ReturnBranchID); err != nil {
				return err
			}
		}
		sh, sErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
			ID: c.ShipmentID, OrganizationID: p.OrganizationID,
		})
		if sErr != nil {
			return ops.NotFoundOr(sErr, "Shipment")
		}

		if in.Outcome != "RETURNED" {
			// A failed return attempt keeps the case open; the parcel is still
			// the network's problem.
			if _, uErr := q.UpdateRTOCaseStatus(ctx, dbgen.UpdateRTOCaseStatusParams{
				ID: c.ID, OrganizationID: p.OrganizationID,
				ExpectedStatus: c.Status, ToStatus: StatusReturnFailed,
			}); uErr != nil {
				return ops.ConflictOr(uErr, "This return changed state while it was being updated.")
			}
			if _, eErr := s.trans.RecordEvent(ctx, tx, in.Device.Actor(p, in.Facility), sh, "RTO",
				"Return to sender attempted but not completed", shipment.Request{
					InternalRemarks: in.Remarks,
					Metadata:        map[string]any{"rtoCase": c.CaseCode},
				}); eErr != nil {
				return eErr
			}
			var dErr error
			detail, dErr = s.loadCase(ctx, q, p, c.PublicID)
			return dErr
		}

		if in.ReceivedBy == "" {
			return apierr.Validation("Record who took the return.",
				map[string]any{"field": "receivedBy"})
		}
		if _, uErr := q.UpdateRTOCaseStatus(ctx, dbgen.UpdateRTOCaseStatusParams{
			ID: c.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: c.Status, ToStatus: StatusReturned,
			ReturnedToName: &in.ReceivedBy,
		}); uErr != nil {
			return ops.ConflictOr(uErr, "This return changed state while it was being completed.")
		}
		if _, tErr := s.trans.Apply(ctx, tx, in.Device.Actor(p, in.Facility), shipment.Request{
			Shipment: sh, To: shipment.StatusRTODelivered,
			Description: "Returned to sender", Direction: shipment.DirectionReverse,
			InternalRemarks: in.Remarks, AuditAction: audit.ActionRTOCompleted,
			SetCustodyUnit: true, CustodyUnitID: nil,
			SetCustodyUser: true, CustodyUserID: nil,
			Metadata: map[string]any{"rtoCase": c.CaseCode, "receivedBy": in.ReceivedBy},
		}); tErr != nil {
			return tErr
		}
		// Close the final leg.
		legs, lErr := q.ListRTOLegs(ctx, c.ID)
		if lErr != nil {
			return apierr.Internal(lErr)
		}
		if len(legs) > 0 {
			if _, aErr := q.AdvanceRTOLeg(ctx, dbgen.AdvanceRTOLegParams{
				RtoCaseID: c.ID, Sequence: legs[len(legs)-1].Sequence, ToStatus: "COMPLETED",
			}); aErr != nil && !ops.IsNoRows(aErr) {
				return apierr.Internal(aErr)
			}
		}
		var dErr error
		detail, dErr = s.loadCase(ctx, q, p, c.PublicID)
		return dErr
	})
	return detail, err
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
