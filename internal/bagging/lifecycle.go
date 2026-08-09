package bagging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// ContentSnapshot is the frozen declaration written at closure.
//
// Everything reconciliation compares against comes from here, not from a live
// read of bag_items. That is the point: the receiving facility is checking what
// the sending facility declared, and a later correction must not be able to
// rewrite the declaration to match what turned up.
type ContentSnapshot struct {
	ClosedAt      time.Time         `json:"closedAt"`
	ClosedBy      string            `json:"closedBy"`
	OriginCode    string            `json:"originCode"`
	DestCode      string            `json:"destinationCode"`
	ShipmentCount int               `json:"shipmentCount"`
	PieceCount    int               `json:"pieceCount"`
	WeightGrams   int64             `json:"totalWeightGrams"`
	GrossGrams    *int              `json:"grossWeightGrams,omitempty"`
	Seals         []string          `json:"seals,omitempty"`
	Items         []SnapshotItem    `json:"items"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// SnapshotItem is one declared shipment.
type SnapshotItem struct {
	AWB         string `json:"awb"`
	ShipmentID  string `json:"shipmentId"`
	PieceCount  int    `json:"pieceCount"`
	WeightGrams int    `json:"weightGrams"`
	Destination string `json:"destinationPincode"`
}

// CloseInput seals a bag.
type CloseInput struct {
	BagID            string
	SealNumbers      []string
	SealType         string
	GrossWeightGrams *int
	Facility         *ops.Facility
}

// Close freezes the bag's contents and records its seals.
//
// The compare-and-swap on status = OPEN is what makes this safe under
// concurrency: two clerks pressing "close" produce one snapshot and one
// conflict, never two snapshots that disagree about what was inside.
func (s *Service) Close(ctx context.Context, p *tenant.Principal, in CloseInput) (*Detail, error) {
	var detail *Detail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		bag, bErr := q.LockBagForUpdate(ctx, dbgen.LockBagForUpdateParams{
			PublicID: in.BagID, OrganizationID: p.OrganizationID,
		})
		if bErr != nil {
			return ops.NotFoundOr(bErr, "Bag")
		}
		if err := p.RequireUnitInScope(bag.OriginUnitID); err != nil {
			return err
		}
		if bag.Status != StatusOpen {
			return invalidState(bag.Status, StatusClosed)
		}
		if bag.ShipmentCount == 0 {
			return apierr.Conflict("BAG_EMPTY",
				"An empty bag cannot be closed. Add shipments or cancel it.")
		}

		origin, fErr := s.units.FacilityByID(ctx, p.OrganizationID, bag.OriginUnitID)
		if fErr != nil {
			return fErr
		}
		dest, dErr := s.units.FacilityByID(ctx, p.OrganizationID, bag.DestinationUnitID)
		if dErr != nil {
			return dErr
		}

		snapshot, sErr := s.buildSnapshot(ctx, q, p, bag, origin, dest, in)
		if sErr != nil {
			return sErr
		}
		raw, mErr := json.Marshal(snapshot)
		if mErr != nil {
			return apierr.Internal(mErr)
		}

		params := dbgen.CloseBagParams{
			ID: bag.ID, OrganizationID: p.OrganizationID,
			ClosedByUserID: &p.UserID, ClosedContents: raw,
		}
		if in.GrossWeightGrams != nil {
			v := int32(*in.GrossWeightGrams)
			params.GrossWeightGrams = &v
		}
		closed, cErr := q.CloseBag(ctx, params)
		if cErr != nil {
			return ops.ConflictOr(cErr, "This bag was closed by someone else a moment ago.")
		}

		for _, seal := range in.SealNumbers {
			if _, sealErr := q.CreateBagSeal(ctx, dbgen.CreateBagSealParams{
				OrganizationID: p.OrganizationID, BagID: bag.ID, SealNumber: seal,
				SealType:        orDefault(in.SealType, "PLASTIC"),
				AppliedByUserID: &p.UserID, AppliedUnitID: &origin.ID,
			}); sealErr != nil {
				if ops.IsUnique(sealErr, "bag_seals_number_unique") {
					return apierr.Conflict(apierr.CodeDuplicate,
						"This seal number is already recorded against another bag.").
						WithDetail("sealNumber", seal)
				}
				return apierr.Internal(fmt.Errorf("record seal: %w", sealErr))
			}
		}

		if _, eErr := s.appendEvent(ctx, q, p, closed, origin, "CLOSED", StatusOpen, StatusClosed,
			fmt.Sprintf("Bag closed with %d shipments", closed.ShipmentCount), "",
			map[string]any{
				"shipmentCount": closed.ShipmentCount, "pieceCount": closed.PieceCount,
				"seals": in.SealNumbers,
			}); eErr != nil {
			return eErr
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionBagClosed, ResourceType: "bag",
			ResourceID: &bag.ID, ResourcePublicID: bag.PublicID,
			OperatingUnitID: &origin.ID,
			Before:          map[string]any{"status": StatusOpen},
			After: map[string]any{
				"status": StatusClosed, "bagCode": bag.BagCode,
				"shipmentCount": closed.ShipmentCount, "pieceCount": closed.PieceCount,
				"totalWeightGrams": closed.TotalWeightGrams, "seals": in.SealNumbers,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}

		var lErr error
		detail, lErr = s.loadDetail(ctx, q, p, bag.PublicID)
		return lErr
	})
	return detail, err
}

func (s *Service) buildSnapshot(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal,
	bag dbgen.Bag, origin, dest *ops.Facility, in CloseInput,
) (*ContentSnapshot, error) {
	onlyActive := true
	items, err := q.ListBagItems(ctx, dbgen.ListBagItemsParams{
		BagID: bag.ID, OnlyActive: &onlyActive,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	snap := &ContentSnapshot{
		ClosedAt: time.Now(), ClosedBy: p.UserPublicID,
		OriginCode: origin.Code, DestCode: dest.Code,
		ShipmentCount: int(bag.ShipmentCount), PieceCount: int(bag.PieceCount),
		WeightGrams: bag.TotalWeightGrams, GrossGrams: in.GrossWeightGrams,
		Seals: in.SealNumbers,
		Items: make([]SnapshotItem, 0, len(items)),
	}
	for _, it := range items {
		snap.Items = append(snap.Items, SnapshotItem{
			AWB: it.Awb, ShipmentID: it.ShipmentPublicID,
			PieceCount: int(it.PieceCount), WeightGrams: int(it.WeightGrams),
			Destination: it.DestinationPincode,
		})
	}
	return snap, nil
}

// Dispatch marks a closed bag as having left.
//
// It is normally driven by manifest dispatch rather than called directly, since
// a bag travels on a manifest; the standalone path exists for the case where a
// driver takes a single bag between two nearby branches.
func (s *Service) Dispatch(ctx context.Context, p *tenant.Principal, bagID string, device ops.Device) (*Detail, error) {
	return s.advance(ctx, p, bagID, StatusDispatched, advanceOptions{
		Device: device, AuditAction: audit.ActionBagDispatched,
		EventType: "DISPATCHED", Description: "Bag dispatched",
		RequireOriginScope: true,
		ShipmentTo:         shipment.StatusOriginDispatched,
	})
}

// Receive takes an inbound bag into a facility.
func (s *Service) Receive(
	ctx context.Context, p *tenant.Principal, bagID string, facility *ops.Facility, device ops.Device,
) (*Detail, error) {
	return s.advance(ctx, p, bagID, StatusReceived, advanceOptions{
		Device: device, Facility: facility, AuditAction: audit.ActionBagReceived,
		EventType: "RECEIVED", Description: "Bag received",
	})
}

// OpenAtDestination breaks the seal and opens a received bag.
func (s *Service) OpenAtDestination(
	ctx context.Context, p *tenant.Principal, bagID string, facility *ops.Facility, device ops.Device,
) (*Detail, error) {
	return s.advance(ctx, p, bagID, StatusOpened, advanceOptions{
		Device: device, Facility: facility, AuditAction: audit.ActionBagOpened,
		EventType: "OPENED", Description: "Bag opened", BreakSeals: true,
	})
}

type advanceOptions struct {
	Device             ops.Device
	Facility           *ops.Facility
	AuditAction        string
	EventType          string
	Description        string
	RequireOriginScope bool
	BreakSeals         bool
	// ShipmentTo, when set, moves every shipment in the bag to that state.
	ShipmentTo shipment.Status
}

func (s *Service) advance(
	ctx context.Context, p *tenant.Principal, bagID, to string, opt advanceOptions,
) (*Detail, error) {
	var detail *Detail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		bag, bErr := q.LockBagForUpdate(ctx, dbgen.LockBagForUpdateParams{
			PublicID: bagID, OrganizationID: p.OrganizationID,
		})
		if bErr != nil {
			return ops.NotFoundOr(bErr, "Bag")
		}
		if !canTransition(bag.Status, to) {
			return invalidState(bag.Status, to)
		}
		if opt.RequireOriginScope {
			if err := p.RequireUnitInScope(bag.OriginUnitID); err != nil {
				return err
			}
		}

		at := opt.Facility
		if at == nil {
			var fErr error
			at, fErr = s.units.FacilityByID(ctx, p.OrganizationID, bag.OriginUnitID)
			if fErr != nil {
				return fErr
			}
		} else if to == StatusReceived || to == StatusOpened {
			// A bag must be received where it was addressed. Receiving it
			// somewhere else is a misroute, and saying so at the door is far
			// cheaper than discovering it during reconciliation.
			if at.ID != bag.DestinationUnitID {
				return apierr.Conflict("BAG_WRONG_DESTINATION",
					"This bag is addressed to another facility.").
					WithDetail("bagCode", bag.BagCode).
					WithDetail("scannedAt", at.Code).
					WithDetail("hint", "Raise a MISROUTED exception to redirect it.")
			}
			if err := p.RequireUnitInScope(at.ID); err != nil {
				return err
			}
		}

		params := dbgen.UpdateBagStatusParams{
			ID: bag.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: bag.Status, ToStatus: to,
		}
		if to == StatusReceived {
			params.ReceivedByUserID = &p.UserID
			params.ReceivedUnitID = &at.ID
		}
		updated, uErr := q.UpdateBagStatus(ctx, params)
		if uErr != nil {
			return ops.ConflictOr(uErr, "This bag changed state while the request was being processed.")
		}

		if opt.BreakSeals {
			if sErr := q.MarkBagSealBroken(ctx, dbgen.MarkBagSealBrokenParams{
				BagID: bag.ID, BrokenByUserID: &p.UserID,
			}); sErr != nil {
				return apierr.Internal(sErr)
			}
		}

		if opt.ShipmentTo != "" {
			if err := s.moveContents(ctx, tx, p, updated, at, opt); err != nil {
				return err
			}
		}

		if _, eErr := s.appendEvent(ctx, q, p, updated, at, opt.EventType, bag.Status, to,
			opt.Description, "", map[string]any{
				"bagCode": bag.BagCode, "shipmentCount": updated.ShipmentCount,
				"facility": at.Code,
			}); eErr != nil {
			return eErr
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: opt.AuditAction, ResourceType: "bag",
			ResourceID: &bag.ID, ResourcePublicID: bag.PublicID,
			OperatingUnitID: &at.ID,
			Before:          map[string]any{"status": bag.Status},
			After: map[string]any{
				"status": to, "bagCode": bag.BagCode, "facility": at.Code,
				"shipmentCount": updated.ShipmentCount,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}

		var lErr error
		detail, lErr = s.loadDetail(ctx, q, p, bag.PublicID)
		return lErr
	})
	return detail, err
}

// moveContents advances every shipment inside a bag.
func (s *Service) moveContents(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal,
	bag dbgen.Bag, at *ops.Facility, opt advanceOptions,
) error {
	q := s.q.WithTx(tx)
	ids, err := q.ListBagItemShipmentIDs(ctx, bag.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	actor := opt.Device.Actor(p, at)
	for _, id := range ids {
		sh, lErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
			ID: id, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return apierr.Internal(lErr)
		}
		if _, fErr := shipment.Find(shipment.Status(sh.CurrentStatus), opt.ShipmentTo); fErr != nil {
			// A parcel already past this point — pulled for an exception, say —
			// is left alone rather than dragged backwards.
			continue
		}
		if _, tErr := s.trans.Apply(ctx, tx, actor, shipment.Request{
			Shipment: sh, To: opt.ShipmentTo,
			Description: opt.Description + " in bag " + bag.BagCode,
			Metadata:    map[string]any{"bagCode": bag.BagCode},
		}); tErr != nil {
			return tErr
		}
	}
	return nil
}

// VerifySealInput records a seal check at the receiving end.
type VerifySealInput struct {
	BagID      string
	SealNumber string
	Result     string
	Remarks    string
	Facility   *ops.Facility
}

// VerifySeal records whether a seal arrived intact.
//
// A broken or mismatched seal is a chain-of-custody event, so it is audited and
// surfaced for an exception rather than merely noted.
func (s *Service) VerifySeal(ctx context.Context, p *tenant.Principal, in VerifySealInput) (*Detail, error) {
	var detail *Detail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		bag, bErr := q.LockBagForUpdate(ctx, dbgen.LockBagForUpdateParams{
			PublicID: in.BagID, OrganizationID: p.OrganizationID,
		})
		if bErr != nil {
			return ops.NotFoundOr(bErr, "Bag")
		}
		seals, sErr := q.ListBagSeals(ctx, bag.ID)
		if sErr != nil {
			return apierr.Internal(sErr)
		}
		var target *dbgen.ListBagSealsRow
		for i := range seals {
			if seals[i].SealNumber == in.SealNumber {
				target = &seals[i]
				break
			}
		}
		if target == nil {
			// A seal number that is not on the bag is itself the finding: the
			// bag was resealed somewhere along the way.
			if _, eErr := s.appendEvent(ctx, q, p, bag, in.Facility, "EXCEPTION", "", "",
				"Seal number does not match this bag", "SEAL_MISMATCH",
				map[string]any{"presentedSeal": in.SealNumber}); eErr != nil {
				return eErr
			}
			return apierr.Conflict("SEAL_MISMATCH",
				"This seal number is not recorded against this bag.").
				WithDetail("presentedSeal", in.SealNumber)
		}
		if _, vErr := q.VerifyBagSeal(ctx, dbgen.VerifyBagSealParams{
			ID: target.ID, OrganizationID: p.OrganizationID,
			VerifiedByUserID: &p.UserID, VerificationResult: &in.Result,
			VerificationRemarks: ops.Optional(in.Remarks),
		}); vErr != nil {
			return apierr.Internal(vErr)
		}
		if in.Result != "INTACT" {
			if _, eErr := s.appendEvent(ctx, q, p, bag, in.Facility, "EXCEPTION", "", "",
				"Seal verification failed: "+in.Result, "SEAL_"+in.Result,
				map[string]any{"sealNumber": in.SealNumber, "result": in.Result}); eErr != nil {
				return eErr
			}
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionBagSealVerified, ResourceType: "bag",
			ResourceID: &bag.ID, ResourcePublicID: bag.PublicID,
			Reason: in.Remarks,
			After:  map[string]any{"sealNumber": in.SealNumber, "result": in.Result},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var lErr error
		detail, lErr = s.loadDetail(ctx, q, p, bag.PublicID)
		return lErr
	})
	return detail, err
}

// CorrectionInput is the privileged post-closure correction.
type CorrectionInput struct {
	BagID       string
	Barcode     string
	Action      string
	Reason      string
	ExceptionID *int64
	Facility    *ops.Facility
}

// Correct adds or removes a shipment from a closed bag.
//
// Constitution §15: "post-close correction requires explicit exception
// workflow". This is that workflow. It demands bag.exception_edit, a written
// reason and a linked exception record, and it never rewrites closed_contents —
// the original declaration stands, and the correction is a separate fact
// layered on top.
func (s *Service) Correct(ctx context.Context, p *tenant.Principal, in CorrectionInput) (*Detail, error) {
	if err := p.Require("bag.exception_edit"); err != nil {
		return nil, err
	}
	if in.Reason == "" {
		return nil, apierr.Validation("A post-closure correction requires a reason.",
			map[string]any{"field": "reason"})
	}
	if in.ExceptionID == nil {
		return nil, apierr.Validation("A post-closure correction must reference an exception.",
			map[string]any{"field": "exceptionId"})
	}

	var detail *Detail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		bag, bErr := q.LockBagForUpdate(ctx, dbgen.LockBagForUpdateParams{
			PublicID: in.BagID, OrganizationID: p.OrganizationID,
		})
		if bErr != nil {
			return ops.NotFoundOr(bErr, "Bag")
		}
		if bag.Status == StatusOpen {
			return apierr.Conflict(apierr.CodeConflict,
				"This bag is open. Add or remove shipments normally.")
		}
		resolved, rErr := q.ResolveScanBarcode(ctx, dbgen.ResolveScanBarcodeParams{
			Barcode: in.Barcode, OrganizationID: p.OrganizationID,
		})
		if rErr != nil {
			return ops.NotFoundOr(rErr, "Shipment")
		}
		if in.Action != "REMOVE" {
			return apierr.Validation(
				"Only removal is supported as a post-closure correction; a shipment found loose should be received normally at the destination.",
				map[string]any{"field": "action"})
		}
		if _, remErr := q.RemoveBagItem(ctx, dbgen.RemoveBagItemParams{
			BagID: bag.ID, ShipmentID: resolved.ID,
			RemovedByUserID: &p.UserID, RemovalReason: &in.Reason,
			ExceptionID: in.ExceptionID,
		}); remErr != nil {
			if ops.IsNoRows(remErr) {
				return apierr.NotFound("Bag item")
			}
			if msg, ok := ops.TriggerMessage(remErr); ok {
				return apierr.Conflict("BAG_CONTENTS_FROZEN", msg)
			}
			return apierr.Internal(remErr)
		}
		// Counters are recalculated but closed_contents is deliberately left
		// alone: it is the declaration, and the declaration is history.
		updated, cErr := q.RecalculateBagCounters(ctx, bag.ID)
		if cErr != nil {
			return apierr.Internal(cErr)
		}
		if _, eErr := s.appendEvent(ctx, q, p, updated, in.Facility, "EXCEPTION", "", "",
			fmt.Sprintf("%s removed from the closed bag under an exception", resolved.Awb),
			"POST_CLOSE_CORRECTION",
			map[string]any{"awb": resolved.Awb, "reason": in.Reason}); eErr != nil {
			return eErr
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionBagCorrected, ResourceType: "bag",
			ResourceID: &bag.ID, ResourcePublicID: bag.PublicID,
			Reason: in.Reason,
			After: map[string]any{
				"awb": resolved.Awb, "action": in.Action, "bagStatus": bag.Status,
				"declarationUnchanged": true,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var lErr error
		detail, lErr = s.loadDetail(ctx, q, p, bag.PublicID)
		return lErr
	})
	return detail, err
}
