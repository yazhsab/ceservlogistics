// Package ops holds the pieces every Release 2 operational module needs:
// resolving which facility an actor is working at, allocating human-readable
// operational codes, and reading the device envelope off a request.
//
// It exists so that "which branch is this clerk standing in, and are they
// allowed to be there" has exactly one answer in the codebase. Ten modules each
// writing their own version of that check is ten chances to write it wrong.
package ops

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Facility is an operating unit an actor may work at.
type Facility struct {
	ID       int64
	PublicID string
	Code     string
	Name     string
	UnitType string
	Status   string
	Pincode  string
}

// IsActive reports whether the unit may take operational work today.
func (f *Facility) IsActive() bool { return f.Status == "ACTIVE" }

// IsHub reports whether the facility terminates line haul.
func (f *Facility) IsHub() bool {
	return f.UnitType == "HUB" || f.UnitType == "GATEWAY" || f.UnitType == "SORTING_CENTER"
}

// Resolver turns a client-supplied operating unit reference into a facility the
// authenticated principal is actually allowed to act at.
type Resolver struct {
	q *dbgen.Queries
}

// NewResolver builds a facility resolver.
func NewResolver(q *dbgen.Queries) *Resolver { return &Resolver{q: q} }

// Facility resolves and authorizes an operating unit.
//
// Two checks, both mandatory: the unit must belong to the caller's tenant, and
// the caller's role scope must cover it. A unit in another tenant is reported
// as not found rather than forbidden, so the endpoint cannot be used to probe
// which ids exist elsewhere (§9).
func (r *Resolver) Facility(ctx context.Context, p *tenant.Principal, publicID string) (*Facility, error) {
	if publicID == "" {
		return nil, apierr.Validation("The operating unit is required.",
			map[string]any{"field": "operatingUnitId"})
	}
	row, err := r.q.GetOperatingUnitByPublicID(ctx, dbgen.GetOperatingUnitByPublicIDParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.NotFound("Operating unit")
		}
		return nil, apierr.Internal(err)
	}
	if row.Status != "ACTIVE" {
		return nil, apierr.Conflict(apierr.CodeConflict,
			"This operating unit is not active.").
			WithDetail("operatingUnitCode", row.Code).
			WithDetail("operatingUnitStatus", row.Status)
	}
	if err := p.RequireUnitInScope(row.ID); err != nil {
		return nil, err
	}
	return &Facility{
		ID: row.ID, PublicID: row.PublicID, Code: row.Code, Name: row.Name,
		UnitType: row.UnitType, Status: row.Status, Pincode: row.Pincode,
	}, nil
}

// FacilityByID resolves a facility already identified internally, without the
// scope check. Used to describe a unit in a response, never to authorize.
func (r *Resolver) FacilityByID(ctx context.Context, orgID, id int64) (*Facility, error) {
	row, err := r.q.GetOperatingUnitByID(ctx, dbgen.GetOperatingUnitByIDParams{
		ID: id, OrganizationID: orgID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.NotFound("Operating unit")
		}
		return nil, apierr.Internal(err)
	}
	return &Facility{
		ID: row.ID, PublicID: row.PublicID, Code: row.Code, Name: row.Name,
		UnitType: row.UnitType, Status: row.Status, Pincode: row.Pincode,
	}, nil
}

// RequireFacility resolves the working facility for a request.
//
// When the caller names one it is resolved and scope-checked. When they do not,
// and their role grants exactly one unit, that unit is used — a counter clerk
// assigned to one branch should not have to name it on every scan. A caller
// with several units in scope must be explicit, because guessing which of three
// hubs somebody is standing in is exactly the kind of assumption that produces
// a parcel scanned into the wrong facility.
func (r *Resolver) RequireFacility(ctx context.Context, p *tenant.Principal, publicID string) (*Facility, error) {
	if publicID != "" {
		return r.Facility(ctx, p, publicID)
	}
	if len(p.ScopedUnitIDs) == 1 {
		return r.FacilityByID(ctx, p.OrganizationID, p.ScopedUnitIDs[0])
	}
	if len(p.ScopedUnitIDs) == 0 && p.HasUnscopedRole {
		return nil, apierr.Validation(
			"Name the operating unit you are working at. Your role covers the whole network, so it cannot be inferred.",
			map[string]any{"field": "operatingUnitId"})
	}
	return nil, apierr.Validation(
		"Name the operating unit you are working at.",
		map[string]any{"field": "operatingUnitId", "unitsInScope": len(p.ScopedUnitIDs)})
}

// ---------------------------------------------------------------------------
// Operational code allocation
// ---------------------------------------------------------------------------

// CodeAllocator issues human-readable codes for bags, manifests, trips, runs
// and pickup requests.
//
// It uses the same one-statement atomic UPSERT as AWB allocation (§22), so two
// clerks at the same counter cannot receive the same bag number. Scoping the
// counter by facility and day keeps contention off a single hot row.
type CodeAllocator struct {
	q *dbgen.Queries
}

// NewCodeAllocator builds an allocator.
func NewCodeAllocator(q *dbgen.Queries) *CodeAllocator { return &CodeAllocator{q: q} }

// Kinds of operational code.
const (
	KindPickupRequest = "PKR"
	KindPickupRun     = "PRN"
	KindBag           = "BAG"
	KindManifest      = "MFT"
	KindTrip          = "TRP"
	KindDeliveryRun   = "DRN"
	KindException     = "EXC"
	KindReconcile     = "REC"
	KindNDRCase       = "NDR"
	KindRTOCase       = "RTO"
)

// Allocate returns the next code of a kind, scoped to a facility and a day.
//
// The result looks like BLR001-BAG-260808-000017: facility, kind, date,
// sequence. It is readable over a phone and sortable in a spreadsheet, which is
// what operations staff actually need from an identifier.
func (a *CodeAllocator) Allocate(ctx context.Context, orgID int64, kind, facilityCode string, at time.Time) (string, error) {
	scope := scopeKey(facilityCode, at)
	row, err := a.q.AllocateOperationalSequence(ctx, dbgen.AllocateOperationalSequenceParams{
		OrganizationID: orgID, Kind: kind, ScopeKey: scope,
	})
	if err != nil {
		if database.IsNoRows(err) {
			// The UPSERT's WHERE guard refused: the counter is exhausted for this
			// scope. Better to fail loudly than to wrap around onto a code that
			// already identifies something else.
			return "", apierr.Conflict(apierr.CodeConflict,
				fmt.Sprintf("The %s sequence for %s is exhausted for today.", kind, facilityCode))
		}
		return "", apierr.Internal(fmt.Errorf("allocate %s sequence: %w", kind, err))
	}
	return fmt.Sprintf("%s-%s-%s-%06d", sanitizeCode(facilityCode), kind, at.Format("060102"), row.CurrentValue), nil
}

// Barcode returns the compact scannable form of a code: no separators, so it
// fits a narrow label and scans reliably.
func Barcode(code string) string {
	return strings.ReplaceAll(code, "-", "")
}

func scopeKey(facilityCode string, at time.Time) string {
	key := sanitizeCode(facilityCode) + "_" + at.Format("060102")
	if len(key) > 32 {
		key = key[:32]
	}
	return key
}

// sanitizeCode keeps only the characters the scope_key CHECK constraint allows.
func sanitizeCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return "X"
	}
	if len(out) > 16 {
		out = out[:16]
	}
	return out
}

// ---------------------------------------------------------------------------
// Device envelope
// ---------------------------------------------------------------------------

// Device headers a field application sends with every operational call.
const (
	HeaderDeviceID    = "X-Device-Id"
	HeaderDeviceModel = "X-Device-Model"
	HeaderSource      = "X-Client-Source"
	HeaderDeviceEvent = "X-Device-Event-Id"
)

// Device is the client envelope attached to an operational event.
type Device struct {
	ID    string
	Model string
	// EventID is generated by the field application before the call is queued.
	// It is what makes a retry over a flaky connection collapse onto the
	// original operation rather than repeating it. Constitution
	// §"Offline/retry safety".
	EventID string
	Source  string
}

// HasEventKey reports whether this device supplied enough to deduplicate on.
func (d Device) HasEventKey() bool { return d.ID != "" && d.EventID != "" }

// DeviceFrom reads the device envelope off a request.
//
// Values are length-bounded because they end up in an indexed column and in
// logs; an unbounded header is a cheap way to bloat both.
func DeviceFrom(r *http.Request) Device {
	return Device{
		ID:      boundedHeader(r, HeaderDeviceID, 128),
		Model:   boundedHeader(r, HeaderDeviceModel, 64),
		EventID: boundedHeader(r, HeaderDeviceEvent, 128),
		Source:  normalizeSource(boundedHeader(r, HeaderSource, 16)),
	}
}

// Actor builds the transition actor for a request.
func (d Device) Actor(p *tenant.Principal, facility *Facility) shipment.Actor {
	a := shipment.Actor{
		Principal: p, Source: d.Source, DeviceID: d.ID, DeviceModel: d.Model,
	}
	if facility != nil {
		id := facility.ID
		a.FacilityID = &id
	}
	return a
}

// WithPosition attaches an approximate location to the actor.
func WithPosition(a shipment.Actor, lat, lng *float64) shipment.Actor {
	a.Latitude, a.Longitude = lat, lng
	return a
}

var validSources = map[string]bool{
	"WEB": true, "MOBILE": true, "SCANNER": true, "API": true, "PARTNER": true,
}

func normalizeSource(s string) string {
	up := strings.ToUpper(strings.TrimSpace(s))
	if validSources[up] {
		return up
	}
	return "API"
}

func boundedHeader(r *http.Request, name string, max int) string {
	v := strings.TrimSpace(r.Header.Get(name))
	if len(v) > max {
		return v[:max]
	}
	return v
}

// ---------------------------------------------------------------------------
// Small shared helpers
// ---------------------------------------------------------------------------

// Ref is the compact {id, code, name} shape used across operational responses.
type Ref struct {
	ID   string `json:"id,omitempty"`
	Code string `json:"code,omitempty"`
	Name string `json:"name,omitempty"`
}

// RefOf builds a Ref from a facility.
func RefOf(f *Facility) *Ref {
	if f == nil {
		return nil
	}
	return &Ref{ID: f.PublicID, Code: f.Code, Name: f.Name}
}

// RefFrom builds a Ref from nullable columns, returning nil when empty.
func RefFrom(publicID, code, name *string) *Ref {
	if publicID == nil && code == nil {
		return nil
	}
	r := &Ref{}
	if publicID != nil {
		r.ID = *publicID
	}
	if code != nil {
		r.Code = *code
	}
	if name != nil {
		r.Name = *name
	}
	return r
}

// Optional converts an empty string to nil for nullable columns.
func Optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Deref returns the value behind a pointer or the zero value.
func Deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// Ptr returns a pointer to a value.
func Ptr[T any](v T) *T { return &v }

// UserID resolves a user's public id to its internal id, scoped to the tenant.
//
// It lives here rather than in each module because half the operational
// endpoints name an agent, and every one of them must confirm that agent
// belongs to the caller's organization before using the id.
func (r *Resolver) UserID(ctx context.Context, orgID int64, publicID string) (int64, error) {
	u, err := r.q.GetUserByPublicID(ctx, dbgen.GetUserByPublicIDParams{
		PublicID: publicID, OrganizationID: orgID,
	})
	if err != nil {
		return 0, err
	}
	return u.ID, nil
}
