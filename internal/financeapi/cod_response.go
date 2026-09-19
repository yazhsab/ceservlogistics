package financeapi

import (
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
)

// Transport projections expose the documented public identifiers and values,
// never database rows or tenant/audit implementation fields.

type obligationView struct {
	ID             string     `json:"id"`
	Awb            string     `json:"awb"`
	ExpectedMinor  int64      `json:"expectedMinor"`
	CollectedMinor int64      `json:"collectedMinor"`
	RemittedMinor  int64      `json:"remittedMinor"`
	AdjustedMinor  int64      `json:"adjustedMinor"`
	Currency       string     `json:"currency"`
	Status         string     `json:"status"`
	CustodianType  *string    `json:"custodianType"`
	CustodianID    *int64     `json:"custodianId"`
	CollectedAt    *time.Time `json:"collectedAt"`
	RemittedAt     *time.Time `json:"remittedAt"`
}

func obligationResponse(row dbgen.CodObligation) obligationView {
	return obligationView{
		ID:             row.PublicID,
		Awb:            row.Awb,
		ExpectedMinor:  row.ExpectedMinor,
		CollectedMinor: row.CollectedMinor,
		RemittedMinor:  row.RemittedMinor,
		AdjustedMinor:  row.AdjustedMinor,
		Currency:       row.Currency,
		Status:         row.Status,
		CustodianType:  row.CustodianType,
		CustodianID:    row.CustodianID,
		CollectedAt:    row.CollectedAt,
		RemittedAt:     row.RemittedAt,
	}
}

func obligationDetailResponse(row dbgen.GetObligationByPublicIDRow) obligationView {
	return obligationView{
		ID:             row.PublicID,
		Awb:            row.Awb,
		ExpectedMinor:  row.ExpectedMinor,
		CollectedMinor: row.CollectedMinor,
		RemittedMinor:  row.RemittedMinor,
		AdjustedMinor:  row.AdjustedMinor,
		Currency:       row.Currency,
		Status:         row.Status,
		CustodianType:  row.CustodianType,
		CustodianID:    row.CustodianID,
		CollectedAt:    row.CollectedAt,
		RemittedAt:     row.RemittedAt,
	}
}

func obligationListResponse(row dbgen.ListCODObligationsRow) obligationView {
	return obligationView{
		ID:             row.PublicID,
		Awb:            row.Awb,
		ExpectedMinor:  row.ExpectedMinor,
		CollectedMinor: row.CollectedMinor,
		RemittedMinor:  row.RemittedMinor,
		AdjustedMinor:  row.AdjustedMinor,
		Currency:       row.Currency,
		Status:         row.Status,
		CustodianType:  row.CustodianType,
		CustodianID:    row.CustodianID,
		CollectedAt:    row.CollectedAt,
		RemittedAt:     row.RemittedAt,
	}
}

func obligationExpectedResponse(row dbgen.ListObligationsForPartyRow) obligationView {
	return obligationView{
		ID:             row.PublicID,
		Awb:            row.Awb,
		ExpectedMinor:  row.ExpectedMinor,
		CollectedMinor: row.CollectedMinor,
		RemittedMinor:  row.RemittedMinor,
		AdjustedMinor:  row.AdjustedMinor,
		Currency:       row.Currency,
		Status:         row.Status,
		CustodianType:  row.CustodianType,
		CustodianID:    row.CustodianID,
		CollectedAt:    row.CollectedAt,
		RemittedAt:     row.RemittedAt,
	}
}

type codCollectionView struct {
	ID          string    `json:"id"`
	AmountMinor int64     `json:"amountMinor"`
	Currency    string    `json:"currency"`
	PaymentMode string    `json:"paymentMode"`
	Reference   *string   `json:"reference"`
	CollectedAt time.Time `json:"collectedAt"`
}

func codCollectionResponse(row dbgen.CodCollection) codCollectionView {
	return codCollectionView{
		ID:          row.PublicID,
		AmountMinor: row.AmountMinor,
		Currency:    row.Currency,
		PaymentMode: row.PaymentMode,
		Reference:   row.Reference,
		CollectedAt: row.CollectedAt,
	}
}

type codTransferView struct {
	ID             string  `json:"id"`
	TransferCode   string  `json:"transferCode"`
	FromType       string  `json:"fromType"`
	FromID         int64   `json:"fromId"`
	ToType         string  `json:"toType"`
	ToID           int64   `json:"toId"`
	DeclaredMinor  int64   `json:"declaredMinor"`
	AcceptedMinor  *int64  `json:"acceptedMinor"`
	VarianceMinor  int64   `json:"varianceMinor"`
	VarianceReason *string `json:"varianceReason"`
	ShipmentCount  int32   `json:"shipmentCount"`
	Status         string  `json:"status"`
	Currency       string  `json:"currency"`
}

func codTransferResponse(row dbgen.CodCustodyTransfer) codTransferView {
	return codTransferView{
		ID:             row.PublicID,
		TransferCode:   row.TransferCode,
		FromType:       row.FromType,
		FromID:         row.FromID,
		ToType:         row.ToType,
		ToID:           row.ToID,
		DeclaredMinor:  row.DeclaredMinor,
		AcceptedMinor:  row.AcceptedMinor,
		VarianceMinor:  row.VarianceMinor,
		VarianceReason: row.VarianceReason,
		ShipmentCount:  row.ShipmentCount,
		Status:         row.Status,
		Currency:       row.Currency,
	}
}

type codReconciliationView struct {
	ID                 string  `json:"id"`
	ReconciliationCode string  `json:"reconciliationCode"`
	PartyType          string  `json:"partyType"`
	PartyID            int64   `json:"partyId"`
	PeriodStart        string  `json:"periodStart"`
	PeriodEnd          string  `json:"periodEnd"`
	ExpectedMinor      int64   `json:"expectedMinor"`
	CountedMinor       int64   `json:"countedMinor"`
	ShortageMinor      int64   `json:"shortageMinor"`
	ExcessMinor        int64   `json:"excessMinor"`
	Status             string  `json:"status"`
	VarianceReason     *string `json:"varianceReason"`
	Currency           string  `json:"currency"`
}

func codReconciliationResponse(row dbgen.CodReconciliation) codReconciliationView {
	return codReconciliationView{
		ID:                 row.PublicID,
		ReconciliationCode: row.ReconciliationCode,
		PartyType:          row.PartyType,
		PartyID:            row.PartyID,
		PeriodStart:        row.PeriodStart.Format("2006-01-02"),
		PeriodEnd:          row.PeriodEnd.Format("2006-01-02"),
		ExpectedMinor:      row.ExpectedMinor,
		CountedMinor:       row.CountedMinor,
		ShortageMinor:      row.ShortageMinor,
		ExcessMinor:        row.ExcessMinor,
		Status:             row.Status,
		VarianceReason:     row.VarianceReason,
		Currency:           row.Currency,
	}
}

type codCountView struct {
	ObligationID  int64   `json:"obligationId"`
	ExpectedMinor int64   `json:"expectedMinor"`
	CountedMinor  int64   `json:"countedMinor"`
	Outcome       string  `json:"outcome"`
	Notes         *string `json:"notes"`
}

func codCountResponse(row dbgen.CodReconciliationItem) codCountView {
	return codCountView{
		ObligationID:  row.ObligationID,
		ExpectedMinor: row.ExpectedMinor,
		CountedMinor:  row.CountedMinor,
		Outcome:       row.Outcome,
		Notes:         row.Notes,
	}
}

type codRemittanceView struct {
	ID              string  `json:"id"`
	RemittanceCode  string  `json:"remittanceCode"`
	FromType        string  `json:"fromType"`
	FromID          int64   `json:"fromId"`
	BeneficiaryType string  `json:"beneficiaryType"`
	AmountMinor     int64   `json:"amountMinor"`
	Currency        string  `json:"currency"`
	PaymentMode     string  `json:"paymentMode"`
	Reference       *string `json:"reference"`
	PaidOn          string  `json:"paidOn"`
	Status          string  `json:"status"`
}

func codRemittanceResponse(row dbgen.CodRemittance) codRemittanceView {
	return codRemittanceView{
		ID:              row.PublicID,
		RemittanceCode:  row.RemittanceCode,
		FromType:        row.FromType,
		FromID:          row.FromID,
		BeneficiaryType: row.BeneficiaryType,
		AmountMinor:     row.AmountMinor,
		Currency:        row.Currency,
		PaymentMode:     row.PaymentMode,
		Reference:       row.Reference,
		PaidOn:          row.PaidOn.Format("2006-01-02"),
		Status:          row.Status,
	}
}

type codAdjustmentView struct {
	ID              string  `json:"id"`
	AdjustmentType  string  `json:"adjustmentType"`
	AmountMinor     int64   `json:"amountMinor"`
	Currency        string  `json:"currency"`
	LiablePartyType *string `json:"liablePartyType"`
	Reason          string  `json:"reason"`
	Status          string  `json:"status"`
	RequestedBy     int64   `json:"requestedBy"`
	ApprovedBy      *int64  `json:"approvedBy"`
}

func codAdjustmentResponse(row dbgen.CodAdjustment) codAdjustmentView {
	return codAdjustmentView{
		ID:              row.PublicID,
		AdjustmentType:  row.AdjustmentType,
		AmountMinor:     row.AmountMinor,
		Currency:        row.Currency,
		LiablePartyType: row.LiablePartyType,
		Reason:          row.Reason,
		Status:          row.Status,
		RequestedBy:     row.RequestedBy,
		ApprovedBy:      row.ApprovedBy,
	}
}

type codDisputeView struct {
	ID            string  `json:"id"`
	DisputeCode   string  `json:"disputeCode"`
	DisputedMinor int64   `json:"disputedMinor"`
	Category      string  `json:"category"`
	Description   string  `json:"description"`
	Status        string  `json:"status"`
	Resolution    *string `json:"resolution"`
}

func codDisputeResponse(row dbgen.CodDispute) codDisputeView {
	return codDisputeView{
		ID:            row.PublicID,
		DisputeCode:   row.DisputeCode,
		DisputedMinor: row.DisputedMinor,
		Category:      row.Category,
		Description:   row.Description,
		Status:        row.Status,
		Resolution:    row.Resolution,
	}
}

type codTransferItemView struct {
	ObligationID int64  `json:"obligationId"`
	AmountMinor  int64  `json:"amountMinor"`
	Status       string `json:"status"`
}

func codTransferItemResponse(row dbgen.ListTransferItemsRow) codTransferItemView {
	return codTransferItemView{
		ObligationID: row.ObligationID,
		AmountMinor:  row.AmountMinor,
		Status:       row.Status,
	}
}
