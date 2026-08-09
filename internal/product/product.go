// Package product owns configurable courier services (M05).
//
// Constitution §M05: product names must never be encoded in business logic.
// Everything that distinguishes one product from another — mode, weight and
// dimension envelope, volumetric divisor, COD and insurance eligibility, SLA,
// cut-off — is a column, so launching a new service is configuration rather
// than a deployment.
package product

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

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

// Modes is the transport-mode enum.
var Modes = []string{"AIR", "SURFACE", "RAIL", "LOCAL"}

// Service exposes courier product reads and administration.
type Service struct {
	db  *database.DB
	q   *dbgen.Queries
	log *slog.Logger
}

// NewService builds the product service.
func NewService(db *database.DB, q *dbgen.Queries, log *slog.Logger) *Service {
	return &Service{db: db, q: q, log: log}
}

// GetActiveByCode resolves a product for booking or quoting at a point in time.
func (s *Service) GetActiveByCode(ctx context.Context, orgID int64, code string, asOf time.Time) (*dbgen.CourierService, error) {
	svc, err := s.q.GetActiveCourierServiceByCode(ctx, dbgen.GetActiveCourierServiceByCodeParams{
		OrganizationID: orgID, Code: code, AsOf: asOf,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.Validation("This courier service is not available.",
				map[string]any{"serviceCode": code})
		}
		return nil, apierr.Internal(fmt.Errorf("load courier service: %w", err))
	}
	return &svc, nil
}

// SLARules is the structured form of courier_services.sla_rules.
type SLARules struct {
	CutoffTime           string `json:"cutoffTime,omitempty"`
	ExcludeSundays       bool   `json:"excludeSundays,omitempty"`
	ExcludeHolidays      bool   `json:"excludeHolidays,omitempty"`
	RemoteAreaExtraHours int    `json:"remoteAreaExtraHours,omitempty"`
	MinTransitHours      int    `json:"minTransitHours,omitempty"`
}

// ParseSLARules decodes the JSON policy block, returning zero values when it is
// absent or malformed rather than failing a booking on configuration noise.
func ParseSLARules(raw []byte) SLARules {
	var out SLARules
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// ---- handlers --------------------------------------------------------------

// Handler exposes courier product administration.
type Handler struct {
	svc   *Service
	audit *audit.Recorder
}

// NewHandler builds the product handler.
func NewHandler(svc *Service, rec *audit.Recorder) *Handler {
	return &Handler{svc: svc, audit: rec}
}

// Routes mounts product endpoints under /courier-services.
func (h *Handler) Routes(r chi.Router) {
	r.With(auth.RequirePermission("courier_service.read")).Get("/", httpx.Wrap(h.list))
	r.With(auth.RequirePermission("courier_service.manage")).Post("/", httpx.Wrap(h.create))
	r.With(auth.RequirePermission("courier_service.read")).Get("/{serviceId}", httpx.Wrap(h.get))
	r.With(auth.RequirePermission("courier_service.manage")).Patch("/{serviceId}", httpx.Wrap(h.update))
	r.With(auth.RequirePermission("courier_service.manage")).Post("/{serviceId}/deactivate", httpx.Wrap(h.deactivate))
}

// View is the API representation of a courier product.
type View struct {
	ID                    string     `json:"id"`
	Code                  string     `json:"code"`
	Name                  string     `json:"name"`
	Description           string     `json:"description,omitempty"`
	Mode                  string     `json:"mode"`
	MinWeightGrams        int32      `json:"minWeightGrams"`
	MaxWeightGrams        int32      `json:"maxWeightGrams"`
	MaxLengthMM           *int32     `json:"maxLengthMm,omitempty"`
	MaxWidthMM            *int32     `json:"maxWidthMm,omitempty"`
	MaxHeightMM           *int32     `json:"maxHeightMm,omitempty"`
	MaxDimensionSumMM     *int32     `json:"maxDimensionSumMm,omitempty"`
	VolumetricDivisor     int32      `json:"volumetricDivisor"`
	WeightRoundingGrams   int32      `json:"weightRoundingGrams"`
	CODAllowed            bool       `json:"codAllowed"`
	MaxCODAmountMinor     *int64     `json:"maxCodAmountMinor,omitempty"`
	InsuranceAllowed      bool       `json:"insuranceAllowed"`
	MaxDeclaredValueMinor *int64     `json:"maxDeclaredValueMinor,omitempty"`
	SLATransitHours       int32      `json:"slaTransitHours"`
	SLARules              SLARules   `json:"slaRules"`
	CutoffTime            string     `json:"cutoffTime,omitempty"`
	Status                string     `json:"status"`
	EffectiveFrom         time.Time  `json:"effectiveFrom"`
	EffectiveTo           *time.Time `json:"effectiveTo,omitempty"`
	SortOrder             int32      `json:"sortOrder"`
	Version               int32      `json:"version"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

type createRequest struct {
	Code                  string     `json:"code"`
	Name                  string     `json:"name"`
	Description           string     `json:"description,omitempty"`
	Mode                  string     `json:"mode"`
	MinWeightGrams        int        `json:"minWeightGrams"`
	MaxWeightGrams        int        `json:"maxWeightGrams"`
	MaxLengthMM           *int       `json:"maxLengthMm,omitempty"`
	MaxWidthMM            *int       `json:"maxWidthMm,omitempty"`
	MaxHeightMM           *int       `json:"maxHeightMm,omitempty"`
	MaxDimensionSumMM     *int       `json:"maxDimensionSumMm,omitempty"`
	VolumetricDivisor     int        `json:"volumetricDivisor"`
	WeightRoundingGrams   int        `json:"weightRoundingGrams"`
	CODAllowed            bool       `json:"codAllowed"`
	MaxCODAmountMinor     *int64     `json:"maxCodAmountMinor,omitempty"`
	InsuranceAllowed      bool       `json:"insuranceAllowed"`
	MaxDeclaredValueMinor *int64     `json:"maxDeclaredValueMinor,omitempty"`
	SLATransitHours       int        `json:"slaTransitHours"`
	SLARules              *SLARules  `json:"slaRules,omitempty"`
	CutoffTime            string     `json:"cutoffTime,omitempty"`
	EffectiveFrom         *time.Time `json:"effectiveFrom,omitempty"`
	EffectiveTo           *time.Time `json:"effectiveTo,omitempty"`
	SortOrder             int        `json:"sortOrder,omitempty"`
}

type updateRequest struct {
	Name                  *string    `json:"name,omitempty"`
	Description           *string    `json:"description,omitempty"`
	Mode                  *string    `json:"mode,omitempty"`
	MinWeightGrams        *int       `json:"minWeightGrams,omitempty"`
	MaxWeightGrams        *int       `json:"maxWeightGrams,omitempty"`
	MaxLengthMM           *int       `json:"maxLengthMm,omitempty"`
	MaxWidthMM            *int       `json:"maxWidthMm,omitempty"`
	MaxHeightMM           *int       `json:"maxHeightMm,omitempty"`
	MaxDimensionSumMM     *int       `json:"maxDimensionSumMm,omitempty"`
	VolumetricDivisor     *int       `json:"volumetricDivisor,omitempty"`
	WeightRoundingGrams   *int       `json:"weightRoundingGrams,omitempty"`
	CODAllowed            *bool      `json:"codAllowed,omitempty"`
	MaxCODAmountMinor     *int64     `json:"maxCodAmountMinor,omitempty"`
	InsuranceAllowed      *bool      `json:"insuranceAllowed,omitempty"`
	MaxDeclaredValueMinor *int64     `json:"maxDeclaredValueMinor,omitempty"`
	SLATransitHours       *int       `json:"slaTransitHours,omitempty"`
	SLARules              *SLARules  `json:"slaRules,omitempty"`
	CutoffTime            *string    `json:"cutoffTime,omitempty"`
	EffectiveTo           *time.Time `json:"effectiveTo,omitempty"`
	SortOrder             *int       `json:"sortOrder,omitempty"`
	Status                *string    `json:"status,omitempty"`
	ExpectedVersion       int32      `json:"expectedVersion"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, err := pagination.ClampLimit(httpx.Query(r, "limit"))
	if err != nil {
		return err
	}
	page, err := httpx.QueryInt(r, "page", 1, 1, 1000)
	if err != nil {
		return err
	}
	params := dbgen.ListCourierServicesParams{
		OrganizationID: p.OrganizationID,
		RowLimit:       int32(limit), RowOffset: int32((page - 1) * limit),
	}
	if status, sErr := httpx.QueryEnum(r, "status", []string{"ACTIVE", "INACTIVE"}); sErr != nil {
		return sErr
	} else if status != "" {
		params.Status = &status
	}
	if mode, mErr := httpx.QueryEnum(r, "mode", Modes); mErr != nil {
		return mErr
	} else if mode != "" {
		params.Mode = &mode
	}
	if httpx.Query(r, "codAllowed") != "" {
		cod, cErr := httpx.QueryBool(r, "codAllowed", false)
		if cErr != nil {
			return cErr
		}
		params.CodAllowed = &cod
	}
	if s := httpx.Query(r, "search"); s != "" {
		params.Search = &s
	}

	rows, err := h.svc.q.ListCourierServices(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]View, 0, len(rows))
	for _, row := range rows {
		total = row.TotalCount
		items = append(items, toView(listRowToService(row)))
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "sortOrder", "asc"))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	servicePublicID, err := httpx.PathPublicID(r, "serviceId", publicid.PrefixCourierService, "Courier service")
	if err != nil {
		return err
	}
	svc, err := h.svc.q.GetCourierServiceByPublicID(r.Context(), dbgen.GetCourierServiceByPublicIDParams{
		PublicID: servicePublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Courier service")
		}
		return apierr.Internal(err)
	}
	return httpx.OK(w, toView(svc))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	code := v.Code("code", req.Code)
	name := v.Text("name", req.Name, 2, 120, true)
	description := v.Text("description", req.Description, 0, 1000, false)
	mode := v.Enum("mode", req.Mode, Modes, true)
	if req.MinWeightGrams == 0 {
		req.MinWeightGrams = 1
	}
	v.IntRange("minWeightGrams", req.MinWeightGrams, 1, 1_000_000)
	v.IntRange("maxWeightGrams", req.MaxWeightGrams, 1, 10_000_000)
	if req.MaxWeightGrams < req.MinWeightGrams {
		v.Add("maxWeightGrams", "Must be greater than or equal to minWeightGrams.")
	}
	if req.VolumetricDivisor == 0 {
		req.VolumetricDivisor = 5000
	}
	v.IntRange("volumetricDivisor", req.VolumetricDivisor, 1, 100_000)
	if req.WeightRoundingGrams == 0 {
		req.WeightRoundingGrams = 500
	}
	v.IntRange("weightRoundingGrams", req.WeightRoundingGrams, 1, 100_000)
	v.IntRange("slaTransitHours", req.SLATransitHours, 1, 8760)
	cutoff := validateCutoff(v, "cutoffTime", req.CutoffTime)
	if req.MaxCODAmountMinor != nil {
		v.NonNegativeMinor("maxCodAmountMinor", *req.MaxCODAmountMinor)
		if !req.CODAllowed {
			v.Add("maxCodAmountMinor", "Cannot be set when codAllowed is false.")
		}
	}
	if req.MaxDeclaredValueMinor != nil {
		v.NonNegativeMinor("maxDeclaredValueMinor", *req.MaxDeclaredValueMinor)
	}
	if err := v.Err(); err != nil {
		return err
	}

	slaRules := []byte("{}")
	if req.SLARules != nil {
		slaRules, _ = json.Marshal(req.SLARules)
	}
	params := dbgen.CreateCourierServiceParams{
		PublicID: publicid.New(publicid.PrefixCourierService), OrganizationID: p.OrganizationID,
		Code: code, Name: name, Description: description, Mode: mode,
		MinWeightGrams: int32(req.MinWeightGrams), MaxWeightGrams: int32(req.MaxWeightGrams),
		MaxLengthMm: int32Ptr(req.MaxLengthMM), MaxWidthMm: int32Ptr(req.MaxWidthMM),
		MaxHeightMm: int32Ptr(req.MaxHeightMM), MaxDimensionSumMm: int32Ptr(req.MaxDimensionSumMM),
		VolumetricDivisor: int32(req.VolumetricDivisor), WeightRoundingGrams: int32(req.WeightRoundingGrams),
		CodAllowed: req.CODAllowed, MaxCodAmountMinor: req.MaxCODAmountMinor,
		InsuranceAllowed: req.InsuranceAllowed, MaxDeclaredValueMinor: req.MaxDeclaredValueMinor,
		SlaTransitHours: int32(req.SLATransitHours), SlaRules: slaRules,
		CutoffTime: cutoff, EffectiveFrom: time.Now(), EffectiveTo: req.EffectiveTo,
		SortOrder: int32(req.SortOrder), CreatedBy: &p.UserID,
	}
	if req.EffectiveFrom != nil {
		params.EffectiveFrom = *req.EffectiveFrom
	}

	svc, err := h.svc.q.CreateCourierService(r.Context(), params)
	if err != nil {
		if database.IsUniqueViolation(err, "courier_services_code_unique") {
			return apierr.Duplicate("A courier service with this code already exists.")
		}
		return apierr.Internal(fmt.Errorf("create courier service: %w", err))
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionCourierServiceCreated, ResourceType: "courier_service",
		ResourceID: &svc.ID, ResourcePublicID: svc.PublicID,
		After: map[string]any{"code": svc.Code, "name": svc.Name, "mode": svc.Mode},
	}))
	return httpx.Created(w, "/api/v1/courier-services/"+svc.PublicID, toView(svc))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	servicePublicID, err := httpx.PathPublicID(r, "serviceId", publicid.PrefixCourierService, "Courier service")
	if err != nil {
		return err
	}
	var req updateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.ExpectedVersion < 1 {
		return apierr.Validation("expectedVersion is required and must be the version you last read.", nil)
	}
	existing, err := h.svc.q.GetCourierServiceByPublicID(r.Context(), dbgen.GetCourierServiceByPublicIDParams{
		PublicID: servicePublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Courier service")
		}
		return apierr.Internal(err)
	}

	v := validate.New()
	params := dbgen.UpdateCourierServiceParams{
		PublicID: servicePublicID, OrganizationID: p.OrganizationID, ExpectedVersion: req.ExpectedVersion,
	}
	if req.Name != nil {
		n := v.Text("name", *req.Name, 2, 120, true)
		params.Name = &n
	}
	if req.Description != nil {
		d := v.Text("description", *req.Description, 0, 1000, false)
		params.Description = &d
	}
	if req.Mode != nil {
		m := v.Enum("mode", *req.Mode, Modes, true)
		params.Mode = &m
	}
	// Weight bounds are validated against the resulting pair, not just the
	// supplied field, so a partial update cannot invert the range.
	minW, maxW := existing.MinWeightGrams, existing.MaxWeightGrams
	if req.MinWeightGrams != nil {
		v.IntRange("minWeightGrams", *req.MinWeightGrams, 1, 1_000_000)
		minW = int32(*req.MinWeightGrams)
		params.MinWeightGrams = &minW
	}
	if req.MaxWeightGrams != nil {
		v.IntRange("maxWeightGrams", *req.MaxWeightGrams, 1, 10_000_000)
		maxW = int32(*req.MaxWeightGrams)
		params.MaxWeightGrams = &maxW
	}
	if maxW < minW {
		v.Add("maxWeightGrams", "Must be greater than or equal to minWeightGrams.")
	}
	params.MaxLengthMm = int32Ptr(req.MaxLengthMM)
	params.MaxWidthMm = int32Ptr(req.MaxWidthMM)
	params.MaxHeightMm = int32Ptr(req.MaxHeightMM)
	params.MaxDimensionSumMm = int32Ptr(req.MaxDimensionSumMM)
	if req.VolumetricDivisor != nil {
		v.IntRange("volumetricDivisor", *req.VolumetricDivisor, 1, 100_000)
		vd := int32(*req.VolumetricDivisor)
		params.VolumetricDivisor = &vd
	}
	if req.WeightRoundingGrams != nil {
		v.IntRange("weightRoundingGrams", *req.WeightRoundingGrams, 1, 100_000)
		wr := int32(*req.WeightRoundingGrams)
		params.WeightRoundingGrams = &wr
	}
	params.CodAllowed = req.CODAllowed
	params.MaxCodAmountMinor = req.MaxCODAmountMinor
	params.InsuranceAllowed = req.InsuranceAllowed
	params.MaxDeclaredValueMinor = req.MaxDeclaredValueMinor
	if req.SLATransitHours != nil {
		v.IntRange("slaTransitHours", *req.SLATransitHours, 1, 8760)
		sh := int32(*req.SLATransitHours)
		params.SlaTransitHours = &sh
	}
	if req.SLARules != nil {
		raw, _ := json.Marshal(req.SLARules)
		params.SlaRules = raw
	}
	if req.CutoffTime != nil {
		params.CutoffTime = validateCutoff(v, "cutoffTime", *req.CutoffTime)
	}
	params.EffectiveTo = req.EffectiveTo
	if req.SortOrder != nil {
		so := int32(*req.SortOrder)
		params.SortOrder = &so
	}
	if req.Status != nil {
		st := v.Enum("status", *req.Status, []string{"ACTIVE", "INACTIVE"}, true)
		params.Status = &st
	}
	if err := v.Err(); err != nil {
		return err
	}

	svc, err := h.svc.q.UpdateCourierService(r.Context(), params)
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.Conflict(apierr.CodeConcurrentModification,
				"This courier service was modified by someone else. Reload it and try again.").
				WithDetail("expectedVersion", req.ExpectedVersion).
				WithDetail("currentVersion", existing.Version)
		}
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionCourierServiceUpdated, ResourceType: "courier_service",
		ResourceID: &svc.ID, ResourcePublicID: svc.PublicID,
		Before: map[string]any{"name": existing.Name, "mode": existing.Mode, "status": existing.Status},
		After:  map[string]any{"name": svc.Name, "mode": svc.Mode, "status": svc.Status},
	}))
	return httpx.OK(w, toView(svc))
}

type deactivateRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) deactivate(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	servicePublicID, err := httpx.PathPublicID(r, "serviceId", publicid.PrefixCourierService, "Courier service")
	if err != nil {
		return err
	}
	var req deactivateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	reason := v.Text("reason", req.Reason, 5, 500, true)
	if err := v.Err(); err != nil {
		return err
	}
	existing, err := h.svc.q.GetCourierServiceByPublicID(r.Context(), dbgen.GetCourierServiceByPublicIDParams{
		PublicID: servicePublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Courier service")
		}
		return apierr.Internal(err)
	}

	// A product with shipments is never deleted; deactivation preserves history
	// while removing it from booking.
	refs, err := h.svc.q.CountCourierServiceReferences(r.Context(), existing.ID)
	if err != nil {
		return apierr.Internal(err)
	}

	svc, err := h.svc.q.DeactivateCourierService(r.Context(), dbgen.DeactivateCourierServiceParams{
		PublicID: servicePublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.Conflict(apierr.CodeConflict, "This courier service is already inactive.")
		}
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionCourierServiceDeactivated, ResourceType: "courier_service",
		ResourceID: &svc.ID, ResourcePublicID: svc.PublicID, Reason: reason,
		After: map[string]any{
			"status": svc.Status, "retainedShipments": refs.ShipmentCount,
			"affectedRateEntries": refs.RateCount, "affectedRoutes": refs.RouteCount,
		},
	}))
	return httpx.OK(w, map[string]any{
		"id": svc.PublicID, "status": svc.Status,
		"retainedShipments": refs.ShipmentCount,
		"warning": map[string]any{
			"rateCardEntries": refs.RateCount,
			"activeRoutes":    refs.RouteCount,
		},
	})
}

// ---- mapping ---------------------------------------------------------------

func toView(s dbgen.CourierService) View {
	v := View{
		ID: s.PublicID, Code: s.Code, Name: s.Name, Description: s.Description, Mode: s.Mode,
		MinWeightGrams: s.MinWeightGrams, MaxWeightGrams: s.MaxWeightGrams,
		MaxLengthMM: s.MaxLengthMm, MaxWidthMM: s.MaxWidthMm, MaxHeightMM: s.MaxHeightMm,
		MaxDimensionSumMM: s.MaxDimensionSumMm,
		VolumetricDivisor: s.VolumetricDivisor, WeightRoundingGrams: s.WeightRoundingGrams,
		CODAllowed: s.CodAllowed, MaxCODAmountMinor: s.MaxCodAmountMinor,
		InsuranceAllowed: s.InsuranceAllowed, MaxDeclaredValueMinor: s.MaxDeclaredValueMinor,
		SLATransitHours: s.SlaTransitHours, SLARules: ParseSLARules(s.SlaRules),
		Status: s.Status, EffectiveFrom: s.EffectiveFrom, EffectiveTo: s.EffectiveTo,
		SortOrder: s.SortOrder, Version: s.Version, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
	if s.CutoffTime != nil {
		v.CutoffTime = *s.CutoffTime
	}
	return v
}

// listRowToService narrows a list row to the table struct; sqlc flattens
// SELECT s.*, count(*) OVER () into its own row type.
func listRowToService(r dbgen.ListCourierServicesRow) dbgen.CourierService {
	return dbgen.CourierService{
		ID: r.ID, PublicID: r.PublicID, OrganizationID: r.OrganizationID, Code: r.Code,
		Name: r.Name, Description: r.Description, Mode: r.Mode,
		MinWeightGrams: r.MinWeightGrams, MaxWeightGrams: r.MaxWeightGrams,
		MaxLengthMm: r.MaxLengthMm, MaxWidthMm: r.MaxWidthMm, MaxHeightMm: r.MaxHeightMm,
		MaxDimensionSumMm: r.MaxDimensionSumMm, VolumetricDivisor: r.VolumetricDivisor,
		WeightRoundingGrams: r.WeightRoundingGrams, CodAllowed: r.CodAllowed,
		MaxCodAmountMinor: r.MaxCodAmountMinor, InsuranceAllowed: r.InsuranceAllowed,
		MaxDeclaredValueMinor: r.MaxDeclaredValueMinor, SlaTransitHours: r.SlaTransitHours,
		SlaRules: r.SlaRules, CutoffTime: r.CutoffTime, Status: r.Status,
		EffectiveFrom: r.EffectiveFrom, EffectiveTo: r.EffectiveTo, SortOrder: r.SortOrder,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version,
	}
}

func int32Ptr(v *int) *int32 {
	if v == nil {
		return nil
	}
	out := int32(*v)
	return &out
}

// validateCutoff checks an "HH:MM" cut-off string.
func validateCutoff(v *validate.Validator, field, raw string) *string {
	if raw == "" {
		return nil
	}
	if len(raw) != 5 || raw[2] != ':' {
		v.Add(field, "Must be a 24-hour time in HH:MM format.")
		return nil
	}
	hh := int(raw[0]-'0')*10 + int(raw[1]-'0')
	mm := int(raw[3]-'0')*10 + int(raw[4]-'0')
	if raw[0] < '0' || raw[0] > '9' || raw[1] < '0' || raw[1] > '9' ||
		raw[3] < '0' || raw[3] > '9' || raw[4] < '0' || raw[4] > '9' ||
		hh > 23 || mm > 59 {
		v.Add(field, "Must be a 24-hour time in HH:MM format.")
		return nil
	}
	out := raw
	return &out
}
