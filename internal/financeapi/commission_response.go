package financeapi

import (
	"encoding/json"
	"github.com/ceserve/courier-os/internal/dbgen"
	"time"
)

// Explicit transport projections preserve the OpenAPI contract and keep database
// identities and tenant/audit columns out of the public commission response.
type commissionRuleResponse struct {
	ID                string  `json:"id"`
	Code              string  `json:"code"`
	Name              string  `json:"name"`
	CommissionType    string  `json:"commissionType"`
	RecipientRole     string  `json:"recipientRole"`
	SchemeCode        string  `json:"schemeCode"`
	FranchiseID       *int64  `json:"franchiseId"`
	FranchiseCategory *string `json:"franchiseCategory"`
	ServiceID         *int64  `json:"serviceId"`
	CustomerCategory  *string `json:"customerCategory"`
	PaymentMode       *string `json:"paymentMode"`
	Specificity       int32   `json:"specificity"`
	Priority          int32   `json:"priority"`
	Status            string  `json:"status"`
}

func ruleListResponse(row dbgen.ListCommissionRulesRow) commissionRuleResponse {
	return commissionRuleResponse{
		ID:                row.PublicID,
		Code:              row.Code,
		Name:              row.Name,
		CommissionType:    row.CommissionType,
		RecipientRole:     row.RecipientRole,
		SchemeCode:        row.SchemeCode,
		FranchiseID:       row.FranchiseID,
		FranchiseCategory: row.FranchiseCategory,
		ServiceID:         row.ServiceID,
		CustomerCategory:  row.CustomerCategory,
		PaymentMode:       row.PaymentMode,
		Specificity:       row.Specificity,
		Priority:          row.Priority,
		Status:            row.Status,
	}
}
func ruleDetailResponse(row dbgen.GetRuleByPublicIDRow) commissionRuleResponse {
	return commissionRuleResponse{
		ID:                row.PublicID,
		Code:              row.Code,
		Name:              row.Name,
		CommissionType:    row.CommissionType,
		RecipientRole:     row.RecipientRole,
		SchemeCode:        row.SchemeCode,
		FranchiseID:       row.FranchiseID,
		FranchiseCategory: row.FranchiseCategory,
		ServiceID:         row.ServiceID,
		CustomerCategory:  row.CustomerCategory,
		PaymentMode:       row.PaymentMode,
		Specificity:       row.Specificity,
		Priority:          row.Priority,
		Status:            row.Status,
	}
}

type commissionVersionResponse struct {
	ID                string          `json:"id"`
	VersionNo         int32           `json:"versionNo"`
	CalculationMethod string          `json:"calculationMethod"`
	Currency          string          `json:"currency"`
	FixedAmountMinor  *int64          `json:"fixedAmountMinor"`
	RateBp            *int32          `json:"rateBp"`
	Basis             *string         `json:"basis"`
	Slabs             json.RawMessage `json:"slabs"`
	MinAmountMinor    *int64          `json:"minAmountMinor"`
	MaxAmountMinor    *int64          `json:"maxAmountMinor"`
	EffectiveFrom     string          `json:"effectiveFrom"`
	EffectiveTo       *string         `json:"effectiveTo"`
	Status            string          `json:"status"`
}

func ruleVersionResponse(row dbgen.CommissionRuleVersion) commissionVersionResponse {
	var end *string
	if row.EffectiveTo != nil {
		value := row.EffectiveTo.Format("2006-01-02")
		end = &value
	}
	return commissionVersionResponse{
		ID:                row.PublicID,
		VersionNo:         row.VersionNo,
		CalculationMethod: row.CalculationMethod,
		Currency:          row.Currency,
		FixedAmountMinor:  row.FixedAmountMinor,
		RateBp:            row.RateBp,
		Basis:             row.Basis,
		Slabs:             row.Slabs,
		MinAmountMinor:    row.MinAmountMinor,
		MaxAmountMinor:    row.MaxAmountMinor,
		EffectiveFrom:     row.EffectiveFrom.Format("2006-01-02"),
		EffectiveTo:       end,
		Status:            row.Status,
	}
}

type commissionCalculationResponse struct {
	ID                string          `json:"id"`
	CommissionType    string          `json:"commissionType"`
	AWB               *string         `json:"awb"`
	QualifyingEvent   string          `json:"qualifyingEvent"`
	QualifiedAt       time.Time       `json:"qualifiedAt"`
	RecipientType     string          `json:"recipientType"`
	FranchiseCode     *string         `json:"franchiseCode"`
	RuleCode          string          `json:"ruleCode"`
	RuleVersionNo     int32           `json:"ruleVersionNo"`
	CalculationMethod string          `json:"calculationMethod"`
	Basis             *string         `json:"basis"`
	BaseAmountMinor   int64           `json:"baseAmountMinor"`
	RateBp            *int32          `json:"rateBp"`
	GrossAmountMinor  int64           `json:"grossAmountMinor"`
	AmountMinor       int64           `json:"amountMinor"`
	Currency          string          `json:"currency"`
	CalculationTrace  json.RawMessage `json:"calculationTrace"`
	Status            string          `json:"status"`
	SettlementID      *int64          `json:"settlementId"`
}

func calculationListResponse(row dbgen.ListCommissionCalculationsRow) commissionCalculationResponse {
	return commissionCalculationResponse{
		ID:                row.PublicID,
		CommissionType:    row.CommissionType,
		AWB:               row.Awb,
		QualifyingEvent:   row.QualifyingEvent,
		QualifiedAt:       row.QualifiedAt,
		RecipientType:     row.RecipientType,
		FranchiseCode:     row.FranchiseCode,
		RuleCode:          row.RuleCode,
		RuleVersionNo:     row.RuleVersionNo,
		CalculationMethod: row.CalculationMethod,
		Basis:             row.Basis,
		BaseAmountMinor:   row.BaseAmountMinor,
		RateBp:            row.RateBp,
		GrossAmountMinor:  row.GrossAmountMinor,
		AmountMinor:       row.AmountMinor,
		Currency:          row.Currency,
		CalculationTrace:  row.CalculationTrace,
		Status:            row.Status,
		SettlementID:      row.SettlementID,
	}
}
func calculationDetailResponse(row dbgen.GetCalculationByPublicIDRow) commissionCalculationResponse {
	return commissionCalculationResponse{
		ID:                row.PublicID,
		CommissionType:    row.CommissionType,
		AWB:               row.Awb,
		QualifyingEvent:   row.QualifyingEvent,
		QualifiedAt:       row.QualifiedAt,
		RecipientType:     row.RecipientType,
		FranchiseCode:     row.FranchiseCode,
		RuleCode:          row.RuleCode,
		RuleVersionNo:     row.RuleVersionNo,
		CalculationMethod: row.CalculationMethod,
		Basis:             row.Basis,
		BaseAmountMinor:   row.BaseAmountMinor,
		RateBp:            row.RateBp,
		GrossAmountMinor:  row.GrossAmountMinor,
		AmountMinor:       row.AmountMinor,
		Currency:          row.Currency,
		CalculationTrace:  row.CalculationTrace,
		Status:            row.Status,
		SettlementID:      row.SettlementID,
	}
}
