// Package geography owns countries, states, districts, cities, PIN codes,
// localities, tenant pricing zones and bulk import (M03).
//
// Geography below the zone level is global reference data: every courier tenant
// delivers to the same India Post PIN codes, so duplicating the dataset per
// tenant would waste memory and let tenants drift from objective fact. Zone
// membership and the remote-area flag are per tenant and live in
// pincode_zone_mappings.
package geography

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/cache"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
	"github.com/ceserve/courier-os/internal/platform/validate"
)

// DefaultCountry is the ISO-3166 alpha-2 code used when a request omits one.
//
// The home market. It is a package constant rather than configuration because
// it is read on hot paths — every pincode lookup in booking and serviceability —
// and a per-request config read there buys nothing: a deployment serving a
// different home market rebuilds anyway, and an organization that is not in the
// home market states its country explicitly.
const DefaultCountry = "NG"

// Service exposes geography lookups and zone resolution.
type Service struct {
	db      *database.DB
	q       *dbgen.Queries
	cache   *cache.Cache
	log     *slog.Logger
	metrics *telemetry.Metrics
	ttl     time.Duration
}

// NewService builds the geography service.
func NewService(db *database.DB, q *dbgen.Queries, c *cache.Cache, log *slog.Logger, m *telemetry.Metrics, ttl time.Duration) *Service {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Service{db: db, q: q, cache: c, log: log, metrics: m, ttl: ttl}
}

// Pincode is the resolved view of a PIN code used by booking, serviceability
// and pricing.
//
// This struct is serialised into the cache, so every field it carries must have
// a JSON representation — including the internal row ids. Marking them
// `json:"-"` would silently zero them on a cache hit and break every lookup
// that depends on them. The API never returns this type directly; handlers
// project it through PincodeView.
type Pincode struct {
	ID           int64    `json:"id"`
	PublicID     string   `json:"publicId"`
	Code         string   `json:"code"`
	StateID      int64    `json:"stateId"`
	StateName    string   `json:"stateName"`
	StateCode    string   `json:"stateCode"`
	GSTStateCode string   `json:"gstStateCode,omitempty"`
	DistrictName string   `json:"districtName,omitempty"`
	CityID       *int64   `json:"cityId,omitempty"`
	CityName     string   `json:"cityName,omitempty"`
	CityTier     string   `json:"cityTier,omitempty"`
	OfficeName   string   `json:"officeName,omitempty"`
	IsRemote     bool     `json:"isRemote"`
	Status       string   `json:"status"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
	CountryCode  string   `json:"countryCode"`
}

// PincodeView is the API projection: no internal identifiers.
type PincodeView struct {
	ID           string   `json:"id"`
	Code         string   `json:"code"`
	State        string   `json:"state"`
	StateCode    string   `json:"stateCode"`
	GSTStateCode string   `json:"gstStateCode,omitempty"`
	District     string   `json:"district,omitempty"`
	City         string   `json:"city,omitempty"`
	CityTier     string   `json:"cityTier,omitempty"`
	OfficeName   string   `json:"officeName,omitempty"`
	IsRemote     bool     `json:"isRemote"`
	Status       string   `json:"status"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
	CountryCode  string   `json:"countryCode"`
}

// View projects the internal record onto the public representation.
func (p *Pincode) View() PincodeView {
	return PincodeView{
		ID: p.PublicID, Code: p.Code, State: p.StateName, StateCode: p.StateCode,
		GSTStateCode: p.GSTStateCode, District: p.DistrictName, City: p.CityName,
		CityTier: p.CityTier, OfficeName: p.OfficeName, IsRemote: p.IsRemote,
		Status: p.Status, Latitude: p.Latitude, Longitude: p.Longitude,
		CountryCode: p.CountryCode,
	}
}

// LookupPincode resolves a PIN code, caching the result.
//
// This is the hottest read in the platform — at least twice per booking and
// once per serviceability check — and PIN code records change perhaps monthly,
// so caching is a clear win. Invalidation is explicit: the import job and the
// admin update path both drop the affected key.
func (s *Service) LookupPincode(ctx context.Context, code, countryISO2 string) (*Pincode, error) {
	if countryISO2 == "" {
		countryISO2 = DefaultCountry
	}
	countryISO2 = validate.AddressCountry(countryISO2, DefaultCountry)
	v := validate.New()
	code = v.PostalCode("pincode", code, countryISO2)
	if err := v.Err(); err != nil {
		return nil, err
	}
	key := s.pincodeCacheKey(countryISO2, code)

	var cached Pincode
	if err := s.cache.GetJSON(ctx, key, &cached); err == nil {
		s.metrics.RecordCache("pincode", "hit")
		return &cached, nil
	}
	s.metrics.RecordCache("pincode", "miss")

	row, err := s.q.LookupPincode(ctx, dbgen.LookupPincodeParams{Code: code, CountryIso2: countryISO2})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.NotFound("Postal code")
		}
		return nil, apierr.Internal(fmt.Errorf("lookup pincode: %w", err))
	}

	p := Pincode{
		ID: row.ID, PublicID: row.PublicID, Code: row.Code, StateID: row.StateID,
		StateName: row.StateName, StateCode: row.StateCode, CityID: row.CityID,
		IsRemote: row.IsRemote, Status: row.Status,
		Latitude: row.Latitude, Longitude: row.Longitude, CountryCode: row.CountryIso2,
	}
	if row.GstStateCode != nil {
		p.GSTStateCode = *row.GstStateCode
	}
	if row.DistrictName != nil {
		p.DistrictName = *row.DistrictName
	}
	if row.CityName != nil {
		p.CityName = *row.CityName
	}
	if row.CityTier != nil {
		p.CityTier = *row.CityTier
	}
	if row.OfficeName != nil {
		p.OfficeName = *row.OfficeName
	}
	s.cache.SetJSON(ctx, key, p, s.ttl)
	return &p, nil
}

// RequireActivePincode resolves a PIN code and rejects a deactivated one.
func (s *Service) RequireActivePincode(ctx context.Context, code, countryISO2 string) (*Pincode, error) {
	p, err := s.LookupPincode(ctx, code, countryISO2)
	if err != nil {
		return nil, err
	}
	if p.Status != "ACTIVE" {
		return nil, apierr.Validation("This PIN code is no longer in service.",
			map[string]any{"pincode": code})
	}
	return p, nil
}

// ZoneAssignment is a tenant's zone decision for a PIN code.
//
// Like Pincode, this is a cached struct: the internal zone id must survive a
// round trip through JSON, so it is named rather than omitted.
type ZoneAssignment struct {
	ZoneID       int64  `json:"zoneId"`
	ZonePublicID string `json:"id"`
	ZoneCode     string `json:"code"`
	ZoneName     string `json:"name"`
	ZoneType     string `json:"type"`
	IsRemote     bool   `json:"isRemote"`
}

// ResolveZone returns the tenant's zone for a PIN code at a point in time.
//
// The effective remote flag is the tenant override when present, falling back
// to the platform-wide default carried on the PIN code itself.
func (s *Service) ResolveZone(ctx context.Context, orgID, pincodeID int64, asOf time.Time) (*ZoneAssignment, error) {
	key := s.zoneCacheKey(orgID, pincodeID)
	var cached ZoneAssignment
	if err := s.cache.GetJSON(ctx, key, &cached); err == nil {
		s.metrics.RecordCache("zone", "hit")
		return &cached, nil
	}
	s.metrics.RecordCache("zone", "miss")

	row, err := s.q.ResolveZoneForPincode(ctx, dbgen.ResolveZoneForPincodeParams{
		OrganizationID: orgID, PincodeID: pincodeID, AsOf: asOf,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.Validation(
				"This PIN code is not mapped to a pricing zone for your organization.", nil)
		}
		return nil, apierr.Internal(fmt.Errorf("resolve zone: %w", err))
	}
	z := ZoneAssignment{
		ZoneID: row.ZoneID, ZonePublicID: row.ZonePublicID, ZoneCode: row.ZoneCode,
		ZoneName: row.ZoneName, ZoneType: row.ZoneType, IsRemote: row.IsRemote,
	}
	// Zone mappings carry effective dates, so a cached entry is only valid for
	// "now"; the TTL is short enough that a future-dated mapping takes effect
	// promptly, and changing a mapping invalidates the key immediately.
	s.cache.SetJSON(ctx, key, z, s.ttl)
	return &z, nil
}

// InvalidatePincode drops cached lookups for one PIN code.
func (s *Service) InvalidatePincode(ctx context.Context, countryISO2, code string) {
	s.cache.Delete(ctx, s.pincodeCacheKey(countryISO2, code))
}

// InvalidateZoneMapping drops a tenant's cached zone decision.
func (s *Service) InvalidateZoneMapping(ctx context.Context, orgID, pincodeID int64) {
	s.cache.Delete(ctx, s.zoneCacheKey(orgID, pincodeID))
}

// InvalidateOrganizationZones drops every cached zone decision for a tenant.
// Used after a bulk zone-mapping import, where enumerating individual keys
// would be far more expensive than one prefix sweep.
func (s *Service) InvalidateOrganizationZones(ctx context.Context, orgID int64) {
	s.cache.DeletePrefix(ctx, s.cache.Key("zone", fmt.Sprint(orgID))+":")
}

func (s *Service) pincodeCacheKey(countryISO2, code string) string {
	return s.cache.Key("pincode", countryISO2, code)
}

func (s *Service) zoneCacheKey(orgID, pincodeID int64) string {
	return s.cache.Key("zone", fmt.Sprint(orgID), fmt.Sprint(pincodeID))
}

// Queries exposes the generated query set to sibling modules that need
// geography reads inside their own transactions.
func (s *Service) Queries() *dbgen.Queries { return s.q }
