package integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/billing"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ledger"
	"github.com/ceserve/courier-os/internal/tenant"
	"github.com/ceserve/courier-os/tests/harness"
)

func billingServices(env *harness.Env) (*billing.Service, *ledger.Service) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rec := audit.NewRecorder(env.Queries, log)
	led := ledger.NewService(env.DB, env.Queries, rec, log)
	return billing.NewService(env.DB, env.Queries, led, rec, log), led
}

// billableShipment seeds a delivered shipment with an immutable charge
// snapshot, which is what billing reads from.
func billableShipment(
	t *testing.T, env *harness.Env, tn *harness.Tenant,
	customerID int64, freight, surcharge int64, at time.Time,
) int64 {
	t.Helper()
	ctx := context.Background()

	var shipmentID int64
	err := env.DB.Pool.QueryRow(ctx, `
		INSERT INTO shipments (public_id, organization_id, awb, customer_id, booking_unit_id,
			courier_service_id, payment_mode, current_status, movement_direction,
			origin_pincode, destination_pincode, piece_count, actual_weight_grams,
			chargeable_weight_grams, currency, total_amount_minor, content_description,
			origin_branch_id, destination_branch_id, booked_at)
		VALUES (gen_seed_public_id('shp'), $1,
			'BIL' || lpad((nextval(pg_get_serial_sequence('shipments','id')))::text, 12, '0'),
			$2, $3, $4, 'CREDIT', 'DELIVERED', 'FORWARD', '100001', '900001', 1,
			500, 500, 'NGN', $5, 'billing test', $3, $6, $7)
		RETURNING id`,
		tn.OrgID, customerID, tn.OriginBranchID, tn.ServiceID,
		freight+surcharge, tn.DestBranchID, at).Scan(&shipmentID)
	if err != nil {
		t.Fatalf("seed shipment: %v", err)
	}

	// The charge snapshot: what the shipment was actually priced at. Billing
	// must read this rather than re-running the pricing engine.
	if _, err := env.DB.Pool.Exec(ctx, `
		INSERT INTO shipment_charge_snapshots (organization_id, shipment_id,
			rate_card_code, rate_card_version_no, pricing_engine_version, currency,
			chargeable_weight_grams, freight_minor, surcharge_total_minor,
			discount_total_minor, taxable_minor, tax_total_minor, rounding_minor,
			total_minor, line_items, calculation_inputs, calculated_at)
		VALUES ($1, $2, 'TEST-CARD', 1, 'test', 'NGN', 500, $3, $4, 0, $5, 0, 0, $5,
			'[]'::jsonb, '{}'::jsonb, now())`,
		tn.OrgID, shipmentID, freight, surcharge, freight+surcharge); err != nil {
		t.Fatalf("seed charge snapshot: %v", err)
	}
	return shipmentID
}

func firstCustomer(t *testing.T, env *harness.Env, tn *harness.Tenant) (int64, string) {
	t.Helper()
	var id int64
	var pubID string
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT id, public_id FROM customers WHERE organization_id=$1 ORDER BY id LIMIT 1`,
		tn.OrgID).Scan(&id, &pubID); err != nil {
		t.Fatalf("find customer: %v", err)
	}
	return id, pubID
}

// TestInvoiceLinesAreSnapshotsOfThePricedCharge is the §19 rule: an issued
// invoice does not change when a rate card does.
func TestInvoiceLinesAreSnapshotsOfThePricedCharge(t *testing.T) {
	env, tn, p := financeEnv(t)
	svc, led := billingServices(env)
	ctx := context.Background()

	customerID, customerPubID := firstCustomer(t, env, tn)
	start, end := monthWindow()
	billableShipment(t, env, tn, customerID, 50000, 7500, start)
	billableShipment(t, env, tn, customerID, 30000, 4500, start.AddDate(0, 0, 1))

	// 18% IGST expressed as configuration, not as code.
	draft, err := svc.Draft(ctx, p, billing.DraftRequest{
		CustomerID: customerPubID, PeriodStart: start, PeriodEnd: end,
		Taxes: []billing.TaxComponentSpec{
			{Code: "IGST", Name: "Integrated GST", RateBp: 1800,
				Metadata: map[string]any{"placeOfSupply": "07"}},
		},
	})
	if err != nil {
		t.Fatalf("draft: %v", err)
	}

	// 50000+7500 + 30000+4500 = 92000 taxable; 18% = 16560; total 108560.
	if draft.Invoice.TaxableMinor != 92000 {
		t.Fatalf("taxable = %d, want 92000", draft.Invoice.TaxableMinor)
	}
	if draft.Invoice.TaxMinor != 16560 {
		t.Fatalf("tax = %d, want 16560 (18%% of 92000)", draft.Invoice.TaxMinor)
	}
	if draft.Invoice.TotalMinor != 108560 {
		t.Fatalf("total = %d, want 108560", draft.Invoice.TotalMinor)
	}
	if len(draft.Taxes) != 1 || draft.Taxes[0].ComponentCode != "IGST" {
		t.Fatalf("tax components = %+v, want one IGST row", draft.Taxes)
	}

	// Every shipment line points at the snapshot it copied from.
	snapshotLines := 0
	for _, l := range draft.Lines {
		if l.ShipmentID != nil {
			if l.ChargeSnapshotID == nil {
				t.Fatalf("line %d bills a shipment but records no charge snapshot", l.LineNo)
			}
			snapshotLines++
		}
	}
	if snapshotLines != 2 {
		t.Fatalf("%d shipment lines, want 2", snapshotLines)
	}

	issued, err := svc.Issue(ctx, p, draft.Invoice.PublicID)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !strings.HasPrefix(issued.InvoiceNumber, "INV/") {
		t.Fatalf("invoice number %q does not look like a statutory series", issued.InvoiceNumber)
	}

	// The customer owes the total; revenue and tax are recognised.
	assertPartyBalance(t, led, p, ledger.PartyCustomer, customerID, 108560,
		"the customer should owe the invoice total")
	assertAccount(t, led, p, ledger.AcctFreightRevenue, 92000,
		"revenue should be recognised net of tax")
	assertAccount(t, led, p, ledger.AcctTaxPayable, 16560,
		"output tax should be owed to the authority")

	// The chain that makes an issued invoice stable is invoice line → charge
	// snapshot, and both ends are frozen.
	//
	// The rate card itself cannot even be edited while active — Release 1's
	// rate_card_versions guard refuses it, so rates change by superseding a
	// version rather than by mutation:
	if _, err := env.DB.Pool.Exec(ctx,
		`UPDATE zone_rates SET base_price_minor = base_price_minor * 10 WHERE organization_id = $1`,
		tn.OrgID); err == nil {
		t.Fatal("an active rate card version was edited in place")
	}

	// And the snapshot the invoice copied from is append-only, so even the
	// source of the number cannot be rewritten after the fact:
	if _, err := env.DB.Pool.Exec(ctx,
		`UPDATE shipment_charge_snapshots SET freight_minor = 1 WHERE organization_id = $1`,
		tn.OrgID); err == nil {
		t.Fatal("a charge snapshot was edited after the shipment was priced")
	}

	after, err := svc.GetInvoice(ctx, p, draft.Invoice.PublicID)
	if err != nil {
		t.Fatalf("re-read invoice: %v", err)
	}
	if after.Invoice.TotalMinor != 108560 {
		t.Fatalf("the invoice total moved to %d; it must stay at 108560", after.Invoice.TotalMinor)
	}

	if err := led.AssertBalanced(ctx, p); err != nil {
		t.Fatalf("ledger unbalanced after invoicing: %v", err)
	}
}

// TestInvoiceNumberingIsConcurrencySafe is the test the specification calls for
// by name. Gaps and duplicates are both unacceptable to a tax authority.
func TestInvoiceNumberingIsConcurrencySafe(t *testing.T) {
	env, tn, p := financeEnv(t)
	svc, _ := billingServices(env)
	ctx := context.Background()

	customerID, customerPubID := firstCustomer(t, env, tn)
	start, end := monthWindow()

	// One draft per shipment, so each can be issued independently.
	const invoices = 12
	drafts := make([]string, 0, invoices)
	for i := 0; i < invoices; i++ {
		billableShipment(t, env, tn, customerID, 10000, 0, start.AddDate(0, 0, i%20))
		d, err := svc.Draft(ctx, p, billing.DraftRequest{
			CustomerID: customerPubID, PeriodStart: start, PeriodEnd: end,
		})
		if err != nil {
			t.Fatalf("draft %d: %v", i, err)
		}
		drafts = append(drafts, d.Invoice.PublicID)
	}

	// Issue them all at once.
	var wg sync.WaitGroup
	wg.Add(len(drafts))
	errs := make([]error, len(drafts))
	for i, id := range drafts {
		go func(idx int, publicID string) {
			defer wg.Done()
			_, errs[idx] = svc.Issue(ctx, p, publicID)
		}(i, id)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent issue %d failed: %v", i, err)
		}
	}

	// Distinct numbers, and no gaps in the series.
	var issued, distinct int64
	mustQueryRow(t, env, `
		SELECT count(*), count(DISTINCT invoice_number) FROM invoices
		WHERE organization_id=$1 AND status <> 'DRAFT'`,
		[]any{p.OrganizationID}, &issued, &distinct)
	if issued != int64(len(drafts)) {
		t.Fatalf("%d invoices issued, want %d", issued, len(drafts))
	}
	if distinct != issued {
		t.Fatalf("%d invoices share only %d distinct numbers — the series has duplicates",
			issued, distinct)
	}

	// The year-scoped row is the live counter; the scope_key='' row is the
	// configuration template seeded at provisioning.
	var maxSeq int64
	mustQueryRow(t, env, `
		SELECT current_value FROM invoice_sequences
		WHERE organization_id=$1 AND document_type='INVOICE' AND series_code='DEFAULT'
		  AND scope_key <> ''`,
		[]any{p.OrganizationID}, &maxSeq)
	if maxSeq != issued {
		t.Fatalf("the sequence reached %d for %d invoices — the series has a gap", maxSeq, issued)
	}
}

// TestShipmentIsBilledOnlyOnce proves a re-run cannot invoice the same parcel
// twice.
func TestShipmentIsBilledOnlyOnce(t *testing.T) {
	env, tn, p := financeEnv(t)
	svc, _ := billingServices(env)
	ctx := context.Background()

	customerID, customerPubID := firstCustomer(t, env, tn)
	start, end := monthWindow()
	billableShipment(t, env, tn, customerID, 25000, 0, start)

	first, err := svc.Draft(ctx, p, billing.DraftRequest{
		CustomerID: customerPubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("first draft: %v", err)
	}
	if first.Invoice.ShipmentCount != 1 {
		t.Fatalf("first invoice covers %d shipments, want 1", first.Invoice.ShipmentCount)
	}

	// A second run over the same period finds nothing left to bill.
	_, err = svc.Draft(ctx, p, billing.DraftRequest{
		CustomerID: customerPubID, PeriodStart: start, PeriodEnd: end,
	})
	if err == nil {
		t.Fatal("a second billing run invoiced the same shipment again")
	}
	if !containsAny(err.Error(), "no unbilled shipments", "BILLING_NOTHING_TO_BILL") {
		t.Fatalf("expected a nothing-to-bill refusal, got: %v", err)
	}
}

// TestInvoicePaymentAndCreditNote walks issue → part payment → credit note and
// checks the receivable at each step.
func TestInvoicePaymentAndCreditNote(t *testing.T) {
	env, tn, p := financeEnv(t)
	svc, led := billingServices(env)
	ctx := context.Background()

	customerID, customerPubID := firstCustomer(t, env, tn)
	start, end := monthWindow()
	billableShipment(t, env, tn, customerID, 100000, 0, start)

	draft, err := svc.Draft(ctx, p, billing.DraftRequest{
		CustomerID: customerPubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if _, err := svc.Issue(ctx, p, draft.Invoice.PublicID); err != nil {
		t.Fatalf("issue: %v", err)
	}
	assertPartyBalance(t, led, p, ledger.PartyCustomer, customerID, 100000,
		"the customer should owe the invoice")

	// Part payment.
	_, updated, err := svc.RecordPayment(ctx, p, billing.PayRequest{
		InvoiceID: draft.Invoice.PublicID, AmountMinor: 60000,
		PaymentMode: "BANK_TRANSFER", Reference: "NEFT-A1",
	})
	if err != nil {
		t.Fatalf("payment: %v", err)
	}
	if updated.Status != billing.StatusPartiallyPaid {
		t.Fatalf("status = %s, want PARTIALLY_PAID", updated.Status)
	}
	assertPartyBalance(t, led, p, ledger.PartyCustomer, customerID, 40000,
		"the customer should owe the unpaid balance")

	// The same gateway reference replayed must not bank twice.
	_, again, err := svc.RecordPayment(ctx, p, billing.PayRequest{
		InvoiceID: draft.Invoice.PublicID, AmountMinor: 60000,
		PaymentMode: "BANK_TRANSFER", Reference: "NEFT-A1",
	})
	if err != nil {
		t.Fatalf("replayed payment: %v", err)
	}
	if again.PaidMinor != 60000 {
		t.Fatalf("a replayed payment banked twice: paid = %d, want 60000", again.PaidMinor)
	}

	// Credit the rest away — and prove maker/checker.
	note, err := svc.RaiseCreditNote(ctx, p, billing.CreditNoteRequest{
		InvoiceID: draft.Invoice.PublicID, NoteType: "CREDIT",
		ReasonCode: "SERVICE_FAILURE", Reason: "shipment delayed beyond the committed SLA",
		AmountMinor: 40000,
	})
	if err != nil {
		t.Fatalf("raise credit note: %v", err)
	}
	if _, err := svc.IssueCreditNote(ctx, p, note.PublicID); err == nil {
		t.Fatal("a user issued their own credit note")
	}

	checkerID, _, _ := env.NewUser(t, tn.OrgID,
		strings.ToLower("crnchk-"+harness.RandomKey()[:8])+"@test.local", "FINANCE_MANAGER", nil)
	checker := &tenant.Principal{UserID: checkerID, OrganizationID: tn.OrgID, OrganizationCurrency: "NGN"}
	issuedNote, err := svc.IssueCreditNote(ctx, checker, note.PublicID)
	if err != nil {
		t.Fatalf("issue credit note: %v", err)
	}
	if !strings.HasPrefix(issuedNote.NoteNumber, "CRN/") {
		t.Fatalf("note number %q does not look like a statutory series", issuedNote.NoteNumber)
	}

	// The receivable is cleared and the invoice is settled.
	assertPartyBalance(t, led, p, ledger.PartyCustomer, customerID, 0,
		"the credit note should clear the remaining receivable")

	final, err := svc.GetInvoice(ctx, p, draft.Invoice.PublicID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if final.Invoice.Status != billing.StatusPaid {
		t.Fatalf("invoice status = %s, want PAID once paid and credited", final.Invoice.Status)
	}

	if err := led.AssertBalanced(ctx, p); err != nil {
		t.Fatalf("ledger unbalanced after payment and credit: %v", err)
	}
}

// TestIssuedInvoiceIsImmutable proves the statutory document cannot be edited.
func TestIssuedInvoiceIsImmutable(t *testing.T) {
	env, tn, p := financeEnv(t)
	svc, _ := billingServices(env)
	ctx := context.Background()

	customerID, customerPubID := firstCustomer(t, env, tn)
	start, end := monthWindow()
	billableShipment(t, env, tn, customerID, 50000, 0, start)

	draft, err := svc.Draft(ctx, p, billing.DraftRequest{
		CustomerID: customerPubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	issued, err := svc.Issue(ctx, p, draft.Invoice.PublicID)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"edit the total", `UPDATE invoices SET total_minor = 1 WHERE id = $1`},
		{"edit the number", `UPDATE invoices SET invoice_number = 'FAKE/1' WHERE id = $1`},
		{"edit the issue date", `UPDATE invoices SET issue_date = current_date - 30 WHERE id = $1`},
		{"delete the invoice", `DELETE FROM invoices WHERE id = $1`},
		{"add a line", `INSERT INTO invoice_lines (public_id, organization_id, invoice_id,
			line_no, line_type, description, amount_minor, total_minor, currency)
			SELECT gen_seed_public_id('ivl'), organization_id, id, 99, 'OTHER', 'sneaky',
			1000, 1000, 'NGN' FROM invoices WHERE id = $1`},
		{"delete the lines", `DELETE FROM invoice_lines WHERE invoice_id = $1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := env.DB.Pool.Exec(ctx, tc.sql, issued.ID); err == nil {
				t.Fatalf("%s succeeded on an issued invoice", tc.name)
			}
		})
	}
}

// TestSummaryInvoiceKeepsShipmentDetail proves a rolled-up invoice still
// records every AWB, so the customer can be given the breakdown.
func TestSummaryInvoiceKeepsShipmentDetail(t *testing.T) {
	env, tn, p := financeEnv(t)
	svc, _ := billingServices(env)
	ctx := context.Background()

	customerID, customerPubID := firstCustomer(t, env, tn)
	start, end := monthWindow()
	for i := 0; i < 5; i++ {
		billableShipment(t, env, tn, customerID, 10000, 1000, start.AddDate(0, 0, i))
	}

	draft, err := svc.Draft(ctx, p, billing.DraftRequest{
		CustomerID: customerPubID, PeriodStart: start, PeriodEnd: end, Summary: true,
	})
	if err != nil {
		t.Fatalf("draft: %v", err)
	}

	if len(draft.Lines) != 1 {
		t.Fatalf("a summary invoice has %d lines, want 1", len(draft.Lines))
	}
	if draft.Invoice.ShipmentCount != 5 {
		t.Fatalf("shipment count = %d, want 5", draft.Invoice.ShipmentCount)
	}
	if draft.Invoice.TotalMinor != 55000 {
		t.Fatalf("total = %d, want 55000", draft.Invoice.TotalMinor)
	}

	// Every AWB is still recorded.
	var recorded int64
	mustQueryRow(t, env, `SELECT count(*) FROM invoice_shipments WHERE invoice_id=$1`,
		[]any{draft.Invoice.ID}, &recorded)
	if recorded != 5 {
		t.Fatalf("%d shipments recorded against a summary invoice, want 5", recorded)
	}
}

// TestIndianGSTSplitIsConfiguration proves the tax engine is generic: an
// intra-state CGST/SGST split is two configured rows, not a code path.
func TestIndianGSTSplitIsConfiguration(t *testing.T) {
	env, tn, p := financeEnv(t)
	svc, led := billingServices(env)
	ctx := context.Background()

	customerID, customerPubID := firstCustomer(t, env, tn)
	start, end := monthWindow()
	billableShipment(t, env, tn, customerID, 100000, 0, start)

	draft, err := svc.Draft(ctx, p, billing.DraftRequest{
		CustomerID: customerPubID, PeriodStart: start, PeriodEnd: end,
		Taxes: []billing.TaxComponentSpec{
			{Code: "CGST", Name: "Central GST", RateBp: 900,
				Metadata: map[string]any{"placeOfSupply": "29"}},
			{Code: "SGST", Name: "State GST", RateBp: 900,
				Metadata: map[string]any{"placeOfSupply": "29"}},
		},
	})
	if err != nil {
		t.Fatalf("draft: %v", err)
	}

	if len(draft.Taxes) != 2 {
		t.Fatalf("%d tax components, want CGST and SGST", len(draft.Taxes))
	}
	for _, tc := range draft.Taxes {
		if tc.TaxMinor != 9000 {
			t.Fatalf("%s = %d, want 9000 (9%% of 100000)", tc.ComponentCode, tc.TaxMinor)
		}
	}
	if draft.Invoice.TaxMinor != 18000 {
		t.Fatalf("total tax = %d, want 18000", draft.Invoice.TaxMinor)
	}

	if _, err := svc.Issue(ctx, p, draft.Invoice.PublicID); err != nil {
		t.Fatalf("issue: %v", err)
	}
	assertAccount(t, led, p, ledger.AcctTaxPayable, 18000,
		"both GST components should land in output tax")
}

// TestBillingIsTenantIsolated proves one tenant cannot bill another's customer.
func TestBillingIsTenantIsolated(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	a := env.NewTenant(t, geo, harness.TenantOptions{})
	b := env.NewTenant(t, geo, harness.TenantOptions{})

	pa := &tenant.Principal{UserID: a.AdminUserID, OrganizationID: a.OrgID, OrganizationCurrency: "NGN"}
	pb := &tenant.Principal{UserID: b.AdminUserID, OrganizationID: b.OrgID, OrganizationCurrency: "NGN"}
	svc, _ := billingServices(env)
	ctx := context.Background()

	customerID, customerPubID := firstCustomer(t, env, a)
	start, end := monthWindow()
	billableShipment(t, env, a, customerID, 50000, 0, start)

	// B cannot draft against A's customer.
	if _, err := svc.Draft(ctx, pb, billing.DraftRequest{
		CustomerID: customerPubID, PeriodStart: start, PeriodEnd: end,
	}); err == nil {
		t.Fatal("tenant B drafted an invoice for tenant A's customer")
	}

	draft, err := svc.Draft(ctx, pa, billing.DraftRequest{
		CustomerID: customerPubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("tenant A draft: %v", err)
	}
	// B cannot issue or read it.
	if _, err := svc.Issue(ctx, pb, draft.Invoice.PublicID); err == nil {
		t.Fatal("tenant B issued tenant A's invoice")
	}
	if _, err := svc.GetInvoice(ctx, pb, draft.Invoice.PublicID); err == nil {
		t.Fatal("tenant B read tenant A's invoice")
	}
}

var _ = fmt.Sprintf
var _ dbgen.Invoice
