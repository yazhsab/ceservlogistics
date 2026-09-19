package financeapi

import (
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
)

// Transport projections expose the documented public identifiers and values,
// never database rows or tenant/audit implementation fields.

type settlementView struct {
	CollectionsMinor    int64   `json:"collectionsMinor"`
	ID                  string  `json:"id"`
	SettlementNumber    string  `json:"settlementNumber"`
	FranchiseCode       string  `json:"franchiseCode"`
	FranchiseName       string  `json:"franchiseName"`
	PeriodType          string  `json:"periodType"`
	PeriodStart         string  `json:"periodStart"`
	PeriodEnd           string  `json:"periodEnd"`
	Status              string  `json:"status"`
	Currency            string  `json:"currency"`
	CommissionMinor     int64   `json:"commissionMinor"`
	IncentiveMinor      int64   `json:"incentiveMinor"`
	CodLiabilityMinor   int64   `json:"codLiabilityMinor"`
	ChargesMinor        int64   `json:"chargesMinor"`
	PenaltiesMinor      int64   `json:"penaltiesMinor"`
	AdjustmentsMinor    int64   `json:"adjustmentsMinor"`
	TaxMinor            int64   `json:"taxMinor"`
	WithholdingMinor    int64   `json:"withholdingMinor"`
	OpeningBalanceMinor int64   `json:"openingBalanceMinor"`
	NetAmountMinor      int64   `json:"netAmountMinor"`
	PaidMinor           int64   `json:"paidMinor"`
	CalculationHash     *string `json:"calculationHash"`
	CalculatedBy        *int64  `json:"calculatedBy"`
	ApprovedBy          *int64  `json:"approvedBy"`
}

func settlementDetailResponse(row dbgen.GetSettlementByPublicIDRow) settlementView {
	return settlementView{
		CollectionsMinor:    row.CollectionsMinor,
		ID:                  row.PublicID,
		SettlementNumber:    row.SettlementNumber,
		FranchiseCode:       row.FranchiseCode,
		FranchiseName:       row.FranchiseName,
		PeriodType:          row.PeriodType,
		PeriodStart:         row.PeriodStart.Format("2006-01-02"),
		PeriodEnd:           row.PeriodEnd.Format("2006-01-02"),
		Status:              row.Status,
		Currency:            row.Currency,
		CommissionMinor:     row.CommissionMinor,
		IncentiveMinor:      row.IncentiveMinor,
		CodLiabilityMinor:   row.CodLiabilityMinor,
		ChargesMinor:        row.ChargesMinor,
		PenaltiesMinor:      row.PenaltiesMinor,
		AdjustmentsMinor:    row.AdjustmentsMinor,
		TaxMinor:            row.TaxMinor,
		WithholdingMinor:    row.WithholdingMinor,
		OpeningBalanceMinor: row.OpeningBalanceMinor,
		NetAmountMinor:      row.NetAmountMinor,
		PaidMinor:           row.PaidMinor,
		CalculationHash:     row.CalculationHash,
		CalculatedBy:        row.CalculatedBy,
		ApprovedBy:          row.ApprovedBy,
	}
}

func settlementListResponse(row dbgen.ListSettlementsRow) settlementView {
	return settlementView{
		CollectionsMinor:    row.CollectionsMinor,
		ID:                  row.PublicID,
		SettlementNumber:    row.SettlementNumber,
		FranchiseCode:       row.FranchiseCode,
		FranchiseName:       row.FranchiseName,
		PeriodType:          row.PeriodType,
		PeriodStart:         row.PeriodStart.Format("2006-01-02"),
		PeriodEnd:           row.PeriodEnd.Format("2006-01-02"),
		Status:              row.Status,
		Currency:            row.Currency,
		CommissionMinor:     row.CommissionMinor,
		IncentiveMinor:      row.IncentiveMinor,
		CodLiabilityMinor:   row.CodLiabilityMinor,
		ChargesMinor:        row.ChargesMinor,
		PenaltiesMinor:      row.PenaltiesMinor,
		AdjustmentsMinor:    row.AdjustmentsMinor,
		TaxMinor:            row.TaxMinor,
		WithholdingMinor:    row.WithholdingMinor,
		OpeningBalanceMinor: row.OpeningBalanceMinor,
		NetAmountMinor:      row.NetAmountMinor,
		PaidMinor:           row.PaidMinor,
		CalculationHash:     row.CalculationHash,
		CalculatedBy:        row.CalculatedBy,
		ApprovedBy:          row.ApprovedBy,
	}
}

type settlementLineView struct {
	ID             string  `json:"id"`
	LineNo         int32   `json:"lineNo"`
	Category       string  `json:"category"`
	Description    string  `json:"description"`
	AmountMinor    int64   `json:"amountMinor"`
	Currency       string  `json:"currency"`
	Quantity       int32   `json:"quantity"`
	SourceType     string  `json:"sourceType"`
	SourceID       *int64  `json:"sourceId"`
	SourcePublicID *string `json:"sourcePublicId"`
	Awb            *string `json:"awb"`
}

func settlementLineResponse(row dbgen.ListSettlementLinesRow) settlementLineView {
	return settlementLineView{
		ID:             row.PublicID,
		LineNo:         row.LineNo,
		Category:       row.Category,
		Description:    row.Description,
		AmountMinor:    row.AmountMinor,
		Currency:       row.Currency,
		Quantity:       row.Quantity,
		SourceType:     row.SourceType,
		SourceID:       row.SourceID,
		SourcePublicID: row.SourcePublicID,
		Awb:            row.Awb,
	}
}

type settlementPaymentView struct {
	ID            string     `json:"id"`
	PaymentNumber string     `json:"paymentNumber"`
	Direction     string     `json:"direction"`
	AmountMinor   int64      `json:"amountMinor"`
	Currency      string     `json:"currency"`
	PaymentMode   string     `json:"paymentMode"`
	Reference     *string    `json:"reference"`
	PaidOn        string     `json:"paidOn"`
	Status        string     `json:"status"`
	RecordedBy    int64      `json:"recordedBy"`
	ConfirmedBy   *int64     `json:"confirmedBy"`
	ConfirmedAt   *time.Time `json:"confirmedAt"`
	FailureReason *string    `json:"failureReason"`
}

func settlementPaymentResponse(row dbgen.SettlementPayment) settlementPaymentView {
	return settlementPaymentView{
		ID:            row.PublicID,
		PaymentNumber: row.PaymentNumber,
		Direction:     row.Direction,
		AmountMinor:   row.AmountMinor,
		Currency:      row.Currency,
		PaymentMode:   row.PaymentMode,
		Reference:     row.Reference,
		PaidOn:        row.PaidOn.Format("2006-01-02"),
		Status:        row.Status,
		RecordedBy:    row.RecordedBy,
		ConfirmedBy:   row.ConfirmedBy,
		ConfirmedAt:   row.ConfirmedAt,
		FailureReason: row.FailureReason,
	}
}

type settlementApprovalView struct {
	ID             string    `json:"id"`
	Action         string    `json:"action"`
	NetAmountMinor int64     `json:"netAmountMinor"`
	Currency       string    `json:"currency"`
	Comment        *string   `json:"comment"`
	ActorID        int64     `json:"actorId"`
	ActorName      *string   `json:"actorName"`
	ActedAt        time.Time `json:"actedAt"`
}

func settlementApprovalResponse(row dbgen.ListSettlementApprovalsRow) settlementApprovalView {
	return settlementApprovalView{
		ID:             row.PublicID,
		Action:         row.Action,
		NetAmountMinor: row.NetAmountMinor,
		Currency:       row.Currency,
		Comment:        row.Comment,
		ActorID:        row.ActorID,
		ActorName:      row.ActorName,
		ActedAt:        row.ActedAt,
	}
}

type settlementCategoryView struct {
	Category    string `json:"category"`
	AmountMinor int64  `json:"amountMinor"`
	LineCount   int64  `json:"lineCount"`
}

func settlementCategoryResponse(row dbgen.SumSettlementLinesByCategoryRow) settlementCategoryView {
	return settlementCategoryView{
		Category:    row.Category,
		AmountMinor: row.AmountMinor,
		LineCount:   row.LineCount,
	}
}

func settlementPaymentListResponse(row dbgen.ListSettlementPaymentsRow) settlementPaymentView {
	return settlementPaymentView{
		ID:            row.PublicID,
		PaymentNumber: row.PaymentNumber,
		Direction:     row.Direction,
		AmountMinor:   row.AmountMinor,
		Currency:      row.Currency,
		PaymentMode:   row.PaymentMode,
		Reference:     row.Reference,
		PaidOn:        row.PaidOn.Format("2006-01-02"),
		Status:        row.Status,
		RecordedBy:    row.RecordedBy,
		ConfirmedBy:   row.ConfirmedBy,
		ConfirmedAt:   row.ConfirmedAt,
		FailureReason: row.FailureReason,
	}
}
