package shipment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/auth"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/idempotency"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/pricing"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Handler exposes the shipment API.
type Handler struct {
	booker      *Booker
	idempotency *idempotency.Executor
	maxPackages int
}

// NewHandler builds the shipment handler.
func NewHandler(b *Booker, idem *idempotency.Executor, maxPackages int) *Handler {
	if maxPackages <= 0 {
		maxPackages = 50
	}
	return &Handler{booker: b, idempotency: idem, maxPackages: maxPackages}
}

// Routes mounts shipment endpoints under /shipments.
func (h *Handler) Routes(r chi.Router) {
	r.With(auth.RequirePermission("shipment.create")).Post("/", httpx.Wrap(h.create))
	r.With(auth.RequirePermission("shipment.read")).Get("/", httpx.Wrap(h.list))
	r.With(auth.RequirePermission("shipment.read")).Get("/{shipmentId}", httpx.Wrap(h.get))
	r.With(auth.RequirePermission("shipment.read")).Get("/{shipmentId}/events", httpx.Wrap(h.events))
	r.With(auth.RequirePermission("shipment.cancel")).Post("/{shipmentId}/cancel", httpx.Wrap(h.cancel))
	r.With(auth.RequirePermission("shipment.label")).Get("/{shipmentId}/label", httpx.Wrap(h.label))
}

// ---- views -----------------------------------------------------------------

// Detail is the full shipment representation.
type Detail struct {
	ID              string    `json:"id"`
	AWB             string    `json:"awb"`
	ReferenceNumber string    `json:"referenceNumber,omitempty"`
	Status          string    `json:"status"`
	StatusChangedAt time.Time `json:"statusChangedAt"`
	PaymentMode     string    `json:"paymentMode"`
	BookedAt        time.Time `json:"bookedAt"`
	CreatedAt       time.Time `json:"createdAt"`

	Customer Ref `json:"customer"`
	Service  Ref `json:"service"`

	Origin      Endpoint `json:"origin"`
	Destination Endpoint `json:"destination"`

	Route     *RouteView     `json:"route,omitempty"`
	Charges   *ChargesView   `json:"charges,omitempty"`
	Packages  []PackageView  `json:"packages"`
	Addresses map[string]any `json:"addresses,omitempty"`

	PieceCount            int32      `json:"pieceCount"`
	ActualWeightGrams     int32      `json:"actualWeightGrams"`
	VolumetricWeightGrams int32      `json:"volumetricWeightGrams"`
	ChargeableWeightGrams int32      `json:"chargeableWeightGrams"`
	Currency              string     `json:"currency"`
	DeclaredValueMinor    int64      `json:"declaredValueMinor"`
	CODAmountMinor        int64      `json:"codAmountMinor"`
	TotalAmountMinor      int64      `json:"totalAmountMinor"`
	InsuranceRequired     bool       `json:"insuranceRequired"`
	SLAHours              *int32     `json:"slaHours,omitempty"`
	PromisedDeliveryAt    *time.Time `json:"promisedDeliveryAt,omitempty"`

	ContentDescription  string `json:"contentDescription"`
	SpecialInstructions string `json:"specialInstructions,omitempty"`
	IsFragile           bool   `json:"isFragile"`
	IsDangerousGoods    bool   `json:"isDangerousGoods"`

	CancelledAt        *time.Time `json:"cancelledAt,omitempty"`
	CancellationReason string     `json:"cancellationReason,omitempty"`

	AllowedTransitions []string `json:"allowedTransitions"`
}

// Ref is a compact reference to a related entity.
type Ref struct {
	ID   string `json:"id,omitempty"`
	Code string `json:"code"`
	Name string `json:"name,omitempty"`
}

// Endpoint describes one end of a shipment.
type Endpoint struct {
	Pincode  string `json:"pincode"`
	Branch   *Ref   `json:"branch,omitempty"`
	Hub      *Ref   `json:"hub,omitempty"`
	IsRemote bool   `json:"isRemote"`
}

// RouteView is the persisted route snapshot.
type RouteView struct {
	RouteCode        string          `json:"routeCode,omitempty"`
	ResolutionSource string          `json:"resolutionSource"`
	TransitHours     int32           `json:"transitHours"`
	SLAHours         int32           `json:"slaHours"`
	Legs             json.RawMessage `json:"legs"`
	ExplanationID    string          `json:"explanationId,omitempty"`
}

// ChargesView is the persisted price snapshot.
type ChargesView struct {
	Currency             string          `json:"currency"`
	RateCardCode         string          `json:"rateCardCode"`
	RateCardVersion      int32           `json:"rateCardVersion"`
	PricingEngineVersion string          `json:"pricingEngineVersion"`
	OriginZoneCode       string          `json:"originZoneCode,omitempty"`
	DestinationZoneCode  string          `json:"destinationZoneCode,omitempty"`
	FreightMinor         int64           `json:"freightMinor"`
	SurchargeTotalMinor  int64           `json:"surchargeTotalMinor"`
	DiscountTotalMinor   int64           `json:"discountTotalMinor"`
	TaxableMinor         int64           `json:"taxableMinor"`
	TaxTotalMinor        int64           `json:"taxTotalMinor"`
	RoundingMinor        int64           `json:"roundingMinor"`
	TotalMinor           int64           `json:"totalMinor"`
	LineItems            json.RawMessage `json:"lineItems"`
}

// PackageView is one piece.
type PackageView struct {
	ID                    string `json:"id"`
	Sequence              int32  `json:"sequence"`
	PieceBarcode          string `json:"pieceBarcode"`
	Reference             string `json:"reference,omitempty"`
	ActualWeightGrams     int32  `json:"actualWeightGrams"`
	VolumetricWeightGrams int32  `json:"volumetricWeightGrams"`
	LengthMM              *int32 `json:"lengthMm,omitempty"`
	WidthMM               *int32 `json:"widthMm,omitempty"`
	HeightMM              *int32 `json:"heightMm,omitempty"`
	ContentDescription    string `json:"contentDescription,omitempty"`
	DeclaredValueMinor    int64  `json:"declaredValueMinor"`
}

// ---- create ----------------------------------------------------------------

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	// The raw body is read once so it can serve both decoding and the
	// idempotency request digest.
	body, err := ReadBody(w, r)
	if err != nil {
		return err
	}
	var req BookingRequest
	if err := DecodeStrict(body, &req); err != nil {
		return err
	}
	if err := h.validateBooking(&req); err != nil {
		return err
	}
	key, err := httpx.IdempotencyKey(r, false)
	if err != nil {
		return err
	}

	// Preparation (customer, routing, pricing, AWB) runs outside the
	// transaction; only the writes run inside it.
	var prepared *Prepared
	out, replayed, err := h.idempotency.Run(r.Context(), idempotency.Request{
		OrganizationID: p.OrganizationID, UserID: &p.UserID,
		Endpoint: "POST /api/v1/shipments", Key: key, Body: body,
		RequestID: httpx.RequestID(r.Context()),
	}, func(ctx context.Context) error {
		var pErr error
		prepared, pErr = h.booker.Prepare(ctx, p, req)
		return pErr
	}, func(ctx context.Context, tx pgx.Tx) (idempotency.Outcome, error) {
		detail, bErr := h.booker.Commit(ctx, tx, prepared)
		if bErr != nil {
			return idempotency.Outcome{}, bErr
		}
		return idempotency.Outcome{
			Status: http.StatusCreated, Body: detail,
			ResourceType: "shipment", ResourcePublicID: detail.ID,
		}, nil
	})
	if err != nil {
		return err
	}

	if replayed {
		// The client is told explicitly that this is a replay, so a retry after
		// a timeout is distinguishable from a fresh booking.
		w.Header().Set("Idempotent-Replay", "true")
	}
	location := ""
	if out.ResourcePublicID != "" {
		location = "/api/v1/shipments/" + out.ResourcePublicID
	}
	if location != "" {
		w.Header().Set("Location", location)
	}
	return httpx.JSON(w, out.Status, out.Body)
}

// validateBooking checks the payload before any database work.
func (h *Handler) validateBooking(req *BookingRequest) error {
	return ValidateBooking(req, h.maxPackages)
}

// ValidateBooking checks a booking payload and normalises it in place.
//
// Exported so the partner transport validates identically: an external booking
// and an internal one must be accepted or rejected on exactly the same grounds.
func ValidateBooking(req *BookingRequest, maxPackages int) error {
	if maxPackages <= 0 {
		maxPackages = 50
	}
	v := validate.New()
	v.PublicID("customerId", req.CustomerID, publicid.PrefixCustomer, true)
	v.Code("serviceCode", req.ServiceCode)
	v.Enum("paymentMode", req.PaymentMode, pricing.PaymentModes, true)
	req.ContentDescription = v.Text("contentDescription", req.ContentDescription, 2, 500, true)
	req.SpecialInstructions = v.Text("specialInstructions", req.SpecialInstructions, 0, 1000, false)
	if req.ReferenceNumber != "" {
		req.ReferenceNumber = v.Text("referenceNumber", req.ReferenceNumber, 1, 64, false)
	}
	v.NonNegativeMinor("declaredValueMinor", req.DeclaredValueMinor)
	v.NonNegativeMinor("codAmountMinor", req.CODAmountMinor)

	validateAddress(v, "sender", &req.Sender)
	validateAddress(v, "recipient", &req.Recipient)

	if len(req.Packages) == 0 {
		v.Add("packages", "At least one package is required.")
	}
	if len(req.Packages) > maxPackages {
		v.Addf("packages", "A shipment may contain at most %d packages.", maxPackages)
	}
	var declaredSum int64
	for i, pkg := range req.Packages {
		v.IntRange(fmt.Sprintf("packages[%d].actualWeightGrams", i), pkg.ActualWeightGrams, 1, 10_000_000)
		for _, dim := range []struct {
			name  string
			value int
		}{{"lengthMm", pkg.LengthMM}, {"widthMm", pkg.WidthMM}, {"heightMm", pkg.HeightMM}} {
			if dim.value != 0 {
				v.IntRange(fmt.Sprintf("packages[%d].%s", i, dim.name), dim.value, 1, 100_000)
			}
		}
		v.NonNegativeMinor(fmt.Sprintf("packages[%d].declaredValueMinor", i), pkg.DeclaredValueMinor)
		declaredSum += pkg.DeclaredValueMinor
	}
	// A per-package breakdown that disagrees with the shipment total would make
	// an insurance claim ambiguous.
	if declaredSum > 0 && req.DeclaredValueMinor > 0 && declaredSum != req.DeclaredValueMinor {
		v.Addf("declaredValueMinor",
			"Must equal the sum of package declared values (%d).", declaredSum)
	}
	if req.DeclaredValueMinor == 0 && declaredSum > 0 {
		req.DeclaredValueMinor = declaredSum
	}

	switch req.PaymentMode {
	case "COD":
		if req.CODAmountMinor <= 0 {
			v.Add("codAmountMinor", "A COD shipment must specify a positive COD amount.")
		}
	default:
		if req.CODAmountMinor > 0 {
			v.Add("codAmountMinor", "Only COD shipments may specify a COD amount.")
		}
	}
	if req.InsuranceRequired && req.DeclaredValueMinor <= 0 {
		v.Add("declaredValueMinor", "Insurance requires a positive declared value.")
	}
	if req.BookingUnitID != "" {
		v.PublicID("bookingUnitId", req.BookingUnitID, publicid.PrefixOperatingUnit, false)
	}
	return v.Err()
}

func validateAddress(v *validate.Validator, prefix string, a *Address) {
	if a.AddressID != "" {
		// A saved address supplies the mandatory fields; only the identifier is
		// validated here and the rest is filled in during resolution.
		v.PublicID(prefix+".addressId", a.AddressID, publicid.PrefixCustomerAddress, false)
		if a.Pincode != "" {
			a.Pincode = v.Pincode(prefix+".pincode", a.Pincode)
		}
		a.ContactName = v.Text(prefix+".contactName", a.ContactName, 0, 160, false)
		a.Phone = v.Phone(prefix+".phone", a.Phone, false)
		return
	}
	a.ContactName = v.Text(prefix+".contactName", a.ContactName, 2, 160, true)
	a.Phone = v.Phone(prefix+".phone", a.Phone, true)
	a.AltPhone = v.Phone(prefix+".altPhone", a.AltPhone, false)
	a.Line1 = v.Text(prefix+".line1", a.Line1, 3, 200, true)
	a.Line2 = v.Text(prefix+".line2", a.Line2, 0, 200, false)
	a.Landmark = v.Text(prefix+".landmark", a.Landmark, 0, 120, false)
	a.City = v.Text(prefix+".city", a.City, 1, 120, true)
	a.State = v.Text(prefix+".state", a.State, 1, 120, true)
	a.Pincode = v.Pincode(prefix+".pincode", a.Pincode)
	a.CompanyName = v.Text(prefix+".companyName", a.CompanyName, 0, 160, false)
	if a.Email != "" {
		a.Email = v.Email(prefix+".email", a.Email)
	}
	v.Latitude(prefix+".latitude", a.Latitude)
	v.Longitude(prefix+".longitude", a.Longitude)
}

// ---- read ------------------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, err := pagination.ClampLimit(httpx.Query(r, "limit"))
	if err != nil {
		return err
	}
	cursor, err := pagination.DecodeCursor(httpx.Query(r, "cursor"), "desc")
	if err != nil {
		return err
	}
	statuses, err := httpx.QueryEnumList(r, "status", StatusStrings(), len(AllStatuses))
	if err != nil {
		return err
	}
	from, err := httpx.QueryTime(r, "bookedFrom")
	if err != nil {
		return err
	}
	to, err := httpx.QueryTime(r, "bookedTo")
	if err != nil {
		return err
	}

	params := dbgen.ListShipmentsParams{
		OrganizationID: p.OrganizationID,
		// One extra row detects whether another page exists without a COUNT.
		RowLimit:   int32(limit + 1),
		BookedFrom: from, BookedTo: to,
	}
	if len(statuses) > 0 {
		params.Statuses = statuses
	}
	if cursor != nil && cursor.Time != nil {
		params.CursorCreatedAt = cursor.Time
		params.CursorID = cursor.ID
	}
	if s := httpx.Query(r, "search"); s != "" {
		params.Search = &s
	}
	if pm, pErr := httpx.QueryEnum(r, "paymentMode", pricing.PaymentModes); pErr != nil {
		return pErr
	} else if pm != "" {
		params.PaymentMode = &pm
	}
	if pin := httpx.Query(r, "originPincode"); pin != "" {
		params.OriginPincode = &pin
	}
	if pin := httpx.Query(r, "destinationPincode"); pin != "" {
		params.DestinationPincode = &pin
	}
	if customerID, cErr := httpx.QueryPublicID(r, "customerId", publicid.PrefixCustomer); cErr != nil {
		return cErr
	} else if customerID != "" {
		c, lErr := h.booker.q.GetCustomerByPublicID(r.Context(), dbgen.GetCustomerByPublicIDParams{
			PublicID: customerID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			if database.IsNoRows(lErr) {
				return apierr.NotFound("Customer")
			}
			return apierr.Internal(lErr)
		}
		params.CustomerID = &c.ID
	}
	if serviceCode := httpx.Query(r, "serviceCode"); serviceCode != "" {
		svc, sErr := h.booker.q.GetCourierServiceByCode(r.Context(), dbgen.GetCourierServiceByCodeParams{
			OrganizationID: p.OrganizationID, Code: serviceCode,
		})
		if sErr != nil {
			if database.IsNoRows(sErr) {
				return apierr.NotFound("Courier service")
			}
			return apierr.Internal(sErr)
		}
		params.CourierServiceID = &svc.ID
	}
	// Operating-unit and customer scoping. shipment.read_all lifts the unit
	// restriction for support and finance roles.
	if scope := p.UnitScope("shipment.read_all"); scope != nil {
		params.ScopedUnitIds = scope
	}
	if ids := p.CustomerScope(); ids != nil {
		params.CustomerIds = ids
	}

	rows, err := h.booker.q.ListShipments(r.Context(), params)
	if err != nil {
		return apierr.Internal(fmt.Errorf("list shipments: %w", err))
	}
	items := make([]map[string]any, 0, len(rows))
	for _, s := range rows {
		items = append(items, map[string]any{
			"id": s.PublicID, "awb": s.Awb, "referenceNumber": s.ReferenceNumber,
			"status": s.CurrentStatus, "statusChangedAt": s.StatusChangedAt,
			"paymentMode": s.PaymentMode, "pieceCount": s.PieceCount,
			"chargeableWeightGrams": s.ChargeableWeightGrams, "currency": s.Currency,
			"totalAmountMinor": s.TotalAmountMinor, "codAmountMinor": s.CodAmountMinor,
			"originPincode": s.OriginPincode, "destinationPincode": s.DestinationPincode,
			"promisedDeliveryAt": s.PromisedDeliveryAt, "bookedAt": s.BookedAt,
			"customer":              map[string]any{"id": s.CustomerPublicID, "code": s.CustomerCode, "name": s.CustomerName},
			"service":               map[string]any{"code": s.ServiceCode, "name": s.ServiceName},
			"originBranchCode":      s.OriginBranchCode,
			"destinationBranchCode": s.DestinationBranchCode,
			"recipientName":         s.RecipientName,
			"recipientCity":         s.RecipientCity,
			"_cursorCreatedAt":      s.CreatedAt,
			"_cursorId":             s.ID,
		})
	}
	page := pagination.NewCursorPage(items, limit, "createdAt", "desc", func(item map[string]any) pagination.Cursor {
		created, _ := item["_cursorCreatedAt"].(time.Time)
		id, _ := item["_cursorId"].(int64)
		return pagination.Cursor{Time: &created, ID: id, Dir: "desc"}
	})
	for _, item := range page.Data {
		delete(item, "_cursorCreatedAt")
		delete(item, "_cursorId")
	}
	return httpx.OK(w, page)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	shipmentID, err := httpx.PathPublicID(r, "shipmentId", publicid.PrefixShipment, "Shipment")
	if err != nil {
		return err
	}
	detail, err := h.booker.Get(r.Context(), p, shipmentID)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) events(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	shipmentID, err := httpx.PathPublicID(r, "shipmentId", publicid.PrefixShipment, "Shipment")
	if err != nil {
		return err
	}
	row, err := h.booker.q.GetShipmentByPublicID(r.Context(), dbgen.GetShipmentByPublicIDParams{
		PublicID: shipmentID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Shipment")
		}
		return apierr.Internal(err)
	}
	if err := h.booker.authorizeShipmentAccess(p, row); err != nil {
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
	rows, err := h.booker.q.ListShipmentEvents(r.Context(), dbgen.ListShipmentEventsParams{
		ShipmentID: row.ID, RowLimit: int32(limit), RowOffset: int32((page - 1) * limit),
	})
	if err != nil {
		return apierr.Internal(err)
	}
	items := make([]map[string]any, 0, len(rows))
	for _, e := range rows {
		items = append(items, map[string]any{
			"id": e.PublicID, "sequence": e.Sequence, "eventType": e.EventType,
			"fromStatus": e.FromStatus, "toStatus": e.ToStatus,
			"occurredAt": e.OccurredAt, "recordedAt": e.RecordedAt,
			"description": e.Description, "reasonCode": e.ReasonCode,
			"actorName": e.ActorName, "actorType": e.ActorType,
			"operatingUnitCode": e.OperatingUnitCode, "locationPincode": e.LocationPincode,
		})
	}
	return httpx.OK(w, map[string]any{"data": items})
}

// ---- cancel ----------------------------------------------------------------

type cancelRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	shipmentID, err := httpx.PathPublicID(r, "shipmentId", publicid.PrefixShipment, "Shipment")
	if err != nil {
		return err
	}
	var req cancelRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	detail, err := h.booker.Cancel(r.Context(), p, shipmentID, req.Reason)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

// ---- label -----------------------------------------------------------------

func (h *Handler) label(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	shipmentID, err := httpx.PathPublicID(r, "shipmentId", publicid.PrefixShipment, "Shipment")
	if err != nil {
		return err
	}
	format, err := httpx.QueryEnum(r, "format", []string{"JSON", "ZPL"})
	if err != nil {
		return err
	}
	if format == "" {
		format = "JSON"
	}
	label, err := h.booker.Label(r.Context(), p, shipmentID, format)
	if err != nil {
		return err
	}
	if format == "ZPL" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(RenderZPL(*label)))
		return nil
	}
	return httpx.OK(w, label)
}

// ---- authorization ---------------------------------------------------------

// authorizeShipmentAccess enforces object-level access on a shipment.
//
// Tenant isolation is already handled by the query's organization_id predicate;
// this adds the operating-unit and customer-portal dimensions. A caller outside
// scope receives 404 rather than 403, so the endpoint cannot be used to probe
// which AWBs exist in other branches.
func (b *Booker) authorizeShipmentAccess(p *tenant.Principal, s dbgen.GetShipmentByPublicIDRow) error {
	return b.authorizeShipmentAccessRaw(p, s.CustomerID, s.OriginBranchID, s.DestinationBranchID, s.BookingUnitID)
}

func (b *Booker) authorizeShipmentAccessRaw(
	p *tenant.Principal, customerID int64, originBranch, destBranch, bookingUnit *int64,
) error {
	if !p.IsCustomerInScope(customerID) {
		return apierr.NotFound("Shipment")
	}
	scope := p.UnitScope("shipment.read_all")
	if scope == nil {
		return nil
	}
	for _, unit := range []*int64{originBranch, destBranch, bookingUnit} {
		if unit == nil {
			continue
		}
		for _, allowed := range scope {
			if allowed == *unit {
				return nil
			}
		}
	}
	return apierr.NotFound("Shipment")
}

// ---- detail assembly -------------------------------------------------------

func (b *Booker) buildDetail(ctx context.Context, q *dbgen.Queries, s dbgen.Shipment) (*Detail, error) {
	full, err := q.GetShipmentByPublicID(ctx, dbgen.GetShipmentByPublicIDParams{
		PublicID: s.PublicID, OrganizationID: s.OrganizationID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return b.loadDetail(ctx, q, full)
}

// loadDetail assembles the full shipment view from its row plus the three
// snapshots and the package list — five queries, no N+1.
func (b *Booker) loadDetail(ctx context.Context, q *dbgen.Queries, s dbgen.GetShipmentByPublicIDRow) (*Detail, error) {
	d := &Detail{
		ID: s.PublicID, AWB: s.Awb, Status: s.CurrentStatus, StatusChangedAt: s.StatusChangedAt,
		PaymentMode: s.PaymentMode, BookedAt: s.BookedAt, CreatedAt: s.CreatedAt,
		Customer: Ref{ID: s.CustomerPublicID, Code: s.CustomerCode, Name: s.CustomerName},
		Service:  Ref{ID: s.ServicePublicID, Code: s.ServiceCode, Name: s.ServiceName},
		Origin: Endpoint{
			Pincode: s.OriginPincode, IsRemote: s.IsRemoteOrigin,
			Branch: refFrom(s.OriginBranchPublicID, s.OriginBranchCode, s.OriginBranchName),
			Hub:    refFromCode(s.OriginHubCode),
		},
		Destination: Endpoint{
			Pincode: s.DestinationPincode, IsRemote: s.IsRemoteDestination,
			Branch: refFrom(s.DestinationBranchPublicID, s.DestinationBranchCode, s.DestinationBranchName),
			Hub:    refFromCode(s.DestinationHubCode),
		},
		PieceCount: s.PieceCount, ActualWeightGrams: s.ActualWeightGrams,
		VolumetricWeightGrams: s.VolumetricWeightGrams, ChargeableWeightGrams: s.ChargeableWeightGrams,
		Currency: s.Currency, DeclaredValueMinor: s.DeclaredValueMinor,
		CODAmountMinor: s.CodAmountMinor, TotalAmountMinor: s.TotalAmountMinor,
		InsuranceRequired: s.InsuranceRequired, SLAHours: s.SlaHours,
		PromisedDeliveryAt: s.PromisedDeliveryAt,
		ContentDescription: s.ContentDescription, IsFragile: s.IsFragile,
		IsDangerousGoods: s.IsDangerousGoods, CancelledAt: s.CancelledAt,
		Packages: []PackageView{},
	}
	if s.ReferenceNumber != nil {
		d.ReferenceNumber = *s.ReferenceNumber
	}
	if s.SpecialInstructions != nil {
		d.SpecialInstructions = *s.SpecialInstructions
	}
	if s.CancellationReason != nil {
		d.CancellationReason = *s.CancellationReason
	}
	for _, t := range AllowedFrom(Status(s.CurrentStatus)) {
		d.AllowedTransitions = append(d.AllowedTransitions, string(t))
	}
	if d.AllowedTransitions == nil {
		d.AllowedTransitions = []string{}
	}

	packages, err := q.ListShipmentPackages(ctx, s.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, p := range packages {
		pv := PackageView{
			ID: p.PublicID, Sequence: p.Sequence, PieceBarcode: p.PieceBarcode,
			ActualWeightGrams: p.ActualWeightGrams, VolumetricWeightGrams: p.VolumetricWeightGrams,
			LengthMM: p.LengthMm, WidthMM: p.WidthMm, HeightMM: p.HeightMm,
			DeclaredValueMinor: p.DeclaredValueMinor,
		}
		if p.Reference != nil {
			pv.Reference = *p.Reference
		}
		if p.ContentDescription != nil {
			pv.ContentDescription = *p.ContentDescription
		}
		d.Packages = append(d.Packages, pv)
	}

	if charges, cErr := q.GetShipmentChargeSnapshot(ctx, s.ID); cErr == nil {
		d.Charges = &ChargesView{
			Currency: charges.Currency, RateCardCode: charges.RateCardCode,
			RateCardVersion: charges.RateCardVersionNo, PricingEngineVersion: charges.PricingEngineVersion,
			FreightMinor: charges.FreightMinor, SurchargeTotalMinor: charges.SurchargeTotalMinor,
			DiscountTotalMinor: charges.DiscountTotalMinor, TaxableMinor: charges.TaxableMinor,
			TaxTotalMinor: charges.TaxTotalMinor, RoundingMinor: charges.RoundingMinor,
			TotalMinor: charges.TotalMinor, LineItems: charges.LineItems,
		}
		if charges.OriginZoneCode != nil {
			d.Charges.OriginZoneCode = *charges.OriginZoneCode
		}
		if charges.DestinationZoneCode != nil {
			d.Charges.DestinationZoneCode = *charges.DestinationZoneCode
		}
	} else if !database.IsNoRows(cErr) {
		return nil, apierr.Internal(cErr)
	}

	if route, rErr := q.GetShipmentRouteSnapshot(ctx, s.ID); rErr == nil {
		d.Route = &RouteView{
			ResolutionSource: route.ResolutionSource, TransitHours: route.TransitHours,
			SLAHours: route.SlaHours, Legs: route.Legs,
		}
		if route.RouteCode != nil {
			d.Route.RouteCode = *route.RouteCode
		}
		if route.ExplanationID != nil {
			d.Route.ExplanationID = *route.ExplanationID
		}
	} else if !database.IsNoRows(rErr) {
		return nil, apierr.Internal(rErr)
	}

	addresses, err := q.ListShipmentAddressSnapshots(ctx, s.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	if len(addresses) > 0 {
		d.Addresses = map[string]any{}
		for _, a := range addresses {
			d.Addresses[snakeToCamel(a.Role)] = map[string]any{
				"contactName": a.ContactName, "companyName": a.CompanyName,
				"phone": a.Phone, "altPhone": a.AltPhone, "email": a.Email,
				"line1": a.Line1, "line2": a.Line2, "landmark": a.Landmark,
				"city": a.CityName, "state": a.StateName, "pincode": a.Pincode,
				"countryCode": a.CountryCode,
			}
		}
	}
	return d, nil
}

// ---- helpers ---------------------------------------------------------------

func refFrom(id *string, code, name *string) *Ref {
	if code == nil {
		return nil
	}
	ref := &Ref{Code: *code}
	if id != nil {
		ref.ID = *id
	}
	if name != nil {
		ref.Name = *name
	}
	return ref
}

func refFromCode(code *string) *Ref {
	if code == nil {
		return nil
	}
	return &Ref{Code: *code}
}

func snakeToCamel(s string) string {
	switch s {
	case "SENDER":
		return "sender"
	case "RECIPIENT":
		return "recipient"
	case "RETURN":
		return "return"
	default:
		return s
	}
}

// readBody buffers the request body so it can serve both JSON decoding and the
// idempotency digest. The BodyLimit middleware has already capped its size.
// ReadBody reads the raw request body once, so it can serve both decoding and
// an idempotency digest. Exported for the partner transport, which needs the
// same two uses of one body.
func ReadBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, apierr.New(http.StatusRequestEntityTooLarge, apierr.CodePayloadTooLarge,
				"Request body is larger than the permitted limit.")
		}
		return nil, apierr.BadRequest("Request body could not be read.")
	}
	if len(body) == 0 {
		return nil, apierr.BadRequest("Request body must not be empty.")
	}
	return body, nil
}

// decodeStrict rejects unknown fields, which is the mass-assignment control:
// a client cannot smuggle organizationId, status or awb into a booking.
// DecodeStrict decodes with unknown-field rejection, which is the
// mass-assignment control (§35).
func DecodeStrict(body []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apierr.Validation("Request body could not be parsed.",
			map[string]any{"detail": err.Error()})
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apierr.BadRequest("Request body must contain exactly one JSON object.")
	}
	return nil
}

func strPtr(s string) *string { return &s }
