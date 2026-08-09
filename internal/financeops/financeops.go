// Package financeops connects operational state changes to the finance core.
//
// # The gap this closes
//
// Commission (M21), COD custody (M23) and settlement (M24) were each built,
// each individually correct, and each individually tested — and none of them
// were reachable from the operational flow that is supposed to trigger them.
// Delivering a COD parcel recorded the cash on the delivery attempt and opened
// no obligation; delivering for a franchise raised no commission. A settlement
// generated for a month of real deliveries came back arithmetically correct and
// economically empty: every total zero.
//
// The seam was known and recorded in the wrong place — a comment in
// `tests/integration/cod_test.go` reads "which is what the booking path will do
// once M23 is wired to it". That is a note nobody reads until they are already
// debugging the thing it describes.
//
// # Why an observer rather than calls in the delivery service
//
// The transition engine already exposes the seam that notifications and
// webhooks use, and it has the properties this needs:
//
//   - It runs **inside the state-change transaction**. A commission raised for a
//     delivery that then fails to commit must not exist, and a COD obligation
//     must appear at exactly the moment the parcel is marked delivered. Sharing
//     one transaction is what makes that true without any compensation logic.
//   - It fires for **every** transition to a state, wherever it originated. A
//     delivery completed through the agent app, through an operations override
//     or through a partner API all pass through `Transitioner.Apply`, so none of
//     them can silently skip finance.
//   - It keeps `internal/delivery` from importing `internal/commission` and
//     `internal/cod`, which would tie last-mile operations to the finance
//     module graph for something last-mile does not care about.
//
// # Idempotency
//
// Nothing here adds its own. `OpenObligation` is idempotent on shipment via a
// unique index, and `Calculate` probes (shipment, type, recipient) before
// writing. Both inherit the transition's own exactly-once guarantee, so a
// retried delivery cannot raise a second commission or a second liability.
//
// # What a failure here does
//
// It fails the transition. That is deliberate and worth stating plainly,
// because the alternative was tempting: swallow the error, mark the parcel
// delivered, and let finance catch up later. That produces a delivered parcel
// with no record of the cash the agent is holding, which is the single worst
// state this system can be in. If commission or COD cannot be recorded, the
// delivery does not complete and the agent retries.
package financeops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/commission"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// CODOpener is the part of the COD service this package needs.
type CODOpener interface {
	OpenObligation(ctx context.Context, tx pgx.Tx, p *tenant.Principal, sh dbgen.Shipment) (*dbgen.CodObligation, error)
}

// CommissionRaiser is the part of the commission service this package needs.
type CommissionRaiser interface {
	Calculate(ctx context.Context, tx pgx.Tx, p *tenant.Principal, in commission.CalculateRequest) (*dbgen.CommissionCalculation, bool, error)
}

// Service raises finance records from operational transitions.
type Service struct {
	q    *dbgen.Queries
	cod  CODOpener
	comm CommissionRaiser
	log  *slog.Logger
}

// New builds the connector.
func New(q *dbgen.Queries, codSvc CODOpener, commSvc CommissionRaiser, log *slog.Logger) *Service {
	return &Service{q: q, cod: codSvc, comm: commSvc, log: log}
}

// ShipmentObserver returns the transition observer to register on the engine.
func (s *Service) ShipmentObserver() shipment.Observer {
	return func(ctx context.Context, tx pgx.Tx, p *tenant.Principal, res *shipment.Result) error {
		switch res.To {
		case shipment.StatusBooked:
			return s.onBooked(ctx, tx, p, res)
		case shipment.StatusDelivered:
			return s.onDelivered(ctx, tx, p, res)
		default:
			// Every other transition is operational detail with no financial
			// consequence. Handling only the states that earn or owe keeps this
			// off the hot path for the dozen scans a parcel takes in between.
			return nil
		}
	}
}

// onBooked raises the booking commission for the franchise that took the order.
//
// COD is deliberately *not* opened here. The liability begins when cash is
// actually collected, not when a parcel is booked as COD — a booked-and-
// cancelled parcel would otherwise leave a phantom obligation for money nobody
// ever held.
func (s *Service) onBooked(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, res *shipment.Result,
) error {
	sh := res.Shipment
	if sh.BookingUnitID == nil {
		return nil
	}
	franchiseID, err := s.franchiseForUnit(ctx, tx, p, *sh.BookingUnitID)
	if err != nil || franchiseID == nil {
		return err
	}
	return s.raise(ctx, tx, p, res, commission.TypeBooking, *franchiseID, sh.BookingUnitID)
}

// onDelivered opens the COD liability and raises the delivery commission.
func (s *Service) onDelivered(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, res *shipment.Result,
) error {
	sh := res.Shipment

	// 1. The cash. Opened before commission because it is the more consequential
	//    of the two: a missing commission is an argument at month end, a missing
	//    obligation is cash with no owner.
	if sh.CodAmountMinor > 0 {
		if _, err := s.cod.OpenObligation(ctx, tx, p, sh); err != nil {
			// A shipment that already has an obligation is not an error: a
			// retried delivery reaches here twice by design.
			var apiErr *apierr.Error
			if errors.As(err, &apiErr) && apiErr.Code == "COD_NOT_APPLICABLE" {
				return nil
			}
			return fmt.Errorf("open COD obligation for %s: %w", sh.Awb, err)
		}
	}

	// 2. The earning, credited to whoever owns the delivering branch.
	if sh.DestinationBranchID == nil {
		return nil
	}
	franchiseID, err := s.franchiseForUnit(ctx, tx, p, *sh.DestinationBranchID)
	if err != nil || franchiseID == nil {
		return err
	}
	return s.raise(ctx, tx, p, res, commission.TypeDelivery, *franchiseID, sh.DestinationBranchID)
}

// raise computes and posts one commission from the shipment's own price
// snapshot.
//
// The snapshot, not the current rate card: commission is a share of what the
// customer was actually charged, and that number was frozen at booking. Reading
// live pricing here would make a franchise's earnings drift every time somebody
// edited a tariff.
func (s *Service) raise(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, res *shipment.Result,
	commissionType string, franchiseID int64, unitID *int64,
) error {
	sh := res.Shipment
	q := s.q.WithTx(tx)

	basis := commission.Basis{ShipmentCount: 1, CODAmountMinor: sh.CodAmountMinor}
	snap, err := q.GetShipmentChargeSnapshot(ctx, sh.ID)
	switch {
	case err == nil:
		basis.FreightMinor = snap.FreightMinor
		basis.SurchargeMinor = snap.SurchargeTotalMinor
		basis.DiscountMinor = snap.DiscountTotalMinor
		basis.TaxableMinor = snap.TaxableMinor
		basis.TaxMinor = snap.TaxTotalMinor
		basis.TotalMinor = snap.TotalMinor
		basis.ChargeableWeightG = int64(snap.ChargeableWeightGrams)
	case database.IsNoRows(err):
		// No snapshot means the shipment was not priced through booking — a
		// migrated or seeded row. A percentage rule would compute zero, which
		// is honest; a fixed rule still pays. Better than refusing the
		// delivery.
		s.log.WarnContext(ctx, "commission raised without a price snapshot",
			slog.String("awb", sh.Awb), slog.String("type", commissionType))
	default:
		return fmt.Errorf("read charge snapshot for %s: %w", sh.Awb, err)
	}

	shipmentID := sh.ID
	eventID := res.Event.ID
	_, _, err = s.comm.Calculate(ctx, tx, p, commission.CalculateRequest{
		Facts: commission.Facts{
			CommissionType:  commissionType,
			FranchiseID:     &franchiseID,
			OperatingUnitID: unitID,
			ServiceID:       &sh.CourierServiceID,
			PaymentMode:     sh.PaymentMode,
		},
		Basis:             basis,
		ShipmentID:        &shipmentID,
		QualifyingEvent:   string(res.To),
		QualifyingEventID: &eventID,
		RecipientType:     "FRANCHISE",
		RecipientID:       franchiseID,
		FranchiseID:       &franchiseID,
		// Posted in the same transaction: a commission that is calculated but
		// not posted is invisible to the ledger and to settlement, which is
		// the state this whole package exists to avoid.
		PostImmediately: true,
	})
	if err != nil {
		// No rule matched is the ordinary case for a tenant that has not
		// configured commission yet, and must not fail a delivery.
		var apiErr *apierr.Error
		if errors.As(err, &apiErr) &&
			(apiErr.Code == commission.CodeNoRule || apiErr.Code == commission.CodeNoRecipient) {
			return nil
		}
		return fmt.Errorf("raise %s commission for %s: %w", commissionType, sh.Awb, err)
	}
	return nil
}

// franchiseForUnit returns the franchise owning an operating unit, or nil when
// the unit is company-operated.
func (s *Service) franchiseForUnit(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, unitID int64,
) (*int64, error) {
	row, err := s.q.WithTx(tx).GetFranchiseByOperatingUnit(ctx, dbgen.GetFranchiseByOperatingUnitParams{
		OrganizationID:  p.OrganizationID,
		OperatingUnitID: unitID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			// A company branch earns nothing: there is no counterparty.
			return nil, nil
		}
		return nil, fmt.Errorf("resolve franchise for unit %d: %w", unitID, err)
	}
	id := row.ID
	return &id, nil
}
