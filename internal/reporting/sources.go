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
	case TypeVAT:
		return s.vatSource(), nil
	case TypeWithholdingTax:
		return s.withholdingSource(), nil
	case TypeProfitLoss:
		return s.ledgerSummarySource([]string{"REVENUE", "EXPENSE"}), nil
	case TypeBalanceSheet:
		return s.ledgerSummarySource([]string{"ASSET", "LIABILITY", "EQUITY"}), nil
	case TypeCashBook:
		return s.cashBookSource(), nil
	case TypeReceivablesAging:
		return s.receivablesSource(), nil
	case TypeFranchiseCollection:
		return s.franchiseCollectionSource(), nil
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

// Nigeria statutory and management finance exports. Amounts remain in minor
// units so CSV consumers never lose kobo through floating-point conversion.
func (s *Service) vatSource() *source {
	return &source{header: []string{"invoiceNumber", "issueDate", "customer", "taxCode", "rateBasisPoints", "taxableMinor", "vatMinor", "currency"}, fetch: func(ctx context.Context, orgID int64, run dbgen.ReportRun, afterID int64) ([]record, error) {
		start, end := period(run)
		rows, err := s.db.Pool.Query(ctx, `SELECT tc.id, i.invoice_number, i.issue_date, COALESCE(i.bill_to->>'name',''), tc.component_code, tc.rate_bp, tc.taxable_minor, tc.tax_minor, tc.currency FROM tax_components tc JOIN invoices i ON i.id=tc.invoice_id WHERE tc.organization_id=$1 AND tc.id>$2 AND i.issue_date >= $3 AND i.issue_date < $4 AND tc.component_code='VAT' ORDER BY tc.id LIMIT $5`, orgID, afterID, start, end, chunkSize)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []record
		for rows.Next() {
			var id int64
			var number, customer, code, currency string
			var issue time.Time
			var rate int32
			var taxable, tax int64
			if err := rows.Scan(&id, &number, &issue, &customer, &code, &rate, &taxable, &tax, &currency); err != nil {
				return nil, err
			}
			out = append(out, record{id: id, fields: []string{number, issue.Format("2006-01-02"), customer, code, strconv.Itoa(int(rate)), minorString(taxable), minorString(tax), currency}})
		}
		return out, rows.Err()
	}}
}

func (s *Service) withholdingSource() *source {
	return &source{header: []string{"settlementNumber", "franchiseCode", "franchiseName", "periodStart", "periodEnd", "withholdingMinor", "currency", "status"}, fetch: func(ctx context.Context, orgID int64, run dbgen.ReportRun, afterID int64) ([]record, error) {
		start, end := period(run)
		rows, err := s.db.Pool.Query(ctx, `SELECT s.id,s.settlement_number,f.code,f.name,s.period_start,s.period_end,s.withholding_minor,s.currency,s.status FROM settlements s JOIN franchises f ON f.id=s.franchise_id WHERE s.organization_id=$1 AND s.id>$2 AND s.period_end >= $3 AND s.period_start < $4 AND s.withholding_minor<>0 ORDER BY s.id LIMIT $5`, orgID, afterID, start, end, chunkSize)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []record
		for rows.Next() {
			var id, amount int64
			var number, code, name, currency, status string
			var ps, pe time.Time
			if err := rows.Scan(&id, &number, &code, &name, &ps, &pe, &amount, &currency, &status); err != nil {
				return nil, err
			}
			out = append(out, record{id: id, fields: []string{number, code, name, ps.Format("2006-01-02"), pe.Format("2006-01-02"), minorString(amount), currency, status}})
		}
		return out, rows.Err()
	}}
}

func (s *Service) ledgerSummarySource(types []string) *source {
	return &source{header: []string{"accountCode", "accountName", "accountType", "debitMinor", "creditMinor", "normalBalanceMinor", "currency"}, fetch: func(ctx context.Context, orgID int64, run dbgen.ReportRun, afterID int64) ([]record, error) {
		start, end := period(run)
		if len(types) > 0 && types[0] != "REVENUE" {
			start = time.Unix(0, 0).UTC()
		}
		rows, err := s.db.Pool.Query(ctx, `SELECT a.id,a.code,a.name,a.account_type,COALESCE(SUM(e.debit_minor),0)::bigint,COALESCE(SUM(e.credit_minor),0)::bigint,CASE WHEN a.normal_balance='DEBIT' THEN COALESCE(SUM(e.debit_minor-e.credit_minor),0) ELSE COALESCE(SUM(e.credit_minor-e.debit_minor),0) END::bigint,a.currency FROM ledger_accounts a LEFT JOIN journal_entries e ON e.account_id=a.id AND EXISTS (SELECT 1 FROM journal_transactions j WHERE j.id=e.transaction_id AND j.status='POSTED' AND j.posting_date >= $3 AND j.posting_date < $4) WHERE a.organization_id=$1 AND a.id>$2 AND a.account_type=ANY($5::text[]) GROUP BY a.id ORDER BY a.id LIMIT $6`, orgID, afterID, start, end, types, chunkSize)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []record
		for rows.Next() {
			var id, debit, credit, balance int64
			var code, name, typ, currency string
			if err := rows.Scan(&id, &code, &name, &typ, &debit, &credit, &balance, &currency); err != nil {
				return nil, err
			}
			out = append(out, record{id: id, fields: []string{code, name, typ, minorString(debit), minorString(credit), minorString(balance), currency}})
		}
		return out, rows.Err()
	}}
}

func (s *Service) cashBookSource() *source {
	return &source{header: []string{"postingDate", "transactionNumber", "purpose", "description", "debitMinor", "creditMinor", "currency"}, fetch: func(ctx context.Context, orgID int64, run dbgen.ReportRun, afterID int64) ([]record, error) {
		start, end := period(run)
		rows, err := s.db.Pool.Query(ctx, `SELECT e.id,j.posting_date,j.transaction_number,j.purpose,j.description,e.debit_minor,e.credit_minor,e.currency FROM journal_entries e JOIN journal_transactions j ON j.id=e.transaction_id JOIN ledger_accounts a ON a.id=e.account_id WHERE e.organization_id=$1 AND e.id>$2 AND j.status='POSTED' AND j.posting_date >= $3 AND j.posting_date < $4 AND a.code='1000' ORDER BY e.id LIMIT $5`, orgID, afterID, start, end, chunkSize)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []record
		for rows.Next() {
			var id, debit, credit int64
			var d time.Time
			var no, purpose, desc, currency string
			if err := rows.Scan(&id, &d, &no, &purpose, &desc, &debit, &credit, &currency); err != nil {
				return nil, err
			}
			out = append(out, record{id: id, fields: []string{d.Format("2006-01-02"), no, purpose, desc, minorString(debit), minorString(credit), currency}})
		}
		return out, rows.Err()
	}}
}

func (s *Service) receivablesSource() *source {
	return &source{header: []string{"invoiceNumber", "customerCode", "customerName", "issueDate", "dueDate", "status", "totalMinor", "paidMinor", "creditedMinor", "outstandingMinor", "daysOverdue", "currency"}, fetch: func(ctx context.Context, orgID int64, run dbgen.ReportRun, afterID int64) ([]record, error) {
		_, end := period(run)
		rows, err := s.db.Pool.Query(ctx, `SELECT i.id,i.invoice_number,c.code,c.name,i.issue_date,i.due_date,i.status,i.total_minor,i.paid_minor,i.credited_minor,(i.total_minor-i.paid_minor-i.credited_minor)::bigint,GREATEST(0,($3::date-i.due_date))::int,i.currency FROM invoices i JOIN customers c ON c.id=i.customer_id WHERE i.organization_id=$1 AND i.id>$2 AND i.issue_date<$3 AND i.status NOT IN ('DRAFT','CANCELLED','PAID','WRITTEN_OFF') AND i.total_minor>i.paid_minor+i.credited_minor ORDER BY i.id LIMIT $4`, orgID, afterID, end, chunkSize)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []record
		for rows.Next() {
			var id, total, paid, credited, outstanding int64
			var days int32
			var no, code, name, status, currency string
			var issue, due time.Time
			if err := rows.Scan(&id, &no, &code, &name, &issue, &due, &status, &total, &paid, &credited, &outstanding, &days, &currency); err != nil {
				return nil, err
			}
			out = append(out, record{id: id, fields: []string{no, code, name, issue.Format("2006-01-02"), due.Format("2006-01-02"), status, minorString(total), minorString(paid), minorString(credited), minorString(outstanding), strconv.Itoa(int(days)), currency}})
		}
		return out, rows.Err()
	}}
}

func (s *Service) franchiseCollectionSource() *source {
	return &source{header: []string{"collectionId", "awb", "franchiseCode", "franchiseName", "amountMinor", "paymentMode", "reference", "status", "collectedAt", "remittedAt", "settlementNumber", "currency"}, fetch: func(ctx context.Context, orgID int64, run dbgen.ReportRun, afterID int64) ([]record, error) {
		start, end := period(run)
		rows, err := s.db.Pool.Query(ctx, `SELECT fc.id,fc.public_id,sh.awb,f.code,f.name,fc.amount_minor,fc.payment_mode,COALESCE(fc.reference,''),fc.status,fc.collected_at,fc.remitted_at,COALESCE(s.settlement_number,''),fc.currency FROM franchise_collections fc JOIN shipments sh ON sh.id=fc.shipment_id JOIN franchises f ON f.id=fc.franchise_id LEFT JOIN settlements s ON s.id=fc.settlement_id WHERE fc.organization_id=$1 AND fc.id>$2 AND fc.collected_at >= $3 AND fc.collected_at < $4 ORDER BY fc.id LIMIT $5`, orgID, afterID, start, end, chunkSize)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []record
		for rows.Next() {
			var id, amount int64
			var publicID, awb, code, name, mode, ref, status, settlement, currency string
			var collected time.Time
			var remitted *time.Time
			if err := rows.Scan(&id, &publicID, &awb, &code, &name, &amount, &mode, &ref, &status, &collected, &remitted, &settlement, &currency); err != nil {
				return nil, err
			}
			out = append(out, record{id: id, fields: []string{publicID, awb, code, name, minorString(amount), mode, ref, status, collected.UTC().Format(time.RFC3339), timeString(remitted), settlement, currency}})
		}
		return out, rows.Err()
	}}
}

// Ensure dateString stays referenced while report types that use it are added.
var _ = dateString
