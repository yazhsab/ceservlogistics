// Package collection records prepaid shipment money collected at franchise
// counters. It is intentionally separate from COD, which is destination cash.
package collection

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

const (
	PermRead   = "collection.read"
	PermRecord = "collection.record"
	PermManage = "collection.manage"
)

var paymentModes = map[string]bool{"CASH": true, "POS": true, "TRANSFER": true, "BANK_DEPOSIT": true}

type Service struct {
	db    *database.DB
	q     *dbgen.Queries
	audit *audit.Recorder
}

func NewService(db *database.DB, q *dbgen.Queries, recorder *audit.Recorder) *Service {
	return &Service{db: db, q: q, audit: recorder}
}

type RecordRequest struct {
	ShipmentID  string
	AmountMinor int64
	PaymentMode string
	Reference   string
	CollectedAt time.Time
	Notes       string
}

func (s *Service) Record(ctx context.Context, p *tenant.Principal, in RecordRequest) (*dbgen.FranchiseCollection, bool, error) {
	if in.AmountMinor <= 0 {
		return nil, false, apierr.Validation("The collected amount must be positive.", nil)
	}
	if !paymentModes[in.PaymentMode] {
		return nil, false, apierr.Validation("Payment mode must be CASH, POS, TRANSFER or BANK_DEPOSIT.", nil)
	}
	if in.PaymentMode != "CASH" && len(in.Reference) < 3 {
		return nil, false, apierr.Validation("Non-cash collection requires a transaction reference.", nil)
	}
	if in.CollectedAt.IsZero() {
		in.CollectedAt = time.Now().UTC()
	}
	var out *dbgen.FranchiseCollection
	var replayed bool
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		sh, err := q.GetShipmentByPublicID(ctx, dbgen.GetShipmentByPublicIDParams{PublicID: in.ShipmentID, OrganizationID: p.OrganizationID})
		if err != nil {
			return ops.NotFoundOr(err, "Shipment")
		}
		if existing, err := q.GetFranchiseCollectionByShipment(ctx, dbgen.GetFranchiseCollectionByShipmentParams{OrganizationID: p.OrganizationID, ShipmentID: sh.ID}); err == nil {
			out, replayed = &existing, true
			return nil
		} else if !database.IsNoRows(err) {
			return apierr.Internal(err)
		}
		if sh.PaymentMode != "PREPAID" {
			return apierr.Conflict("COLLECTION_NOT_PREPAID", "Only a prepaid shipment is collected at the origin counter.")
		}
		if sh.BookingUnitID == nil {
			return apierr.Conflict("COLLECTION_NO_UNIT", "The shipment has no booking unit.")
		}
		if err := p.RequireUnitInScope(*sh.BookingUnitID); err != nil {
			return err
		}
		franchise, err := q.GetFranchiseByOperatingUnit(ctx, dbgen.GetFranchiseByOperatingUnitParams{OrganizationID: p.OrganizationID, OperatingUnitID: *sh.BookingUnitID})
		if err != nil {
			if database.IsNoRows(err) {
				return apierr.Conflict("COLLECTION_NOT_FRANCHISE", "This booking was not made at a franchise counter.")
			}
			return apierr.Internal(err)
		}
		if in.AmountMinor != sh.TotalAmountMinor {
			return apierr.Conflict("COLLECTION_AMOUNT_MISMATCH", fmt.Sprintf("Collect exactly %d minor units for this prepaid shipment.", sh.TotalAmountMinor)).WithDetail("expectedMinor", sh.TotalAmountMinor)
		}
		created, err := q.CreateFranchiseCollection(ctx, dbgen.CreateFranchiseCollectionParams{
			PublicID: publicid.New("fcl"), OrganizationID: p.OrganizationID,
			ShipmentID: sh.ID, FranchiseID: franchise.ID, OperatingUnitID: *sh.BookingUnitID,
			CustomerID: sh.CustomerID, AmountMinor: in.AmountMinor, Currency: sh.Currency,
			PaymentMode: in.PaymentMode, Reference: ops.Optional(in.Reference),
			CollectedAt: in.CollectedAt, CollectedBy: p.UserID,
			Notes: ops.Optional(in.Notes), RequestID: ops.Optional(httpx.RequestID(ctx)),
		})
		if err != nil {
			return apierr.Internal(err)
		}
		out = &created
		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "franchise.collection.recorded", ResourceType: "franchise_collection",
			ResourceID: &created.ID, ResourcePublicID: created.PublicID,
			Metadata: map[string]any{"awb": sh.Awb, "amountMinor": created.AmountMinor, "paymentMode": created.PaymentMode},
		}))
	})
	return out, replayed, err
}

func (s *Service) List(ctx context.Context, p *tenant.Principal, status string, offset, limit int32) ([]dbgen.ListFranchiseCollectionsRow, error) {
	return s.q.ListFranchiseCollections(ctx, dbgen.ListFranchiseCollectionsParams{
		OrganizationID: p.OrganizationID, Status: ops.Optional(status), ScopedUnitIds: p.UnitScope("collection.manage"),
		RowOffset: offset, RowLimit: limit,
	})
}
