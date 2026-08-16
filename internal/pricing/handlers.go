package pricing

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/auth"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/geography"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/money"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/product"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Handler exposes the quote endpoint and rate card administration.
type Handler struct {
	engine  *Engine
	q       *dbgen.Queries
	geo     *geography.Service
	product *product.Service
	audit   *audit.Recorder
}

// NewHandler builds the pricing handler.
func NewHandler(e *Engine, q *dbgen.Queries, geo *geography.Service, prod *product.Service, rec *audit.Recorder) *Handler {
	return &Handler{engine: e, q: q, geo: geo, product: prod, audit: rec}
}

// Routes mounts pricing endpoints under /pricing.
func (h *Handler) Routes(r chi.Router) {
	r.With(auth.RequirePermission("pricing.quote")).Post("/quote", httpx.Wrap(h.quote))
	r.With(auth.RequirePermission("rate_card.read")).Get("/state-base-rates", httpx.Wrap(h.listStateBaseRates))
	r.With(auth.RequirePermission("rate_card.manage")).Post("/state-base-rates", httpx.Wrap(h.upsertStateBaseRate))
	r.With(auth.RequirePermission("rate_card.read")).Get("/package-types", httpx.Wrap(h.listPackageTypes))
	r.With(auth.RequirePermission("rate_card.manage")).Post("/package-types", httpx.Wrap(h.upsertPackageType))
}

// PaymentModes is the accepted payment-mode enum.
var PaymentModes = []string{"PREPAID", "COD", "CREDIT", "TO_PAY"}

type packageRequest struct {
	ActualWeightGrams int `json:"actualWeightGrams"`
	LengthMM          int `json:"lengthMm,omitempty"`
	WidthMM           int `json:"widthMm,omitempty"`
	HeightMM          int `json:"heightMm,omitempty"`
}

// QuoteRequest is the quote payload. Exported because the partner transport
// (M32) accepts the same body on its own scope-gated endpoint.
type QuoteRequest struct {
	OriginPincode      string           `json:"originPincode"`
	DestinationPincode string           `json:"destinationPincode"`
	ServiceCode        string           `json:"serviceCode"`
	CustomerID         string           `json:"customerId,omitempty"`
	Packages           []packageRequest `json:"packages"`
	PaymentMode        string           `json:"paymentMode"`
	DeclaredValueMinor int64            `json:"declaredValueMinor,omitempty"`
	CODAmountMinor     int64            `json:"codAmountMinor,omitempty"`
	InsuranceRequired  bool             `json:"insuranceRequired,omitempty"`
	At                 *time.Time       `json:"at,omitempty"`
}

// quote prices a hypothetical shipment.
//
// It deliberately reuses the same zone resolution and the same engine as
// booking, so a quote and the price actually charged cannot drift.
func (h *Handler) quote(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req QuoteRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}

	in, err := h.buildQuoteInput(r.Context(), p, req)
	if err != nil {
		return err
	}
	q, err := h.engine.Quote(r.Context(), *in)
	if err != nil {
		return err
	}
	return httpx.OK(w, q)
}

// QuoteFor prices a request outside the internal HTTP endpoint.
//
// The partner API (M32) calls this so an external quote is computed by exactly
// the same code — same validation, same zone resolution, same rate card — as
// the one the operations console shows. A partner and a branch quoting the
// same parcel must never see different numbers.
func (h *Handler) QuoteFor(
	ctx context.Context, p *tenant.Principal, req QuoteRequest,
) (*Quote, error) {
	in, err := h.buildQuoteInput(ctx, p, req)
	if err != nil {
		return nil, err
	}
	return h.engine.Quote(ctx, *in)
}

// buildQuoteInput validates a quote request and resolves every reference it
// names into the engine's input struct.
func (h *Handler) buildQuoteInput(ctx context.Context, p *tenant.Principal, req QuoteRequest) (*QuoteInput, error) {
	v := validate.New()
	originPin := v.Pincode("originPincode", req.OriginPincode)
	destPin := v.Pincode("destinationPincode", req.DestinationPincode)
	serviceCode := v.Code("serviceCode", req.ServiceCode)
	paymentMode := v.Enum("paymentMode", req.PaymentMode, PaymentModes, true)
	v.NonNegativeMinor("declaredValueMinor", req.DeclaredValueMinor)
	v.NonNegativeMinor("codAmountMinor", req.CODAmountMinor)
	if len(req.Packages) == 0 {
		v.Add("packages", "At least one package is required.")
	}
	if len(req.Packages) > 50 {
		v.Add("packages", "A quote may cover at most 50 packages.")
	}
	for i, pkg := range req.Packages {
		v.IntRange(idx("packages[%d].actualWeightGrams", i), pkg.ActualWeightGrams, 1, 10_000_000)
		for _, dim := range []struct {
			name  string
			value int
		}{
			{"lengthMm", pkg.LengthMM}, {"widthMm", pkg.WidthMM}, {"heightMm", pkg.HeightMM},
		} {
			if dim.value != 0 {
				v.IntRange(idx("packages[%d]."+dim.name, i), dim.value, 1, 100_000)
			}
		}
	}
	if paymentMode == "COD" && req.CODAmountMinor <= 0 {
		v.Add("codAmountMinor", "A COD shipment must specify a positive COD amount.")
	}
	if paymentMode != "COD" && req.CODAmountMinor > 0 {
		v.Add("codAmountMinor", "Only COD shipments may specify a COD amount.")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}

	at := time.Now()
	if req.At != nil {
		at = *req.At
	}

	origin, err := h.geo.RequireActivePincode(ctx, originPin, geography.DefaultCountry)
	if err != nil {
		return nil, err
	}
	dest, err := h.geo.RequireActivePincode(ctx, destPin, geography.DefaultCountry)
	if err != nil {
		return nil, err
	}
	originZone, err := h.geo.ResolveZone(ctx, p.OrganizationID, origin.ID, at)
	if err != nil {
		return nil, err
	}
	destZone, err := h.geo.ResolveZone(ctx, p.OrganizationID, dest.ID, at)
	if err != nil {
		return nil, err
	}
	svc, err := h.product.GetActiveByCode(ctx, p.OrganizationID, serviceCode, at)
	if err != nil {
		return nil, err
	}
	if err := ValidateAgainstService(svc, req.toPackages(), req.CODAmountMinor, req.DeclaredValueMinor, paymentMode); err != nil {
		return nil, err
	}

	in := &QuoteInput{
		OrganizationID: p.OrganizationID,
		Service:        svc,
		OriginZoneID:   originZone.ZoneID, OriginZoneCode: originZone.ZoneCode,
		DestZoneID: destZone.ZoneID, DestZoneCode: destZone.ZoneCode,
		OriginStateID: origin.StateID, DestStateID: dest.StateID,
		Packages:           req.toPackages(),
		PaymentMode:        paymentMode,
		DeclaredValueMinor: req.DeclaredValueMinor,
		CODAmountMinor:     req.CODAmountMinor,
		InsuranceRequired:  req.InsuranceRequired,
		OriginIsRemote:     originZone.IsRemote,
		DestIsRemote:       destZone.IsRemote,
		// GST is intra-state when both ends sit in the same state.
		IsIntraState: origin.StateID == dest.StateID,
		Currency:     money.Currency(p.OrganizationCurrency),
		At:           at,
	}

	if req.CustomerID != "" {
		if !publicid.Valid(publicid.PrefixCustomer, req.CustomerID) {
			return nil, apierr.Validation("customerId is not a valid customer identifier.", nil)
		}
		customer, cErr := h.q.GetCustomerByPublicID(ctx, dbgen.GetCustomerByPublicIDParams{
			PublicID: req.CustomerID, OrganizationID: p.OrganizationID,
		})
		if cErr != nil {
			if database.IsNoRows(cErr) {
				return nil, apierr.NotFound("Customer")
			}
			return nil, apierr.Internal(cErr)
		}
		// A portal principal may only quote for its own customers.
		if !p.IsCustomerInScope(customer.ID) {
			return nil, apierr.Forbidden("You do not have access to this customer.")
		}
		in.CustomerID = &customer.ID
		if customer.OwningUnitID != nil {
			if f, fErr := h.q.GetFranchiseByOperatingUnit(ctx, dbgen.GetFranchiseByOperatingUnitParams{
				OperatingUnitID: *customer.OwningUnitID, OrganizationID: customer.OrganizationID}); fErr == nil {
				in.FranchiseID = &f.ID
			}
		}
	}
	return in, nil
}

func (req QuoteRequest) toPackages() []Package {
	out := make([]Package, 0, len(req.Packages))
	for _, p := range req.Packages {
		out = append(out, Package{
			ActualWeightGrams: int32(p.ActualWeightGrams),
			LengthMM:          int32(p.LengthMM),
			WidthMM:           int32(p.WidthMM),
			HeightMM:          int32(p.HeightMM),
		})
	}
	return out
}

// ValidateAgainstService enforces the product's physical and commercial
// envelope. It is shared by quoting and booking so a quote can never succeed
// for a shipment that booking would reject.
func ValidateAgainstService(svc *dbgen.CourierService, packages []Package, codMinor, declaredMinor int64, paymentMode string) error {
	v := validate.New()
	var total int32
	for i, pkg := range packages {
		total += pkg.ActualWeightGrams
		if svc.MaxLengthMm != nil && pkg.LengthMM > *svc.MaxLengthMm {
			v.Addf(idx("packages[%d].lengthMm", i), "Exceeds the %d mm limit for %s.", *svc.MaxLengthMm, svc.Code)
		}
		if svc.MaxWidthMm != nil && pkg.WidthMM > *svc.MaxWidthMm {
			v.Addf(idx("packages[%d].widthMm", i), "Exceeds the %d mm limit for %s.", *svc.MaxWidthMm, svc.Code)
		}
		if svc.MaxHeightMm != nil && pkg.HeightMM > *svc.MaxHeightMm {
			v.Addf(idx("packages[%d].heightMm", i), "Exceeds the %d mm limit for %s.", *svc.MaxHeightMm, svc.Code)
		}
		if svc.MaxDimensionSumMm != nil {
			sum := pkg.LengthMM + pkg.WidthMM + pkg.HeightMM
			if sum > *svc.MaxDimensionSumMm {
				v.Addf(idx("packages[%d]", i),
					"Combined dimensions %d mm exceed the %d mm limit for %s.", sum, *svc.MaxDimensionSumMm, svc.Code)
			}
		}
	}
	if total < svc.MinWeightGrams {
		v.Addf("packages", "Total weight %dg is below the %dg minimum for %s.", total, svc.MinWeightGrams, svc.Code)
	}
	if total > svc.MaxWeightGrams {
		v.Addf("packages", "Total weight %dg exceeds the %dg maximum for %s.", total, svc.MaxWeightGrams, svc.Code)
	}
	if paymentMode == "COD" && !svc.CodAllowed {
		v.Addf("paymentMode", "%s does not support cash on delivery.", svc.Code)
	}
	if svc.MaxCodAmountMinor != nil && codMinor > *svc.MaxCodAmountMinor {
		v.Addf("codAmountMinor", "Exceeds the COD limit for %s.", svc.Code)
	}
	if svc.MaxDeclaredValueMinor != nil && declaredMinor > *svc.MaxDeclaredValueMinor {
		v.Addf("declaredValueMinor", "Exceeds the declared value limit for %s.", svc.Code)
	}
	return v.Err()
}

// idx builds an indexed field path for validation errors, e.g. packages[2].lengthMm.
func idx(format string, i int) string {
	return fmt.Sprintf(format, i)
}
