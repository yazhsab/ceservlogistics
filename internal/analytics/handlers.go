package analytics

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Handler serves the command centre.
type Handler struct {
	svc   *Service
	units *ops.Resolver
}

func NewHandler(svc *Service, units *ops.Resolver) *Handler {
	return &Handler{svc: svc, units: units}
}

// Routes mounts /command-centre.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", require(PermCommandRead, h.overview))
	r.Get("/trend", require(PermCommandRead, h.trend))
	r.Get("/units", require(PermCommandRead, h.byUnit))
	r.Get("/services", require(PermCommandRead, h.byService))
	r.Get("/backlog", require(PermCommandRead, h.backlog))
	r.Get("/snapshots", require(PermCommandRead, h.snapshots))
}

func require(permission string, next httpx.Handler) http.HandlerFunc {
	return httpx.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		p, err := tenant.Require(r)
		if err != nil {
			return err
		}
		if err := p.Require(permission); err != nil {
			return err
		}
		return next(w, r)
	})
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) error {
	p, window, err := h.window(r)
	if err != nil {
		return err
	}
	out, err := h.svc.Overview(r.Context(), p, window)
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) trend(w http.ResponseWriter, r *http.Request) error {
	p, window, err := h.window(r)
	if err != nil {
		return err
	}
	points, err := h.svc.Trend(r.Context(), p, window)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{
		"data": points, "consistency": consistency.PeriodTotals,
	})
}

func (h *Handler) byUnit(w http.ResponseWriter, r *http.Request) error {
	p, window, err := h.window(r)
	if err != nil {
		return err
	}
	unitType, err := httpx.QueryEnum(r, "unitType", []string{
		"HEAD_OFFICE", "REGIONAL_HUB", "TRANSIT_HUB", "DELIVERY_HUB",
		"COMPANY_BRANCH", "FRANCHISE_BRANCH",
	})
	if err != nil {
		return err
	}
	rows, err := h.svc.ByUnit(r.Context(), p, window, unitType, limit(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": rows})
}

func (h *Handler) byService(w http.ResponseWriter, r *http.Request) error {
	p, window, err := h.window(r)
	if err != nil {
		return err
	}
	rows, err := h.svc.ByService(r.Context(), p, window, limit(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": rows})
}

func (h *Handler) backlog(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	rows, err := h.svc.Backlog(r.Context(), p, limit(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{
		"data": rows, "consistency": consistency.LiveBacklog,
	})
}

func (h *Handler) snapshots(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	hours, err := httpx.QueryInt(r, "hours", 24, 1, 24*90)
	if err != nil {
		return err
	}
	unitID, err := h.unitFilter(r, p)
	if err != nil {
		return err
	}
	points, err := h.svc.Snapshots(r.Context(), p,
		time.Now().Add(-time.Duration(hours)*time.Hour), unitID, limit(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{
		"data": points, "consistency": consistency.Snapshots,
	})
}

// window reads the shared query parameters.
//
// The operating-unit filter is resolved through the principal's scope, not
// taken at face value: a branch manager asking for another branch's numbers is
// asking a question they are not entitled to the answer to (§9).
func (h *Handler) window(r *http.Request) (*tenant.Principal, Window, error) {
	p, err := tenant.Require(r)
	if err != nil {
		return nil, Window{}, err
	}
	from, err := optDate(r, "from")
	if err != nil {
		return nil, Window{}, err
	}
	to, err := optDate(r, "to")
	if err != nil {
		return nil, Window{}, err
	}
	unitID, err := h.unitFilter(r, p)
	if err != nil {
		return nil, Window{}, err
	}

	window := Window{UnitID: unitID}
	if from != nil {
		window.From = *from
	}
	if to != nil {
		window.To = *to
	}

	if code := httpx.Query(r, "serviceCode"); code != "" {
		svc, sErr := h.svc.q.GetCourierServiceByCode(r.Context(), dbgen.GetCourierServiceByCodeParams{
			OrganizationID: p.OrganizationID, Code: code,
		})
		if sErr != nil {
			return nil, Window{}, apierr.NotFound("Courier service")
		}
		window.ServiceID = &svc.ID
	}
	return p, window, nil
}

// unitFilter resolves an optional operating-unit narrowing.
//
// A principal whose role is scoped to particular units is confined to them even
// when it asks for nothing: otherwise the command centre would be the one place
// in the platform where operating-unit scope did not apply, and a branch
// manager would read the whole network's figures off it.
func (h *Handler) unitFilter(r *http.Request, p *tenant.Principal) (*int64, error) {
	raw := httpx.Query(r, "unitId")
	if raw == "" {
		// No explicit filter. A scoped principal still sees only its own unit;
		// an unscoped one sees the network.
		scope := p.UnitScope()
		switch {
		case scope == nil:
			return nil, nil
		case len(scope) == 0:
			return nil, apierr.Forbidden("You are not assigned to any operating unit.")
		default:
			// One unit is the common case for a branch or hub manager. With
			// several, the network-wide view is refused rather than silently
			// showing one of them.
			if len(scope) == 1 {
				return &scope[0], nil
			}
			return nil, apierr.Validation(
				"Your access covers several operating units. Name one with unitId.",
				map[string]any{"operatingUnits": p.ScopedUnitPublicIDs})
		}
	}

	if !publicid.Valid(publicid.PrefixOperatingUnit, raw) {
		return nil, apierr.Validation("unitId is not a valid operating unit identifier.", nil)
	}
	unit, err := h.svc.q.GetOperatingUnitByPublicID(r.Context(), dbgen.GetOperatingUnitByPublicIDParams{
		PublicID: raw, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, apierr.NotFound("Operating unit")
	}
	if err := p.RequireUnitInScope(unit.ID); err != nil {
		return nil, err
	}
	return &unit.ID, nil
}

func optDate(r *http.Request, name string) (*time.Time, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(dateLayout, raw)
	if err != nil {
		return nil, apierr.Validation("Invalid date.",
			map[string]any{name: "expected YYYY-MM-DD"})
	}
	return &t, nil
}

func limit(r *http.Request) int32 {
	n, err := httpx.QueryInt(r, "limit", 20, 1, 200)
	if err != nil {
		return 20
	}
	return int32(n)
}
