// Package hubops implements M14: hub-floor operations — expected inbound,
// reconciliation, exceptions and the workload summary.
//
// Reconciliation is the heart of it. Every handover between facilities makes a
// claim ("this bag holds 42 shipments") and the receiving facility tests it.
// The claim comes from the frozen closure snapshot, never from a live read, so
// a correction made after closure cannot quietly make the numbers agree.
//
// Three outcomes matter and each becomes an exception with an owner:
//
//	MISSING   declared but not scanned  — potential theft or misroute
//	EXCESS    scanned but not declared  — someone else's parcel, or a bagging error
//	DAMAGED   scanned but unfit         — a claim is coming
package hubops

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/bagging"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Exception types, matching the Constitution's M14 list.
var ExceptionTypes = []string{
	"MISSING", "EXCESS", "DAMAGED", "MISROUTED", "SEAL_BROKEN", "UNKNOWN_BARCODE",
	"WEIGHT_MISMATCH", "COUNT_MISMATCH", "WRONG_FACILITY", "CUSTODY_OVERRIDE", "OTHER",
}

// ResolutionActions an exception can be closed with.
var ResolutionActions = []string{
	"FOUND", "RETURNED_TO_ORIGIN", "FORWARDED", "REBAGGED", "DELIVERED",
	"WRITTEN_OFF", "CLAIM_FILED", "CORRECTED", "NO_ACTION",
}

// Service implements hub-floor operations.
type Service struct {
	db    *database.DB
	q     *dbgen.Queries
	units *ops.Resolver
	codes *ops.CodeAllocator
	trans *shipment.Transitioner
	audit *audit.Recorder
	log   *slog.Logger
}

// NewService builds the hub operations service.
func NewService(
	db *database.DB, q *dbgen.Queries, units *ops.Resolver, codes *ops.CodeAllocator,
	trans *shipment.Transitioner, rec *audit.Recorder, log *slog.Logger,
) *Service {
	return &Service{db: db, q: q, units: units, codes: codes, trans: trans, audit: rec, log: log}
}

// StartReconciliationInput begins a count.
type StartReconciliationInput struct {
	SubjectType string
	BagID       string
	ManifestID  string
	Facility    *ops.Facility
}

// StartReconciliation opens a count against a received bag or manifest.
//
// The expected list is materialised from the closure snapshot at this moment,
// which is what makes the count reproducible: two people reconciling the same
// bag are checking against the same declaration, and the partial unique index
// stops them both opening one.
func (s *Service) StartReconciliation(
	ctx context.Context, p *tenant.Principal, in StartReconciliationInput,
) (*ReconciliationDetail, error) {
	if in.Facility == nil {
		return nil, apierr.Validation("Reconciliation requires the operating unit you are working at.",
			map[string]any{"field": "operatingUnitId"})
	}
	if err := p.RequireUnitInScope(in.Facility.ID); err != nil {
		return nil, err
	}

	code, err := s.codes.Allocate(ctx, p.OrganizationID, ops.KindReconcile, in.Facility.Code, time.Now())
	if err != nil {
		return nil, err
	}

	var detail *ReconciliationDetail
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		params := dbgen.CreateReconciliationParams{
			PublicID: publicid.New(publicid.PrefixReconciliation), OrganizationID: p.OrganizationID,
			ReconciliationCode: code, SubjectType: in.SubjectType,
			OperatingUnitID: in.Facility.ID, StartedByUserID: &p.UserID,
			Metadata: []byte("{}"),
		}

		var expected []expectedItem
		switch in.SubjectType {
		case "BAG":
			bag, bErr := q.LockBagForUpdate(ctx, dbgen.LockBagForUpdateParams{
				PublicID: in.BagID, OrganizationID: p.OrganizationID,
			})
			if bErr != nil {
				return ops.NotFoundOr(bErr, "Bag")
			}
			if bag.Status != bagging.StatusReceived && bag.Status != bagging.StatusOpened {
				return apierr.Conflict("BAG_NOT_RECEIVED",
					"A bag must be received before it can be reconciled.").
					WithDetail("currentStatus", bag.Status)
			}
			expected, err = declaredBagItems(bag)
			if err != nil {
				return err
			}
			params.BagID = &bag.ID
		case "MANIFEST":
			m, mErr := q.LockManifestForUpdate(ctx, dbgen.LockManifestForUpdateParams{
				PublicID: in.ManifestID, OrganizationID: p.OrganizationID,
			})
			if mErr != nil {
				return ops.NotFoundOr(mErr, "Manifest")
			}
			if m.Status != "RECEIVED" {
				return apierr.Conflict("MANIFEST_NOT_RECEIVED",
					"A manifest must be received before it can be reconciled.").
					WithDetail("currentStatus", m.Status)
			}
			expected, err = declaredManifestItems(m)
			if err != nil {
				return err
			}
			params.ManifestID = &m.ID
		default:
			return apierr.Validation("Reconciliation subject must be BAG or MANIFEST.",
				map[string]any{"field": "subjectType"})
		}

		params.ExpectedCount = int32(len(expected))
		rec, cErr := q.CreateReconciliation(ctx, params)
		if cErr != nil {
			switch {
			case ops.IsUnique(cErr, "reconciliations_active_bag_idx"),
				ops.IsUnique(cErr, "reconciliations_active_manifest_idx"):
				return apierr.Conflict("RECONCILIATION_IN_PROGRESS",
					"Somebody is already reconciling this. Join their count instead of starting a second one.")
			}
			return apierr.Internal(fmt.Errorf("create reconciliation: %w", cErr))
		}

		for _, item := range expected {
			if _, iErr := q.CreateReconciliationItem(ctx, dbgen.CreateReconciliationItemParams{
				OrganizationID: p.OrganizationID, ReconciliationID: rec.ID,
				ItemType: item.itemType, RawBarcode: item.barcode,
				ShipmentID: item.shipmentID, BagID: item.bagID,
				Outcome: "EXPECTED", WasDeclared: true,
			}); iErr != nil {
				if ops.IsUnique(iErr, "reconciliation_items_unique") {
					continue
				}
				return apierr.Internal(fmt.Errorf("seed reconciliation item: %w", iErr))
			}
		}

		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionReconciliationStart, ResourceType: "reconciliation",
			ResourceID: &rec.ID, ResourcePublicID: rec.PublicID,
			OperatingUnitID: &in.Facility.ID,
			After: map[string]any{
				"code": code, "subjectType": in.SubjectType, "expectedCount": len(expected),
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}

		var dErr error
		detail, dErr = s.loadReconciliation(ctx, q, p, rec.PublicID)
		return dErr
	})
	return detail, err
}

type expectedItem struct {
	itemType   string
	barcode    string
	shipmentID *int64
	bagID      *int64
}

// declaredBagItems reads the frozen closure snapshot.
func declaredBagItems(bag dbgen.Bag) ([]expectedItem, error) {
	if len(bag.ClosedContents) == 0 {
		return nil, apierr.Conflict("BAG_NO_DECLARATION",
			"This bag has no closure declaration to reconcile against.")
	}
	var snap bagging.ContentSnapshot
	if err := json.Unmarshal(bag.ClosedContents, &snap); err != nil {
		return nil, apierr.Internal(fmt.Errorf("decode bag declaration: %w", err))
	}
	out := make([]expectedItem, 0, len(snap.Items))
	for _, it := range snap.Items {
		out = append(out, expectedItem{itemType: "SHIPMENT", barcode: it.AWB})
	}
	return out, nil
}

// declaredManifestItems reads the frozen manifest declaration. A manifest is
// counted at bag level: the hub verifies that every bag arrived, and each bag
// is reconciled separately when it is opened.
func declaredManifestItems(m dbgen.Manifest) ([]expectedItem, error) {
	if len(m.ClosedContents) == 0 {
		return nil, apierr.Conflict("MANIFEST_NO_DECLARATION",
			"This manifest has no closure declaration to reconcile against.")
	}
	var snap struct {
		Bags []struct {
			BagCode string `json:"bagCode"`
			Barcode string `json:"barcode"`
		} `json:"bags"`
		Loose []struct {
			AWB string `json:"awb"`
		} `json:"looseShipments"`
	}
	if err := json.Unmarshal(m.ClosedContents, &snap); err != nil {
		return nil, apierr.Internal(fmt.Errorf("decode manifest declaration: %w", err))
	}
	out := make([]expectedItem, 0, len(snap.Bags)+len(snap.Loose))
	for _, b := range snap.Bags {
		out = append(out, expectedItem{itemType: "BAG", barcode: b.Barcode})
	}
	for _, l := range snap.Loose {
		out = append(out, expectedItem{itemType: "SHIPMENT", barcode: l.AWB})
	}
	return out, nil
}

// ScanInput records one barcode against an open reconciliation.
type ScanInput struct {
	ReconciliationID string
	Barcodes         []string
	Damaged          []string
	Facility         *ops.Facility
}

// ScanResult reports what one barcode did to the count.
type ScanResult struct {
	Barcode string `json:"barcode"`
	Outcome string `json:"outcome"`
	Message string `json:"message,omitempty"`
}

// Scan records barcodes against an open reconciliation.
//
// A barcode on the declaration becomes MATCHED. One that is not becomes EXCESS
// and raises an exception immediately — an unexpected parcel at a hub is
// somebody else's problem arriving early, and the sooner it has an owner the
// sooner it moves.
func (s *Service) Scan(ctx context.Context, p *tenant.Principal, in ScanInput) (*ReconciliationDetail, []ScanResult, error) {
	damaged := make(map[string]bool, len(in.Damaged))
	for _, b := range in.Damaged {
		damaged[b] = true
	}
	results := make([]ScanResult, 0, len(in.Barcodes)+len(in.Damaged))

	var detail *ReconciliationDetail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		rec, rErr := q.LockReconciliationForUpdate(ctx, dbgen.LockReconciliationForUpdateParams{
			PublicID: in.ReconciliationID, OrganizationID: p.OrganizationID,
		})
		if rErr != nil {
			return ops.NotFoundOr(rErr, "Reconciliation")
		}
		if rec.Status != "IN_PROGRESS" {
			return apierr.Conflict("RECONCILIATION_CLOSED",
				"This reconciliation is finished.").WithDetail("currentStatus", rec.Status)
		}
		if err := p.RequireUnitInScope(rec.OperatingUnitID); err != nil {
			return err
		}

		all := append(append([]string{}, in.Barcodes...), in.Damaged...)
		seen := make(map[string]bool, len(all))
		for _, barcode := range all {
			if seen[barcode] {
				continue
			}
			seen[barcode] = true

			outcome := "MATCHED"
			if damaged[barcode] {
				outcome = "DAMAGED"
			}
			if _, mErr := q.MarkReconciliationItemScanned(ctx, dbgen.MarkReconciliationItemScannedParams{
				ReconciliationID: rec.ID, RawBarcode: barcode, Outcome: outcome,
				ScannedByUserID: &p.UserID,
			}); mErr != nil {
				if !ops.IsNoRows(mErr) {
					return apierr.Internal(mErr)
				}
				// Not on the declaration: an excess item.
				if err := s.recordExcess(ctx, q, p, rec, barcode, in.Facility); err != nil {
					return err
				}
				results = append(results, ScanResult{
					Barcode: barcode, Outcome: "EXCESS",
					Message: "This item was not declared on the shipment being reconciled.",
				})
				continue
			}
			results = append(results, ScanResult{Barcode: barcode, Outcome: outcome})

			if outcome == "DAMAGED" {
				if err := s.raiseItemException(ctx, q, p, rec, barcode, "DAMAGED",
					"Item found damaged during reconciliation", in.Facility); err != nil {
					return err
				}
			}
		}

		if _, cErr := q.RecalculateReconciliationCounters(ctx, rec.ID); cErr != nil {
			return apierr.Internal(cErr)
		}
		var dErr error
		detail, dErr = s.loadReconciliation(ctx, q, p, rec.PublicID)
		return dErr
	})
	return detail, results, err
}

func (s *Service) recordExcess(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal,
	rec dbgen.Reconciliation, barcode string, at *ops.Facility,
) error {
	var shipmentID *int64
	if resolved, err := q.ResolveScanBarcode(ctx, dbgen.ResolveScanBarcodeParams{
		Barcode: barcode, OrganizationID: p.OrganizationID,
	}); err == nil {
		shipmentID = &resolved.ID
	} else if !ops.IsNoRows(err) {
		return apierr.Internal(err)
	}

	itemType := "SHIPMENT"
	exceptionType := "EXCESS"
	if shipmentID == nil {
		itemType = "UNKNOWN"
		exceptionType = "UNKNOWN_BARCODE"
	}
	if _, err := q.CreateReconciliationItem(ctx, dbgen.CreateReconciliationItemParams{
		OrganizationID: p.OrganizationID, ReconciliationID: rec.ID,
		ItemType: itemType, RawBarcode: barcode, ShipmentID: shipmentID,
		Outcome: "EXCESS", WasDeclared: false,
	}); err != nil {
		if ops.IsUnique(err, "reconciliation_items_unique") {
			return nil
		}
		return apierr.Internal(err)
	}
	return s.raiseItemException(ctx, q, p, rec, barcode, exceptionType,
		"Item scanned that was not on the declaration", at)
}

func (s *Service) raiseItemException(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, rec dbgen.Reconciliation,
	barcode, exceptionType, description string, at *ops.Facility,
) error {
	unitID := rec.OperatingUnitID
	if at != nil {
		unitID = at.ID
	}
	code, err := s.codes.Allocate(ctx, p.OrganizationID, ops.KindException, exceptionCodeScope(at), time.Now())
	if err != nil {
		return err
	}
	var shipmentID *int64
	if resolved, rErr := q.ResolveScanBarcode(ctx, dbgen.ResolveScanBarcodeParams{
		Barcode: barcode, OrganizationID: p.OrganizationID,
	}); rErr == nil {
		shipmentID = &resolved.ID
	}
	_, err = q.CreateOperationalException(ctx, dbgen.CreateOperationalExceptionParams{
		PublicID: publicid.New(publicid.PrefixException), OrganizationID: p.OrganizationID,
		ExceptionCode: code, ExceptionType: exceptionType,
		Severity: severityFor(exceptionType), OperatingUnitID: unitID,
		ShipmentID: shipmentID, BagID: rec.BagID, ManifestID: rec.ManifestID,
		ReconciliationID: &rec.ID, RawBarcode: &barcode,
		Description: description, RaisedByUserID: &p.UserID,
		Metadata: []byte("{}"),
	})
	if err != nil {
		return apierr.Internal(fmt.Errorf("raise exception: %w", err))
	}
	return nil
}

// Complete closes a reconciliation and converts unscanned items to MISSING.
//
// Every missing item raises its own exception. A shortage without an owner is
// how parcels quietly disappear from a network, so the workflow refuses to let
// the count close silently.
func (s *Service) Complete(
	ctx context.Context, p *tenant.Principal, reconciliationID, remarks string, facility *ops.Facility,
) (*ReconciliationDetail, error) {
	var detail *ReconciliationDetail
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		rec, rErr := q.LockReconciliationForUpdate(ctx, dbgen.LockReconciliationForUpdateParams{
			PublicID: reconciliationID, OrganizationID: p.OrganizationID,
		})
		if rErr != nil {
			return ops.NotFoundOr(rErr, "Reconciliation")
		}
		if rec.Status != "IN_PROGRESS" {
			return apierr.Conflict("RECONCILIATION_CLOSED",
				"This reconciliation is already finished.").WithDetail("currentStatus", rec.Status)
		}
		if err := p.RequireUnitInScope(rec.OperatingUnitID); err != nil {
			return err
		}

		if mErr := q.MarkUnscannedItemsMissing(ctx, rec.ID); mErr != nil {
			return apierr.Internal(mErr)
		}
		counted, cErr := q.RecalculateReconciliationCounters(ctx, rec.ID)
		if cErr != nil {
			return apierr.Internal(cErr)
		}

		missing := "MISSING"
		items, iErr := q.ListReconciliationItems(ctx, dbgen.ListReconciliationItemsParams{
			ReconciliationID: rec.ID, Outcome: &missing,
		})
		if iErr != nil {
			return apierr.Internal(iErr)
		}
		for _, it := range items {
			if it.ExceptionID != nil {
				continue
			}
			if err := s.raiseItemException(ctx, q, p, rec, it.RawBarcode, "MISSING",
				"Declared item not found during reconciliation", facility); err != nil {
				return err
			}
		}

		status := "COMPLETED"
		if counted.MissingCount > 0 || counted.ExcessCount > 0 || counted.DamagedCount > 0 {
			status = "COMPLETED_WITH_EXCEPTIONS"
		}
		done, dErr := q.CompleteReconciliation(ctx, dbgen.CompleteReconciliationParams{
			ID: rec.ID, OrganizationID: p.OrganizationID, ToStatus: status,
			CompletedByUserID: &p.UserID, Remarks: ops.Optional(remarks),
		})
		if dErr != nil {
			return ops.ConflictOr(dErr, "This reconciliation was closed by someone else.")
		}

		// A clean bag count moves the bag to RECONCILED.
		if rec.BagID != nil {
			if bag, bErr := q.LockBagByIDForUpdate(ctx, dbgen.LockBagByIDForUpdateParams{
				ID: *rec.BagID, OrganizationID: p.OrganizationID,
			}); bErr == nil && (bag.Status == bagging.StatusReceived || bag.Status == bagging.StatusOpened) {
				if _, uErr := q.UpdateBagStatus(ctx, dbgen.UpdateBagStatusParams{
					ID: bag.ID, OrganizationID: p.OrganizationID,
					ExpectedStatus: bag.Status, ToStatus: bagging.StatusReconciled,
				}); uErr != nil && !ops.IsNoRows(uErr) {
					return apierr.Internal(uErr)
				}
			}
		}
		if rec.ManifestID != nil {
			if m, mErr := q.LockManifestByIDForUpdate(ctx, dbgen.LockManifestByIDForUpdateParams{
				ID: *rec.ManifestID, OrganizationID: p.OrganizationID,
			}); mErr == nil && m.Status == "RECEIVED" {
				if _, uErr := q.UpdateManifestStatus(ctx, dbgen.UpdateManifestStatusParams{
					ID: m.ID, OrganizationID: p.OrganizationID,
					ExpectedStatus: "RECEIVED", ToStatus: "RECONCILED",
				}); uErr != nil && !ops.IsNoRows(uErr) {
					return apierr.Internal(uErr)
				}
			}
		}

		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionReconciliationDone, ResourceType: "reconciliation",
			ResourceID: &rec.ID, ResourcePublicID: rec.PublicID,
			OperatingUnitID: &rec.OperatingUnitID, Reason: remarks,
			After: map[string]any{
				"status": done.Status, "expected": counted.ExpectedCount,
				"matched": counted.MatchedCount, "missing": counted.MissingCount,
				"excess": counted.ExcessCount, "damaged": counted.DamagedCount,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}

		var lErr error
		detail, lErr = s.loadReconciliation(ctx, q, p, rec.PublicID)
		return lErr
	})
	return detail, err
}

func severityFor(exceptionType string) string {
	switch exceptionType {
	case "MISSING", "SEAL_BROKEN":
		return "HIGH"
	case "DAMAGED", "MISROUTED":
		return "MEDIUM"
	case "UNKNOWN_BARCODE", "EXCESS":
		return "LOW"
	default:
		return "MEDIUM"
	}
}

func exceptionCodeScope(at *ops.Facility) string {
	if at == nil {
		return "HUB"
	}
	return at.Code
}
