package financeapi

import (
	"github.com/ceserve/courier-os/internal/dbgen"
)

// Transport projections expose the documented public identifiers and values,
// never database rows or tenant/audit implementation fields.

type invoiceView struct {
	ID               string  `json:"id"`
	InvoiceNumber    string  `json:"invoiceNumber"`
	SeriesCode       string  `json:"seriesCode"`
	CustomerCode     string  `json:"customerCode"`
	CustomerName     string  `json:"customerName"`
	BillingMode      string  `json:"billingMode"`
	PeriodStart      *string `json:"periodStart"`
	PeriodEnd        *string `json:"periodEnd"`
	IssueDate        string  `json:"issueDate"`
	DueDate          string  `json:"dueDate"`
	Status           string  `json:"status"`
	Currency         string  `json:"currency"`
	SubtotalMinor    int64   `json:"subtotalMinor"`
	DiscountMinor    int64   `json:"discountMinor"`
	TaxableMinor     int64   `json:"taxableMinor"`
	TaxMinor         int64   `json:"taxMinor"`
	RoundingMinor    int64   `json:"roundingMinor"`
	TotalMinor       int64   `json:"totalMinor"`
	PaidMinor        int64   `json:"paidMinor"`
	CreditedMinor    int64   `json:"creditedMinor"`
	ShipmentCount    int32   `json:"shipmentCount"`
	DocumentObjectID *int64  `json:"documentObjectId"`
}

func invoiceDetailResponse(row dbgen.GetInvoiceByPublicIDRow) invoiceView {
	return invoiceView{
		ID:               row.PublicID,
		InvoiceNumber:    row.InvoiceNumber,
		SeriesCode:       row.SeriesCode,
		CustomerCode:     row.CustomerCode,
		CustomerName:     row.CustomerName,
		BillingMode:      row.BillingMode,
		PeriodStart:      dateOnly(row.PeriodStart),
		PeriodEnd:        dateOnly(row.PeriodEnd),
		IssueDate:        row.IssueDate.Format("2006-01-02"),
		DueDate:          row.DueDate.Format("2006-01-02"),
		Status:           row.Status,
		Currency:         row.Currency,
		SubtotalMinor:    row.SubtotalMinor,
		DiscountMinor:    row.DiscountMinor,
		TaxableMinor:     row.TaxableMinor,
		TaxMinor:         row.TaxMinor,
		RoundingMinor:    row.RoundingMinor,
		TotalMinor:       row.TotalMinor,
		PaidMinor:        row.PaidMinor,
		CreditedMinor:    row.CreditedMinor,
		ShipmentCount:    row.ShipmentCount,
		DocumentObjectID: row.DocumentObjectID,
	}
}

func invoiceListResponse(row dbgen.ListInvoicesRow) invoiceView {
	return invoiceView{
		ID:               row.PublicID,
		InvoiceNumber:    row.InvoiceNumber,
		SeriesCode:       row.SeriesCode,
		CustomerCode:     row.CustomerCode,
		CustomerName:     row.CustomerName,
		BillingMode:      row.BillingMode,
		PeriodStart:      dateOnly(row.PeriodStart),
		PeriodEnd:        dateOnly(row.PeriodEnd),
		IssueDate:        row.IssueDate.Format("2006-01-02"),
		DueDate:          row.DueDate.Format("2006-01-02"),
		Status:           row.Status,
		Currency:         row.Currency,
		SubtotalMinor:    row.SubtotalMinor,
		DiscountMinor:    row.DiscountMinor,
		TaxableMinor:     row.TaxableMinor,
		TaxMinor:         row.TaxMinor,
		RoundingMinor:    row.RoundingMinor,
		TotalMinor:       row.TotalMinor,
		PaidMinor:        row.PaidMinor,
		CreditedMinor:    row.CreditedMinor,
		ShipmentCount:    row.ShipmentCount,
		DocumentObjectID: row.DocumentObjectID,
	}
}

type invoiceLineView struct {
	ID               string  `json:"id"`
	LineNo           int32   `json:"lineNo"`
	LineType         string  `json:"lineType"`
	Description      string  `json:"description"`
	HsnSacCode       *string `json:"hsnSacCode"`
	Quantity         int32   `json:"quantity"`
	UnitPriceMinor   int64   `json:"unitPriceMinor"`
	AmountMinor      int64   `json:"amountMinor"`
	DiscountMinor    int64   `json:"discountMinor"`
	TaxableMinor     int64   `json:"taxableMinor"`
	TaxMinor         int64   `json:"taxMinor"`
	TotalMinor       int64   `json:"totalMinor"`
	Currency         string  `json:"currency"`
	Awb              *string `json:"awb"`
	ChargeSnapshotID *int64  `json:"chargeSnapshotId"`
}

func invoiceLineResponse(row dbgen.ListInvoiceLinesRow) invoiceLineView {
	return invoiceLineView{
		ID:               row.PublicID,
		LineNo:           row.LineNo,
		LineType:         row.LineType,
		Description:      row.Description,
		HsnSacCode:       row.HsnSacCode,
		Quantity:         row.Quantity,
		UnitPriceMinor:   row.UnitPriceMinor,
		AmountMinor:      row.AmountMinor,
		DiscountMinor:    row.DiscountMinor,
		TaxableMinor:     row.TaxableMinor,
		TaxMinor:         row.TaxMinor,
		TotalMinor:       row.TotalMinor,
		Currency:         row.Currency,
		Awb:              row.Awb,
		ChargeSnapshotID: row.ChargeSnapshotID,
	}
}

type invoiceTaxView struct {
	ComponentCode string `json:"componentCode"`
	ComponentName string `json:"componentName"`
	RateBp        int32  `json:"rateBp"`
	TaxableMinor  int64  `json:"taxableMinor"`
	TaxMinor      int64  `json:"taxMinor"`
	Currency      string `json:"currency"`
}

func invoiceTaxResponse(row dbgen.ListInvoiceTaxComponentsRow) invoiceTaxView {
	return invoiceTaxView{
		ComponentCode: row.ComponentCode,
		ComponentName: row.ComponentName,
		RateBp:        row.RateBp,
		TaxableMinor:  row.TaxableMinor,
		TaxMinor:      row.TaxMinor,
		Currency:      row.Currency,
	}
}

type creditNoteView struct {
	ID            string  `json:"id"`
	NoteNumber    string  `json:"noteNumber"`
	NoteType      string  `json:"noteType"`
	InvoiceNumber *string `json:"invoiceNumber"`
	CustomerCode  string  `json:"customerCode"`
	ReasonCode    string  `json:"reasonCode"`
	Reason        string  `json:"reason"`
	IssueDate     string  `json:"issueDate"`
	Currency      string  `json:"currency"`
	SubtotalMinor int64   `json:"subtotalMinor"`
	TaxMinor      int64   `json:"taxMinor"`
	TotalMinor    int64   `json:"totalMinor"`
	Status        string  `json:"status"`
	CreatedBy     *int64  `json:"createdBy"`
	ApprovedBy    *int64  `json:"approvedBy"`
}

func creditNoteResponse(row dbgen.GetCreditNoteByPublicIDRow) creditNoteView {
	return creditNoteView{
		ID:            row.PublicID,
		NoteNumber:    row.NoteNumber,
		NoteType:      row.NoteType,
		InvoiceNumber: row.InvoiceNumber,
		CustomerCode:  row.CustomerCode,
		ReasonCode:    row.ReasonCode,
		Reason:        row.Reason,
		IssueDate:     row.IssueDate.Format("2006-01-02"),
		Currency:      row.Currency,
		SubtotalMinor: row.SubtotalMinor,
		TaxMinor:      row.TaxMinor,
		TotalMinor:    row.TotalMinor,
		Status:        row.Status,
		CreatedBy:     row.CreatedBy,
		ApprovedBy:    row.ApprovedBy,
	}
}

type invoicePaymentView struct {
	ID            string  `json:"id"`
	PaymentNumber string  `json:"paymentNumber"`
	AmountMinor   int64   `json:"amountMinor"`
	Currency      string  `json:"currency"`
	PaymentMode   string  `json:"paymentMode"`
	Reference     *string `json:"reference"`
	ReceivedOn    string  `json:"receivedOn"`
	Status        string  `json:"status"`
	RecordedBy    int64   `json:"recordedBy"`
}

func creditNoteListResponse(row dbgen.ListCreditNotesRow, invoiceNumber string) creditNoteView {
	return creditNoteView{ID: row.PublicID, NoteNumber: row.NoteNumber, NoteType: row.NoteType,
		InvoiceNumber: &invoiceNumber, CustomerCode: row.CustomerCode, ReasonCode: row.ReasonCode,
		Reason: row.Reason, IssueDate: row.IssueDate.Format("2006-01-02"), Currency: row.Currency,
		SubtotalMinor: row.SubtotalMinor, TaxMinor: row.TaxMinor, TotalMinor: row.TotalMinor,
		Status: row.Status, CreatedBy: row.CreatedBy, ApprovedBy: row.ApprovedBy}
}

func invoicePaymentResponse(row dbgen.InvoicePayment) invoicePaymentView {
	return invoicePaymentView{
		ID:            row.PublicID,
		PaymentNumber: row.PaymentNumber,
		AmountMinor:   row.AmountMinor,
		Currency:      row.Currency,
		PaymentMode:   row.PaymentMode,
		Reference:     row.Reference,
		ReceivedOn:    row.ReceivedOn.Format("2006-01-02"),
		Status:        row.Status,
		RecordedBy:    row.RecordedBy,
	}
}
