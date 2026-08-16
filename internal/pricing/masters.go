package pricing

import (
	"net/http"
	"strings"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

type stateBaseRateRequest struct {
	StateID             string `json:"stateId"`
	RegionalZoneID      string `json:"regionalZoneId,omitempty"`
	ServiceCode         string `json:"serviceCode,omitempty"`
	BaseWeightGrams     int32  `json:"baseWeightGrams"`
	BaseCostMinor       int64  `json:"baseCostMinor"`
	AdditionalStepGrams int32  `json:"additionalStepGrams"`
	AdditionalCostMinor int64  `json:"additionalCostMinor"`
	IsActive            *bool  `json:"isActive,omitempty"`
}

func (h *Handler) listStateBaseRates(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	rows, err := h.engine.db.Pool.Query(r.Context(), `SELECT r.public_id,s.public_id,s.code,s.name,
		COALESCE(z.public_id,''),COALESCE(z.code,''),COALESCE(cs.code,''),r.base_weight_grams,
		r.base_cost_minor,r.additional_step_grams,r.additional_cost_minor,r.currency,r.is_active
		FROM state_base_rates r JOIN states s ON s.id=r.state_id
		LEFT JOIN zones z ON z.id=r.regional_zone_id LEFT JOIN courier_services cs ON cs.id=r.courier_service_id
		WHERE r.organization_id=$1 ORDER BY s.name,cs.code NULLS FIRST`, p.OrganizationID)
	if err != nil {
		return apierr.Internal(err)
	}
	defer rows.Close()
	data := make([]map[string]any, 0)
	for rows.Next() {
		var id, stateID, stateCode, stateName, zoneID, zoneCode, service, currency string
		var bw, sw int32
		var base, step int64
		var active bool
		if err := rows.Scan(&id, &stateID, &stateCode, &stateName, &zoneID, &zoneCode, &service, &bw, &base, &sw, &step, &currency, &active); err != nil {
			return apierr.Internal(err)
		}
		data = append(data, map[string]any{"id": id, "stateId": stateID, "stateCode": stateCode, "stateName": stateName,
			"regionalZoneId": zoneID, "regionalZoneCode": zoneCode, "serviceCode": service, "baseWeightGrams": bw,
			"baseCostMinor": base, "additionalStepGrams": sw, "additionalCostMinor": step, "currency": currency, "isActive": active})
	}
	return httpx.OK(w, map[string]any{"data": data})
}

func (h *Handler) upsertStateBaseRate(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req stateBaseRateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	if !publicid.Valid(publicid.PrefixState, req.StateID) {
		v.Add("stateId", "Select a valid Nigerian state.")
	}
	v.IntRange("baseWeightGrams", int(req.BaseWeightGrams), 1, 10_000_000)
	v.NonNegativeMinor("baseCostMinor", req.BaseCostMinor)
	v.IntRange("additionalStepGrams", int(req.AdditionalStepGrams), 1, 10_000_000)
	v.NonNegativeMinor("additionalCostMinor", req.AdditionalCostMinor)
	service := strings.ToUpper(strings.TrimSpace(req.ServiceCode))
	if err := v.Err(); err != nil {
		return err
	}
	var stateID int64
	if err := h.engine.db.Pool.QueryRow(r.Context(), `SELECT id FROM states WHERE public_id=$1`, req.StateID).Scan(&stateID); err != nil {
		return apierr.NotFound("State")
	}
	var zoneID *int64
	if req.RegionalZoneID != "" {
		var id int64
		if err := h.engine.db.Pool.QueryRow(r.Context(), `SELECT id FROM zones WHERE public_id=$1 AND organization_id=$2`, req.RegionalZoneID, p.OrganizationID).Scan(&id); err != nil {
			return apierr.NotFound("Zone")
		}
		zoneID = &id
	}
	var serviceID *int64
	if service != "" {
		var id int64
		if err := h.engine.db.Pool.QueryRow(r.Context(), `SELECT id FROM courier_services WHERE organization_id=$1 AND code=$2`, p.OrganizationID, service).Scan(&id); err != nil {
			return apierr.NotFound("Courier service")
		}
		serviceID = &id
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	var id string
	err = h.engine.db.Pool.QueryRow(r.Context(), `INSERT INTO state_base_rates(public_id,organization_id,state_id,regional_zone_id,courier_service_id,base_weight_grams,base_cost_minor,additional_step_grams,additional_cost_minor,currency,is_active,created_by)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT (organization_id,state_id,courier_service_id) DO UPDATE SET regional_zone_id=EXCLUDED.regional_zone_id,base_weight_grams=EXCLUDED.base_weight_grams,base_cost_minor=EXCLUDED.base_cost_minor,additional_step_grams=EXCLUDED.additional_step_grams,additional_cost_minor=EXCLUDED.additional_cost_minor,is_active=EXCLUDED.is_active RETURNING public_id`, publicid.New("sbr"), p.OrganizationID, stateID, zoneID, serviceID, req.BaseWeightGrams, req.BaseCostMinor, req.AdditionalStepGrams, req.AdditionalCostMinor, p.OrganizationCurrency, active, p.ActorUserID()).Scan(&id)
	if err != nil {
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{Action: "pricing.state_base_rate.saved", ResourceType: "state_base_rate", ResourcePublicID: id}))
	return httpx.OK(w, map[string]any{"id": id})
}

type packageTypeRequest struct {
	Code              string `json:"code"`
	Name              string `json:"name"`
	LengthMM          int32  `json:"lengthMm"`
	WidthMM           int32  `json:"widthMm"`
	HeightMM          int32  `json:"heightMm"`
	VolumetricDivisor int32  `json:"volumetricDivisor"`
	MaxWeightGrams    *int32 `json:"maxWeightGrams,omitempty"`
	IsActive          *bool  `json:"isActive,omitempty"`
}

func (h *Handler) listPackageTypes(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	rows, err := h.engine.db.Pool.Query(r.Context(), `SELECT public_id,code,name,length_mm,width_mm,height_mm,volumetric_divisor,max_weight_grams,is_active FROM package_types WHERE organization_id=$1 ORDER BY name`, p.OrganizationID)
	if err != nil {
		return apierr.Internal(err)
	}
	defer rows.Close()
	data := make([]map[string]any, 0)
	for rows.Next() {
		var id, code, name string
		var l, wid, ht, div int32
		var max *int32
		var active bool
		if err := rows.Scan(&id, &code, &name, &l, &wid, &ht, &div, &max, &active); err != nil {
			return apierr.Internal(err)
		}
		vol := int64(l) * int64(wid) * int64(ht) * 1000 / int64(div)
		data = append(data, map[string]any{"id": id, "code": code, "name": name, "lengthMm": l, "widthMm": wid, "heightMm": ht, "volumetricDivisor": div, "volumetricWeightGrams": vol, "maxWeightGrams": max, "isActive": active})
	}
	return httpx.OK(w, map[string]any{"data": data})
}

func (h *Handler) upsertPackageType(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req packageTypeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	code := v.Code("code", req.Code)
	name := v.Required("name", req.Name)
	v.Length("name", name, 2, 120)
	v.IntRange("volumetricDivisor", int(req.VolumetricDivisor), 1, 100000)
	for key, val := range map[string]int32{"lengthMm": req.LengthMM, "widthMm": req.WidthMM, "heightMm": req.HeightMM} {
		v.IntRange(key, int(val), 0, 100000)
	}
	if err := v.Err(); err != nil {
		return err
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	var id string
	err = h.engine.db.Pool.QueryRow(r.Context(), `INSERT INTO package_types(public_id,organization_id,code,name,length_mm,width_mm,height_mm,volumetric_divisor,max_weight_grams,is_active) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(organization_id,code) DO UPDATE SET name=EXCLUDED.name,length_mm=EXCLUDED.length_mm,width_mm=EXCLUDED.width_mm,height_mm=EXCLUDED.height_mm,volumetric_divisor=EXCLUDED.volumetric_divisor,max_weight_grams=EXCLUDED.max_weight_grams,is_active=EXCLUDED.is_active RETURNING public_id`, publicid.New("pgt"), p.OrganizationID, code, name, req.LengthMM, req.WidthMM, req.HeightMM, req.VolumetricDivisor, req.MaxWeightGrams, active).Scan(&id)
	if err != nil {
		if database.IsUniqueViolation(err, "package_types_code_unique") {
			return apierr.Duplicate("A package type with this code already exists.")
		}
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{Action: "pricing.package_type.saved", ResourceType: "package_type", ResourcePublicID: id}))
	return httpx.OK(w, map[string]any{"id": id})
}
