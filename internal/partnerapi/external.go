package partnerapi

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/partner"
	"github.com/ceserve/courier-os/internal/pickup"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/idempotency"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/pricing"
	"github.com/ceserve/courier-os/internal/serviceability"
	"github.com/ceserve/courier-os/internal/shipment"
)

// ---------------------------------------------------------------------------
// Serviceability and pricing
// ---------------------------------------------------------------------------

type serviceabilityRequest struct {
	OriginPincode      string     `json:"originPincode"`
	DestinationPincode string     `json:"destinationPincode"`
	ServiceCode        string     `json:"serviceCode"`
	At                 *time.Time `json:"at,omitempty"`
}

func (h *Handler) checkServiceability(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	var req serviceabilityRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	origin := v.Pincode("originPincode", req.OriginPincode)
	dest := v.Pincode("destinationPincode", req.DestinationPincode)
	serviceCode := v.Code("serviceCode", req.ServiceCode)
	if err := v.Err(); err != nil {
		return err
	}
	at := time.Now()
	if req.At != nil {
		at = *req.At
	}

	// IncludeExplain is deliberately false for partners: the explanation names
	// internal routing rules and facility codes, which is operational detail
	// another company has no business seeing.
	res, err := h.routing.Resolve(r.Context(), serviceability.Request{
		OrganizationID: p.OrganizationID,
		OriginPincode:  origin, DestPincode: dest,
		ServiceCode: serviceCode, At: at,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, res)
}

func (h *Handler) quote(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	var req pricing.QuoteRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	q, err := h.pricing.QuoteFor(r.Context(), p, req)
	if err != nil {
		return err
	}
	return httpx.OK(w, q)
}

// ---------------------------------------------------------------------------
// Shipments
// ---------------------------------------------------------------------------

// createShipment books through the same Booker the internal API uses.
//
// Idempotency-Key is required here, unlike on the internal endpoint. An
// operations clerk who double-submits sees the duplicate and cancels it; a
// partner's retry loop does not, so an unkeyed retry after a timeout would
// quietly produce two parcels and two invoices.
func (h *Handler) createShipment(w http.ResponseWriter, r *http.Request) error {
	auth, err := authFrom(r)
	if err != nil {
		return err
	}
	p := auth.Principal

	key, err := httpx.IdempotencyKey(r, true)
	if err != nil {
		return err
	}
	body, err := shipment.ReadBody(w, r)
	if err != nil {
		return err
	}
	var req shipment.BookingRequest
	if err := shipment.DecodeStrict(body, &req); err != nil {
		return err
	}
	if err := shipment.ValidateBooking(&req, 0); err != nil {
		return err
	}

	var prepared *shipment.Prepared
	out, replayed, err := h.idempotency.Run(r.Context(), idempotency.Request{
		OrganizationID: p.OrganizationID,
		// No UserID: an API key has no user row behind it, and the column is a
		// foreign key. The key is identified in the audit trail instead.
		Endpoint: "POST /api/v1/partner/shipments", Key: key, Body: body,
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
		w.Header().Set("Idempotent-Replay", "true")
	}
	if out.ResourcePublicID != "" {
		w.Header().Set("Location", "/api/v1/partner/shipments/"+out.ResourcePublicID)
	}
	return httpx.JSON(w, out.Status, out.Body)
}

func (h *Handler) getShipment(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "shipmentId", publicid.PrefixShipment, "Shipment")
	if err != nil {
		return err
	}
	detail, err := h.booker.Get(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type cancelRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) cancelShipment(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "shipmentId", publicid.PrefixShipment, "Shipment")
	if err != nil {
		return err
	}
	var req cancelRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	detail, err := h.booker.Cancel(r.Context(), p, id, req.Reason)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) label(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "shipmentId", publicid.PrefixShipment, "Shipment")
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
	label, err := h.booker.Label(r.Context(), p, id, format)
	if err != nil {
		return err
	}
	if format == "ZPL" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(shipment.RenderZPL(*label)))
		return nil
	}
	return httpx.OK(w, label)
}

// proofOfDelivery returns the delivery POD for a shipment.
//
// Addressed by shipment rather than by POD id, because a partner knows the
// shipment it booked and has no way to learn a POD identifier otherwise.
func (h *Handler) proofOfDelivery(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "shipmentId", publicid.PrefixShipment, "Shipment")
	if err != nil {
		return err
	}
	// Resolve through the shipment first: that is what applies the tenant and
	// object-level checks before any POD row is touched.
	row, err := h.booker.Queries().GetShipmentByPublicID(r.Context(),
		dbgen.GetShipmentByPublicIDParams{PublicID: id, OrganizationID: p.OrganizationID})
	if err != nil {
		return apierr.NotFound("Shipment")
	}
	proof, err := h.booker.Queries().GetProofOfDeliveryForShipment(r.Context(),
		dbgen.GetProofOfDeliveryForShipmentParams{ShipmentID: row.ID, PodType: "DELIVERY"})
	if err != nil {
		return apierr.NotFound("Proof of delivery")
	}
	detail, err := h.pod.Get(r.Context(), p, proof.PublicID)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

// ---------------------------------------------------------------------------
// Tracking
// ---------------------------------------------------------------------------

// track returns the public timeline for one AWB, confined to the key's tenant.
//
// The unauthenticated /api/v1/track endpoint resolves an AWB globally, which is
// correct there — an AWB is unique across the platform and a consignee has no
// tenant. Here it would be a cross-tenant read, so the AWB is confirmed to
// belong to this organization first and a miss is a plain 404 either way.
func (h *Handler) track(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	awb, err := httpx.PathParam(r, "awb", 40)
	if err != nil {
		return err
	}
	if _, qErr := h.booker.Queries().GetShipmentByAWB(r.Context(), dbgen.GetShipmentByAWBParams{
		Awb: awb, OrganizationID: p.OrganizationID,
	}); qErr != nil {
		return apierr.NotFound("Shipment")
	}
	res, err := h.tracking.Track(r.Context(), awb)
	if err != nil {
		return err
	}
	return httpx.OK(w, res)
}

// ---------------------------------------------------------------------------
// Pickups
// ---------------------------------------------------------------------------

type createPickupRequest struct {
	CustomerID    string         `json:"customerId"`
	PickupType    string         `json:"pickupType"`
	AddressID     string         `json:"addressId,omitempty"`
	ContactName   string         `json:"contactName,omitempty"`
	ContactPhone  string         `json:"contactPhone,omitempty"`
	Line1         string         `json:"line1,omitempty"`
	Line2         string         `json:"line2,omitempty"`
	Landmark      string         `json:"landmark,omitempty"`
	City          string         `json:"city,omitempty"`
	State         string         `json:"state,omitempty"`
	Pincode       string         `json:"pincode,omitempty"`
	ScheduledDate string         `json:"scheduledDate"`
	WindowStart   string         `json:"windowStart"`
	WindowEnd     string         `json:"windowEnd"`
	ExpectedPiece int            `json:"expectedPieceCount,omitempty"`
	Instructions  string         `json:"specialInstructions,omitempty"`
	ShipmentIDs   []string       `json:"shipmentIds,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

func (h *Handler) createPickup(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	var req createPickupRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}

	v := validate.New()
	v.PublicID("customerId", req.CustomerID, publicid.PrefixCustomer, true)
	pickupType := v.Enum("pickupType", req.PickupType,
		[]string{"SCHEDULED", "ON_DEMAND", "BULK"}, true)
	scheduled := parseDate(v, "scheduledDate", req.ScheduledDate)
	start := parseTime(v, "windowStart", req.WindowStart)
	end := parseTime(v, "windowEnd", req.WindowEnd)
	if start != nil && end != nil && !end.After(*start) {
		v.Add("windowEnd", "must be after the start of the window")
	}
	if req.AddressID == "" {
		v.Required("line1", req.Line1)
		v.Required("contactName", req.ContactName)
		v.Phone("contactPhone", req.ContactPhone, true)
		v.Pincode("pincode", req.Pincode)
	} else {
		v.PublicID("addressId", req.AddressID, publicid.PrefixCustomerAddress, false)
	}
	pieces := req.ExpectedPiece
	if pieces < 1 {
		pieces = 1
	}
	v.IntRange("expectedPieceCount", pieces, 1, 5000)
	for i, id := range req.ShipmentIDs {
		v.PublicID(fieldIdx("shipmentIds", i), id, publicid.PrefixShipment, true)
	}
	if err := v.Err(); err != nil {
		return err
	}

	detail, err := h.pickup.Create(r.Context(), p, pickup.CreateInput{
		CustomerID: req.CustomerID, PickupType: pickupType, AddressID: req.AddressID,
		ContactName: req.ContactName, ContactPhone: req.ContactPhone,
		Line1: req.Line1, Line2: req.Line2, Landmark: req.Landmark,
		City: req.City, State: req.State, Pincode: req.Pincode,
		ScheduledDate: scheduled, WindowStart: *start, WindowEnd: *end,
		ExpectedPiece: pieces, Instructions: req.Instructions,
		MaxAttempts: 3, ShipmentIDs: req.ShipmentIDs, Metadata: req.Metadata,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/partner/pickups/"+detail.ID, detail)
}

func (h *Handler) getPickup(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "pickupId", publicid.PrefixPickupRequest, "Pickup request")
	if err != nil {
		return err
	}
	detail, err := h.pickup.Get(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

// ---------------------------------------------------------------------------
// Self-description
// ---------------------------------------------------------------------------

func (h *Handler) whoami(w http.ResponseWriter, r *http.Request) error {
	auth, err := authFrom(r)
	if err != nil {
		return err
	}
	scopes := auth.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	limit := int(auth.RateLimit)
	if limit <= 0 {
		limit = h.defaultRateLimit
	}
	return httpx.OK(w, map[string]any{
		"keyName":            auth.KeyName,
		"organization":       auth.Principal.OrganizationCode,
		"scopes":             scopes,
		"rateLimitPerMinute": limit,
		"availableScopes":    partner.AllScopes,
	})
}
