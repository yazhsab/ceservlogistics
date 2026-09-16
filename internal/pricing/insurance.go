package pricing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
)

// InsuranceQuote is the exact server-priced offer. Its fingerprint binds
// acceptance to the current quote and pricing inputs, not to a UI label.
type InsuranceQuote struct {
	PolicyVersion      string               `json:"policyVersion"`
	RateBP             *int32               `json:"rateBp,omitempty"`
	Rules              []InsuranceRuleQuote `json:"rules,omitempty"`
	DeclaredValueMinor int64                `json:"declaredValueMinor"`
	PremiumMinor       int64                `json:"premiumMinor"`
	Currency           string               `json:"currency"`
	QuoteFingerprint   string               `json:"quoteFingerprint"`
}

// InsuranceRuleQuote preserves the configuration and result of each applied
// INSURANCE surcharge. The surcharge engine is the only calculator.
type InsuranceRuleQuote struct {
	RuleID         string `json:"ruleId"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	CalcType       string `json:"calcType"`
	AppliesTo      string `json:"appliesTo"`
	RateBP         *int32 `json:"rateBp,omitempty"`
	ValueMinor     *int64 `json:"valueMinor,omitempty"`
	MinAmountMinor *int64 `json:"minAmountMinor,omitempty"`
	MaxAmountMinor *int64 `json:"maxAmountMinor,omitempty"`
	IsTaxable      bool   `json:"isTaxable"`
	BasisMinor     int64  `json:"basisMinor"`
	PremiumMinor   int64  `json:"premiumMinor"`
	Explanation    string `json:"explanation"`
}

func validateInsuranceRequest(in QuoteInput) error {
	if !in.InsuranceRequired {
		return nil
	}
	if !in.Service.InsuranceAllowed {
		return apierr.Conflict("INSURANCE_NOT_AVAILABLE", "This courier product does not offer insurance.")
	}
	if in.DeclaredValueMinor <= 0 || in.DeclaredValueMinor > 1_000_000_000_000 {
		return apierr.Validation("Insurance requires a positive declared value within the permitted limit.", nil)
	}
	return nil
}

func (offer *InsuranceQuote) addRule(rule dbgen.SurchargeRule, basis, amount int64, explanation string) {
	offer.Rules = append(offer.Rules, InsuranceRuleQuote{
		RuleID: rule.PublicID, Code: rule.Code, Name: rule.Name, CalcType: rule.CalcType, AppliesTo: rule.AppliesTo,
		RateBP: rule.PercentageBp, ValueMinor: rule.ValueMinor, MinAmountMinor: rule.MinAmountMinor, MaxAmountMinor: rule.MaxAmountMinor,
		IsTaxable: rule.IsTaxable, BasisMinor: basis, PremiumMinor: amount, Explanation: explanation,
	})
	offer.PremiumMinor += amount
	// A single declared-value percentage can be shown as a headline rate. Never
	// misrepresent fixed, capped or composite premiums as a simple percentage.
	offer.RateBP = nil
	if len(offer.Rules) == 1 && rule.CalcType == "PERCENTAGE" && rule.AppliesTo == "DECLARED_VALUE" && rule.MinAmountMinor == nil && rule.MaxAmountMinor == nil {
		offer.RateBP = rule.PercentageBp
	}
}

func fingerprintInsurance(quote *Quote, in QuoteInput) {
	if quote.Insurance == nil {
		return
	}
	// The fingerprint is a deterministic revision identifier, not a bearer
	// credential. Booking recomputes it and authenticates the accepting actor.
	in.At = time.Time{}
	data, _ := json.Marshal(struct {
		Quote *Quote
		Input QuoteInput
	}{quote, in})
	sum := sha256.Sum256(data)
	quote.Insurance.QuoteFingerprint = hex.EncodeToString(sum[:])
}
