package manifest

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
	"github.com/ceserve/courier-os/internal/platform/jobs"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// JobGenerateManifestPDF is the background job kind for the printable document.
const JobGenerateManifestPDF = "manifest.generate_document"

// ContentSnapshot is the frozen declaration written at closure (§16).
type ContentSnapshot struct {
	ClosedAt      time.Time      `json:"closedAt"`
	ClosedBy      string         `json:"closedBy"`
	OriginCode    string         `json:"originCode"`
	DestCode      string         `json:"destinationCode"`
	BagCount      int            `json:"bagCount"`
	LooseCount    int            `json:"looseShipmentCount"`
	ShipmentCount int            `json:"totalShipmentCount"`
	PieceCount    int            `json:"totalPieceCount"`
	WeightGrams   int64          `json:"totalWeightGrams"`
	DeclaredGrams *int64         `json:"declaredWeightGrams,omitempty"`
	Bags          []SnapshotBag  `json:"bags"`
	Loose         []SnapshotLine `json:"looseShipments"`
}

// SnapshotBag is one declared bag.
type SnapshotBag struct {
	BagCode       string `json:"bagCode"`
	Barcode       string `json:"barcode"`
	ShipmentCount int    `json:"shipmentCount"`
	PieceCount    int    `json:"pieceCount"`
	WeightGrams   int64  `json:"weightGrams"`
	Destination   string `json:"destinationCode"`
}

// SnapshotLine is one declared loose shipment.
type SnapshotLine struct {
	AWB         string `json:"awb"`
	PieceCount  int    `json:"pieceCount"`
	WeightGrams int    `json:"weightGrams"`
	Destination string `json:"destinationPincode"`
}

// CloseInput freezes a manifest.
type CloseInput struct {
	ManifestID          string
	DeclaredWeightGrams *int64
}

// Close freezes the manifest and queues its printable document.
//
// The PDF is generated asynchronously (§38): a manifest with three hundred
// lines must not make the clerk wait, and the document is derived from the
// frozen snapshot so generating it later cannot produce a different answer.
func (s *Service) Close(ctx context.Context, p *tenant.Principal, in CloseInput) (*Detail, error) {
	var detail *Detail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		m, mErr := q.LockManifestForUpdate(ctx, dbgen.LockManifestForUpdateParams{
			PublicID: in.ManifestID, OrganizationID: p.OrganizationID,
		})
		if mErr != nil {
			return ops.NotFoundOr(mErr, "Manifest")
		}
		if err := p.RequireUnitInScope(m.OriginUnitID); err != nil {
			return err
		}
		if m.Status != StatusDraft {
			return invalidState(m.Status, StatusClosed)
		}
		if m.BagCount == 0 && m.LooseShipmentCount == 0 {
			return apierr.Conflict("MANIFEST_EMPTY",
				"An empty manifest cannot be closed.")
		}

		origin, fErr := s.units.FacilityByID(ctx, p.OrganizationID, m.OriginUnitID)
		if fErr != nil {
			return fErr
		}
		dest, dErr := s.units.FacilityByID(ctx, p.OrganizationID, m.DestinationUnitID)
		if dErr != nil {
			return dErr
		}
		snapshot, sErr := s.buildSnapshot(ctx, q, p, m, origin, dest, in.DeclaredWeightGrams)
		if sErr != nil {
			return sErr
		}
		raw, jErr := json.Marshal(snapshot)
		if jErr != nil {
			return apierr.Internal(jErr)
		}

		closed, cErr := q.CloseManifest(ctx, dbgen.CloseManifestParams{
			ID: m.ID, OrganizationID: p.OrganizationID,
			ClosedByUserID: &p.UserID, ClosedContents: raw,
			DeclaredWeightGrams: in.DeclaredWeightGrams,
		})
		if cErr != nil {
			return ops.ConflictOr(cErr, "This manifest was closed by someone else a moment ago.")
		}

		if _, eErr := s.appendEvent(ctx, q, p, closed, origin, "CLOSED", StatusDraft, StatusClosed,
			fmt.Sprintf("Manifest closed with %d bags and %d loose shipments",
				closed.BagCount, closed.LooseShipmentCount),
			map[string]any{
				"bagCount": closed.BagCount, "shipmentCount": closed.TotalShipmentCount,
				"pieceCount": closed.TotalPieceCount,
			}); eErr != nil {
			return eErr
		}

		// Queue the PDF inside the transaction so it cannot be enqueued for a
		// closure that then rolls back.
		if _, jobErr := s.jobs.EnqueueTx(ctx, tx, JobGenerateManifestPDF,
			map[string]any{"manifestId": closed.PublicID},
			jobs.EnqueueOptions{
				OrganizationID: &p.OrganizationID,
				DedupeKey:      "manifest-pdf:" + closed.PublicID,
				CreatedBy:      &p.UserID,
			}); jobErr != nil {
			return apierr.Internal(fmt.Errorf("queue manifest document: %w", jobErr))
		}

		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionManifestClosed, ResourceType: "manifest",
			ResourceID: &m.ID, ResourcePublicID: m.PublicID,
			OperatingUnitID: &origin.ID,
			Before:          map[string]any{"status": StatusDraft},
			After: map[string]any{
				"status": StatusClosed, "manifestCode": m.ManifestCode,
				"bagCount": closed.BagCount, "shipmentCount": closed.TotalShipmentCount,
				"totalWeightGrams": closed.TotalWeightGrams,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var lErr error
		detail, lErr = s.loadDetail(ctx, q, p, m.PublicID)
		return lErr
	})
	return detail, err
}

func (s *Service) buildSnapshot(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, m dbgen.Manifest,
	origin, dest *ops.Facility, declared *int64,
) (*ContentSnapshot, error) {
	active := true
	bags, err := q.ListManifestBags(ctx, dbgen.ListManifestBagsParams{
		ManifestID: m.ID, OnlyActive: &active,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	loose, err := q.ListManifestLooseShipments(ctx, dbgen.ListManifestLooseShipmentsParams{
		ManifestID: m.ID, OnlyActive: &active,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	snap := &ContentSnapshot{
		ClosedAt: time.Now(), ClosedBy: p.UserPublicID,
		OriginCode: origin.Code, DestCode: dest.Code,
		BagCount: int(m.BagCount), LooseCount: int(m.LooseShipmentCount),
		ShipmentCount: int(m.TotalShipmentCount), PieceCount: int(m.TotalPieceCount),
		WeightGrams: m.TotalWeightGrams, DeclaredGrams: declared,
		Bags: make([]SnapshotBag, 0, len(bags)), Loose: make([]SnapshotLine, 0, len(loose)),
	}
	for _, b := range bags {
		snap.Bags = append(snap.Bags, SnapshotBag{
			BagCode: b.BagCode, Barcode: b.Barcode,
			ShipmentCount: int(b.DeclaredShipmentCount), PieceCount: int(b.DeclaredPieceCount),
			WeightGrams: b.DeclaredWeightGrams, Destination: b.BagDestinationCode,
		})
	}
	for _, l := range loose {
		snap.Loose = append(snap.Loose, SnapshotLine{
			AWB: l.Awb, PieceCount: int(l.PieceCount), WeightGrams: int(l.WeightGrams),
			Destination: l.DestinationPincode,
		})
	}
	return snap, nil
}

// Dispatch sends a closed manifest on its way.
//
// Every shipment on it — bagged and loose — moves in the same transaction, so
// the manifest and the parcels it describes can never disagree about whether
// they left.
func (s *Service) Dispatch(
	ctx context.Context, p *tenant.Principal, manifestID string, device ops.Device,
) (*Detail, error) {
	var detail *Detail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		m, mErr := q.LockManifestForUpdate(ctx, dbgen.LockManifestForUpdateParams{
			PublicID: manifestID, OrganizationID: p.OrganizationID,
		})
		if mErr != nil {
			return ops.NotFoundOr(mErr, "Manifest")
		}
		if err := p.RequireUnitInScope(m.OriginUnitID); err != nil {
			return err
		}
		if !canTransition(m.Status, StatusDispatched) {
			return invalidState(m.Status, StatusDispatched)
		}

		origin, fErr := s.units.FacilityByID(ctx, p.OrganizationID, m.OriginUnitID)
		if fErr != nil {
			return fErr
		}
		updated, uErr := q.UpdateManifestStatus(ctx, dbgen.UpdateManifestStatusParams{
			ID: m.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: m.Status, ToStatus: StatusDispatched,
			DispatchedByUserID: &p.UserID,
		})
		if uErr != nil {
			return ops.ConflictOr(uErr, "This manifest changed state while it was being dispatched.")
		}

		// Bags on the manifest leave with it.
		active := true
		bags, bErr := q.ListManifestBags(ctx, dbgen.ListManifestBagsParams{
			ManifestID: m.ID, OnlyActive: &active,
		})
		if bErr != nil {
			return apierr.Internal(bErr)
		}
		for _, b := range bags {
			// b.ID is the manifest_bags row id; the bag itself is addressed by
			// its public id.
			bag, lErr := q.LockBagForUpdate(ctx, dbgen.LockBagForUpdateParams{
				PublicID: b.BagPublicID, OrganizationID: p.OrganizationID,
			})
			if lErr != nil {
				return ops.NotFoundOr(lErr, "Bag")
			}
			if bag.Status != "CLOSED" {
				return apierr.Conflict("BAG_NOT_CLOSED",
					"A bag on this manifest is not closed.").WithDetail("bagCode", bag.BagCode)
			}
			if _, sErr := q.UpdateBagStatus(ctx, dbgen.UpdateBagStatusParams{
				ID: bag.ID, OrganizationID: p.OrganizationID,
				ExpectedStatus: "CLOSED", ToStatus: "DISPATCHED",
			}); sErr != nil {
				return ops.ConflictOr(sErr, "A bag on this manifest changed state.")
			}
		}

		if err := s.moveShipments(ctx, tx, p, updated, origin, device,
			dispatchTarget, "Dispatched on manifest "+m.ManifestCode); err != nil {
			return err
		}

		if _, eErr := s.appendEvent(ctx, q, p, updated, origin, "DISPATCHED",
			m.Status, StatusDispatched, "Manifest dispatched",
			map[string]any{"shipmentCount": updated.TotalShipmentCount}); eErr != nil {
			return eErr
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionManifestDispatch, ResourceType: "manifest",
			ResourceID: &m.ID, ResourcePublicID: m.PublicID,
			OperatingUnitID: &origin.ID,
			Before:          map[string]any{"status": m.Status},
			After: map[string]any{
				"status": StatusDispatched, "manifestCode": m.ManifestCode,
				"shipmentCount": updated.TotalShipmentCount,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var lErr error
		detail, lErr = s.loadDetail(ctx, q, p, m.PublicID)
		return lErr
	})
	return detail, err
}

// Receive takes an inbound manifest at the destination.
func (s *Service) Receive(
	ctx context.Context, p *tenant.Principal, manifestID string,
	facility *ops.Facility, device ops.Device,
) (*Detail, error) {
	var detail *Detail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		m, mErr := q.LockManifestForUpdate(ctx, dbgen.LockManifestForUpdateParams{
			PublicID: manifestID, OrganizationID: p.OrganizationID,
		})
		if mErr != nil {
			return ops.NotFoundOr(mErr, "Manifest")
		}
		if !canTransition(m.Status, StatusReceived) {
			return invalidState(m.Status, StatusReceived)
		}
		if facility == nil {
			return apierr.Validation("Receiving a manifest requires the operating unit you are working at.",
				map[string]any{"field": "operatingUnitId"})
		}
		if facility.ID != m.DestinationUnitID {
			return apierr.Conflict("MANIFEST_WRONG_DESTINATION",
				"This manifest is addressed to another facility.").
				WithDetail("manifestCode", m.ManifestCode).
				WithDetail("scannedAt", facility.Code)
		}
		if err := p.RequireUnitInScope(facility.ID); err != nil {
			return err
		}

		updated, uErr := q.UpdateManifestStatus(ctx, dbgen.UpdateManifestStatusParams{
			ID: m.ID, OrganizationID: p.OrganizationID,
			ExpectedStatus: m.Status, ToStatus: StatusReceived,
			ReceivedByUserID: &p.UserID,
		})
		if uErr != nil {
			return ops.ConflictOr(uErr, "This manifest changed state while it was being received.")
		}

		active := true
		bags, bErr := q.ListManifestBags(ctx, dbgen.ListManifestBagsParams{
			ManifestID: m.ID, OnlyActive: &active,
		})
		if bErr != nil {
			return apierr.Internal(bErr)
		}
		for _, b := range bags {
			bag, lErr := q.LockBagForUpdate(ctx, dbgen.LockBagForUpdateParams{
				PublicID: b.BagPublicID, OrganizationID: p.OrganizationID,
			})
			if lErr != nil {
				return ops.NotFoundOr(lErr, "Bag")
			}
			if bag.Status != "DISPATCHED" {
				continue
			}
			if _, sErr := q.UpdateBagStatus(ctx, dbgen.UpdateBagStatusParams{
				ID: bag.ID, OrganizationID: p.OrganizationID,
				ExpectedStatus: "DISPATCHED", ToStatus: "RECEIVED",
				ReceivedByUserID: &p.UserID, ReceivedUnitID: &facility.ID,
			}); sErr != nil {
				return ops.ConflictOr(sErr, "A bag on this manifest changed state.")
			}
			if _, mbErr := q.MarkManifestBagReceived(ctx, dbgen.MarkManifestBagReceivedParams{
				ManifestID: m.ID, BagID: bag.ID, Status: "RECEIVED",
				ReceivedByUserID: &p.UserID,
			}); mbErr != nil {
				return apierr.Internal(mbErr)
			}
		}

		if err := s.moveShipments(ctx, tx, p, updated, facility, device,
			receiveTargetAt(facility), "Received at "+facility.Code); err != nil {
			return err
		}

		if _, eErr := s.appendEvent(ctx, q, p, updated, facility, "RECEIVED",
			m.Status, StatusReceived, "Manifest received at "+facility.Code,
			map[string]any{"shipmentCount": updated.TotalShipmentCount}); eErr != nil {
			return eErr
		}
		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionManifestReceived, ResourceType: "manifest",
			ResourceID: &m.ID, ResourcePublicID: m.PublicID,
			OperatingUnitID: &facility.ID,
			Before:          map[string]any{"status": m.Status},
			After: map[string]any{
				"status": StatusReceived, "manifestCode": m.ManifestCode,
				"facility": facility.Code, "shipmentCount": updated.TotalShipmentCount,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}
		var lErr error
		detail, lErr = s.loadDetail(ctx, q, p, m.PublicID)
		return lErr
	})
	return detail, err
}

// targetFunc chooses the state a shipment moves to.
type targetFunc func(sh dbgen.ListManifestShipmentIDsRow) (shipment.Status, bool)

// dispatchTarget maps a shipment's current state to its dispatched state.
func dispatchTarget(sh dbgen.ListManifestShipmentIDsRow) (shipment.Status, bool) {
	switch shipment.Status(sh.CurrentStatus) {
	case shipment.StatusOriginBagged, shipment.StatusOriginBranchReceived:
		return shipment.StatusOriginDispatched, true
	case shipment.StatusTransitHubReceived, shipment.StatusDestinationHubReceived:
		return shipment.StatusTransitHubDispatched, true
	default:
		// Reverse-moving parcels stay RTO_IN_TRANSIT; the event records the
		// movement without a status change.
		return "", false
	}
}

// receiveTargetAt maps a shipment's current state to its received state at a
// specific facility.
func receiveTargetAt(at *ops.Facility) targetFunc {
	return func(sh dbgen.ListManifestShipmentIDsRow) (shipment.Status, bool) {
		if shipment.Status(sh.CurrentStatus) != shipment.StatusInTransit &&
			shipment.Status(sh.CurrentStatus) != shipment.StatusOriginDispatched &&
			shipment.Status(sh.CurrentStatus) != shipment.StatusTransitHubDispatched {
			return "", false
		}
		switch {
		case sh.DestinationBranchID != nil && *sh.DestinationBranchID == at.ID:
			return shipment.StatusDestinationBranchReceived, true
		case sh.DestinationHubID != nil && *sh.DestinationHubID == at.ID:
			return shipment.StatusDestinationHubReceived, true
		default:
			return shipment.StatusTransitHubReceived, true
		}
	}
}

// moveShipments advances every shipment travelling on a manifest.
func (s *Service) moveShipments(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, m dbgen.Manifest,
	at *ops.Facility, device ops.Device, target targetFunc, description string,
) error {
	q := s.q.WithTx(tx)
	rows, err := q.ListManifestShipmentIDs(ctx, m.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	actor := device.Actor(p, at)

	for _, row := range rows {
		sh, lErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
			ID: row.ID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return apierr.Internal(lErr)
		}
		to, ok := target(row)
		if !ok {
			// No state change, but the movement is still a fact worth recording.
			if _, eErr := s.trans.RecordEvent(ctx, tx, actor, sh, "MANIFEST",
				description, shipment.Request{
					Metadata: map[string]any{"manifestCode": m.ManifestCode},
				}); eErr != nil {
				return eErr
			}
			continue
		}
		if _, fErr := shipment.Find(shipment.Status(sh.CurrentStatus), to); fErr != nil {
			continue
		}
		req := shipment.Request{
			Shipment: sh, To: to, Description: description,
			Metadata: map[string]any{"manifestCode": m.ManifestCode},
		}
		// Receipt takes custody at the receiving facility and empties any bag
		// or trip reference; dispatch only clears the facility.
		if to == shipment.StatusDestinationBranchReceived ||
			to == shipment.StatusDestinationHubReceived ||
			to == shipment.StatusTransitHubReceived {
			unitID := at.ID
			req.SetCustodyUnit, req.CustodyUnitID = true, &unitID
			req.SetTrip, req.TripID = true, nil
		}
		if _, tErr := s.trans.Apply(ctx, tx, actor, req); tErr != nil {
			return tErr
		}
	}
	return nil
}
