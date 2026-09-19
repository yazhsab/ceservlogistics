// Package billing implements M25: invoices, credit and debit notes, and the
// periodic billing run.
//
// # Lines are snapshots
//
// An invoice line copies the amount, the description, the HSN/SAC code and the
// tax from the shipment's charge snapshot at the moment of issue. It never
// joins back to a rate card at read time. That is what makes an issued invoice
// a statutory document rather than a view: editing a rate card tomorrow cannot
// change what was billed today (§19, §26).
//
// The charge snapshot is itself immutable, written once when the shipment was
// priced, so billing reads a frozen number rather than re-running the pricing
// engine — which would not necessarily agree with what the customer was quoted.
//
// # Numbering
//
// Invoice numbers come from an atomic UPSERT on invoice_sequences. Concurrent
// issuers serialise on that row, so N of them get N distinct numbers with no
// gaps and no process-local lock. Gaps matter: a tax authority reading a series
// with a hole in it asks what happened to the missing document.
//
// # Tax
//
// The tax engine is deliberately generic. tax_components holds named components
// with rates in basis points; India GST is expressed as CGST/SGST or IGST rows
// with place-of-supply metadata, not as hardcoded logic. Changing regime is
// configuration, not a code change.
//
// # The accounting
//
//	issue    DR Trade Receivable (customer)  CR Freight Revenue
//	                                         CR Surcharge Revenue
//	                                         CR Tax Payable
//	payment  DR Cash and Bank                CR Trade Receivable (customer)
//	credit   DR Discount Allowed             CR Trade Receivable (customer)
//	         DR Tax Payable
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ledger"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/money"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Invoice statuses.
const (
	StatusDraft         = "DRAFT"
	StatusIssued        = "ISSUED"
	StatusPartiallyPaid = "PARTIALLY_PAID"
	StatusPaid          = "PAID"
	StatusOverdue       = "OVERDUE"
	StatusCancelled     = "CANCELLED"
	StatusWrittenOff    = "WRITTEN_OFF"
)

// Billing modes.
const (
	ModeImmediate = "IMMEDIATE"
	ModePeriodic  = "PERIODIC"
)

// Line types.
const (
	LineFreight    = "FREIGHT"
	LineSurcharge  = "SURCHARGE"
	LineCODFee     = "COD_FEE"
	LineInsurance  = "INSURANCE"
	LineHandling   = "HANDLING"
	LineDiscount   = "DISCOUNT"
	LineAdjustment = "ADJUSTMENT"
	LineSummary    = "SUMMARY"
	LineOther      = "OTHER"
)

// Document types for numbering.
const (
	DocInvoice    = "INVOICE"
	DocCreditNote = "CREDIT_NOTE"
	DocDebitNote  = "DEBIT_NOTE"
)

// Error codes.
const (
	CodeNothingToBill   = "BILLING_NOTHING_TO_BILL"
	CodeAlreadyIssued   = "INVOICE_ALREADY_ISSUED"
	CodeNotIssued       = "INVOICE_NOT_ISSUED"
	CodeInvalidState    = "INVOICE_INVALID_STATE"
	CodeOverpayment     = "INVOICE_OVERPAYMENT"
	CodeSelfApproval    = "CREDIT_NOTE_SELF_APPROVAL"
	CodeCreditExceeds   = "CREDIT_NOTE_EXCEEDS_INVOICE"
	CodeSequenceExhaust = "INVOICE_SEQUENCE_EXHAUSTED"
	CodeAlreadyPaid     = "INVOICE_ALREADY_PAID"
)

// Service owns invoicing.
type Service struct {
	db     *database.DB
	q      *dbgen.Queries
	ledger *ledger.Service
	audit  *audit.Recorder
	log    *slog.Logger

	// DefaultTermsDays is used when a customer has no business account with
	// agreed payment terms.
	DefaultTermsDays int
}

func NewService(
	db *database.DB, q *dbgen.Queries, led *ledger.Service,
	rec *audit.Recorder, log *slog.Logger,
) *Service {
	return &Service{db: db, q: q, ledger: led, audit: rec, log: log, DefaultTermsDays: 30}
}

// TaxComponentSpec is a configured tax to apply. Held as data so the regime is
// configuration: two 9% rows make Indian intra-state GST, one 18% row makes
// inter-state, and neither is special-cased in code.
type TaxComponentSpec struct {
	Code     string         `json:"code"`
	Name     string         `json:"name"`
	RateBp   int32          `json:"rateBp"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// DraftRequest asks for an invoice covering a customer's shipments.
type DraftRequest struct {
	CustomerID  string
	BillingMode string
	PeriodStart time.Time
	PeriodEnd   time.Time
	IssueDate   time.Time
	// Summary produces one rolled-up line instead of a line per shipment.
	// A customer with 4,000 monthly shipments does not want 4,000 lines, but
	// invoice_shipments still records every AWB so the detail is available.
	Summary bool
	// Taxes to apply. Empty means no tax, which is legitimate for an
	// unregistered supplier.
	Taxes []TaxComponentSpec
	Notes string
	// BillingRunID links this invoice to a bulk run.
	BillingRunID *int64
}

// Result carries an invoice and its lines.
type Result struct {
	Invoice dbgen.Invoice
	Lines   []dbgen.ListInvoiceLinesRow
	Taxes   []dbgen.ListInvoiceTaxComponentsRow
}

// Draft builds an unissued invoice from a customer's unbilled shipments.
//
// Nothing is numbered and nothing is posted until Issue: a draft can be
// reviewed, discarded or rebuilt, and only issuing consumes a statutory number.
func (s *Service) Draft(
	ctx context.Context, p *tenant.Principal, in DraftRequest,
) (*Result, error) {
	if in.PeriodEnd.Before(in.PeriodStart) {
		return nil, apierr.Validation("The period ends before it starts.", nil)
	}
	if in.IssueDate.IsZero() {
		in.IssueDate = time.Now().UTC()
	}
	if in.BillingMode == "" {
		in.BillingMode = ModePeriodic
	}

	var out *Result
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		customer, err := q.GetCustomerByPublicID(ctx, dbgen.GetCustomerByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: in.CustomerID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Customer")
		}

		// FOR UPDATE inside this transaction: a concurrent run for the same
		// customer blocks here rather than invoicing the same shipments.
		shipments, err := q.ListBillableShipments(ctx, dbgen.ListBillableShipmentsParams{
			OrganizationID: p.OrganizationID,
			CustomerID:     customer.ID,
			PeriodStart:    in.PeriodStart,
			PeriodEnd:      in.PeriodEnd.AddDate(0, 0, 1), // exclusive
			Limit:          10000,
		})
		if err != nil {
			return apierr.Internal(err)
		}
		if len(shipments) == 0 {
			return apierr.Conflict(CodeNothingToBill,
				fmt.Sprintf("Customer %s has no unbilled shipments in this period.", customer.Code))
		}

		termsDays := s.DefaultTermsDays
		var businessAccountID *int64
		// The query keys on customer_id alone; the customer was already resolved
		// under this tenant, so the account it points at is this tenant's.
		if acct, aErr := q.GetBusinessAccountByCustomer(ctx, customer.ID); aErr == nil &&
			acct.OrganizationID == p.OrganizationID {
			businessAccountID = &acct.ID
			if acct.PaymentTermsDays > 0 {
				termsDays = int(acct.PaymentTermsDays)
			}
		}

		billTo, _ := json.Marshal(map[string]any{
			"customerCode": customer.Code, "name": customer.Name,
			"gstNumber": customer.GstNumber, "panNumber": customer.PanNumber,
		})
		supplier, _ := json.Marshal(map[string]any{
			"organizationCode": p.OrganizationCode, "name": p.OrganizationCode,
		})

		invoice, err := q.CreateInvoice(ctx, dbgen.CreateInvoiceParams{
			PublicID:       publicid.New("inv"),
			OrganizationID: p.OrganizationID,
			// A draft carries a placeholder; the statutory number is allocated
			// at issue so an abandoned draft does not burn one.
			InvoiceNumber:     "DRAFT-" + publicid.New("tmp")[4:14],
			SeriesCode:        "DEFAULT",
			DocumentType:      "INVOICE",
			CustomerID:        customer.ID,
			BusinessAccountID: businessAccountID,
			BillingRunID:      in.BillingRunID,
			BillingMode:       in.BillingMode,
			PeriodStart:       &in.PeriodStart,
			PeriodEnd:         &in.PeriodEnd,
			IssueDate:         in.IssueDate,
			DueDate:           in.IssueDate.AddDate(0, 0, termsDays),
			Currency:          p.OrganizationCurrency,
			BillTo:            billTo,
			Supplier:          supplier,
			TaxMetadata:       []byte("{}"),
			Notes:             ops.Optional(in.Notes),
			CreatedBy:         &p.UserID,
			RequestID:         ops.Optional(httpx.RequestID(ctx)),
		})
		if err != nil {
			return apierr.Internal(err)
		}

		res, bErr := s.buildLines(ctx, q, p, invoice, shipments, in)
		if bErr != nil {
			return bErr
		}
		out = res
		return nil
	})
	return out, err
}

// buildLines writes the snapshot lines and the header totals.
func (s *Service) buildLines(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal,
	invoice dbgen.Invoice, shipments []dbgen.ListBillableShipmentsRow, in DraftRequest,
) (*Result, error) {
	lineNo := int32(0)
	var subtotal, discount, taxable int64

	addLine := func(l dbgen.CreateInvoiceLineParams) error {
		lineNo++
		l.PublicID = publicid.New("ivl")
		l.OrganizationID = p.OrganizationID
		l.InvoiceID = invoice.ID
		l.LineNo = lineNo
		l.Currency = invoice.Currency
		if l.Metadata == nil {
			l.Metadata = []byte("{}")
		}
		_, err := q.CreateInvoiceLine(ctx, l)
		return err
	}

	if in.Summary {
		// One rolled-up line. Every AWB still lands in invoice_shipments, so
		// the customer can be given the detail without the invoice carrying it.
		var freight, surcharge, disc int64
		for _, sh := range shipments {
			freight += sh.FreightMinor
			surcharge += sh.SurchargeTotalMinor
			disc += sh.DiscountTotalMinor
		}
		net := freight + surcharge - disc
		if err := addLine(dbgen.CreateInvoiceLineParams{
			LineType: LineSummary,
			Description: fmt.Sprintf("Courier services, %d shipment(s), %s to %s",
				len(shipments),
				in.PeriodStart.Format("2 Jan 2006"), in.PeriodEnd.Format("2 Jan 2006")),
			Quantity:       int32(len(shipments)),
			UnitPriceMinor: 0,
			AmountMinor:    freight + surcharge,
			DiscountMinor:  disc,
			TaxableMinor:   net,
			TotalMinor:     net,
		}); err != nil {
			return nil, apierr.Internal(err)
		}
		subtotal, discount, taxable = freight+surcharge, disc, net
	} else {
		for _, sh := range shipments {
			net := sh.FreightMinor + sh.SurchargeTotalMinor - sh.DiscountTotalMinor
			shipmentID := sh.ID
			snapshotID := sh.ChargeSnapshotID
			if err := addLine(dbgen.CreateInvoiceLineParams{
				LineType: LineFreight,
				Description: fmt.Sprintf("AWB %s, %s to %s, %dg",
					sh.Awb, sh.OriginPincode, sh.DestinationPincode, sh.ChargeableWeightGrams),
				Quantity:         1,
				UnitPriceMinor:   sh.FreightMinor,
				AmountMinor:      sh.FreightMinor + sh.SurchargeTotalMinor,
				DiscountMinor:    sh.DiscountTotalMinor,
				TaxableMinor:     net,
				TotalMinor:       net,
				ShipmentID:       &shipmentID,
				ChargeSnapshotID: &snapshotID,
			}); err != nil {
				return nil, apierr.Internal(err)
			}
			subtotal += sh.FreightMinor + sh.SurchargeTotalMinor
			discount += sh.DiscountTotalMinor
			taxable += net
		}
	}

	// Tax, applied to the taxable total. Each configured component becomes a
	// row, so an invoice can always explain its own tax without consulting
	// current configuration.
	var taxTotal int64
	for _, spec := range in.Taxes {
		amount := money.ApplyBP(taxable, money.BasisPoints(spec.RateBp))
		if amount == 0 {
			continue
		}
		meta := []byte("{}")
		if len(spec.Metadata) > 0 {
			if b, err := json.Marshal(spec.Metadata); err == nil {
				meta = b
			}
		}
		if _, err := q.CreateTaxComponent(ctx, dbgen.CreateTaxComponentParams{
			PublicID:       publicid.New("txc"),
			OrganizationID: p.OrganizationID,
			InvoiceID:      &invoice.ID,
			ComponentCode:  spec.Code,
			ComponentName:  spec.Name,
			RateBp:         spec.RateBp,
			TaxableMinor:   taxable,
			TaxMinor:       amount,
			Currency:       invoice.Currency,
			Metadata:       meta,
		}); err != nil {
			return nil, apierr.Internal(err)
		}
		taxTotal += amount
	}

	// Tax lands on the header rather than being spread across lines, so the
	// line totals and the header agree with the deferred invoice total check.
	if taxTotal > 0 {
		if err := addLine(dbgen.CreateInvoiceLineParams{
			LineType:     LineOther,
			Description:  "Tax",
			Quantity:     1,
			AmountMinor:  taxTotal,
			TaxableMinor: 0,
			TaxMinor:     taxTotal,
			TotalMinor:   taxTotal,
		}); err != nil {
			return nil, apierr.Internal(err)
		}
	}

	total := taxable + taxTotal

	for _, sh := range shipments {
		if _, err := q.AddInvoiceShipment(ctx, dbgen.AddInvoiceShipmentParams{
			OrganizationID: p.OrganizationID,
			InvoiceID:      invoice.ID,
			ShipmentID:     sh.ID,
			Awb:            sh.Awb,
			AmountMinor:    sh.FreightMinor + sh.SurchargeTotalMinor - sh.DiscountTotalMinor,
			TaxMinor:       0,
		}); err != nil {
			// A shipment already billed on another invoice.
			if ops.IsUnique(err, "invoice_shipments_billed_once_idx") {
				return nil, apierr.Conflict(CodeInvalidState,
					fmt.Sprintf("Shipment %s has already been invoiced.", sh.Awb)).
					WithDetail("awb", sh.Awb)
			}
			return nil, apierr.Internal(err)
		}
	}

	updated, err := q.ApplyInvoiceTotals(ctx, dbgen.ApplyInvoiceTotalsParams{
		OrganizationID: p.OrganizationID, ID: invoice.ID,
		SubtotalMinor: subtotal, DiscountMinor: discount,
		TaxableMinor: taxable, TaxMinor: taxTotal,
		RoundingMinor: 0, TotalMinor: total,
		ShipmentCount: int32(len(shipments)),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}

	lines, err := q.ListInvoiceLines(ctx, invoice.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	taxes, err := q.ListInvoiceTaxComponents(ctx, &invoice.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return &Result{Invoice: updated, Lines: lines, Taxes: taxes}, nil
}

// Issue stamps a statutory number on a draft and posts it to the ledger.
func (s *Service) Issue(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.Invoice, error) {
	var out *dbgen.Invoice

	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		header, err := q.GetInvoiceByPublicID(ctx, dbgen.GetInvoiceByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: publicID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Invoice")
		}
		locked, err := q.LockInvoice(ctx, dbgen.LockInvoiceParams{
			OrganizationID: p.OrganizationID, ID: header.ID,
		})
		if err != nil {
			return apierr.Internal(err)
		}
		if locked.Status != StatusDraft {
			return apierr.Conflict(CodeAlreadyIssued,
				fmt.Sprintf("Invoice %s is %s and cannot be issued again.",
					locked.InvoiceNumber, locked.Status))
		}
		if locked.TotalMinor <= 0 {
			return apierr.Conflict(CodeNothingToBill,
				"An invoice for zero cannot be issued.")
		}

		number, err := s.allocateNumber(ctx, q, p.OrganizationID,
			locked.SeriesCode, DocInvoice, locked.IssueDate)
		if err != nil {
			return err
		}

		// DR Trade Receivable (customer)  CR revenue and tax
		legs := []ledger.Leg{
			{
				AccountCode: ledger.AcctTradeReceivable, Debit: locked.TotalMinor,
				PartyType: ledger.PartyCustomer, PartyID: locked.CustomerID,
				CustomerID: &locked.CustomerID,
				Memo:       "Invoice " + number,
			},
			{
				AccountCode: ledger.AcctFreightRevenue, Credit: locked.TaxableMinor,
				CustomerID: &locked.CustomerID,
				Memo:       "Courier services billed",
			},
		}
		if locked.TaxMinor > 0 {
			legs = append(legs, ledger.Leg{
				AccountCode: ledger.AcctTaxPayable, Credit: locked.TaxMinor,
				CustomerID: &locked.CustomerID,
				Memo:       "Output tax on " + number,
			})
		}

		posting, err := s.ledger.Post(ctx, tx, p, ledger.Posting{
			SourceType:     ledger.SourceInvoice,
			SourceID:       &locked.ID,
			SourcePublicID: locked.PublicID,
			Purpose:        "ISSUE",
			Description:    "Invoice " + number,
			Currency:       locked.Currency,
			PostingDate:    locked.IssueDate,
			Legs:           legs,
			Metadata: map[string]any{
				"invoiceNumber": number, "shipmentCount": locked.ShipmentCount,
			},
		})
		if err != nil {
			return err
		}

		issued, err := q.IssueInvoice(ctx, dbgen.IssueInvoiceParams{
			OrganizationID: p.OrganizationID, ID: locked.ID,
			InvoiceNumber: number, IssuedBy: &p.UserID,
			JournalTransactionID: &posting.Transaction.ID,
		})
		if err != nil {
			if ops.IsNoRows(err) {
				return apierr.Conflict(CodeAlreadyIssued,
					"This invoice was issued by another request.")
			}
			if ops.IsUnique(err, "invoices_number_idx") {
				return apierr.Conflict(apierr.CodeConflict,
					fmt.Sprintf("Invoice number %s is already in use.", number))
			}
			return apierr.Internal(err)
		}

		out = &issued
		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "invoice.issued", ResourceType: "invoice",
			ResourceID: &locked.ID, ResourcePublicID: locked.PublicID,
			Metadata: map[string]any{
				"invoiceNumber": number, "totalMinor": locked.TotalMinor,
				"customerId": locked.CustomerID, "shipmentCount": locked.ShipmentCount,
			},
		}))
	})
	return out, err
}

// PayRequest records a customer payment.
type PayRequest struct {
	InvoiceID   string
	AmountMinor int64
	PaymentMode string
	Reference   string
	ReceivedOn  time.Time
}

// RecordPayment banks a customer payment against an invoice.
//
// Idempotent on (invoice, reference) so a gateway retrying a webhook does not
// bank the same money twice.
func (s *Service) RecordPayment(
	ctx context.Context, p *tenant.Principal, in PayRequest,
) (*dbgen.InvoicePayment, *dbgen.Invoice, error) {
	if in.AmountMinor <= 0 {
		return nil, nil, apierr.Validation("A payment must be for a positive amount.", nil)
	}
	if in.ReceivedOn.IsZero() {
		in.ReceivedOn = time.Now().UTC()
	}

	var payment *dbgen.InvoicePayment
	var updated *dbgen.Invoice

	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		header, err := q.GetInvoiceByPublicID(ctx, dbgen.GetInvoiceByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: in.InvoiceID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Invoice")
		}

		// Replay probe.
		if in.Reference != "" {
			if existing, fErr := q.FindPaymentByReference(ctx, dbgen.FindPaymentByReferenceParams{
				OrganizationID: p.OrganizationID, InvoiceID: header.ID,
				Reference: ops.Optional(in.Reference),
			}); fErr == nil {
				inv, _ := q.GetInvoiceByID(ctx, dbgen.GetInvoiceByIDParams{
					OrganizationID: p.OrganizationID, ID: header.ID,
				})
				payment, updated = &existing, &inv
				return nil
			} else if !ops.IsNoRows(fErr) {
				return apierr.Internal(fErr)
			}
		}

		locked, err := q.LockInvoice(ctx, dbgen.LockInvoiceParams{
			OrganizationID: p.OrganizationID, ID: header.ID,
		})
		if err != nil {
			return apierr.Internal(err)
		}
		if locked.Status == StatusDraft {
			return apierr.Conflict(CodeNotIssued,
				"A draft invoice cannot be paid; issue it first.")
		}
		if locked.Status == StatusCancelled {
			return apierr.Conflict(CodeInvalidState, "A cancelled invoice cannot be paid.")
		}

		outstanding := locked.TotalMinor - locked.PaidMinor - locked.CreditedMinor
		if outstanding <= 0 {
			return apierr.Conflict(CodeAlreadyPaid,
				fmt.Sprintf("Invoice %s is already settled.", locked.InvoiceNumber))
		}
		if in.AmountMinor > outstanding {
			return apierr.Conflict(CodeOverpayment,
				fmt.Sprintf("This payment of %d exceeds the %d still outstanding.",
					in.AmountMinor, outstanding)).
				WithDetail("outstandingMinor", outstanding)
		}

		number, err := s.allocateNumber(ctx, q, p.OrganizationID, "PAY", DocInvoice, in.ReceivedOn)
		if err != nil {
			return err
		}

		posting, err := s.ledger.Post(ctx, tx, p, ledger.Posting{
			SourceType:     ledger.SourcePayment,
			SourceID:       &locked.ID,
			SourcePublicID: locked.PublicID,
			Purpose:        "INVOICE_PAYMENT:" + number,
			Description:    "Payment " + number + " for " + locked.InvoiceNumber,
			Currency:       locked.Currency, PostingDate: in.ReceivedOn,
			Legs: []ledger.Leg{
				{AccountCode: ledger.AcctCash, Debit: in.AmountMinor,
					CustomerID: &locked.CustomerID, Memo: "Received from customer"},
				{AccountCode: ledger.AcctTradeReceivable, Credit: in.AmountMinor,
					PartyType: ledger.PartyCustomer, PartyID: locked.CustomerID,
					CustomerID: &locked.CustomerID,
					Memo:       "Settled against " + locked.InvoiceNumber},
			},
		})
		if err != nil {
			return err
		}

		created, err := q.CreateInvoicePayment(ctx, dbgen.CreateInvoicePaymentParams{
			PublicID:             publicid.New("ipm"),
			OrganizationID:       p.OrganizationID,
			InvoiceID:            locked.ID,
			CustomerID:           locked.CustomerID,
			PaymentNumber:        number,
			AmountMinor:          in.AmountMinor,
			Currency:             locked.Currency,
			PaymentMode:          in.PaymentMode,
			Reference:            ops.Optional(in.Reference),
			ReceivedOn:           in.ReceivedOn,
			JournalTransactionID: &posting.Transaction.ID,
			RecordedBy:           p.UserID,
			RequestID:            ops.Optional(httpx.RequestID(ctx)),
		})
		if err != nil {
			return apierr.Internal(err)
		}

		advanced, err := q.RecordInvoicePayment(ctx, dbgen.RecordInvoicePaymentParams{
			OrganizationID: p.OrganizationID, ID: locked.ID, AmountMinor: in.AmountMinor,
		})
		if err != nil {
			return apierr.Internal(err)
		}

		payment, updated = &created, &advanced
		return nil
	})
	return payment, updated, err
}

// CreditNoteRequest raises a credit or debit note against an invoice.
type CreditNoteRequest struct {
	InvoiceID   string
	NoteType    string // CREDIT or DEBIT
	ReasonCode  string
	Reason      string
	AmountMinor int64
	TaxMinor    int64
	IssueDate   time.Time
}

// RaiseCreditNote creates a draft note. It moves no money until issued by a
// second person.
func (s *Service) RaiseCreditNote(
	ctx context.Context, p *tenant.Principal, in CreditNoteRequest,
) (*dbgen.CreditNote, error) {
	if in.AmountMinor <= 0 {
		return nil, apierr.Validation("A note must be for a positive amount.", nil)
	}
	if len([]rune(in.Reason)) < 5 {
		return nil, apierr.Validation("A credit or debit note requires a reason.",
			map[string]any{"reason": "at least 5 characters"})
	}
	if in.NoteType == "" {
		in.NoteType = "CREDIT"
	}
	if in.IssueDate.IsZero() {
		in.IssueDate = time.Now().UTC()
	}

	var out *dbgen.CreditNote
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		invoice, err := q.GetInvoiceByPublicID(ctx, dbgen.GetInvoiceByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: in.InvoiceID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Invoice")
		}
		if invoice.Status == StatusDraft {
			return apierr.Conflict(CodeNotIssued,
				"A draft invoice has nothing to credit; cancel it instead.")
		}

		total := in.AmountMinor + in.TaxMinor
		if in.NoteType == "CREDIT" {
			// A credit note cannot exceed what is still owed, or the customer
			// would end up with a negative receivable that nothing explains.
			outstanding := invoice.TotalMinor - invoice.PaidMinor - invoice.CreditedMinor
			if total > outstanding {
				return apierr.Conflict(CodeCreditExceeds,
					fmt.Sprintf("A credit of %d exceeds the %d outstanding on %s.",
						total, outstanding, invoice.InvoiceNumber)).
					WithDetail("outstandingMinor", outstanding)
			}
		}

		created, err := q.CreateCreditNote(ctx, dbgen.CreateCreditNoteParams{
			PublicID:       publicid.New("crn"),
			OrganizationID: p.OrganizationID,
			NoteNumber:     "DRAFT-" + publicid.New("tmp")[4:14],
			NoteType:       in.NoteType,
			InvoiceID:      &invoice.ID,
			CustomerID:     invoice.CustomerID,
			ReasonCode:     in.ReasonCode,
			Reason:         in.Reason,
			IssueDate:      in.IssueDate,
			Currency:       invoice.Currency,
			SubtotalMinor:  in.AmountMinor,
			TaxMinor:       in.TaxMinor,
			TotalMinor:     total,
			TaxMetadata:    []byte("{}"),
			CreatedBy:      &p.UserID,
			RequestID:      ops.Optional(httpx.RequestID(ctx)),
		})
		if err != nil {
			return apierr.Internal(err)
		}
		if _, err := q.CreateCreditNoteLine(ctx, dbgen.CreateCreditNoteLineParams{
			OrganizationID: p.OrganizationID, CreditNoteID: created.ID, LineNo: 1,
			Description: in.Reason, AmountMinor: in.AmountMinor,
			TaxMinor: in.TaxMinor, TotalMinor: total, Currency: invoice.Currency,
		}); err != nil {
			return apierr.Internal(err)
		}
		out = &created
		return nil
	})
	return out, err
}

// IssueCreditNote approves and posts a note.
//
// Maker/checker: anything that reduces a receivable needs a second person.
func (s *Service) IssueCreditNote(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.CreditNote, error) {
	var out *dbgen.CreditNote

	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		note, err := q.GetCreditNoteByPublicID(ctx, dbgen.GetCreditNoteByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: publicID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Credit note")
		}
		if note.Status != StatusDraft {
			return apierr.Conflict(CodeInvalidState,
				fmt.Sprintf("This note is %s and cannot be issued again.", note.Status))
		}
		if note.CreatedBy != nil && *note.CreatedBy == p.UserID {
			return apierr.Forbidden(
				"A credit or debit note must be issued by someone other than the person who raised it.")
		}

		docType := DocCreditNote
		if note.NoteType == "DEBIT" {
			docType = DocDebitNote
		}
		number, err := s.allocateNumber(ctx, q, p.OrganizationID, "DEFAULT", docType, note.IssueDate)
		if err != nil {
			return err
		}

		// A credit note reverses part of the sale; a debit note adds to it.
		var legs []ledger.Leg
		if note.NoteType == "CREDIT" {
			legs = []ledger.Leg{
				{AccountCode: ledger.AcctDiscountAllowed, Debit: note.SubtotalMinor,
					CustomerID: &note.CustomerID, Memo: "Credit note " + number},
				{AccountCode: ledger.AcctTradeReceivable, Credit: note.TotalMinor,
					PartyType: ledger.PartyCustomer, PartyID: note.CustomerID,
					CustomerID: &note.CustomerID, Memo: "Receivable reduced by " + number},
			}
			if note.TaxMinor > 0 {
				legs = append(legs, ledger.Leg{
					AccountCode: ledger.AcctTaxPayable, Debit: note.TaxMinor,
					CustomerID: &note.CustomerID, Memo: "Output tax reversed"})
			}
		} else {
			legs = []ledger.Leg{
				{AccountCode: ledger.AcctTradeReceivable, Debit: note.TotalMinor,
					PartyType: ledger.PartyCustomer, PartyID: note.CustomerID,
					CustomerID: &note.CustomerID, Memo: "Debit note " + number},
				{AccountCode: ledger.AcctOtherRevenue, Credit: note.SubtotalMinor,
					CustomerID: &note.CustomerID, Memo: "Additional charge " + number},
			}
			if note.TaxMinor > 0 {
				legs = append(legs, ledger.Leg{
					AccountCode: ledger.AcctTaxPayable, Credit: note.TaxMinor,
					CustomerID: &note.CustomerID, Memo: "Output tax on " + number})
			}
		}

		posting, err := s.ledger.Post(ctx, tx, p, ledger.Posting{
			SourceType:     ledger.SourceCreditNote,
			SourceID:       &note.ID,
			SourcePublicID: note.PublicID,
			Purpose:        note.NoteType,
			Description:    note.NoteType + " note " + number,
			Currency:       note.Currency, PostingDate: note.IssueDate,
			Legs: legs,
			Metadata: map[string]any{
				"noteNumber": number, "reasonCode": note.ReasonCode,
			},
		})
		if err != nil {
			return err
		}

		approver := p.UserID
		issued, err := q.IssueCreditNote(ctx, dbgen.IssueCreditNoteParams{
			OrganizationID: p.OrganizationID, ID: note.ID,
			NoteNumber: number, IssuedBy: &approver, ApprovedBy: &approver,
			JournalTransactionID: &posting.Transaction.ID,
		})
		if err != nil {
			if ops.IsNoRows(err) {
				return apierr.Conflict(CodeSelfApproval,
					"This note is no longer a draft, or you raised it yourself.")
			}
			return apierr.Internal(err)
		}

		// Reduce what the invoice still asks for.
		if note.InvoiceID != nil && note.NoteType == "CREDIT" {
			if _, err := q.RecordInvoiceCredit(ctx, dbgen.RecordInvoiceCreditParams{
				OrganizationID: p.OrganizationID, ID: *note.InvoiceID,
				AmountMinor: note.TotalMinor,
			}); err != nil && !ops.IsNoRows(err) {
				return apierr.Internal(err)
			}
		}

		out = &issued
		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "billing.note.issued", ResourceType: "credit_note",
			ResourceID: &note.ID, ResourcePublicID: note.PublicID, Reason: note.Reason,
			Metadata: map[string]any{
				"noteNumber": number, "noteType": note.NoteType,
				"totalMinor": note.TotalMinor, "raisedBy": note.CreatedBy,
				"issuedBy": p.UserID,
			},
		}))
	})
	return out, err
}

// Outstanding is what a customer owes.
type Outstanding struct {
	OutstandingMinor int64  `json:"outstandingMinor"`
	OverdueMinor     int64  `json:"overdueMinor"`
	InvoiceCount     int64  `json:"invoiceCount"`
	Currency         string `json:"currency"`
}

// CustomerOutstanding sums the open invoices for a customer.
func (s *Service) CustomerOutstanding(
	ctx context.Context, p *tenant.Principal, customerPublicID string,
) (*Outstanding, error) {
	customer, err := s.q.GetCustomerByPublicID(ctx, dbgen.GetCustomerByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: customerPublicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Customer")
	}
	row, err := s.q.GetCustomerOutstanding(ctx, dbgen.GetCustomerOutstandingParams{
		OrganizationID: p.OrganizationID, CustomerID: customer.ID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return &Outstanding{
		OutstandingMinor: row.OutstandingMinor,
		OverdueMinor:     row.OverdueMinor,
		InvoiceCount:     row.InvoiceCount,
		Currency:         p.OrganizationCurrency,
	}, nil
}

// GetInvoice returns an invoice with its lines and tax breakdown.
func (s *Service) GetInvoice(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*Result, error) {
	header, err := s.q.GetInvoiceByPublicID(ctx, dbgen.GetInvoiceByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Invoice")
	}
	lines, err := s.q.ListInvoiceLines(ctx, header.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	taxes, err := s.q.ListInvoiceTaxComponents(ctx, &header.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	invoice, err := s.q.GetInvoiceByID(ctx, dbgen.GetInvoiceByIDParams{
		OrganizationID: p.OrganizationID, ID: header.ID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return &Result{Invoice: invoice, Lines: lines, Taxes: taxes}, nil
}

// ListInvoices returns the invoice register.
func (s *Service) ListInvoices(
	ctx context.Context, p *tenant.Principal,
	status string, customerID, cursor *int64, limit int32,
) ([]dbgen.ListInvoicesRow, error) {
	rows, err := s.q.ListInvoices(ctx, dbgen.ListInvoicesParams{
		OrganizationID: p.OrganizationID,
		Status:         ops.Optional(status),
		CustomerID:     customerID,
		CursorID:       cursor,
		Limit:          limit,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

// ListInvoiceCreditNotes lets a checker retrieve drafts raised by another user.
// Resolve both invoice and cursor inside the caller's tenant before listing.
func (s *Service) ListInvoiceCreditNotes(ctx context.Context, p *tenant.Principal, invoiceID, cursor string, limit int32) ([]dbgen.ListCreditNotesRow, string, error) {
	header, err := s.q.GetInvoiceByPublicID(ctx, dbgen.GetInvoiceByPublicIDParams{OrganizationID: p.OrganizationID, PublicID: invoiceID})
	if err != nil {
		return nil, "", ops.NotFoundOr(err, "Invoice")
	}
	var cursorID *int64
	if cursor != "" {
		note, err := s.q.GetCreditNoteByPublicID(ctx, dbgen.GetCreditNoteByPublicIDParams{OrganizationID: p.OrganizationID, PublicID: cursor})
		if err != nil {
			return nil, "", ops.NotFoundOr(err, "Credit note cursor")
		}
		if note.InvoiceID == nil || *note.InvoiceID != header.ID {
			return nil, "", apierr.Validation("The cursor belongs to a different invoice.", nil)
		}
		cursorID = &note.ID
	}
	rows, err := s.q.ListCreditNotes(ctx, dbgen.ListCreditNotesParams{OrganizationID: p.OrganizationID, InvoiceID: &header.ID, CursorID: cursorID, Limit: limit + 1})
	if err != nil {
		return nil, "", apierr.Internal(err)
	}
	return rows, header.InvoiceNumber, nil
}

// allocateNumber produces the next statutory number in a series.
//
// The financial-year scope means a series restarts each year without colliding
// with the previous one, which is what an Indian GST series requires.
func (s *Service) allocateNumber(
	ctx context.Context, q *dbgen.Queries, orgID int64,
	series, docType string, on time.Time,
) (string, error) {
	scope := financialYear(on)
	prefix := map[string]string{
		DocInvoice: "INV", DocCreditNote: "CRN", DocDebitNote: "DBN",
	}[docType]
	if series == "PAY" {
		prefix = "PAY"
	}

	// The row seeded at provisioning carries scope_key '' and acts as the
	// configuration template: it is where an operator sets the prefix and the
	// padding for a series. Allocation happens against the financial-year scope,
	// so a series restarts each year; without reading the template first, that
	// configuration would never take effect.
	padding := int32(6)
	if tmpl, tErr := q.GetInvoiceSequence(ctx, dbgen.GetInvoiceSequenceParams{
		OrganizationID: orgID, SeriesCode: series, DocumentType: docType, ScopeKey: "",
	}); tErr == nil {
		if tmpl.Prefix != "" {
			prefix = tmpl.Prefix
		}
		padding = tmpl.Padding
	} else if !ops.IsNoRows(tErr) {
		return "", apierr.Internal(tErr)
	}

	row, err := q.AllocateInvoiceNumber(ctx, dbgen.AllocateInvoiceNumberParams{
		OrganizationID: orgID, SeriesCode: series, DocumentType: docType,
		Prefix: prefix, ScopeKey: scope,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return "", apierr.Conflict(CodeSequenceExhaust,
				fmt.Sprintf("The %s number series is exhausted for %s.", docType, scope))
		}
		return "", apierr.Internal(err)
	}
	if row.Prefix != "" {
		prefix = row.Prefix
	}
	return fmt.Sprintf("%s/%s/%0*d", prefix, scope, padding, row.CurrentValue), nil
}

// financialYear returns the Indian financial year containing a date, as
// "2026-27". April to March; configurable later if another jurisdiction needs
// a different boundary.
func financialYear(t time.Time) string {
	year := t.Year()
	if t.Month() < time.April {
		year--
	}
	return fmt.Sprintf("%d-%02d", year, (year+1)%100)
}
