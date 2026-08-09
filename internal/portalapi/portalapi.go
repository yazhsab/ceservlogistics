// Package portalapi is the HTTP transport for the three Release 4 audience
// surfaces: the customer portal (M27), the franchise portal (M28) and the
// hub/branch console (M29).
//
// One transport for three portals, following ADR 0011 for the same reason
// opsapi and financeapi do: the three share their scope resolution, their
// pagination and their error shaping, and the scope resolution is precisely the
// part that must not drift between them.
//
// Every route here is scoped by *who is asking*, never by what they ask for.
// A customer endpoint reaches customer_users; a franchise endpoint reaches the
// principal's operating-unit grants; a console endpoint reaches the facility
// the operator is assigned to. A client-supplied account or facility is a
// business input that gets checked, never an authorization claim (§9).
package portalapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/portal"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
	"github.com/ceserve/courier-os/internal/tracking"
)

// Handler serves the portals and the console.
type Handler struct {
	svc      *portal.Service
	booker   *shipment.Booker
	tracking *tracking.Service
	units    *ops.Resolver
	queries  *dbgen.Queries
}

// Services groups what the transport needs.
type Services struct {
	Portal   *portal.Service
	Booker   *shipment.Booker
	Tracking *tracking.Service
	Units    *ops.Resolver
	Queries  *dbgen.Queries
}

// New builds the portal transport.
func New(s Services) *Handler {
	return &Handler{
		svc: s.Portal, booker: s.Booker, tracking: s.Tracking,
		units: s.Units, queries: s.Queries,
	}
}

// CustomerRoutes mounts /portal/customer.
func (h *Handler) CustomerRoutes(r chi.Router) {
	r.Get("/summary", require(portal.PermCustomer, h.customerSummary))
	r.Get("/accounts", require(portal.PermCustomer, h.customerAccounts))
	r.Get("/shipments", require(portal.PermCustomer, h.customerShipments))
	r.Get("/shipments/{shipmentId}", require(portal.PermCustomer, h.customerShipment))
	r.Get("/shipments/{shipmentId}/track", require(portal.PermCustomer, h.customerTrack))
	r.Get("/invoices", require(portal.PermCustomer, h.customerInvoices))
}

// FranchiseRoutes mounts /portal/franchise.
func (h *Handler) FranchiseRoutes(r chi.Router) {
	r.Get("/summary", require(portal.PermFranchise, h.franchiseSummary))
	r.Get("/shipments", require(portal.PermFranchise, h.franchiseShipments))
	r.Get("/settlements", require(portal.PermFranchise, h.franchiseSettlements))
}

// ConsoleRoutes mounts /console.
func (h *Handler) ConsoleRoutes(r chi.Router) {
	r.Get("/lookup/{barcode}", require(portal.PermConsole, h.consoleLookup))
	r.Get("/summary", require(portal.PermConsole, h.consoleSummary))
	r.Get("/queue", require(portal.PermConsole, h.consoleQueue))
	r.Get("/bags", require(portal.PermConsole, h.consoleBags))
	r.Get("/inbound", require(portal.PermConsole, h.consoleInbound))
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

// ---------------------------------------------------------------------------
// M27 Customer portal
// ---------------------------------------------------------------------------

func (h *Handler) customerSummary(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	days, err := httpx.QueryInt(r, "days", 90, 1, 400)
	if err != nil {
		return err
	}
	out, err := h.svc.Summary(r.Context(), p, days)
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) customerAccounts(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	accounts, err := h.svc.Accounts(r.Context(), p)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": accounts})
}

// customerShipments lists the customer's own shipments.
//
// It reuses ListShipments rather than adding a portal query, because the
// customer filter that makes it safe is already a first-class predicate there —
// and a second listing implementation would be a second place for the scope to
// be forgotten.
func (h *Handler) customerShipments(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	ids, err := portal.CustomerScope(p)
	if err != nil {
		return err
	}

	params, limit, err := h.listParams(r, p)
	if err != nil {
		return err
	}

	// One customer at a time: a login linked to several accounts picks which,
	// and the choice is checked against the link rather than trusted.
	customerID := ids[0]
	if raw := httpx.Query(r, "customerId"); raw != "" {
		id, rErr := h.resolveCustomer(r, p, raw)
		if rErr != nil {
			return rErr
		}
		customerID = id
	}
	params.CustomerID = &customerID

	rows, err := h.queries.ListShipments(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	rows, next := trimPage(rows, limit)

	// The portal projection, not the operations one: a customer sees their own
	// parcel's progress, not the internal facility codes it passed through.
	items := make([]map[string]any, 0, len(rows))
	for _, s := range rows {
		items = append(items, map[string]any{
			"id": s.PublicID, "awb": s.Awb, "reference": s.ReferenceNumber,
			"status": s.CurrentStatus, "statusChangedAt": s.StatusChangedAt,
			"service": s.ServiceName, "pieces": s.PieceCount,
			"originPincode": s.OriginPincode, "destinationPincode": s.DestinationPincode,
			"recipientName": s.RecipientName, "recipientCity": s.RecipientCity,
			"paymentMode": s.PaymentMode, "totalAmountMinor": s.TotalAmountMinor,
			"codAmountMinor": s.CodAmountMinor, "currency": s.Currency,
			"promisedDeliveryAt": s.PromisedDeliveryAt, "bookedAt": s.BookedAt,
		})
	}
	out := map[string]any{"data": items}
	if next != "" {
		out["nextCursor"] = next
	}
	return httpx.OK(w, out)
}

func (h *Handler) customerShipment(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	if _, err := portal.CustomerScope(p); err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "shipmentId", publicid.PrefixShipment, "Shipment")
	if err != nil {
		return err
	}
	// Booker.Get applies IsCustomerInScope, so another account's shipment is a
	// 404 here without the portal having to repeat the check.
	detail, err := h.booker.Get(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) customerTrack(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	if _, err := portal.CustomerScope(p); err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "shipmentId", publicid.PrefixShipment, "Shipment")
	if err != nil {
		return err
	}
	// Resolved through the shipment first, which is what applies the customer
	// scope. Tracking by AWB alone would not.
	detail, err := h.booker.Get(r.Context(), p, id)
	if err != nil {
		return err
	}
	result, err := h.tracking.Track(r.Context(), detail.AWB)
	if err != nil {
		return err
	}
	return httpx.OK(w, result)
}

func (h *Handler) customerInvoices(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	status, err := httpx.QueryEnum(r, "status",
		[]string{"DRAFT", "ISSUED", "PARTIALLY_PAID", "PAID", "OVERDUE", "CANCELLED", "WRITTEN_OFF"})
	if err != nil {
		return err
	}
	cursor, limit, err := cursorPage(r)
	if err != nil {
		return err
	}
	rows, err := h.svc.Invoices(r.Context(), p, status, cursor, limit)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": rows})
}

func (h *Handler) resolveCustomer(
	r *http.Request, p *tenant.Principal, publicID string,
) (int64, error) {
	c, err := h.queries.GetCustomerByPublicID(r.Context(), dbgen.GetCustomerByPublicIDParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return 0, apierr.NotFound("Customer")
	}
	// The link, not the tenant, is the boundary here.
	if !p.IsCustomerInScope(c.ID) {
		return 0, apierr.NotFound("Customer")
	}
	return c.ID, nil
}

// ---------------------------------------------------------------------------
// M28 Franchise portal
// ---------------------------------------------------------------------------

func (h *Handler) franchiseSummary(w http.ResponseWriter, r *http.Request) error {
	p, f, err := h.franchise(r)
	if err != nil {
		return err
	}
	from, to, err := window(r)
	if err != nil {
		return err
	}
	out, err := h.svc.FranchiseOverview(r.Context(), p, f, from, to)
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) franchiseShipments(w http.ResponseWriter, r *http.Request) error {
	p, f, err := h.franchise(r)
	if err != nil {
		return err
	}
	role, err := httpx.QueryEnum(r, "role",
		[]string{portal.RoleOrigin, portal.RoleDestination, portal.RoleAny})
	if err != nil {
		return err
	}
	status, err := httpx.QueryEnum(r, "status", shipment.StatusStrings())
	if err != nil {
		return err
	}
	cursor, limit, err := cursorPage(r)
	if err != nil {
		return err
	}

	items, err := h.svc.FranchiseShipments(r.Context(), p, f, role, status, cursor, limit)
	if err != nil {
		return err
	}
	if role == "" {
		role = portal.RoleOrigin
	}
	return httpx.OK(w, map[string]any{
		"franchise": map[string]any{"id": f.PublicID, "code": f.Code, "name": f.Name},
		// Echoed back so a client cannot mistake the default for "everything".
		"role": role,
		"data": items,
	})
}

func (h *Handler) franchiseSettlements(w http.ResponseWriter, r *http.Request) error {
	p, f, err := h.franchise(r)
	if err != nil {
		return err
	}
	status, err := httpx.QueryEnum(r, "status", []string{
		"DRAFT", "CALCULATED", "UNDER_REVIEW", "APPROVED",
		"PARTIALLY_PAID", "PAID", "CLOSED", "CANCELLED",
	})
	if err != nil {
		return err
	}
	cursor, limit, err := cursorPage(r)
	if err != nil {
		return err
	}
	rows, err := h.svc.FranchiseSettlements(r.Context(), p, f, status, cursor, limit)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{
		"franchise": map[string]any{"id": f.PublicID, "code": f.Code, "name": f.Name},
		"data":      rows,
	})
}

func (h *Handler) franchise(r *http.Request) (*tenant.Principal, *portal.Franchise, error) {
	p, err := tenant.Require(r)
	if err != nil {
		return nil, nil, err
	}
	f, err := h.svc.ResolveFranchise(r.Context(), p, httpx.Query(r, "franchiseId"))
	if err != nil {
		return nil, nil, err
	}
	return p, f, nil
}

// ---------------------------------------------------------------------------
// M29 Console
// ---------------------------------------------------------------------------

func (h *Handler) consoleLookup(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	barcode, err := httpx.PathParam(r, "barcode", 64)
	if err != nil {
		return err
	}
	parcel, err := h.svc.Lookup(r.Context(), p, barcode)
	if err != nil {
		return err
	}
	return httpx.OK(w, parcel)
}

func (h *Handler) consoleSummary(w http.ResponseWriter, r *http.Request) error {
	p, unit, err := h.facility(r)
	if err != nil {
		return err
	}
	out, err := h.svc.FacilityOverview(r.Context(), p, unit)
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) consoleQueue(w http.ResponseWriter, r *http.Request) error {
	p, unit, err := h.facility(r)
	if err != nil {
		return err
	}
	status, err := httpx.QueryEnum(r, "status", shipment.StatusStrings())
	if err != nil {
		return err
	}
	cursor, limit, err := cursorPage(r)
	if err != nil {
		return err
	}
	items, err := h.svc.Queue(r.Context(), p, unit.ID, status, cursor, limit)
	if err != nil {
		return err
	}
	out := map[string]any{
		"unit": map[string]any{"id": unit.PublicID, "code": unit.Code},
		"data": items,
	}
	if int32(len(items)) == limit && len(items) > 0 {
		// The cursor is the internal id of the last row, which the client
		// echoes back. It is opaque to them and never an object identifier.
		if last, lErr := h.queries.GetShipmentByPublicID(r.Context(),
			dbgen.GetShipmentByPublicIDParams{
				PublicID: items[len(items)-1].ID, OrganizationID: p.OrganizationID,
			}); lErr == nil {
			out["nextCursor"] = last.ID
		}
	}
	return httpx.OK(w, out)
}

func (h *Handler) consoleBags(w http.ResponseWriter, r *http.Request) error {
	p, unit, err := h.facility(r)
	if err != nil {
		return err
	}
	_, limit, err := cursorPage(r)
	if err != nil {
		return err
	}
	bags, err := h.svc.Bags(r.Context(), p, unit.ID, limit)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": bags})
}

func (h *Handler) consoleInbound(w http.ResponseWriter, r *http.Request) error {
	p, unit, err := h.facility(r)
	if err != nil {
		return err
	}
	_, limit, err := cursorPage(r)
	if err != nil {
		return err
	}
	manifests, err := h.svc.Inbound(r.Context(), p, unit.ID, limit)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": manifests})
}

// facility resolves which facility the console is showing.
//
// It reuses the same resolver every Release 2 operational endpoint uses, so the
// console cannot end up with a looser idea of "where am I" than a scan does.
func (h *Handler) facility(r *http.Request) (*tenant.Principal, *ops.Facility, error) {
	rc, err := h.units.Request(r, true)
	if err != nil {
		return nil, nil, err
	}
	return rc.Principal, rc.Facility, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func cursorPage(r *http.Request) (*int64, int32, error) {
	limit, err := httpx.QueryInt(r, "limit", 50, 1, 200)
	if err != nil {
		return nil, 0, err
	}
	raw, err := httpx.QueryInt(r, "cursor", 0, 0, 1<<62)
	if err != nil {
		return nil, 0, err
	}
	var cursor *int64
	if raw > 0 {
		c := int64(raw)
		cursor = &c
	}
	return cursor, int32(limit), nil
}

// window reads a from/to date range, defaulting to the last 30 days.
func window(r *http.Request) (time.Time, time.Time, error) {
	to := time.Now().UTC().Truncate(24 * time.Hour)
	from := to.AddDate(0, 0, -29)

	if raw := httpx.Query(r, "from"); raw != "" {
		t, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return time.Time{}, time.Time{}, apierr.Validation("Invalid date.",
				map[string]any{"from": "expected YYYY-MM-DD"})
		}
		from = t
	}
	if raw := httpx.Query(r, "to"); raw != "" {
		t, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return time.Time{}, time.Time{}, apierr.Validation("Invalid date.",
				map[string]any{"to": "expected YYYY-MM-DD"})
		}
		to = t
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, apierr.Validation(
			"The end of the window is before its start.", nil)
	}
	// Bounded for the same reason the command centre is: cheap per day times an
	// unbounded range is still unbounded.
	if to.Sub(from) > 400*24*time.Hour {
		return time.Time{}, time.Time{}, apierr.Validation(
			"The window is too wide. Use a report for a longer period.",
			map[string]any{"maxDays": 400})
	}
	return from, to, nil
}

// listParams builds the shared shipment-listing parameters for both portals.
//
// Keyset, like the internal endpoint: these tables grow without bound and an
// offset page deep into a customer's history costs the rows it skips.
func (h *Handler) listParams(
	r *http.Request, p *tenant.Principal,
) (dbgen.ListShipmentsParams, int, error) {
	limit, err := pagination.ClampLimit(httpx.Query(r, "limit"))
	if err != nil {
		return dbgen.ListShipmentsParams{}, 0, err
	}
	cursor, err := pagination.DecodeCursor(httpx.Query(r, "cursor"), "desc")
	if err != nil {
		return dbgen.ListShipmentsParams{}, 0, err
	}
	statuses, err := httpx.QueryEnumList(r, "status",
		shipment.StatusStrings(), len(shipment.AllStatuses))
	if err != nil {
		return dbgen.ListShipmentsParams{}, 0, err
	}

	params := dbgen.ListShipmentsParams{
		OrganizationID: p.OrganizationID,
		// One extra row detects a further page without a COUNT.
		RowLimit: int32(limit + 1),
		Search:   ops.Optional(httpx.Query(r, "search")),
	}
	if len(statuses) > 0 {
		params.Statuses = statuses
	}
	if cursor != nil && cursor.Time != nil {
		params.CursorCreatedAt = cursor.Time
		params.CursorID = cursor.ID
	}
	return params, limit, nil
}

// trimPage drops the probe row and encodes the cursor for the next page, using
// the same Cursor type the internal listing emits so a client can move between
// the two surfaces without re-learning pagination.
func trimPage(rows []dbgen.ListShipmentsRow, limit int) ([]dbgen.ListShipmentsRow, string) {
	if len(rows) <= limit {
		return rows, ""
	}
	rows = rows[:limit]
	last := rows[len(rows)-1]
	created := last.CreatedAt
	return rows, pagination.Cursor{Time: &created, ID: last.ID, Dir: "desc"}.Encode()
}
