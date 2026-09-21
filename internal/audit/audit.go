// Package audit records security- and business-sensitive actions.
//
// Constitution §34: audit rows carry tenant, actor, action, resource,
// timestamp, request id, reason and before/after state, and are not casually
// editable — the audit_events table has a trigger that rejects UPDATE and
// DELETE outright.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/logging"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Action codes. Keep them namespaced and stable; dashboards and alerting rules
// key on these strings.
const (
	ActionLoginSucceeded         = "auth.login_succeeded"
	ActionLoginFailed            = "auth.login_failed"
	ActionLoginBlocked           = "auth.login_blocked"
	ActionLogout                 = "auth.logout"
	ActionTokenRefreshed         = "auth.token_refreshed"
	ActionTokenReuseDetected     = "auth.token_reuse_detected"
	ActionPasswordChanged        = "auth.password_changed"
	ActionPasswordResetRequested = "auth.password_reset_requested"
	ActionPasswordResetCompleted = "auth.password_reset_completed"
	ActionSessionRevoked         = "auth.session_revoked"

	ActionUserCreated     = "user.created"
	ActionUserUpdated     = "user.updated"
	ActionUserActivated   = "user.activated"
	ActionUserDeactivated = "user.deactivated"
	ActionRoleAssigned    = "user.role_assigned"
	ActionRoleRevoked     = "user.role_revoked"

	ActionRoleCreated            = "role.created"
	ActionRoleUpdated            = "role.updated"
	ActionRoleDeleted            = "role.deleted"
	ActionRolePermissionsChanged = "role.permissions_changed"

	ActionOrganizationUpdated = "organization.updated"

	ActionRegionCreated            = "region.created"
	ActionRegionUpdated            = "region.updated"
	ActionOperatingUnitCreated     = "operating_unit.created"
	ActionOperatingUnitUpdated     = "operating_unit.updated"
	ActionOperatingUnitDeactivated = "operating_unit.deactivated"
	ActionCapabilityChanged        = "operating_unit.capability_changed"
	ActionFranchiseCreated         = "franchise.created"
	ActionFranchiseUpdated         = "franchise.updated"
	ActionAgreementCreated         = "franchise_agreement.created"
	ActionAgreementUpdated         = "franchise_agreement.updated"

	ActionZoneCreated        = "zone.created"
	ActionZoneUpdated        = "zone.updated"
	ActionZoneMappingChanged = "zone.mapping_changed"
	ActionImportStarted      = "import.started"
	ActionImportCompleted    = "import.completed"
	ActionPincodeUpdated     = "pincode.updated"

	ActionServiceAreaCreated        = "service_area.created"
	ActionServiceAreaUpdated        = "service_area.updated"
	ActionServiceabilityRuleCreated = "serviceability_rule.created"
	ActionServiceabilityRuleUpdated = "serviceability_rule.updated"
	ActionRouteCreated              = "route.created"
	ActionRouteUpdated              = "route.updated"
	ActionRoutingOverrideCreated    = "routing_override.created"
	ActionRoutingOverrideUpdated    = "routing_override.updated"
	ActionClosureDeclared           = "closure.declared"
	ActionClosureCancelled          = "closure.cancelled"

	ActionCourierServiceCreated     = "courier_service.created"
	ActionCourierServiceUpdated     = "courier_service.updated"
	ActionCourierServiceDeactivated = "courier_service.deactivated"

	ActionRateCardCreated          = "rate_card.created"
	ActionRateCardUpdated          = "rate_card.updated"
	ActionRateCardVersionCreated   = "rate_card.version_created"
	ActionRateCardVersionActivated = "rate_card.version_activated"
	ActionRateCardVersionArchived  = "rate_card.version_archived"
	ActionRateCardPricingChanged   = "rate_card.pricing_changed"
	ActionTaxRuleChanged           = "tax_rule.changed"

	ActionCustomerCreated    = "customer.created"
	ActionCustomerUpdated    = "customer.updated"
	ActionCustomerSuspended  = "customer.suspended"
	ActionCustomerReinstated = "customer.reinstated"
	ActionCreditChanged      = "customer.credit_changed"
	ActionAddressCreated     = "customer.address_created"
	ActionAddressUpdated     = "customer.address_updated"

	ActionShipmentBooked    = "shipment.booked"
	ActionShipmentCorrected = "shipment.corrected"
	ActionShipmentCancelled = "shipment.cancelled"
	ActionLabelGenerated    = "shipment.label_generated"

	// Release 2 — physical operations. Constitution §34 requires an audit trail
	// for manual state overrides, damage and loss declarations, and anything
	// that corrects a frozen record after the fact; the rest are here because
	// an operations dispute is normally settled by asking who did what, when.
	ActionPickupCreated     = "pickup.created"
	ActionPickupScheduled   = "pickup.scheduled"
	ActionPickupAssigned    = "pickup.assigned"
	ActionPickupAccepted    = "pickup.accepted"
	ActionPickupRejected    = "pickup.rejected"
	ActionPickupCompleted   = "pickup.completed"
	ActionPickupFailed      = "pickup.failed"
	ActionPickupCancelled   = "pickup.cancelled"
	ActionPickupRunCreated  = "pickup.run_created"
	ActionPickupRunStarted  = "pickup.run_started"
	ActionPickupRunClosed   = "pickup.run_closed"
	ActionScanRejected      = "scan.rejected"
	ActionScanException     = "scan.exception"
	ActionShipmentHeld      = "shipment.held"
	ActionShipmentReleased  = "shipment.released"
	ActionCustodyOverridden = "shipment.custody_overridden"

	ActionBagCreated       = "bag.created"
	ActionBagItemAdded     = "bag.item_added"
	ActionBagItemRemoved   = "bag.item_removed"
	ActionBagClosed        = "bag.closed"
	ActionBagDispatched    = "bag.dispatched"
	ActionBagReceived      = "bag.received"
	ActionBagOpened        = "bag.opened"
	ActionBagReconciled    = "bag.reconciled"
	ActionBagCorrected     = "bag.corrected_after_close"
	ActionBagSealVerified  = "bag.seal_verified"
	ActionManifestCreated  = "manifest.created"
	ActionManifestClosed   = "manifest.closed"
	ActionManifestDispatch = "manifest.dispatched"
	ActionManifestReceived = "manifest.received"
	ActionManifestReconcil = "manifest.reconciled"
	ActionManifestCancel   = "manifest.cancelled"

	ActionCarrierChanged = "carrier.changed"
	ActionVehicleChanged = "vehicle.changed"
	ActionDriverChanged  = "driver.changed"
	ActionTripCreated    = "trip.created"
	ActionTripDeparted   = "trip.departed"
	ActionTripArrived    = "trip.arrived"
	ActionTripClosed     = "trip.closed"
	ActionTripCancelled  = "trip.cancelled"
	ActionTripCrewChange = "trip.crew_changed"

	ActionExceptionRaised     = "exception.raised"
	ActionExceptionAssigned   = "exception.assigned"
	ActionExceptionResolved   = "exception.resolved"
	ActionReconciliationStart = "reconciliation.started"
	ActionReconciliationDone  = "reconciliation.completed"

	ActionDeliveryRunCreated = "delivery.run_created"
	ActionDeliveryAssigned   = "delivery.assigned"
	ActionDeliveryDispatched = "delivery.dispatched"
	ActionDeliveryCompleted  = "delivery.completed"
	ActionDeliveryFailed     = "delivery.failed"
	ActionDeliveryRunClosed  = "delivery.run_closed"
	ActionOTPIssued          = "delivery.otp_issued"

	ActionNDROpened        = "ndr.opened"
	ActionNDRAttempted     = "ndr.attempt_recorded"
	ActionNDRActionSet     = "ndr.action_set"
	ActionNDRAddressFixed  = "ndr.address_corrected"
	ActionNDRResolved      = "ndr.resolved"
	ActionNDRReasonChanged = "ndr.reason_changed"

	ActionRTOInitiated = "rto.initiated"
	ActionRTOProgress  = "rto.progressed"
	ActionRTOCompleted = "rto.completed"

	ActionPODSubmitted  = "pod.submitted"
	ActionPODDownloaded = "pod.downloaded"

	ActionShipmentLost    = "shipment.declared_lost"
	ActionShipmentDamaged = "shipment.declared_damaged"
)

// Actor types.
const (
	ActorUser      = "USER"
	ActorSystem    = "SYSTEM"
	ActorAnonymous = "ANONYMOUS"
	ActorAPIKey    = "API_KEY"
)

// Entry describes one auditable action.
type Entry struct {
	OrganizationID   *int64
	ActorUserID      *int64
	ActorType        string
	ActorLabel       string
	Action           string
	ResourceType     string
	ResourceID       *int64
	ResourcePublicID string
	OperatingUnitID  *int64
	Reason           string
	Before           any
	After            any
	Metadata         map[string]any
}

// Recorder writes audit entries.
type Recorder struct {
	q   *dbgen.Queries
	log *slog.Logger
}

// NewRecorder builds a Recorder over the shared pool.
func NewRecorder(q *dbgen.Queries, log *slog.Logger) *Recorder {
	return &Recorder{q: q, log: log}
}

// Record writes an audit entry outside any caller transaction.
//
// Failure is logged but not returned: an audit write must not roll back a
// business operation that already succeeded. Entries written *inside* a
// business transaction should use RecordTx instead, where atomicity is wanted.
func (r *Recorder) Record(ctx context.Context, e Entry) {
	if err := r.write(ctx, r.q, e); err != nil {
		r.log.Error("failed to record audit event",
			slog.String("action", e.Action),
			slog.String("resource_type", e.ResourceType),
			slog.String("error", err.Error()))
	}
}

// RecordTx writes an audit entry inside an existing transaction, so that the
// business change and its audit trail commit or roll back together.
func (r *Recorder) RecordTx(ctx context.Context, tx pgx.Tx, e Entry) error {
	return r.write(ctx, r.q.WithTx(tx), e)
}

// FromPrincipal pre-fills the actor and tenant fields from the request context.
func FromPrincipal(ctx context.Context, e Entry) Entry {
	p := tenant.FromContext(ctx)
	if p == nil {
		if e.ActorType == "" {
			e.ActorType = ActorAnonymous
		}
		return e
	}
	if e.OrganizationID == nil {
		org := p.OrganizationID
		e.OrganizationID = &org
	}
	if e.ActorUserID == nil {
		// Nil for a partner principal: there is no user row behind an API key,
		// and actor_user_id is a foreign key.
		e.ActorUserID = p.ActorUserID()
	}
	if e.ActorType == "" {
		e.ActorType = ActorUser
		if p.IsPartner {
			e.ActorType = ActorAPIKey
		}
	}
	if e.ActorLabel == "" {
		// For a partner the key's name is the only identity available, and it
		// is the one an investigator needs: it says which integration did this.
		e.ActorLabel = p.Email
		if p.IsPartner {
			e.ActorLabel = "api-key:" + p.PartnerKeyName
		}
	}
	if p.ImpersonatedOrg {
		if e.Metadata == nil {
			e.Metadata = map[string]any{}
		}
		e.Metadata["impersonatedByPlatformOperator"] = true
		e.Metadata["actorOrganization"] = p.OrganizationCode
	}
	return e
}

func (r *Recorder) write(ctx context.Context, q *dbgen.Queries, e Entry) error {
	if e.ActorType == "" {
		e.ActorType = ActorSystem
	}
	before, err := marshalState(e.Before)
	if err != nil {
		return err
	}
	after, err := marshalState(e.After)
	if err != nil {
		return err
	}
	meta := []byte("{}")
	if len(e.Metadata) > 0 {
		if meta, err = json.Marshal(e.Metadata); err != nil {
			return err
		}
	}
	params := dbgen.InsertAuditEventParams{
		PublicID:        publicid.New(publicid.PrefixAuditEvent),
		OrganizationID:  e.OrganizationID,
		ActorUserID:     e.ActorUserID,
		ActorType:       e.ActorType,
		Action:          e.Action,
		ResourceType:    e.ResourceType,
		ResourceID:      e.ResourceID,
		OperatingUnitID: e.OperatingUnitID,
		BeforeState:     before,
		AfterState:      after,
		Metadata:        meta,
	}
	if e.ActorLabel != "" {
		params.ActorLabel = &e.ActorLabel
	}
	if e.ResourcePublicID != "" {
		params.ResourcePublicID = &e.ResourcePublicID
	}
	if e.Reason != "" {
		params.Reason = &e.Reason
	}
	if reqID := httpx.RequestID(ctx); reqID != "" {
		params.RequestID = &reqID
	}
	if ip := httpx.ClientIP(ctx); ip != "" {
		params.ClientIp = &ip
	}
	_, err = q.InsertAuditEvent(ctx, params)
	if err != nil && database.IsNoRows(err) {
		return nil
	}
	return err
}

func marshalState(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

// Log emits a structured log line for a security event that is also audited.
// Both exist deliberately: logs are for real-time operations, audit rows are the
// durable record.
func Log(ctx context.Context, action string, attrs ...any) {
	logging.FromContext(ctx).Info("audit", append([]any{slog.String("audit_action", action)}, attrs...)...)
}
