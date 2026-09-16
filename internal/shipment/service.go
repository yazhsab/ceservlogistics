package shipment

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

// The read and cancel operations live on the Booker rather than in the HTTP
// handler because Release 4 gave them a second caller: a partner integration
// authenticated by an API key reaches the same behaviour through
// /api/v1/partner. Two transports calling one service is the only arrangement
// where "may this shipment be cancelled" cannot drift between them.

// Get returns one shipment, enforcing tenant, operating-unit and customer
// scope.
//
// A caller outside scope gets 404 rather than 403, so the endpoint cannot be
// used to learn which shipments exist in other branches or other accounts.
func (b *Booker) Get(ctx context.Context, p *tenant.Principal, shipmentID string) (*Detail, error) {
	row, err := b.q.GetShipmentByPublicID(ctx, dbgen.GetShipmentByPublicIDParams{
		PublicID: shipmentID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.NotFound("Shipment")
		}
		return nil, apierr.Internal(err)
	}
	if err := b.authorizeShipmentAccess(p, row); err != nil {
		return nil, err
	}
	return b.loadDetail(ctx, b.q, row)
}

// Cancel transitions a shipment to CANCELLED.
//
// The shipment row is locked first and the transition is a compare-and-swap on
// current_status, so two concurrent cancellations produce one cancellation and
// one clear conflict rather than two events.
func (b *Booker) Cancel(
	ctx context.Context, p *tenant.Principal, shipmentID, reason string,
) (*Detail, error) {
	v := validate.New()
	reason = v.Text("reason", reason, 5, 500, true)
	if err := v.Err(); err != nil {
		return nil, err
	}

	// A partner principal has no user row, so every actor column is nil for it
	// and the event is attributed by actor_type instead.
	actorUser := p.ActorUserID()
	actorType := actorTypeFor(p, false)

	var detail *Detail
	err := b.db.InTx(ctx, func(tx pgx.Tx) error {
		q := b.q.WithTx(tx)
		locked, lErr := q.LockShipmentForUpdate(ctx, dbgen.LockShipmentForUpdateParams{
			PublicID: shipmentID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			if database.IsNoRows(lErr) {
				return apierr.NotFound("Shipment")
			}
			return apierr.Internal(lErr)
		}
		if _, tErr := Validate(Status(locked.CurrentStatus), StatusCancelled); tErr != nil {
			return tErr
		}
		if pErr := b.authorizeShipmentAccessRaw(p, locked.CustomerID,
			locked.OriginBranchID, locked.DestinationBranchID, locked.BookingUnitID); pErr != nil {
			return pErr
		}

		updated, uErr := q.ApplyShipmentTransition(ctx, dbgen.ApplyShipmentTransitionParams{
			ID: locked.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: locked.CurrentStatus, ToStatus: string(StatusCancelled),
			ActorUserID: actorUser, Reason: &reason,
		})
		if uErr != nil {
			if database.IsNoRows(uErr) {
				return apierr.Conflict(apierr.CodeConcurrentModification,
					"This shipment changed state while the cancellation was being processed. Reload and try again.")
			}
			return apierr.Internal(uErr)
		}
		event, eErr := q.AppendShipmentEvent(ctx, dbgen.AppendShipmentEventParams{
			PublicID: publicid.New(publicid.PrefixShipmentEvent), OrganizationID: p.OrganizationID,
			ShipmentID: updated.ID, Sequence: updated.EventSequence, EventType: "CANCELLED",
			FromStatus: &locked.CurrentStatus, ToStatus: string(StatusCancelled),
			ActorUserID: actorUser, ActorType: actorType,
			OperatingUnitID: updated.BookingUnitID,
			Description:     "Shipment cancelled",
			InternalRemarks: &reason, ReasonCode: strPtr("CUSTOMER_REQUEST"),
			RequestID: optional(httpx.RequestID(ctx)), Metadata: []byte("{}"),
		})
		if eErr != nil {
			return apierr.Internal(fmt.Errorf("append cancellation event: %w", eErr))
		}

		// Cancelling a credit booking returns the reserved headroom.
		if updated.PaymentMode == "CREDIT" {
			payerID := updated.CustomerID
			commercial, cErr := q.GetShipmentCommercialSnapshot(ctx, dbgen.GetShipmentCommercialSnapshotParams{OrganizationID: p.OrganizationID, ShipmentID: updated.ID})
			if cErr == nil && commercial.TransportCustomerID != nil {
				payerID = *commercial.TransportCustomerID
			} else if cErr != nil && !database.IsNoRows(cErr) {
				return apierr.Internal(cErr)
			}
			if rErr := b.customer.ReleaseCredit(ctx, tx, p.OrganizationID,
				payerID, updated.TotalAmountMinor, &updated.ID, actorUser,
				"Shipment "+updated.Awb+" cancelled"); rErr != nil {
				return rErr
			}
		}

		if aErr := b.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionShipmentCancelled, ResourceType: "shipment",
			ResourceID: &updated.ID, ResourcePublicID: updated.PublicID,
			OperatingUnitID: updated.BookingUnitID, Reason: reason,
			Before: map[string]any{"status": locked.CurrentStatus},
			After:  map[string]any{"status": updated.CurrentStatus, "awb": updated.Awb},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}

		// Same observers as every other state change: a cancellation notifies
		// the customer and fires shipment.cancelled to subscribed partners.
		// Cancellation does not go through the Transitioner — it predates it and
		// has its own credit-release step — so the hook is invoked explicitly
		// rather than being inherited.
		if oErr := b.notify(ctx, tx, p, &Result{
			Shipment: updated, Event: event,
			From: Status(locked.CurrentStatus), To: StatusCancelled,
		}); oErr != nil {
			return oErr
		}

		full, gErr := q.GetShipmentByPublicID(ctx, dbgen.GetShipmentByPublicIDParams{
			PublicID: shipmentID, OrganizationID: p.OrganizationID,
		})
		if gErr != nil {
			return apierr.Internal(gErr)
		}
		detail, gErr = b.loadDetail(ctx, q, full)
		return gErr
	})
	if err != nil {
		return nil, err
	}
	b.metrics.RecordBusiness("booking", "cancelled")
	return detail, nil
}

// notify runs the transition observers for a state change made outside the
// Transitioner. Nil-safe: observers are optional wiring.
func (b *Booker) notify(ctx context.Context, tx pgx.Tx, p *tenant.Principal, res *Result) error {
	if b.transitioner == nil {
		return nil
	}
	return b.transitioner.runObservers(ctx, tx, p, res)
}

// SetTransitioner attaches the transition engine so cancellation raises the
// same observers as every other state change.
func (b *Booker) SetTransitioner(t *Transitioner) { b.transitioner = t }

// Label renders the shipping label for a shipment.
//
// Returns the structured label; the caller decides whether to serve it as JSON
// or as ZPL via RenderZPL. Generating a label is audited because it is the
// point at which a parcel acquires a physical identity.
func (b *Booker) Label(
	ctx context.Context, p *tenant.Principal, shipmentID, format string,
) (*Label, error) {
	data, err := b.q.GetShipmentLabelData(ctx, dbgen.GetShipmentLabelDataParams{
		PublicID: shipmentID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.NotFound("Shipment")
		}
		return nil, apierr.Internal(err)
	}
	if data.CurrentStatus == string(StatusCancelled) {
		return nil, apierr.Conflict(apierr.CodeConflict, "A cancelled shipment cannot produce a label.")
	}
	row, err := b.q.GetShipmentByPublicID(ctx, dbgen.GetShipmentByPublicIDParams{
		PublicID: shipmentID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	if err := b.authorizeShipmentAccess(p, row); err != nil {
		return nil, err
	}
	packages, err := b.q.ListShipmentPackages(ctx, row.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}

	label := buildLabel(data, packages, p.OrganizationCode)
	b.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: audit.ActionLabelGenerated, ResourceType: "shipment",
		ResourceID: &row.ID, ResourcePublicID: shipmentID,
		Metadata: map[string]any{"format": format},
	}))
	return &label, nil
}

// RenderZPL turns a label into ZPL II for a thermal printer.
func RenderZPL(l Label) string { return renderZPL(l) }

// Queries exposes the generated query set to sibling transports that need to
// read shipment rows directly. Read-only use only; every write goes through a
// service method so its rules cannot be bypassed.
func (b *Booker) Queries() *dbgen.Queries { return b.q }

// DB exposes the pool for transports that must compose a shipment write with
// another module's write in one transaction.
func (b *Booker) DB() *database.DB { return b.db }
