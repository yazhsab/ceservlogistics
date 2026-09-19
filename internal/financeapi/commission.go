package financeapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/commission"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
)

// ---------------------------------------------------------------------------
// M21 Commission
// ---------------------------------------------------------------------------

func (h *Handler) commissionRoutes(r chi.Router) {
	r.Get("/rules", require("commission.read", h.listCommissionRules))
	r.Post("/rules", require("commission.config", h.createCommissionRule))
	r.Get("/rules/{ruleId}", require("commission.read", h.getCommissionRule))
	r.Post("/rules/{ruleId}/versions", require("commission.config", h.createRuleVersion))

	// Simulation runs the same resolver and the same calculator as a real
	// posting, so a preview cannot disagree with what will actually be paid.
	r.Post("/simulate", require("commission.simulate", h.simulateCommission))

	r.Get("/calculations", require("commission.read", h.listCalculations))
	r.Get("/calculations/{calculationId}", require("commission.read", h.getCalculation))
	r.Post("/calculations/{calculationId}/post", require("commission.post", h.postCommission))
	r.Post("/calculations/{calculationId}/reverse", require("commission.reverse", h.reverseCommission))
}

type ruleRequest struct {
	SchemeCode        string `json:"schemeCode"`
	Code              string `json:"code"`
	Name              string `json:"name"`
	CommissionType    string `json:"commissionType"`
	RecipientRole     string `json:"recipientRole"`
	FranchiseID       string `json:"franchiseId,omitempty"`
	FranchiseCategory string `json:"franchiseCategory,omitempty"`
	ServiceCode       string `json:"serviceCode,omitempty"`
	CustomerCategory  string `json:"customerCategory,omitempty"`
	PaymentMode       string `json:"paymentMode,omitempty"`
	Priority          int32  `json:"priority,omitempty"`
	Description       string `json:"description,omitempty"`
}

func (h *Handler) createCommissionRule(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	in, err := body[ruleRequest](w, r)
	if err != nil {
		return err
	}
	rule, err := h.commission.CreateRule(r.Context(), p, commission.RuleInput{
		SchemeCode: in.SchemeCode, Code: in.Code, Name: in.Name,
		CommissionType: in.CommissionType, RecipientRole: in.RecipientRole,
		FranchisePublicID: in.FranchiseID, FranchiseCategory: in.FranchiseCategory,
		ServiceCode: in.ServiceCode, CustomerCategory: in.CustomerCategory,
		PaymentMode: in.PaymentMode, Priority: in.Priority, Description: in.Description,
	})
	if err != nil {
		return err
	}
	detail, _, err := h.commission.GetRule(r.Context(), p, rule.PublicID)
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/commission/rules/"+rule.PublicID, ruleDetailResponse(*detail))
}

type versionRequest struct {
	Method           string            `json:"calculationMethod"`
	FixedAmountMinor *int64            `json:"fixedAmountMinor,omitempty"`
	RateBp           *int32            `json:"rateBp,omitempty"`
	Basis            string            `json:"basis,omitempty"`
	Slabs            []commission.Slab `json:"slabs,omitempty"`
	MinAmountMinor   *int64            `json:"minAmountMinor,omitempty"`
	MaxAmountMinor   *int64            `json:"maxAmountMinor,omitempty"`
	EffectiveFrom    string            `json:"effectiveFrom"`
	Notes            string            `json:"notes,omitempty"`
}

func (h *Handler) createRuleVersion(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "ruleId", "crl", "Commission rule")
	if err != nil {
		return err
	}
	in, err := body[versionRequest](w, r)
	if err != nil {
		return err
	}
	from, err := reqDate(in.EffectiveFrom, "effectiveFrom")
	if err != nil {
		return err
	}
	version, err := h.commission.CreateVersion(r.Context(), p, id, commission.VersionInput{
		Method: in.Method, FixedAmountMinor: in.FixedAmountMinor, RateBp: in.RateBp,
		Basis: in.Basis, Slabs: in.Slabs,
		MinAmountMinor: in.MinAmountMinor, MaxAmountMinor: in.MaxAmountMinor,
		EffectiveFrom: from, Notes: in.Notes,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "", ruleVersionResponse(*version))
}

func (h *Handler) listCommissionRules(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	rows, total, err := h.commission.ListRules(r.Context(), p,
		r.URL.Query().Get("commissionType"), r.URL.Query().Get("status"),
		optInt64(r, "franchiseId"), limit(r), offset(r))
	if err != nil {
		return err
	}
	data := make([]commissionRuleResponse, 0, len(rows))
	for _, row := range rows {
		data = append(data, ruleListResponse(row))
	}
	return httpx.OK(w, map[string]any{"data": data, "total": total})
}

func (h *Handler) getCommissionRule(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "ruleId", "crl", "Commission rule")
	if err != nil {
		return err
	}
	rule, versions, err := h.commission.GetRule(r.Context(), p, id)
	if err != nil {
		return err
	}
	data := make([]commissionVersionResponse, 0, len(versions))
	for _, version := range versions {
		data = append(data, ruleVersionResponse(version))
	}
	return httpx.OK(w, map[string]any{"rule": ruleDetailResponse(*rule), "versions": data})
}

type simulateRequest struct {
	CommissionType    string `json:"commissionType"`
	FranchiseID       string `json:"franchiseId,omitempty"`
	FranchiseCategory string `json:"franchiseCategory,omitempty"`
	CustomerCategory  string `json:"customerCategory,omitempty"`
	PaymentMode       string `json:"paymentMode,omitempty"`
	On                string `json:"on,omitempty"`
	Basis             struct {
		FreightMinor      int64 `json:"freightMinor"`
		SurchargeMinor    int64 `json:"surchargeMinor"`
		TaxableMinor      int64 `json:"taxableMinor"`
		TotalMinor        int64 `json:"totalMinor"`
		CODAmountMinor    int64 `json:"codAmountMinor"`
		ChargeableWeightG int64 `json:"chargeableWeightGrams"`
		ShipmentCount     int64 `json:"shipmentCount"`
	} `json:"basis"`
}

func (h *Handler) simulateCommission(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	in, err := body[simulateRequest](w, r)
	if err != nil {
		return err
	}
	if in.CommissionType == "" {
		return apierr.Validation("A simulation needs a commission type.", nil)
	}

	on := time.Now().UTC()
	if in.On != "" {
		d, dErr := reqDate(in.On, "on")
		if dErr != nil {
			return dErr
		}
		on = d
	}

	franchiseID, err := h.commission.ResolveFranchiseID(r.Context(), p, in.FranchiseID)
	if err != nil {
		return err
	}

	result, err := h.commission.Simulate(r.Context(), p, commission.SimulateRequest{
		Facts: commission.Facts{
			CommissionType: in.CommissionType, FranchiseID: franchiseID,
			FranchiseCategory: in.FranchiseCategory,
			CustomerCategory:  in.CustomerCategory, PaymentMode: in.PaymentMode,
		},
		Basis: commission.Basis{
			FreightMinor: in.Basis.FreightMinor, SurchargeMinor: in.Basis.SurchargeMinor,
			TaxableMinor: in.Basis.TaxableMinor, TotalMinor: in.Basis.TotalMinor,
			CODAmountMinor:    in.Basis.CODAmountMinor,
			ChargeableWeightG: in.Basis.ChargeableWeightG,
			ShipmentCount:     in.Basis.ShipmentCount,
		},
		On: on,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, result)
}

func (h *Handler) listCalculations(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	rows, err := h.commission.ListCalculations(r.Context(), p,
		r.URL.Query().Get("commissionType"), r.URL.Query().Get("status"),
		optInt64(r, "franchiseId"), optInt64(r, "cursor"), limit(r))
	if err != nil {
		return err
	}
	data := make([]commissionCalculationResponse, 0, len(rows))
	for _, row := range rows {
		data = append(data, calculationListResponse(row))
	}
	return httpx.OK(w, map[string]any{"data": data})
}

func (h *Handler) getCalculation(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "calculationId", "ccl", "Commission calculation")
	if err != nil {
		return err
	}
	calc, err := h.commission.GetCalculation(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, calculationDetailResponse(*calc))
}

func (h *Handler) postCommission(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "calculationId", "ccl", "Commission calculation")
	if err != nil {
		return err
	}
	calc, err := h.commission.Post(r.Context(), p, id)
	if err != nil {
		return err
	}
	detail, err := h.commission.GetCalculation(r.Context(), p, calc.PublicID)
	if err != nil {
		return err
	}
	return httpx.OK(w, calculationDetailResponse(*detail))
}

func (h *Handler) reverseCommission(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "calculationId", "ccl", "Commission calculation")
	if err != nil {
		return err
	}
	in, err := body[reasonRequest](w, r)
	if err != nil {
		return err
	}
	calc, err := h.commission.Reverse(r.Context(), p, id, in.Reason)
	if err != nil {
		return err
	}
	detail, err := h.commission.GetCalculation(r.Context(), p, calc.PublicID)
	if err != nil {
		return err
	}
	return httpx.OK(w, calculationDetailResponse(*detail))
}
