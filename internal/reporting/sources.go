package reporting

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
)

// A source is one report's data, expressed as a resumable chunked read.
//
// The contract is deliberately narrow: given the last id seen, return the next
// chunk. Nothing accumulates, nothing counts rows up front, and no source may
// return more than chunkSize records. That is what keeps the engine's memory
// bounded regardless of how many rows a report covers.

// record is one output row plus the key that resumes after it.
type record struct {
	id     int64
	fields []string
}

// source describes how to read one report type.
type source struct {
	header []string
	fetch  func(ctx context.Context, orgID int64, run dbgen.ReportRun, afterID int64) ([]record, error)
}

// runParams is what the request stored, read back at generation time.
//
// Persisted rather than re-derived so a downloaded file can always be traced to
// the question it answered — a report whose filters cannot be reconstructed is
// a spreadsheet of unknown provenance.
type runParams struct {
	UnitID *int64 `json:"unitId,omitempty"`
	Status string `json:"status,omitempty"`
}

func encodeParams(in RunRequest) ([]byte, error) {
	return json.Marshal(runParams{UnitID: in.UnitID, Status: in.Status})
}

func decodeParams(raw []byte) runParams {
	var p runParams
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	return p
}

// period returns the run's window, defaulting to something bounded rather than
// letting a null date walk the whole table.
func period(run dbgen.ReportRun) (time.Time, time.Time) {
	end := time.Now().UTC()
	if run.PeriodEnd != nil {
		// Inclusive of the end date, so a one-day report covers that day.
		end = run.PeriodEnd.AddDate(0, 0, 1)
	}
	start := end.AddDate(0, 0, -30)
	if run.PeriodStart != nil {
		start = *run.PeriodStart
	}
	return start, end
}

func (s *Service) sourceFor(run dbgen.ReportRun) (*source, error) {
	switch run.ReportType {
	case TypeShipmentVolume, TypeSLA, TypeRTO, TypeExceptions:
		return s.shipmentSource(), nil
	case TypeBranchPerformance, TypeFranchisePerf, TypeServicePerformance, TypeRevenue:
		return s.performanceSource(), nil
	case TypeCODAging:
		return s.codAgingSource(), nil
	case TypeCommission, TypeSettlement:
		return s.commissionSource(), nil
	case TypeNDR:
		return s.ndrSource(), nil
	default:
		return nil, apierr.Validation("This report type has no data source.",
			map[string]any{"reportType": run.ReportType})
	}
}

// ---------------------------------------------------------------------------
// Shipments
// ---------------------------------------------------------------------------

func (s *Service) shipmentSource() *source {
	return &source{
		header: []string{
			"awb", "reference", "status", "paymentMode", "customerCode", "customerName",
			"serviceCode", "originBranch", "destinationBranch",
			"originPincode", "destinationPincode",
			"pieces", "actualWeightGrams", "chargeableWeightGrams",
			"totalAmountMinor", "codAmountMinor", "currency",
			"bookedAt", "statusChangedAt", "promisedDeliveryAt",
		},
		fetch: func(ctx context.Context, orgID int64, run dbgen.ReportRun, afterID int64) ([]record, error) {
			start, end := period(run)
			params := decodeParams(run.Parameters)
			rows, err := s.q.ReportShipments(ctx, dbgen.ReportShipmentsParams{
				OrganizationID: orgID,
				PeriodStart:    start, PeriodEnd: end,
				UnitID: params.UnitID, Status: ops.Optional(params.Status),
				AfterID: afterID, ChunkSize: chunkSize,
			})
			if err != nil {
				return nil, err
			}
			out := make([]record, 0, len(rows))
			for _, r := range rows {
				out = append(out, record{id: r.ID, fields: []string{
					r.Awb, str(r.ReferenceNumber), r.CurrentStatus, r.PaymentMode,
					r.CustomerCode, r.CustomerName, r.ServiceCode,
					str(r.OriginBranchCode), str(r.DestinationBranchCode),
					r.OriginPincode, r.DestinationPincode,
					strconv.Itoa(int(r.PieceCount)),
					strconv.Itoa(int(r.ActualWeightGrams)),
					strconv.Itoa(int(r.ChargeableWeightGrams)),
					minorString(r.TotalAmountMinor), minorString(r.CodAmountMinor), r.Currency,
					r.BookedAt.UTC().Format(time.RFC3339),
					r.StatusChangedAt.UTC().Format(time.RFC3339),
					timeString(r.PromisedDeliveryAt),
				}})
			}
			return out, nil
		},
	}
}

// ---------------------------------------------------------------------------
// Performance — reads the rollup, not the shipments
// ---------------------------------------------------------------------------

func (s *Service) performanceSource() *source {
	return &source{
		header: []string{
			"date", "unitCode", "unitName", "unitType",
			"booked", "delivered", "ndr", "rto", "slaBreaches",
			"revenueMinor", "codAmountMinor", "deliveryRateBasisPoints",
		},
		fetch: func(ctx context.Context, orgID int64, run dbgen.ReportRun, afterID int64) ([]record, error) {
			start, end := period(run)
			rows, err := s.q.ReportBranchPerformance(ctx, dbgen.ReportBranchPerformanceParams{
				OrganizationID: orgID,
				PeriodStart:    start, PeriodEnd: end,
				AfterID: afterID, ChunkSize: chunkSize,
			})
			if err != nil {
				return nil, err
			}
			out := make([]record, 0, len(rows))
			for _, r := range rows {
				// Integer basis points, like every other proportion in the
				// platform: a float here would render differently in every
				// spreadsheet that opened the file.
				rate := int64(0)
				if r.BookedCount > 0 {
					rate = int64(r.DeliveredCount) * 10_000 / int64(r.BookedCount)
				}
				out = append(out, record{id: r.ID, fields: []string{
					r.StatDate.Format("2006-01-02"),
					r.UnitCode, r.UnitName, r.UnitType,
					strconv.Itoa(int(r.BookedCount)), strconv.Itoa(int(r.DeliveredCount)),
					strconv.Itoa(int(r.NdrCount)), strconv.Itoa(int(r.RtoCount)),
					strconv.Itoa(int(r.SlaBreachCount)),
					minorString(r.RevenueMinor), minorString(r.CodAmountMinor),
					strconv.FormatInt(rate, 10),
				}})
			}
			return out, nil
		},
	}
}

// ---------------------------------------------------------------------------
// COD aging
// ---------------------------------------------------------------------------

func (s *Service) codAgingSource() *source {
	return &source{
		header: []string{
			"awb", "status", "custodianType", "franchiseCode", "franchiseName",
			"expectedMinor", "collectedMinor", "remittedMinor", "outstandingMinor",
			"currency", "collectedAt", "remittedAt", "daysHeld",
		},
		fetch: func(ctx context.Context, orgID int64, run dbgen.ReportRun, afterID int64) ([]record, error) {
			rows, err := s.q.ReportCODAging(ctx, dbgen.ReportCODAgingParams{
				OrganizationID: orgID, AfterID: afterID, ChunkSize: chunkSize,
			})
			if err != nil {
				return nil, err
			}
			out := make([]record, 0, len(rows))
			for _, r := range rows {
				out = append(out, record{id: r.ID, fields: []string{
					r.Awb, r.Status, str(r.CustodianType),
					str(r.FranchiseCode), str(r.FranchiseName),
					minorString(r.ExpectedMinor), minorString(r.CollectedMinor),
					minorString(r.RemittedMinor),
					// Computed rather than stored, so it cannot disagree with
					// the two numbers beside it.
					minorString(r.CollectedMinor - r.RemittedMinor),
					r.Currency,
					timeString(r.CollectedAt), timeString(r.RemittedAt),
					strconv.Itoa(int(r.DaysHeld)),
				}})
			}
			return out, nil
		},
	}
}

// ---------------------------------------------------------------------------
// Commission
// ---------------------------------------------------------------------------

func (s *Service) commissionSource() *source {
	return &source{
		header: []string{
			"entryId", "franchiseCode", "franchiseName", "entryType", "commissionType",
			"amountMinor", "currency", "earnedOn", "awb", "settlementNumber", "memo",
		},
		fetch: func(ctx context.Context, orgID int64, run dbgen.ReportRun, afterID int64) ([]record, error) {
			start, end := period(run)
			rows, err := s.q.ReportCommissionEntries(ctx, dbgen.ReportCommissionEntriesParams{
				OrganizationID: orgID,
				PeriodStart:    start, PeriodEnd: end,
				AfterID: afterID, ChunkSize: chunkSize,
			})
			if err != nil {
				return nil, err
			}
			out := make([]record, 0, len(rows))
			for _, r := range rows {
				out = append(out, record{id: r.ID, fields: []string{
					r.PublicID, r.FranchiseCode, r.FranchiseName,
					r.EntryType, r.CommissionType,
					minorString(r.AmountMinor), r.Currency,
					r.EarnedOn.Format("2006-01-02"),
					str(r.Awb), str(r.SettlementNumber), str(r.Memo),
				}})
			}
			return out, nil
		},
	}
}

// ---------------------------------------------------------------------------
// NDR
// ---------------------------------------------------------------------------

func (s *Service) ndrSource() *source {
	return &source{
		header: []string{
			"caseCode", "awb", "status", "reasonCode", "currentAction",
			"attempts", "branchCode", "destinationPincode", "openedAt", "resolvedAt",
		},
		fetch: func(ctx context.Context, orgID int64, run dbgen.ReportRun, afterID int64) ([]record, error) {
			start, end := period(run)
			rows, err := s.q.ReportNDRCases(ctx, dbgen.ReportNDRCasesParams{
				OrganizationID: orgID,
				PeriodStart:    start, PeriodEnd: end,
				AfterID: afterID, ChunkSize: chunkSize,
			})
			if err != nil {
				return nil, err
			}
			out := make([]record, 0, len(rows))
			for _, r := range rows {
				out = append(out, record{id: r.ID, fields: []string{
					r.CaseCode, r.Awb, r.Status, r.CurrentReasonCode,
					str(r.CurrentAction), strconv.Itoa(int(r.AttemptCount)),
					str(r.BranchCode), r.DestinationPincode,
					r.OpenedAt.UTC().Format(time.RFC3339), timeString(r.ResolvedAt),
				}})
			}
			return out, nil
		},
	}
}

// Ensure dateString stays referenced while report types that use it are added.
var _ = dateString
