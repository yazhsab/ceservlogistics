package network

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/money"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Franchise enums.
var (
	franchiseCategories = []string{"PLATINUM", "GOLD", "SILVER", "STANDARD"}
	franchiseStatuses   = []string{"ONBOARDING", "ACTIVE", "SUSPENDED", "TERMINATED"}
	settlementCycles    = []string{"WEEKLY", "FORTNIGHTLY", "MONTHLY"}
	agreementStatuses   = []string{"DRAFT", "ACTIVE", "EXPIRED", "TERMINATED"}
)

type createFranchiseRequest struct {
	Code            string         `json:"code"`
	Name            string         `json:"name"`
	OperatingUnitID string         `json:"operatingUnitId"`
	Category        string         `json:"category,omitempty"`
	OwnerName       string         `json:"ownerName"`
	OwnerUserID     string         `json:"ownerUserId,omitempty"`
	OwnerPhone      string         `json:"ownerPhone"`
	OwnerEmail      string         `json:"ownerEmail,omitempty"`
	GSTNumber       string         `json:"gstNumber,omitempty"`
	PANNumber       string         `json:"panNumber,omitempty"`
	Status          string         `json:"status,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type updateFranchiseRequest struct {
	Name            *string        `json:"name,omitempty"`
	Category        *string        `json:"category,omitempty"`
	OwnerName       *string        `json:"ownerName,omitempty"`
	OwnerUserID     *string        `json:"ownerUserId,omitempty"`
	OwnerPhone      *string        `json:"ownerPhone,omitempty"`
	OwnerEmail      *string        `json:"ownerEmail,omitempty"`
	GSTNumber       *string        `json:"gstNumber,omitempty"`
	PANNumber       *string        `json:"panNumber,omitempty"`
	Status          *string        `json:"status,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	ExpectedVersion int32          `json:"expectedVersion"`
}

type createAgreementRequest struct {
	AgreementNumber      string         `json:"agreementNumber"`
	EffectiveFrom        time.Time      `json:"effectiveFrom"`
	EffectiveTo          *time.Time     `json:"effectiveTo,omitempty"`
	Currency             string         `json:"currency,omitempty"`
	SecurityDepositMinor int64          `json:"securityDepositMinor"`
	CreditLimitMinor     int64          `json:"creditLimitMinor"`
	CommissionPlanCode   string         `json:"commissionPlanCode,omitempty"`
	SettlementCycle      string         `json:"settlementCycle,omitempty"`
	Terms                map[string]any `json:"terms,omitempty"`
	Status               string         `json:"status,omitempty"`
	SignedAt             *time.Time     `json:"signedAt,omitempty"`
}

func (h *Handler) listFranchises(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, page, err := listWindow(r)
	if err != nil {
		return err
	}
	params := dbgen.ListFranchisesParams{
		OrganizationID: p.OrganizationID,
		RowLimit:       int32(limit), RowOffset: int32((page - 1) * limit),
	}
	if status, sErr := httpx.QueryEnum(r, "status", franchiseStatuses); sErr != nil {
		return sErr
	} else if status != "" {
		params.Status = &status
	}
	if cat, cErr := httpx.QueryEnum(r, "category", franchiseCategories); cErr != nil {
		return cErr
	} else if cat != "" {
		params.Category = &cat
	}
	if s := httpx.Query(r, "search"); s != "" {
		params.Search = &s
	}
	// A franchise owner or operator sees only their own franchise.
	if scope := p.UnitScope("franchise.manage"); scope != nil {
		params.ScopedUnitIds = scope
	}

	rows, err := h.svc.q.ListFranchises(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]map[string]any, 0, len(rows))
	for _, f := range rows {
		total = f.TotalCount
		items = append(items, map[string]any{
			"id": f.PublicID, "code": f.Code, "name": f.Name, "category": f.Category,
			"status": f.Status, "ownerName": f.OwnerName, "ownerPhone": f.OwnerPhone,
			"operatingUnit": map[string]string{"code": f.UnitCode, "name": f.UnitName, "pincode": f.UnitPincode},
			"onboardedAt":   f.OnboardedAt, "version": f.Version,
		})
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "code", "asc"))
}

func (h *Handler) getFranchise(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	franchiseID, err := httpx.PathPublicID(r, "franchiseId", publicid.PrefixFranchise, "Franchise")
	if err != nil {
		return err
	}
	f, err := h.svc.q.GetFranchiseByPublicID(r.Context(), dbgen.GetFranchiseByPublicIDParams{
		PublicID: franchiseID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Franchise")
		}
		return apierr.Internal(err)
	}
	if err := p.RequireUnitInScope(f.OperatingUnitID); err != nil && !p.Can("franchise.manage") {
		return err
	}
	out := map[string]any{
		"id": f.PublicID, "code": f.Code, "name": f.Name, "category": f.Category, "status": f.Status,
		"ownerName": f.OwnerName, "ownerPhone": f.OwnerPhone, "ownerEmail": f.OwnerEmail,
		"gstNumber": f.GstNumber, "panNumber": f.PanNumber,
		"operatingUnit": map[string]any{
			"code": f.UnitCode, "name": f.UnitName, "unitType": f.UnitType, "pincode": f.UnitPincode,
		},
		"onboardedAt": f.OnboardedAt, "terminatedAt": f.TerminatedAt,
		"metadata": decodeJSONMap(f.Metadata), "version": f.Version,
		"createdAt": f.CreatedAt, "updatedAt": f.UpdatedAt,
	}
	if f.OwnerUserPublicID != nil {
		out["ownerUser"] = map[string]string{"id": *f.OwnerUserPublicID, "email": derefStr(f.OwnerUserEmail)}
	}
	return httpx.OK(w, out)
}

func (h *Handler) createFranchise(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createFranchiseRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	code := v.Code("code", req.Code)
	name := v.Text("name", req.Name, 2, 160, true)
	ownerName := v.Text("ownerName", req.OwnerName, 2, 160, true)
	ownerPhone := v.Phone("ownerPhone", req.OwnerPhone, true)
	unitPublicID := v.PublicID("operatingUnitId", req.OperatingUnitID, publicid.PrefixOperatingUnit, true)
	category := "STANDARD"
	if req.Category != "" {
		category = v.Enum("category", req.Category, franchiseCategories, true)
	}
	status := "ACTIVE"
	if req.Status != "" {
		status = v.Enum("status", req.Status, franchiseStatuses, true)
	}
	var ownerEmail string
	if req.OwnerEmail != "" {
		ownerEmail = v.Email("ownerEmail", req.OwnerEmail)
	}
	// Validated against the organization's country, not against India. A
	// Nigerian operator supplies a TIN and an RC number; asking them for a
	// GSTIN and refusing what they type is how a market gets blocked on its
	// first franchise.
	gst := v.TaxRegistration("gstNumber", req.GSTNumber, p.OrganizationCountry, false)
	pan := v.BusinessRegistration("panNumber", req.PANNumber, p.OrganizationCountry, false)
	if err := v.Err(); err != nil {
		return err
	}

	unit, err := h.svc.q.GetOperatingUnitByPublicID(r.Context(), dbgen.GetOperatingUnitByPublicIDParams{
		PublicID: unitPublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.Validation("The operating unit does not exist.", nil)
		}
		return apierr.Internal(err)
	}
	if unit.UnitType != UnitFranchiseBranch {
		return apierr.Validation("A franchise must be attached to a FRANCHISE_BRANCH operating unit.",
			map[string]any{"unitType": unit.UnitType})
	}

	params := dbgen.CreateFranchiseParams{
		PublicID: publicid.New(publicid.PrefixFranchise), OrganizationID: p.OrganizationID,
		Code: code, Name: name, OperatingUnitID: unit.ID, Category: category,
		OwnerName: ownerName, OwnerPhone: ownerPhone, OwnerEmail: optional(ownerEmail),
		GstNumber: optional(gst), PanNumber: optional(pan), Status: status,
		Metadata: encodeJSONMap(req.Metadata), CreatedBy: &p.UserID,
	}
	if status == "ACTIVE" {
		now := time.Now()
		params.OnboardedAt = &now
	}
	if req.OwnerUserID != "" {
		owner, oErr := h.svc.q.GetUserByPublicID(r.Context(), dbgen.GetUserByPublicIDParams{
			PublicID: req.OwnerUserID, OrganizationID: p.OrganizationID,
		})
		if oErr != nil {
			return apierr.Validation("The owner user does not exist in your organization.", nil)
		}
		params.OwnerUserID = &owner.ID
	}

	f, err := h.svc.q.CreateFranchise(r.Context(), params)
	if err != nil {
		switch {
		case database.IsUniqueViolation(err, "franchises_code_unique"):
			return apierr.Duplicate("A franchise with this code already exists.")
		case database.IsUniqueViolation(err, "franchises_operating_unit_id_key"):
			return apierr.Duplicate("This operating unit is already operated by another franchise.")
		}
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionFranchiseCreated, ResourceType: "franchise",
		ResourceID: &f.ID, ResourcePublicID: f.PublicID, OperatingUnitID: &unit.ID,
		After: map[string]any{"code": f.Code, "name": f.Name, "category": f.Category, "status": f.Status},
	}))
	return httpx.Created(w, "/api/v1/network/franchises/"+f.PublicID, map[string]any{
		"id": f.PublicID, "code": f.Code, "name": f.Name,
		"status": f.Status, "category": f.Category, "version": f.Version,
	})
}

func (h *Handler) updateFranchise(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	franchiseID, err := httpx.PathPublicID(r, "franchiseId", publicid.PrefixFranchise, "Franchise")
	if err != nil {
		return err
	}
	var req updateFranchiseRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.ExpectedVersion < 1 {
		return apierr.Validation("expectedVersion is required and must be the version you last read.", nil)
	}
	existing, err := h.svc.q.GetFranchiseByPublicID(r.Context(), dbgen.GetFranchiseByPublicIDParams{
		PublicID: franchiseID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Franchise")
		}
		return apierr.Internal(err)
	}

	v := validate.New()
	params := dbgen.UpdateFranchiseParams{
		PublicID: franchiseID, OrganizationID: p.OrganizationID, ExpectedVersion: req.ExpectedVersion,
	}
	if req.Name != nil {
		n := v.Text("name", *req.Name, 2, 160, true)
		params.Name = &n
	}
	if req.Category != nil {
		c := v.Enum("category", *req.Category, franchiseCategories, true)
		params.Category = &c
	}
	if req.OwnerName != nil {
		o := v.Text("ownerName", *req.OwnerName, 2, 160, true)
		params.OwnerName = &o
	}
	if req.OwnerPhone != nil {
		ph := v.Phone("ownerPhone", *req.OwnerPhone, true)
		params.OwnerPhone = &ph
	}
	if req.OwnerEmail != nil {
		em := v.Email("ownerEmail", *req.OwnerEmail)
		params.OwnerEmail = &em
	}
	if req.GSTNumber != nil {
		g := v.TaxRegistration("gstNumber", *req.GSTNumber, p.OrganizationCountry, false)
		params.GstNumber = &g
	}
	if req.PANNumber != nil {
		pn := v.BusinessRegistration("panNumber", *req.PANNumber, p.OrganizationCountry, false)
		params.PanNumber = &pn
	}
	if req.Status != nil {
		st := v.Enum("status", *req.Status, franchiseStatuses, true)
		params.Status = &st
	}
	if req.Metadata != nil {
		params.Metadata = encodeJSONMap(req.Metadata)
	}
	if req.OwnerUserID != nil && *req.OwnerUserID != "" {
		owner, oErr := h.svc.q.GetUserByPublicID(r.Context(), dbgen.GetUserByPublicIDParams{
			PublicID: *req.OwnerUserID, OrganizationID: p.OrganizationID,
		})
		if oErr != nil {
			v.Add("ownerUserId", "The owner user does not exist in your organization.")
		} else {
			params.OwnerUserID = &owner.ID
		}
	}
	if err := v.Err(); err != nil {
		return err
	}

	f, err := h.svc.q.UpdateFranchise(r.Context(), params)
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.Conflict(apierr.CodeConcurrentModification,
				"This franchise was modified by someone else. Reload it and try again.").
				WithDetail("expectedVersion", req.ExpectedVersion).
				WithDetail("currentVersion", existing.Version)
		}
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionFranchiseUpdated, ResourceType: "franchise",
		ResourceID: &f.ID, ResourcePublicID: f.PublicID, OperatingUnitID: &f.OperatingUnitID,
		Before: map[string]any{"name": existing.Name, "status": existing.Status, "category": existing.Category},
		After:  map[string]any{"name": f.Name, "status": f.Status, "category": f.Category},
	}))
	return httpx.OK(w, map[string]any{
		"id": f.PublicID, "code": f.Code, "name": f.Name,
		"status": f.Status, "category": f.Category, "version": f.Version,
	})
}

func (h *Handler) listAgreements(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	franchiseID, err := httpx.PathPublicID(r, "franchiseId", publicid.PrefixFranchise, "Franchise")
	if err != nil {
		return err
	}
	f, err := h.svc.q.GetFranchiseByPublicID(r.Context(), dbgen.GetFranchiseByPublicIDParams{
		PublicID: franchiseID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Franchise")
		}
		return apierr.Internal(err)
	}
	limit, page, err := listWindow(r)
	if err != nil {
		return err
	}
	rows, err := h.svc.q.ListFranchiseAgreements(r.Context(), dbgen.ListFranchiseAgreementsParams{
		OrganizationID: p.OrganizationID, FranchiseID: &f.ID,
		RowLimit: int32(limit), RowOffset: int32((page - 1) * limit),
	})
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]map[string]any, 0, len(rows))
	for _, a := range rows {
		total = a.TotalCount
		items = append(items, agreementListView(a))
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "effectiveFrom", "desc"))
}

func (h *Handler) createAgreement(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	franchiseID, err := httpx.PathPublicID(r, "franchiseId", publicid.PrefixFranchise, "Franchise")
	if err != nil {
		return err
	}
	var req createAgreementRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	f, err := h.svc.q.GetFranchiseByPublicID(r.Context(), dbgen.GetFranchiseByPublicIDParams{
		PublicID: franchiseID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Franchise")
		}
		return apierr.Internal(err)
	}

	v := validate.New()
	number := v.Text("agreementNumber", req.AgreementNumber, 3, 64, true)
	if req.EffectiveFrom.IsZero() {
		v.Add("effectiveFrom", "This field is required.")
	}
	v.EffectiveRange("effectiveFrom", "effectiveTo", &req.EffectiveFrom, req.EffectiveTo)
	v.NonNegativeMinor("securityDepositMinor", req.SecurityDepositMinor)
	v.NonNegativeMinor("creditLimitMinor", req.CreditLimitMinor)
	// Default to the organization's own currency, and accept any currency the
	// ledger supports. The previous literal list omitted NGN entirely, so a
	// Nigerian tenant could not write a franchise agreement in its own money;
	// reading the supported list from one place is what stops that recurring.
	currency := p.OrganizationCurrency
	if currency == "" {
		currency = string(money.NGN)
	}
	if req.Currency != "" {
		currency = v.Enum("currency", req.Currency, money.SupportedCodes(), true)
	}
	cycle := "MONTHLY"
	if req.SettlementCycle != "" {
		cycle = v.Enum("settlementCycle", req.SettlementCycle, settlementCycles, true)
	}
	status := "DRAFT"
	if req.Status != "" {
		status = v.Enum("status", req.Status, agreementStatuses, true)
	}
	if err := v.Err(); err != nil {
		return err
	}

	a, err := h.svc.q.CreateFranchiseAgreement(r.Context(), dbgen.CreateFranchiseAgreementParams{
		PublicID: publicid.New(publicid.PrefixAgreement), OrganizationID: p.OrganizationID,
		FranchiseID: f.ID, AgreementNumber: number, Status: status,
		EffectiveFrom: req.EffectiveFrom, EffectiveTo: req.EffectiveTo, Currency: currency,
		SecurityDepositMinor: req.SecurityDepositMinor, CreditLimitMinor: req.CreditLimitMinor,
		CommissionPlanCode: optional(req.CommissionPlanCode), SettlementCycle: cycle,
		Terms: encodeJSONMap(req.Terms), SignedAt: req.SignedAt, CreatedBy: &p.UserID,
	})
	if err != nil {
		switch {
		case database.IsUniqueViolation(err, "franchise_agreements_number_unique"):
			return apierr.Duplicate("An agreement with this number already exists.")
		case database.IsUniqueViolation(err, "franchise_agreements_active_idx"):
			return apierr.Conflict(apierr.CodeConflict,
				"This franchise already has an active agreement. Expire it before activating another.")
		}
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionAgreementCreated, ResourceType: "franchise_agreement",
		ResourceID: &a.ID, ResourcePublicID: a.PublicID, OperatingUnitID: &f.OperatingUnitID,
		After: map[string]any{
			"agreementNumber": a.AgreementNumber, "status": a.Status,
			"creditLimitMinor": a.CreditLimitMinor, "settlementCycle": a.SettlementCycle,
		},
	}))
	return httpx.Created(w, "", agreementView(a))
}

// agreementListView projects a list row; sqlc flattens SELECT a.*, count(*)
// OVER () into its own row type rather than embedding the table struct.
func agreementListView(a dbgen.ListFranchiseAgreementsRow) map[string]any {
	out := map[string]any{
		"id": a.PublicID, "agreementNumber": a.AgreementNumber, "status": a.Status,
		"effectiveFrom": a.EffectiveFrom, "effectiveTo": a.EffectiveTo,
		"currency": a.Currency, "securityDepositMinor": a.SecurityDepositMinor,
		"creditLimitMinor": a.CreditLimitMinor, "settlementCycle": a.SettlementCycle,
		"terms": decodeJSONMap(a.Terms), "signedAt": a.SignedAt, "createdAt": a.CreatedAt,
	}
	if a.CommissionPlanCode != nil {
		out["commissionPlanCode"] = *a.CommissionPlanCode
	}
	return out
}

func agreementView(a dbgen.FranchiseAgreement) map[string]any {
	out := map[string]any{
		"id": a.PublicID, "agreementNumber": a.AgreementNumber, "status": a.Status,
		"effectiveFrom": a.EffectiveFrom, "effectiveTo": a.EffectiveTo,
		"currency": a.Currency, "securityDepositMinor": a.SecurityDepositMinor,
		"creditLimitMinor": a.CreditLimitMinor, "settlementCycle": a.SettlementCycle,
		"terms": decodeJSONMap(a.Terms), "signedAt": a.SignedAt, "createdAt": a.CreatedAt,
	}
	if a.CommissionPlanCode != nil {
		out["commissionPlanCode"] = *a.CommissionPlanCode
	}
	return out
}

func encodeJSONMap(m map[string]any) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func decodeJSONMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}
