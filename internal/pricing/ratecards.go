package pricing

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/auth"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Rate card enums.
var (
	rateCardScopes = []string{"RETAIL", "BUSINESS", "FRANCHISE"}
	surchargeTypes = []string{"FUEL", "REMOTE_AREA", "COD", "INSURANCE", "HANDLING", "OVERSIZE", "DOCUMENTATION", "PACKAGING", "APPOINTMENT", "CUSTOM"}
	surchargeCalc  = []string{"FIXED", "PERCENTAGE", "PER_KG"}
	surchargeBases = []string{"FREIGHT", "FREIGHT_PLUS_SURCHARGES", "DECLARED_VALUE", "COD_AMOUNT"}
	discountTypes  = []string{"FIXED", "PERCENTAGE"}
	discountBases  = []string{"FREIGHT", "FREIGHT_PLUS_SURCHARGES"}
	// India, then the single-rate vocabulary most other markets use. The list
	// mirrors the CHECK on tax_rules.tax_type; a value here that the database
	// refuses is a 500 where a 422 belongs.
	//
	// A Nigerian VAT rule could have used CUSTOM, but a line reading
	// "CUSTOM 7.5%" on a customer's invoice is the kind of small dishonesty
	// that erodes trust in the document.
	taxTypes = []string{
		"CGST", "SGST", "IGST", "UTGST", "CESS",
		"VAT", "WHT",
		"CUSTOM",
	}
)

// RateCardRoutes mounts rate card administration under /rate-cards.
func (h *Handler) RateCardRoutes(r chi.Router) {
	read := auth.RequirePermission("rate_card.read")
	manage := auth.RequirePermission("rate_card.manage")
	activate := auth.RequirePermission("rate_card.activate")

	r.With(read).Get("/", httpx.Wrap(h.listRateCards))
	r.With(manage).Post("/", httpx.Wrap(h.createRateCard))
	r.With(read).Get("/{cardId}/versions", httpx.Wrap(h.listVersions))
	r.With(manage).Post("/{cardId}/versions", httpx.Wrap(h.createVersion))
	r.With(read).Get("/versions/{versionId}", httpx.Wrap(h.getVersion))
	r.With(manage).Put("/versions/{versionId}/zone-rates", httpx.Wrap(h.upsertZoneRate))
	r.With(manage).Post("/versions/{versionId}/weight-slabs", httpx.Wrap(h.createWeightSlab))
	r.With(manage).Post("/versions/{versionId}/surcharges", httpx.Wrap(h.createSurcharge))
	r.With(manage).Post("/versions/{versionId}/discounts", httpx.Wrap(h.createDiscount))
	r.With(activate).Post("/versions/{versionId}/activate", httpx.Wrap(h.activateVersion))
	r.With(activate).Post("/versions/{versionId}/archive", httpx.Wrap(h.archiveVersion))
}

// TaxRoutes mounts tax rule administration under /tax-rules.
func (h *Handler) TaxRoutes(r chi.Router) {
	r.With(auth.RequirePermission("rate_card.read")).Get("/", httpx.Wrap(h.listTaxRules))
	r.With(auth.RequirePermission("tax_rule.manage")).Post("/", httpx.Wrap(h.createTaxRule))
	r.With(auth.RequirePermission("tax_rule.manage")).Delete("/{taxRuleId}", httpx.Wrap(h.deactivateTaxRule))
}

// ---- rate cards ------------------------------------------------------------

type createRateCardRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope"`
	CustomerID  string `json:"customerId,omitempty"`
	FranchiseID string `json:"franchiseId,omitempty"`
	IsDefault   bool   `json:"isDefault,omitempty"`
}

func (h *Handler) listRateCards(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, page, err := window(r)
	if err != nil {
		return err
	}
	params := dbgen.ListRateCardsParams{
		OrganizationID: p.OrganizationID,
		RowLimit:       int32(limit), RowOffset: int32((page - 1) * limit),
	}
	if scope, sErr := httpx.QueryEnum(r, "scope", rateCardScopes); sErr != nil {
		return sErr
	} else if scope != "" {
		params.Scope = &scope
	}
	rows, err := h.q.ListRateCards(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]map[string]any, 0, len(rows))
	for _, c := range rows {
		total = c.TotalCount
		items = append(items, map[string]any{
			"id": c.PublicID, "code": c.Code, "name": c.Name, "scope": c.Scope,
			"currency": c.Currency, "isDefault": c.IsDefault, "status": c.Status,
			"customerCode": c.CustomerCode, "franchiseCode": c.FranchiseCode,
			"latestVersion": c.LatestVersion, "activeVersion": c.ActiveVersion,
		})
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "code", "asc"))
}

func (h *Handler) createRateCard(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createRateCardRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	code := v.Code("code", req.Code)
	name := v.Text("name", req.Name, 2, 160, true)
	description := v.Text("description", req.Description, 0, 1000, false)
	scope := v.Enum("scope", req.Scope, rateCardScopes, true)
	if err := v.Err(); err != nil {
		return err
	}

	params := dbgen.CreateRateCardParams{
		PublicID: publicid.New(publicid.PrefixRateCard), OrganizationID: p.OrganizationID,
		Code: code, Name: name, Description: description, Scope: scope,
		Currency: p.OrganizationCurrency, IsDefault: req.IsDefault, CreatedBy: &p.UserID,
	}
	switch scope {
	case "BUSINESS":
		if req.CustomerID == "" {
			return apierr.Validation("A BUSINESS rate card must name a customer.", nil)
		}
		customer, cErr := h.q.GetCustomerByPublicID(r.Context(), dbgen.GetCustomerByPublicIDParams{
			PublicID: req.CustomerID, OrganizationID: p.OrganizationID,
		})
		if cErr != nil {
			return apierr.Validation("The customer does not exist.", nil)
		}
		params.CustomerID = &customer.ID
	case "FRANCHISE":
		if req.FranchiseID == "" {
			return apierr.Validation("A FRANCHISE rate card must name a franchise.", nil)
		}
		f, fErr := h.q.GetFranchiseByPublicID(r.Context(), dbgen.GetFranchiseByPublicIDParams{
			PublicID: req.FranchiseID, OrganizationID: p.OrganizationID,
		})
		if fErr != nil {
			return apierr.Validation("The franchise does not exist.", nil)
		}
		params.FranchiseID = &f.ID
	default:
		if req.CustomerID != "" || req.FranchiseID != "" {
			return apierr.Validation("A RETAIL rate card must not name a customer or franchise.", nil)
		}
	}

	card, err := h.q.CreateRateCard(r.Context(), params)
	if err != nil {
		switch {
		case database.IsUniqueViolation(err, "rate_cards_code_unique"):
			return apierr.Duplicate("A rate card with this code already exists.")
		case database.IsUniqueViolation(err, "rate_cards_default_idx"):
			return apierr.Conflict(apierr.CodeConflict,
				"Another retail rate card is already the organization default.")
		}
		return apierr.Internal(fmt.Errorf("create rate card: %w", err))
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRateCardCreated, ResourceType: "rate_card",
		ResourceID: &card.ID, ResourcePublicID: card.PublicID,
		After: map[string]any{"code": card.Code, "scope": card.Scope, "isDefault": card.IsDefault},
	}))
	return httpx.Created(w, "/api/v1/rate-cards/"+card.PublicID, map[string]any{
		"id": card.PublicID, "code": card.Code, "name": card.Name,
		"scope": card.Scope, "currency": card.Currency, "isDefault": card.IsDefault,
	})
}

// ---- versions --------------------------------------------------------------

type createVersionRequest struct {
	EffectiveFrom time.Time  `json:"effectiveFrom"`
	EffectiveTo   *time.Time `json:"effectiveTo,omitempty"`
	Notes         string     `json:"notes,omitempty"`
}

func (h *Handler) listVersions(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	card, err := h.loadCard(r, p)
	if err != nil {
		return err
	}
	limit, page, err := window(r)
	if err != nil {
		return err
	}
	rows, err := h.q.ListRateCardVersions(r.Context(), dbgen.ListRateCardVersionsParams{
		RateCardID: card.ID, OrganizationID: p.OrganizationID,
		RowLimit: int32(limit), RowOffset: int32((page - 1) * limit),
	})
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]map[string]any, 0, len(rows))
	for _, ver := range rows {
		total = ver.TotalCount
		items = append(items, map[string]any{
			"id": ver.PublicID, "version": ver.Version, "status": ver.Status,
			"effectiveFrom": ver.EffectiveFrom, "effectiveTo": ver.EffectiveTo,
			"notes": ver.Notes, "activatedAt": ver.ActivatedAt, "activatedBy": ver.ActivatedByName,
			"createdAt": ver.CreatedAt,
		})
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "version", "desc"))
}

func (h *Handler) createVersion(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	card, err := h.loadCard(r, p)
	if err != nil {
		return err
	}
	var req createVersionRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	if req.EffectiveFrom.IsZero() {
		req.EffectiveFrom = time.Now()
	}
	v.EffectiveRange("effectiveFrom", "effectiveTo", &req.EffectiveFrom, req.EffectiveTo)
	notes := v.Text("notes", req.Notes, 0, 2000, false)
	if err := v.Err(); err != nil {
		return err
	}
	ver, err := h.q.CreateRateCardVersion(r.Context(), dbgen.CreateRateCardVersionParams{
		PublicID: publicid.New(publicid.PrefixRateCardVersion), OrganizationID: p.OrganizationID,
		RateCardID: card.ID, EffectiveFrom: req.EffectiveFrom, EffectiveTo: req.EffectiveTo,
		Notes: notes, CreatedBy: &p.UserID,
	})
	if err != nil {
		return apierr.Internal(fmt.Errorf("create rate card version: %w", err))
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRateCardVersionCreated, ResourceType: "rate_card_version",
		ResourceID: &ver.ID, ResourcePublicID: ver.PublicID,
		After: map[string]any{"rateCard": card.Code, "version": ver.Version, "status": ver.Status},
	}))
	return httpx.Created(w, "", map[string]any{
		"id": ver.PublicID, "version": ver.Version, "status": ver.Status,
		"effectiveFrom": ver.EffectiveFrom,
	})
}

func (h *Handler) getVersion(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	ver, err := h.loadVersion(r, p)
	if err != nil {
		return err
	}
	counts, err := h.q.CountVersionPricingRows(r.Context(), ver.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	rates, err := h.q.ListZoneRates(r.Context(), dbgen.ListZoneRatesParams{
		RateCardVersionID: ver.ID, RowLimit: 500, RowOffset: 0,
	})
	if err != nil {
		return apierr.Internal(err)
	}
	rateItems := make([]map[string]any, 0, len(rates))
	for _, zr := range rates {
		rateItems = append(rateItems, map[string]any{
			"id": zr.PublicID, "serviceCode": zr.ServiceCode,
			"originZoneCode": zr.OriginZoneCode, "destinationZoneCode": zr.DestinationZoneCode,
			"baseWeightGrams": zr.BaseWeightGrams, "basePriceMinor": zr.BasePriceMinor,
			"additionalStepGrams": zr.AdditionalStepGrams, "additionalPriceMinor": zr.AdditionalPriceMinor,
			"minChargeableWeightGrams": zr.MinChargeableWeightGrams,
		})
	}
	surcharges, err := h.q.ListAllSurchargeRules(r.Context(), dbgen.ListAllSurchargeRulesParams{
		RateCardVersionID: ver.ID, RowLimit: 200, RowOffset: 0,
	})
	if err != nil {
		return apierr.Internal(err)
	}
	surchargeItems := make([]map[string]any, 0, len(surcharges))
	for _, s := range surcharges {
		surchargeItems = append(surchargeItems, map[string]any{
			"id": s.PublicID, "code": s.Code, "name": s.Name, "type": s.SurchargeType,
			"calcType": s.CalcType, "valueMinor": s.ValueMinor, "percentageBp": s.PercentageBp,
			"appliesTo": s.AppliesTo, "priority": s.Priority, "isTaxable": s.IsTaxable,
			"serviceCode": s.ServiceCode, "conditions": decodeJSON(s.Conditions),
		})
	}
	return httpx.OK(w, map[string]any{
		"id": ver.PublicID, "rateCardCode": ver.RateCardCode, "rateCardId": ver.RateCardPublicID,
		"version": ver.Version, "status": ver.Status, "currency": ver.Currency,
		"effectiveFrom": ver.EffectiveFrom, "effectiveTo": ver.EffectiveTo,
		"notes": ver.Notes, "activatedAt": ver.ActivatedAt,
		"counts": map[string]any{
			"zoneRates": counts.ZoneRateCount, "weightSlabs": counts.SlabCount,
			"surcharges": counts.SurchargeCount, "discounts": counts.DiscountCount,
		},
		"zoneRates":  rateItems,
		"surcharges": surchargeItems,
		"editable":   ver.Status == "DRAFT",
	})
}

type zoneRateRequest struct {
	ServiceCode              string `json:"serviceCode"`
	OriginZoneCode           string `json:"originZoneCode"`
	DestinationZoneCode      string `json:"destinationZoneCode"`
	BaseWeightGrams          int    `json:"baseWeightGrams"`
	BasePriceMinor           int64  `json:"basePriceMinor"`
	AdditionalStepGrams      int    `json:"additionalStepGrams"`
	AdditionalPriceMinor     int64  `json:"additionalPriceMinor"`
	MinChargeableWeightGrams int    `json:"minChargeableWeightGrams,omitempty"`
}

func (h *Handler) upsertZoneRate(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	ver, err := h.loadDraftVersion(r, p)
	if err != nil {
		return err
	}
	var req zoneRateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	serviceCode := v.Code("serviceCode", req.ServiceCode)
	originZone := v.Code("originZoneCode", req.OriginZoneCode)
	destZone := v.Code("destinationZoneCode", req.DestinationZoneCode)
	v.IntRange("baseWeightGrams", req.BaseWeightGrams, 1, 10_000_000)
	v.NonNegativeMinor("basePriceMinor", req.BasePriceMinor)
	if req.AdditionalStepGrams == 0 {
		req.AdditionalStepGrams = req.BaseWeightGrams
	}
	v.IntRange("additionalStepGrams", req.AdditionalStepGrams, 1, 10_000_000)
	v.NonNegativeMinor("additionalPriceMinor", req.AdditionalPriceMinor)
	if err := v.Err(); err != nil {
		return err
	}

	svc, zoneIDs, err := h.resolveRateRefs(r, p, serviceCode, originZone, destZone)
	if err != nil {
		return err
	}
	rate, err := h.q.UpsertZoneRate(r.Context(), dbgen.UpsertZoneRateParams{
		PublicID: publicid.New(publicid.PrefixZoneRate), OrganizationID: p.OrganizationID,
		RateCardVersionID: ver.ID, CourierServiceID: svc,
		OriginZoneID: zoneIDs[0], DestinationZoneID: zoneIDs[1],
		BaseWeightGrams: int32(req.BaseWeightGrams), BasePriceMinor: req.BasePriceMinor,
		AdditionalStepGrams: int32(req.AdditionalStepGrams), AdditionalPriceMinor: req.AdditionalPriceMinor,
		MinChargeableWeightGrams: int32(req.MinChargeableWeightGrams),
	})
	if err != nil {
		return immutableOrInternal(err, "rate")
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRateCardPricingChanged, ResourceType: "zone_rate",
		ResourceID: &rate.ID, ResourcePublicID: rate.PublicID,
		After: map[string]any{
			"versionId": ver.PublicID, "serviceCode": serviceCode,
			"originZone": originZone, "destinationZone": destZone,
			"basePriceMinor": req.BasePriceMinor,
		},
	}))
	return httpx.OK(w, map[string]any{
		"id": rate.PublicID, "serviceCode": serviceCode,
		"originZoneCode": originZone, "destinationZoneCode": destZone,
		"baseWeightGrams": rate.BaseWeightGrams, "basePriceMinor": rate.BasePriceMinor,
		"additionalStepGrams": rate.AdditionalStepGrams, "additionalPriceMinor": rate.AdditionalPriceMinor,
	})
}

type weightSlabRequest struct {
	ServiceCode          string `json:"serviceCode"`
	OriginZoneCode       string `json:"originZoneCode"`
	DestinationZoneCode  string `json:"destinationZoneCode"`
	FromWeightGrams      int    `json:"fromWeightGrams"`
	ToWeightGrams        *int   `json:"toWeightGrams,omitempty"`
	PriceMinor           int64  `json:"priceMinor"`
	AdditionalStepGrams  *int   `json:"additionalStepGrams,omitempty"`
	AdditionalPriceMinor *int64 `json:"additionalPriceMinor,omitempty"`
	Sequence             int    `json:"sequence,omitempty"`
}

func (h *Handler) createWeightSlab(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	ver, err := h.loadDraftVersion(r, p)
	if err != nil {
		return err
	}
	var req weightSlabRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	serviceCode := v.Code("serviceCode", req.ServiceCode)
	originZone := v.Code("originZoneCode", req.OriginZoneCode)
	destZone := v.Code("destinationZoneCode", req.DestinationZoneCode)
	v.IntRange("fromWeightGrams", req.FromWeightGrams, 0, 10_000_000)
	v.NonNegativeMinor("priceMinor", req.PriceMinor)
	if req.ToWeightGrams != nil && *req.ToWeightGrams <= req.FromWeightGrams {
		v.Add("toWeightGrams", "Must be greater than fromWeightGrams.")
	}
	if req.Sequence == 0 {
		req.Sequence = req.FromWeightGrams + 1
	}
	if err := v.Err(); err != nil {
		return err
	}
	svc, zoneIDs, err := h.resolveRateRefs(r, p, serviceCode, originZone, destZone)
	if err != nil {
		return err
	}
	params := dbgen.CreateWeightSlabParams{
		PublicID: publicid.New(publicid.PrefixWeightSlab), OrganizationID: p.OrganizationID,
		RateCardVersionID: ver.ID, CourierServiceID: svc,
		OriginZoneID: zoneIDs[0], DestinationZoneID: zoneIDs[1],
		FromWeightGrams: int32(req.FromWeightGrams), PriceMinor: req.PriceMinor,
		Sequence: int32(req.Sequence),
	}
	if req.ToWeightGrams != nil {
		to := int32(*req.ToWeightGrams)
		params.ToWeightGrams = &to
	}
	if req.AdditionalStepGrams != nil {
		st := int32(*req.AdditionalStepGrams)
		params.AdditionalStepGrams = &st
	}
	params.AdditionalPriceMinor = req.AdditionalPriceMinor

	slab, err := h.q.CreateWeightSlab(r.Context(), params)
	if err != nil {
		// The exclusion constraint refuses overlapping slabs at the storage
		// layer, which is what makes slab pricing deterministic.
		if database.ConstraintName(err) == "weight_slabs_no_overlap" {
			return apierr.Conflict(apierr.CodeConflict,
				"This weight range overlaps an existing slab for the same lane and service.")
		}
		return immutableOrInternal(err, "weight slab")
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRateCardPricingChanged, ResourceType: "weight_slab",
		ResourceID: &slab.ID, ResourcePublicID: slab.PublicID,
		After: map[string]any{
			"versionId": ver.PublicID, "fromWeightGrams": slab.FromWeightGrams,
			"toWeightGrams": slab.ToWeightGrams, "priceMinor": slab.PriceMinor,
		},
	}))
	return httpx.Created(w, "", map[string]any{
		"id": slab.PublicID, "fromWeightGrams": slab.FromWeightGrams,
		"toWeightGrams": slab.ToWeightGrams, "priceMinor": slab.PriceMinor,
	})
}

type surchargeRequest struct {
	Code           string         `json:"code"`
	Name           string         `json:"name"`
	SurchargeType  string         `json:"surchargeType"`
	CalcType       string         `json:"calcType"`
	ValueMinor     *int64         `json:"valueMinor,omitempty"`
	PercentageBP   *int32         `json:"percentageBp,omitempty"`
	AppliesTo      string         `json:"appliesTo,omitempty"`
	MinAmountMinor *int64         `json:"minAmountMinor,omitempty"`
	MaxAmountMinor *int64         `json:"maxAmountMinor,omitempty"`
	ServiceCode    string         `json:"serviceCode,omitempty"`
	Conditions     map[string]any `json:"conditions,omitempty"`
	Priority       int            `json:"priority,omitempty"`
	IsTaxable      *bool          `json:"isTaxable,omitempty"`
}

func (h *Handler) createSurcharge(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	ver, err := h.loadDraftVersion(r, p)
	if err != nil {
		return err
	}
	var req surchargeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	code := v.Code("code", req.Code)
	name := v.Text("name", req.Name, 2, 120, true)
	surchargeType := v.Enum("surchargeType", req.SurchargeType, surchargeTypes, true)
	calcType := v.Enum("calcType", req.CalcType, surchargeCalc, true)
	appliesTo := "FREIGHT"
	if req.AppliesTo != "" {
		appliesTo = v.Enum("appliesTo", req.AppliesTo, surchargeBases, true)
	}
	switch calcType {
	case "PERCENTAGE":
		if req.PercentageBP == nil {
			v.Add("percentageBp", "Required when calcType is PERCENTAGE.")
		} else {
			v.BasisPoints("percentageBp", *req.PercentageBP)
		}
		if req.ValueMinor != nil {
			v.Add("valueMinor", "Must be omitted when calcType is PERCENTAGE.")
		}
	default:
		if req.ValueMinor == nil {
			v.Addf("valueMinor", "Required when calcType is %s.", calcType)
		} else {
			v.NonNegativeMinor("valueMinor", *req.ValueMinor)
		}
		if req.PercentageBP != nil {
			v.Addf("percentageBp", "Must be omitted when calcType is %s.", calcType)
		}
	}
	if req.Priority == 0 {
		req.Priority = 100
	}
	v.IntRange("priority", req.Priority, 0, 1000)
	if err := v.Err(); err != nil {
		return err
	}

	taxable := true
	if req.IsTaxable != nil {
		taxable = *req.IsTaxable
	}
	params := dbgen.CreateSurchargeRuleParams{
		PublicID: publicid.New(publicid.PrefixSurchargeRule), OrganizationID: p.OrganizationID,
		RateCardVersionID: ver.ID, Code: code, Name: name,
		SurchargeType: surchargeType, CalcType: calcType,
		ValueMinor: req.ValueMinor, PercentageBp: req.PercentageBP, AppliesTo: appliesTo,
		MinAmountMinor: req.MinAmountMinor, MaxAmountMinor: req.MaxAmountMinor,
		Conditions: encodeJSON(req.Conditions), Priority: int32(req.Priority), IsTaxable: taxable,
	}
	if req.ServiceCode != "" {
		svc, sErr := h.q.GetCourierServiceByCode(r.Context(), dbgen.GetCourierServiceByCodeParams{
			OrganizationID: p.OrganizationID, Code: req.ServiceCode,
		})
		if sErr != nil {
			return apierr.Validation("The courier service does not exist.", nil)
		}
		params.CourierServiceID = &svc.ID
	}

	rule, err := h.q.CreateSurchargeRule(r.Context(), params)
	if err != nil {
		if database.IsUniqueViolation(err, "surcharge_rules_code_unique") {
			return apierr.Duplicate("A surcharge with this code already exists on this version.")
		}
		return immutableOrInternal(err, "surcharge")
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRateCardPricingChanged, ResourceType: "surcharge_rule",
		ResourceID: &rule.ID, ResourcePublicID: rule.PublicID,
		After: map[string]any{"versionId": ver.PublicID, "code": rule.Code, "calcType": rule.CalcType},
	}))
	return httpx.Created(w, "", map[string]any{
		"id": rule.PublicID, "code": rule.Code, "name": rule.Name,
		"calcType": rule.CalcType, "priority": rule.Priority, "isTaxable": rule.IsTaxable,
	})
}

type discountRequest struct {
	Code             string         `json:"code"`
	Name             string         `json:"name"`
	DiscountType     string         `json:"discountType"`
	ValueMinor       *int64         `json:"valueMinor,omitempty"`
	PercentageBP     *int32         `json:"percentageBp,omitempty"`
	AppliesTo        string         `json:"appliesTo,omitempty"`
	ServiceCode      string         `json:"serviceCode,omitempty"`
	MinSubtotalMinor int64          `json:"minSubtotalMinor,omitempty"`
	MaxDiscountMinor *int64         `json:"maxDiscountMinor,omitempty"`
	Conditions       map[string]any `json:"conditions,omitempty"`
	Priority         int            `json:"priority,omitempty"`
	IsStackable      bool           `json:"isStackable,omitempty"`
}

func (h *Handler) createDiscount(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	ver, err := h.loadDraftVersion(r, p)
	if err != nil {
		return err
	}
	var req discountRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	code := v.Code("code", req.Code)
	name := v.Text("name", req.Name, 2, 120, true)
	discountType := v.Enum("discountType", req.DiscountType, discountTypes, true)
	appliesTo := "FREIGHT"
	if req.AppliesTo != "" {
		appliesTo = v.Enum("appliesTo", req.AppliesTo, discountBases, true)
	}
	if discountType == "PERCENTAGE" {
		if req.PercentageBP == nil {
			v.Add("percentageBp", "Required when discountType is PERCENTAGE.")
		} else if *req.PercentageBP < 0 || *req.PercentageBP > 10000 {
			v.Add("percentageBp", "Must be between 0 and 10000 basis points (0-100%).")
		}
	} else if req.ValueMinor == nil {
		v.Add("valueMinor", "Required when discountType is FIXED.")
	}
	if req.Priority == 0 {
		req.Priority = 100
	}
	v.IntRange("priority", req.Priority, 0, 1000)
	if err := v.Err(); err != nil {
		return err
	}

	params := dbgen.CreateDiscountRuleParams{
		PublicID: publicid.New(publicid.PrefixDiscountRule), OrganizationID: p.OrganizationID,
		RateCardVersionID: ver.ID, Code: code, Name: name, DiscountType: discountType,
		ValueMinor: req.ValueMinor, PercentageBp: req.PercentageBP, AppliesTo: appliesTo,
		MinSubtotalMinor: req.MinSubtotalMinor, MaxDiscountMinor: req.MaxDiscountMinor,
		Conditions: encodeJSON(req.Conditions), Priority: int32(req.Priority),
		IsStackable: req.IsStackable,
	}
	if req.ServiceCode != "" {
		svc, sErr := h.q.GetCourierServiceByCode(r.Context(), dbgen.GetCourierServiceByCodeParams{
			OrganizationID: p.OrganizationID, Code: req.ServiceCode,
		})
		if sErr != nil {
			return apierr.Validation("The courier service does not exist.", nil)
		}
		params.CourierServiceID = &svc.ID
	}
	rule, err := h.q.CreateDiscountRule(r.Context(), params)
	if err != nil {
		if database.IsUniqueViolation(err, "discount_rules_code_unique") {
			return apierr.Duplicate("A discount with this code already exists on this version.")
		}
		return immutableOrInternal(err, "discount")
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRateCardPricingChanged, ResourceType: "discount_rule",
		ResourceID: &rule.ID, ResourcePublicID: rule.PublicID,
		After: map[string]any{"versionId": ver.PublicID, "code": rule.Code},
	}))
	return httpx.Created(w, "", map[string]any{
		"id": rule.PublicID, "code": rule.Code, "name": rule.Name,
		"discountType": rule.DiscountType, "isStackable": rule.IsStackable,
	})
}

// activateVersion publishes a draft.
//
// Activation supersedes the currently active version in the same transaction,
// so there is never a moment where a card has two active versions or none.
// From this point the version's pricing rows are immutable — enforced by
// database triggers, not by convention.
func (h *Handler) activateVersion(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	ver, err := h.loadVersion(r, p)
	if err != nil {
		return err
	}
	if ver.Status != "DRAFT" {
		return apierr.Conflict(apierr.CodeConflict,
			"Only a DRAFT version can be activated.").WithDetail("currentStatus", ver.Status)
	}
	counts, err := h.q.CountVersionPricingRows(r.Context(), ver.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	if counts.ZoneRateCount == 0 && counts.SlabCount == 0 {
		return apierr.Conflict(apierr.CodeConflict,
			"This version has no rates. Add at least one zone rate or weight slab before activating.")
	}

	var activated dbgen.RateCardVersion
	err = h.engine.DB().InTx(r.Context(), func(tx pgx.Tx) error {
		qtx := h.q.WithTx(tx)
		if _, sErr := qtx.SupersedeActiveVersion(r.Context(), dbgen.SupersedeActiveVersionParams{
			// The outgoing version stops exactly where the new one starts, so
			// there is never a gap or an overlap in effective dates.
			RateCardID: ver.RateCardID, EffectiveTo: &ver.EffectiveFrom,
		}); sErr != nil {
			return apierr.Internal(sErr)
		}
		var aErr error
		activated, aErr = qtx.ActivateRateCardVersion(r.Context(), dbgen.ActivateRateCardVersionParams{
			PublicID: ver.PublicID, OrganizationID: p.OrganizationID, ActivatedBy: &p.UserID,
		})
		if aErr != nil {
			if database.IsNoRows(aErr) {
				return apierr.Conflict(apierr.CodeConflict, "This version is no longer a draft.")
			}
			return apierr.Internal(aErr)
		}
		return nil
	})
	if err != nil {
		return err
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRateCardVersionActivated, ResourceType: "rate_card_version",
		ResourceID: &activated.ID, ResourcePublicID: activated.PublicID,
		Before: map[string]any{"status": "DRAFT"},
		After: map[string]any{
			"status": activated.Status, "version": activated.Version,
			"effectiveFrom": activated.EffectiveFrom,
			"zoneRates":     counts.ZoneRateCount, "weightSlabs": counts.SlabCount,
		},
	}))
	return httpx.OK(w, map[string]any{
		"id": activated.PublicID, "version": activated.Version, "status": activated.Status,
		"effectiveFrom": activated.EffectiveFrom, "activatedAt": activated.ActivatedAt,
	})
}

func (h *Handler) archiveVersion(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	ver, err := h.loadVersion(r, p)
	if err != nil {
		return err
	}
	archived, err := h.q.ArchiveRateCardVersion(r.Context(), dbgen.ArchiveRateCardVersionParams{
		PublicID: ver.PublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.Conflict(apierr.CodeConflict,
				"Only DRAFT or SUPERSEDED versions can be archived. An ACTIVE version must be superseded first.")
		}
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRateCardVersionArchived, ResourceType: "rate_card_version",
		ResourceID: &archived.ID, ResourcePublicID: archived.PublicID,
		After: map[string]any{"status": archived.Status},
	}))
	return httpx.OK(w, map[string]any{"id": archived.PublicID, "status": archived.Status})
}

// ---- tax rules -------------------------------------------------------------

type taxRuleRequest struct {
	Code           string     `json:"code"`
	Name           string     `json:"name"`
	TaxType        string     `json:"taxType"`
	PercentageBP   int32      `json:"percentageBp"`
	IntraStateOnly *bool      `json:"intraStateOnly,omitempty"`
	HSNSACCode     string     `json:"hsnSacCode,omitempty"`
	Priority       int        `json:"priority,omitempty"`
	EffectiveFrom  *time.Time `json:"effectiveFrom,omitempty"`
	EffectiveTo    *time.Time `json:"effectiveTo,omitempty"`
}

func (h *Handler) listTaxRules(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, page, err := window(r)
	if err != nil {
		return err
	}
	params := dbgen.ListTaxRulesParams{
		OrganizationID: p.OrganizationID,
		RowLimit:       int32(limit), RowOffset: int32((page - 1) * limit),
	}
	if st, sErr := httpx.QueryEnum(r, "status", []string{"ACTIVE", "INACTIVE"}); sErr != nil {
		return sErr
	} else if st != "" {
		params.Status = &st
	}
	rows, err := h.q.ListTaxRules(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]map[string]any, 0, len(rows))
	for _, t := range rows {
		total = t.TotalCount
		items = append(items, map[string]any{
			"id": t.PublicID, "code": t.Code, "name": t.Name, "taxType": t.TaxType,
			"percentageBp": t.PercentageBp, "intraStateOnly": t.IntraStateOnly,
			"hsnSacCode": t.HsnSacCode, "priority": t.Priority, "status": t.Status,
			"effectiveFrom": t.EffectiveFrom, "effectiveTo": t.EffectiveTo,
		})
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "priority", "asc"))
}

func (h *Handler) createTaxRule(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req taxRuleRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	code := v.Code("code", req.Code)
	name := v.Text("name", req.Name, 2, 120, true)
	taxType := v.Enum("taxType", req.TaxType, taxTypes, true)
	v.BasisPoints("percentageBp", req.PercentageBP)
	if req.Priority == 0 {
		req.Priority = 100
	}
	v.IntRange("priority", req.Priority, 0, 1000)
	if err := v.Err(); err != nil {
		return err
	}
	params := dbgen.CreateTaxRuleParams{
		PublicID: publicid.New(publicid.PrefixTaxRule), OrganizationID: p.OrganizationID,
		Code: code, Name: name, TaxType: taxType, PercentageBp: req.PercentageBP,
		IntraStateOnly: req.IntraStateOnly, Priority: int32(req.Priority),
		EffectiveFrom: time.Now(), EffectiveTo: req.EffectiveTo, CreatedBy: &p.UserID,
	}
	if req.EffectiveFrom != nil {
		params.EffectiveFrom = *req.EffectiveFrom
	}
	if req.HSNSACCode != "" {
		params.HsnSacCode = &req.HSNSACCode
	}
	rule, err := h.q.CreateTaxRule(r.Context(), params)
	if err != nil {
		if database.IsUniqueViolation(err, "tax_rules_code_unique") {
			return apierr.Duplicate("A tax rule with this code already exists.")
		}
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionTaxRuleChanged, ResourceType: "tax_rule",
		ResourceID: &rule.ID, ResourcePublicID: rule.PublicID,
		After: map[string]any{"code": rule.Code, "taxType": rule.TaxType, "percentageBp": rule.PercentageBp},
	}))
	return httpx.Created(w, "", map[string]any{
		"id": rule.PublicID, "code": rule.Code, "taxType": rule.TaxType,
		"percentageBp": rule.PercentageBp, "status": rule.Status,
	})
}

func (h *Handler) deactivateTaxRule(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	ruleID, err := httpx.PathPublicID(r, "taxRuleId", publicid.PrefixTaxRule, "Tax rule")
	if err != nil {
		return err
	}
	now := time.Now()
	rule, err := h.q.SetTaxRuleStatus(r.Context(), dbgen.SetTaxRuleStatusParams{
		PublicID: ruleID, OrganizationID: p.OrganizationID, Status: "INACTIVE", EffectiveTo: &now,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Tax rule")
		}
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionTaxRuleChanged, ResourceType: "tax_rule",
		ResourceID: &rule.ID, ResourcePublicID: rule.PublicID,
		After: map[string]any{"status": rule.Status},
	}))
	return httpx.NoContent(w)
}

// ---- helpers ---------------------------------------------------------------

func (h *Handler) loadCard(r *http.Request, p *tenant.Principal) (dbgen.RateCard, error) {
	cardID, err := httpx.PathPublicID(r, "cardId", publicid.PrefixRateCard, "Rate card")
	if err != nil {
		return dbgen.RateCard{}, err
	}
	card, err := h.q.GetRateCardByPublicID(r.Context(), dbgen.GetRateCardByPublicIDParams{
		PublicID: cardID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return dbgen.RateCard{}, apierr.NotFound("Rate card")
		}
		return dbgen.RateCard{}, apierr.Internal(err)
	}
	return card, nil
}

func (h *Handler) loadVersion(r *http.Request, p *tenant.Principal) (dbgen.GetRateCardVersionByPublicIDRow, error) {
	versionID, err := httpx.PathPublicID(r, "versionId", publicid.PrefixRateCardVersion, "Rate card version")
	if err != nil {
		return dbgen.GetRateCardVersionByPublicIDRow{}, err
	}
	ver, err := h.q.GetRateCardVersionByPublicID(r.Context(), dbgen.GetRateCardVersionByPublicIDParams{
		PublicID: versionID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return dbgen.GetRateCardVersionByPublicIDRow{}, apierr.NotFound("Rate card version")
		}
		return dbgen.GetRateCardVersionByPublicIDRow{}, apierr.Internal(err)
	}
	return ver, nil
}

// loadDraftVersion additionally refuses to edit a published version, so the
// caller gets a clear 409 instead of a trigger exception.
func (h *Handler) loadDraftVersion(r *http.Request, p *tenant.Principal) (dbgen.GetRateCardVersionByPublicIDRow, error) {
	ver, err := h.loadVersion(r, p)
	if err != nil {
		return ver, err
	}
	if ver.Status != "DRAFT" {
		return ver, apierr.Conflict(apierr.CodeImmutableResource,
			"This rate card version is activated and its pricing is immutable. Create a new version instead.").
			WithDetail("currentStatus", ver.Status).WithDetail("version", ver.Version)
	}
	return ver, nil
}

func (h *Handler) resolveRateRefs(
	r *http.Request, p *tenant.Principal, serviceCode, originZone, destZone string,
) (int64, [2]int64, error) {
	svc, err := h.q.GetCourierServiceByCode(r.Context(), dbgen.GetCourierServiceByCodeParams{
		OrganizationID: p.OrganizationID, Code: serviceCode,
	})
	if err != nil {
		return 0, [2]int64{}, apierr.Validation("The courier service does not exist.",
			map[string]any{"serviceCode": serviceCode})
	}
	var zones [2]int64
	for i, code := range []string{originZone, destZone} {
		zone, zErr := h.q.GetZoneByCode(r.Context(), dbgen.GetZoneByCodeParams{
			OrganizationID: p.OrganizationID, Code: code,
		})
		if zErr != nil {
			return 0, [2]int64{}, apierr.Validation("The zone does not exist.",
				map[string]any{"zoneCode": code})
		}
		zones[i] = zone.ID
	}
	return svc.ID, zones, nil
}

// immutableOrInternal converts the rate-card guard trigger's exception into a
// clear 409 rather than a redacted 500.
func immutableOrInternal(err error, what string) error {
	if msg := database.RaisedMessage(err); msg != "" {
		return apierr.Conflict(apierr.CodeImmutableResource,
			"This rate card version is activated and its pricing is immutable. Create a new version instead.").
			WithDetail("detail", msg)
	}
	return apierr.Internal(fmt.Errorf("write %s: %w", what, err))
}

func window(r *http.Request) (limit, page int, err error) {
	limit, err = pagination.ClampLimit(httpx.Query(r, "limit"))
	if err != nil {
		return 0, 0, err
	}
	page, err = httpx.QueryInt(r, "page", 1, 1, 10_000)
	if err != nil {
		return 0, 0, err
	}
	return limit, page, nil
}

func encodeJSON(m map[string]any) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func decodeJSON(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}
