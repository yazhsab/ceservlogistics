package shipment

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/customer"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/geography"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/money"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
	"github.com/ceserve/courier-os/internal/pricing"
	"github.com/ceserve/courier-os/internal/product"
	"github.com/ceserve/courier-os/internal/serviceability"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Address is a sender or recipient supplied at booking.
type Address struct {
	AddressID   string   `json:"addressId,omitempty"`
	ContactName string   `json:"contactName"`
	CompanyName string   `json:"companyName,omitempty"`
	Phone       string   `json:"phone"`
	AltPhone    string   `json:"altPhone,omitempty"`
	Email       string   `json:"email,omitempty"`
	Line1       string   `json:"line1"`
	Line2       string   `json:"line2,omitempty"`
	Landmark    string   `json:"landmark,omitempty"`
	City        string   `json:"city,omitempty"`
	State       string   `json:"state,omitempty"`
	Pincode     string   `json:"pincode"`
	CountryCode string   `json:"countryCode,omitempty"`
	Latitude    *float64 `json:"latitude,omitempty"`
	Longitude   *float64 `json:"longitude,omitempty"`

	// resolved during validation
	sourceAddressID *int64
}

// PackageInput is one piece being booked.
type PackageInput struct {
	Reference          string `json:"reference,omitempty"`
	ActualWeightGrams  int    `json:"actualWeightGrams"`
	LengthMM           int    `json:"lengthMm,omitempty"`
	WidthMM            int    `json:"widthMm,omitempty"`
	HeightMM           int    `json:"heightMm,omitempty"`
	ContentDescription string `json:"contentDescription,omitempty"`
	DeclaredValueMinor int64  `json:"declaredValueMinor,omitempty"`
}

// BookingRequest is the booking payload.
type BookingRequest struct {
	CustomerID          string         `json:"customerId"`
	ReferenceNumber     string         `json:"referenceNumber,omitempty"`
	ServiceCode         string         `json:"serviceCode"`
	PaymentMode         string         `json:"paymentMode"`
	Sender              Address        `json:"sender"`
	Recipient           Address        `json:"recipient"`
	Packages            []PackageInput `json:"packages"`
	DeclaredValueMinor  int64          `json:"declaredValueMinor,omitempty"`
	CODAmountMinor      int64          `json:"codAmountMinor,omitempty"`
	InsuranceRequired   bool           `json:"insuranceRequired,omitempty"`
	ContentDescription  string         `json:"contentDescription"`
	SpecialInstructions string         `json:"specialInstructions,omitempty"`
	IsFragile           bool           `json:"isFragile,omitempty"`
	IsDangerousGoods    bool           `json:"isDangerousGoods,omitempty"`
	BookingUnitID       string         `json:"bookingUnitId,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
}

// Booker executes the booking transaction.
type Booker struct {
	db          *database.DB
	q           *dbgen.Queries
	geo         *geography.Service
	product     *product.Service
	routing     *serviceability.Resolver
	pricing     *pricing.Engine
	customer    *customer.Service
	awb         *Allocator
	audit       *audit.Recorder
	log         *slog.Logger
	metrics     *telemetry.Metrics
	maxPackages int

	// transitioner is set at wiring time so cancellation — which does not go
	// through the transition engine — still raises its observers.
	transitioner *Transitioner
}

// NewBooker builds the booking service.
func NewBooker(
	db *database.DB, q *dbgen.Queries, geo *geography.Service, prod *product.Service,
	routing *serviceability.Resolver, price *pricing.Engine, cust *customer.Service,
	rec *audit.Recorder, log *slog.Logger, m *telemetry.Metrics, maxPackages int,
) *Booker {
	if maxPackages <= 0 {
		maxPackages = 50
	}
	return &Booker{
		db: db, q: q, geo: geo, product: prod, routing: routing, pricing: price,
		customer: cust, awb: NewAllocator(q), audit: rec, log: log, metrics: m,
		maxPackages: maxPackages,
	}
}

// Prepared is the immutable result of the read-only booking preparation phase.
//
// Everything that can fail on business grounds — customer, product,
// serviceability, routing, pricing — and the AWB allocation happen here, using
// pooled connections, before any transaction is opened. Commit then performs
// only writes, on the transaction's connection.
type Prepared struct{ inner *prepared }

// prepared holds everything resolved before the transaction opens.
//
// Every read that can fail on business grounds — customer, product,
// serviceability, routing, pricing — happens here, outside the transaction, so
// the transaction itself is short and holds no locks while waiting on
// validation. Only the AWB allocation, the writes and the credit reservation
// happen inside it.
type prepared struct {
	principal *tenant.Principal
	request   BookingRequest
	at        time.Time

	awb string

	customer *customer.Resolved
	service  *dbgen.CourierService
	routing  *serviceability.Result
	quote    *pricing.Quote

	originPincode *geography.Pincode
	destPincode   *geography.Pincode
	bookingUnitID *int64
	packages      []pricing.Package
}

// Prepare performs every read and validation, and allocates the AWB.
//
// It runs entirely outside a transaction. Constitution §22 wants the booking
// transaction short and uncontended; equally important, issuing a pooled query
// while holding a transaction deadlocks the pool once concurrency exceeds the
// pool size, so the two phases are kept strictly separate.
func (b *Booker) Prepare(ctx context.Context, p *tenant.Principal, req BookingRequest) (*Prepared, error) {
	prep, err := b.prepare(ctx, p, req)
	if err != nil {
		return nil, err
	}
	// AWB allocation is a single atomic statement on its own connection, so the
	// sequence row is never held for the duration of the booking. See
	// Allocator.Allocate for the trade-off this makes.
	awb, err := b.awb.Allocate(ctx, p.OrganizationID, p.OrganizationAWBPrefix, prep.at)
	if err != nil {
		return nil, err
	}
	prep.awb = awb
	return &Prepared{inner: prep}, nil
}

// Commit writes the booking.
//
// The ordering matches the Release 1 specification: shipment, packages, three
// snapshots, credit reservation, the BOOKED event and the audit record, all in
// one transaction that either lands completely or not at all.
func (b *Booker) Commit(ctx context.Context, tx pgx.Tx, prepared *Prepared) (*Detail, error) {
	prep := prepared.inner
	p := prep.principal
	awb := prep.awb

	q := b.q.WithTx(tx)
	shipmentPublicID := publicid.New(publicid.PrefixShipment)

	shipmentParams, err := b.buildShipmentParams(prep, shipmentPublicID, awb)
	if err != nil {
		return nil, err
	}
	created, err := q.CreateShipment(ctx, *shipmentParams)
	if err != nil {
		switch {
		case database.IsUniqueViolation(err, "shipments_awb_key"):
			// The UNIQUE constraint is the last line of defence behind the
			// allocator. Surfacing it as a retryable conflict is honest.
			return nil, apierr.Conflict(apierr.CodeConflict,
				"AWB allocation collided. Please retry the booking.")
		case database.IsUniqueViolation(err, "shipments_customer_reference_idx"):
			return nil, apierr.Conflict(apierr.CodeDuplicate,
				"A shipment already exists for this customer reference.").
				WithDetail("referenceNumber", prep.request.ReferenceNumber)
		}
		return nil, apierr.Internal(fmt.Errorf("create shipment: %w", err))
	}

	if err := b.writePackages(ctx, q, prep, created, awb); err != nil {
		return nil, err
	}
	if err := b.writeAddressSnapshots(ctx, q, prep, created); err != nil {
		return nil, err
	}
	if err := b.writeChargeSnapshot(ctx, q, prep, created); err != nil {
		return nil, err
	}
	if err := b.writeRouteSnapshot(ctx, q, prep, created); err != nil {
		return nil, err
	}

	// Credit is reserved inside the transaction and after the shipment row
	// exists, so the ledger entry can reference the shipment and a credit
	// failure rolls the whole booking back.
	if prep.request.PaymentMode == "CREDIT" {
		if err := b.customer.ReserveCredit(ctx, tx, p.OrganizationID, prep.customer.Customer.ID,
			prep.quote.TotalMinor, &created.ID, p.ActorUserID(), httpx.RequestID(ctx)); err != nil {
			return nil, err
		}
	}

	// A partner booking has no user behind it, so the actor is the key and the
	// user column stays null rather than pointing at a row that does not exist.
	bookedEvent, err := q.AppendShipmentEvent(ctx, dbgen.AppendShipmentEventParams{
		PublicID: publicid.New(publicid.PrefixShipmentEvent), OrganizationID: p.OrganizationID,
		ShipmentID: created.ID, Sequence: 1, EventType: "BOOKED",
		ToStatus: string(StatusBooked), ActorUserID: p.ActorUserID(), ActorType: actorTypeFor(p, false),
		OperatingUnitID: prep.bookingUnitID, LocationPincode: &prep.originPincode.Code,
		Description: fmt.Sprintf("Shipment booked at %s for delivery to %s",
			prep.originPincode.Code, prep.destPincode.Code),
		RequestID: optional(httpx.RequestID(ctx)),
		Metadata:  eventMetadata(prep, awb),
	})
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("append booked event: %w", err))
	}

	// Booking predates the transition engine and does not run through it, so
	// the observers are invoked here instead — otherwise a customer would never
	// be told their parcel was booked and no partner would ever hear about it.
	if err := b.notify(ctx, tx, p, &Result{
		Shipment: created, Event: bookedEvent, From: "", To: StatusBooked,
	}); err != nil {
		return nil, err
	}

	if err := b.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
		Action: audit.ActionShipmentBooked, ResourceType: "shipment",
		ResourceID: &created.ID, ResourcePublicID: created.PublicID,
		OperatingUnitID: prep.bookingUnitID,
		After: map[string]any{
			"awb": awb, "customerCode": prep.customer.Customer.Code,
			"serviceCode": prep.service.Code, "paymentMode": created.PaymentMode,
			"totalAmountMinor": created.TotalAmountMinor, "currency": created.Currency,
			"originPincode": created.OriginPincode, "destinationPincode": created.DestinationPincode,
			"chargeableWeightGrams": created.ChargeableWeightGrams,
			"rateCardVersion":       prep.quote.RateCardVersionID,
			"routeCode":             prep.routing.RouteCode,
		},
	})); err != nil {
		return nil, apierr.Internal(fmt.Errorf("record booking audit: %w", err))
	}

	b.metrics.RecordBusiness("booking", "created")
	b.log.Info("shipment booked",
		slog.String("awb", awb),
		slog.String("shipment_id", created.PublicID),
		slog.String("service", prep.service.Code),
		slog.Int64("total_minor", created.TotalAmountMinor))

	return b.buildDetail(ctx, q, created)
}

// prepare performs every validation and resolution step.
func (b *Booker) prepare(ctx context.Context, p *tenant.Principal, req BookingRequest) (*prepared, error) {
	at := time.Now()
	prep := &prepared{principal: p, request: req, at: at}

	// 1-2. Customer, and the tenant/scope checks around it.
	cust, err := b.customer.ResolveForBooking(ctx, p.OrganizationID, req.CustomerID)
	if err != nil {
		return nil, err
	}
	if !p.IsCustomerInScope(cust.Customer.ID) {
		return nil, apierr.NotFound("Customer")
	}
	if cust.Customer.Status != "ACTIVE" {
		return nil, apierr.Conflict(apierr.CodeConflict,
			"This customer is not active and cannot book shipments.").
			WithDetail("customerStatus", cust.Customer.Status)
	}
	prep.customer = cust

	// 3-4. Sender and recipient.
	origin, err := b.resolveAddress(ctx, p, cust, &prep.request.Sender, "sender")
	if err != nil {
		return nil, err
	}
	dest, err := b.resolveAddress(ctx, p, cust, &prep.request.Recipient, "recipient")
	if err != nil {
		return nil, err
	}
	prep.originPincode, prep.destPincode = origin, dest

	// 5. Packages against the product envelope.
	prep.packages = toPricingPackages(req.Packages)
	svc, err := b.product.GetActiveByCode(ctx, p.OrganizationID, req.ServiceCode, at)
	if err != nil {
		return nil, err
	}
	prep.service = svc
	if err := pricing.ValidateAgainstService(
		svc, prep.packages, req.CODAmountMinor, req.DeclaredValueMinor, req.PaymentMode,
	); err != nil {
		return nil, err
	}

	// 6-7. Serviceability and routing.
	route, err := b.routing.Resolve(ctx, serviceability.Request{
		OrganizationID: p.OrganizationID,
		OriginPincode:  origin.Code, DestPincode: dest.Code,
		OriginCountry: prep.request.Sender.CountryCode,
		DestCountry:   prep.request.Recipient.CountryCode,
		ServiceCode:   svc.Code, At: at, IncludeExplain: true,
	})
	if err != nil {
		return nil, err
	}
	if !route.Serviceable {
		return nil, apierr.Conflict("SHIPMENT_NOT_SERVICEABLE", route.ReasonMessage).
			WithDetail("reasonCode", route.ReasonCode).
			WithDetail("originPincode", origin.Code).
			WithDetail("destinationPincode", dest.Code).
			WithDetail("explanationId", route.ExplanationID)
	}
	prep.routing = route

	// The booking unit defaults to the resolved origin branch, which is the
	// facility that will physically take custody.
	prep.bookingUnitID = route.OriginBranchID()
	if req.BookingUnitID != "" {
		unit, uErr := b.q.GetOperatingUnitByPublicID(ctx, dbgen.GetOperatingUnitByPublicIDParams{
			PublicID: req.BookingUnitID, OrganizationID: p.OrganizationID,
		})
		if uErr != nil {
			return nil, apierr.Validation("The booking operating unit does not exist.", nil)
		}
		if err := p.RequireUnitInScope(unit.ID); err != nil {
			return nil, err
		}
		prep.bookingUnitID = &unit.ID
	}

	// 8. Pricing, using the zones the routing step already resolved.
	quote, err := b.pricing.Quote(ctx, pricing.QuoteInput{
		OrganizationID: p.OrganizationID,
		CustomerID:     &cust.Customer.ID,
		FranchiseID:    cust.FranchiseID,
		Service:        svc,
		OriginZoneID:   route.OriginZoneID(), OriginZoneCode: route.Origin.ZoneCode,
		DestZoneID: route.DestinationZoneID(), DestZoneCode: route.Destination.ZoneCode,
		Packages:           prep.packages,
		PaymentMode:        req.PaymentMode,
		DeclaredValueMinor: req.DeclaredValueMinor,
		CODAmountMinor:     req.CODAmountMinor,
		InsuranceRequired:  req.InsuranceRequired,
		OriginIsRemote:     route.Origin.IsRemote,
		DestIsRemote:       route.Destination.IsRemote,
		IsIntraState:       origin.StateID == dest.StateID,
		Currency:           money.Currency(p.OrganizationCurrency),
		At:                 at,
	})
	if err != nil {
		return nil, err
	}
	prep.quote = quote

	// 9. Payment-mode policy that does not need the transaction.
	if req.PaymentMode == "CREDIT" && cust.CreditProfile == nil {
		return nil, apierr.Conflict(apierr.CodeConflict,
			"This customer has no credit profile. Set a credit limit before booking on credit terms.")
	}
	return prep, nil
}

// resolveAddress validates a booking address and resolves its PIN code.
//
// A saved address id is honoured only when it belongs to the booking customer:
// otherwise a caller could reference another customer's address by id and have
// its details copied into their own shipment.
func (b *Booker) resolveAddress(
	ctx context.Context, p *tenant.Principal, cust *customer.Resolved, addr *Address, role string,
) (*geography.Pincode, error) {
	if addr.AddressID != "" {
		saved, err := b.q.GetCustomerAddressByPublicID(ctx, dbgen.GetCustomerAddressByPublicIDParams{
			PublicID: addr.AddressID, OrganizationID: p.OrganizationID,
		})
		if err != nil {
			if database.IsNoRows(err) {
				return nil, apierr.Validation("The saved address does not exist.",
					map[string]any{"field": role + ".addressId"})
			}
			return nil, apierr.Internal(err)
		}
		if saved.CustomerID != cust.Customer.ID {
			return nil, apierr.Validation("The saved address does not belong to this customer.",
				map[string]any{"field": role + ".addressId"})
		}
		// Explicit fields still win, so a caller can start from a saved address
		// and override the contact for one shipment.
		addr.sourceAddressID = &saved.ID
		fillFromSaved(addr, saved)
	}
	country := strings.ToUpper(strings.TrimSpace(addr.CountryCode))
	if country == "" {
		country = strings.ToUpper(strings.TrimSpace(p.OrganizationCountry))
	}
	if country == "" {
		country = geography.DefaultCountry
	}
	addr.CountryCode = country
	return b.geo.RequireActivePincode(ctx, addr.Pincode, country)
}

func fillFromSaved(addr *Address, saved dbgen.CustomerAddress) {
	if addr.ContactName == "" {
		addr.ContactName = saved.ContactName
	}
	if addr.Phone == "" {
		addr.Phone = saved.ContactPhone
	}
	if addr.AltPhone == "" && saved.AltPhone != nil {
		addr.AltPhone = *saved.AltPhone
	}
	if addr.Line1 == "" {
		addr.Line1 = saved.Line1
	}
	if addr.Line2 == "" && saved.Line2 != nil {
		addr.Line2 = *saved.Line2
	}
	if addr.Landmark == "" && saved.Landmark != nil {
		addr.Landmark = *saved.Landmark
	}
	if addr.City == "" {
		addr.City = saved.CityName
	}
	if addr.State == "" {
		addr.State = saved.StateName
	}
	if addr.Pincode == "" {
		addr.Pincode = saved.Pincode
	}
	if addr.CountryCode == "" {
		addr.CountryCode = saved.CountryCode
	}
	if addr.Latitude == nil {
		addr.Latitude = saved.Latitude
	}
	if addr.Longitude == nil {
		addr.Longitude = saved.Longitude
	}
}

func (b *Booker) buildShipmentParams(prep *prepared, publicID, awb string) (*dbgen.CreateShipmentParams, error) {
	p := prep.principal
	route := prep.routing
	quote := prep.quote

	var totalActual int32
	for _, pkg := range prep.packages {
		totalActual += pkg.ActualWeightGrams
	}

	params := &dbgen.CreateShipmentParams{
		PublicID: publicID, OrganizationID: p.OrganizationID, Awb: awb,
		CustomerID: prep.customer.Customer.ID, CourierServiceID: prep.service.ID,
		BookedByUserID: p.ActorUserID(), BookingUnitID: prep.bookingUnitID,
		PaymentMode:         prep.request.PaymentMode,
		OriginBranchID:      route.OriginBranchID(),
		OriginHubID:         route.OriginHubID(),
		DestinationHubID:    route.DestinationHubID(),
		DestinationBranchID: route.DestinationBranchID(),
		RouteDefinitionID:   route.RouteDefinitionID(),
		OriginPincode:       prep.originPincode.Code,
		DestinationPincode:  prep.destPincode.Code,
		IsRemoteOrigin:      route.Origin.IsRemote,
		IsRemoteDestination: route.Destination.IsRemote,
		// Custody starts with the facility that accepts the parcel.
		CurrentCustodyUnitID:  prep.bookingUnitID,
		PieceCount:            int32(len(prep.packages)),
		ActualWeightGrams:     totalActual,
		VolumetricWeightGrams: quote.Weight.VolumetricGrams,
		ChargeableWeightGrams: quote.Weight.ChargeableGrams,
		Currency:              quote.Currency,
		DeclaredValueMinor:    prep.request.DeclaredValueMinor,
		CodAmountMinor:        prep.request.CODAmountMinor,
		InsuranceRequired:     prep.request.InsuranceRequired,
		TotalAmountMinor:      quote.TotalMinor,
		SlaHours:              &route.SLAHours,
		PromisedDeliveryAt:    route.PromisedDeliveryAt,
		BookedAt:              prep.at,
		ContentDescription:    prep.request.ContentDescription,
		SpecialInstructions:   optional(prep.request.SpecialInstructions),
		IsFragile:             prep.request.IsFragile,
		IsDangerousGoods:      prep.request.IsDangerousGoods,
		Metadata:              encodeJSON(prep.request.Metadata),
	}
	if prep.request.ReferenceNumber != "" {
		params.ReferenceNumber = &prep.request.ReferenceNumber
	}
	return params, nil
}

func (b *Booker) writePackages(ctx context.Context, q *dbgen.Queries, prep *prepared, s dbgen.Shipment, awb string) error {
	for i, pkg := range prep.request.Packages {
		seq := i + 1
		one := []pricing.Package{prep.packages[i]}
		weight := pricing.ComputeChargeableWeight(one, prep.service, 0)
		params := dbgen.CreateShipmentPackageParams{
			PublicID: publicid.New(publicid.PrefixPackage), OrganizationID: s.OrganizationID,
			ShipmentID: s.ID, Sequence: int32(seq), PieceBarcode: PieceBarcode(awb, seq),
			ActualWeightGrams:     int32(pkg.ActualWeightGrams),
			VolumetricWeightGrams: weight.VolumetricGrams,
			// Per-piece chargeable weight is informational (the shipment is
			// priced as a whole); it exists so bagging and hub handling can
			// reason about individual pieces in Release 2.
			ChargeableWeightGrams: weight.ChargeableGrams,
			DeclaredValueMinor:    pkg.DeclaredValueMinor,
		}
		if pkg.Reference != "" {
			params.Reference = &pkg.Reference
		}
		if pkg.ContentDescription != "" {
			params.ContentDescription = &pkg.ContentDescription
		}
		if pkg.LengthMM > 0 {
			l := int32(pkg.LengthMM)
			params.LengthMm = &l
		}
		if pkg.WidthMM > 0 {
			wv := int32(pkg.WidthMM)
			params.WidthMm = &wv
		}
		if pkg.HeightMM > 0 {
			hv := int32(pkg.HeightMM)
			params.HeightMm = &hv
		}
		if _, err := q.CreateShipmentPackage(ctx, params); err != nil {
			return apierr.Internal(fmt.Errorf("create package %d: %w", seq, err))
		}
	}
	return nil
}

// writeAddressSnapshots copies sender and recipient details onto the shipment.
//
// Constitution §M08: editing a customer address later must never rewrite where
// a past parcel was sent. The snapshot table is append-only at the database
// level, so even a defective service cannot mutate it.
func (b *Booker) writeAddressSnapshots(ctx context.Context, q *dbgen.Queries, prep *prepared, s dbgen.Shipment) error {
	for _, spec := range []struct {
		role string
		addr Address
	}{
		{"SENDER", prep.request.Sender},
		{"RECIPIENT", prep.request.Recipient},
	} {
		params := dbgen.CreateAddressSnapshotParams{
			OrganizationID: s.OrganizationID, ShipmentID: s.ID, Role: spec.role,
			SourceAddressID: spec.addr.sourceAddressID,
			ContactName:     spec.addr.ContactName, Phone: spec.addr.Phone,
			CompanyName: optional(spec.addr.CompanyName), AltPhone: optional(spec.addr.AltPhone),
			Email: optional(spec.addr.Email), Line1: spec.addr.Line1,
			Line2: optional(spec.addr.Line2), Landmark: optional(spec.addr.Landmark),
			CityName: spec.addr.City, StateName: spec.addr.State,
			Pincode: spec.addr.Pincode, CountryCode: spec.addr.CountryCode,
			Latitude: spec.addr.Latitude, Longitude: spec.addr.Longitude,
		}
		if _, err := q.CreateAddressSnapshot(ctx, params); err != nil {
			return apierr.Internal(fmt.Errorf("snapshot %s address: %w", spec.role, err))
		}
	}
	return nil
}

// writeChargeSnapshot persists the exact price and the inputs that produced it.
func (b *Booker) writeChargeSnapshot(ctx context.Context, q *dbgen.Queries, prep *prepared, s dbgen.Shipment) error {
	quote := prep.quote
	lineItems, err := json.Marshal(quote.LineItems)
	if err != nil {
		return apierr.Internal(err)
	}
	inputs, err := json.Marshal(map[string]any{
		"weight":              quote.Weight,
		"packages":            prep.packages,
		"paymentMode":         prep.request.PaymentMode,
		"declaredValueMinor":  prep.request.DeclaredValueMinor,
		"codAmountMinor":      prep.request.CODAmountMinor,
		"insuranceRequired":   prep.request.InsuranceRequired,
		"originIsRemote":      prep.routing.Origin.IsRemote,
		"destinationIsRemote": prep.routing.Destination.IsRemote,
		"isIntraState":        prep.originPincode.StateID == prep.destPincode.StateID,
		"serviceCode":         prep.service.Code,
		"volumetricDivisor":   prep.service.VolumetricDivisor,
		"calculatedAt":        prep.at,
	})
	if err != nil {
		return apierr.Internal(err)
	}

	cardID := quote.RateCardDBID()
	versionID := quote.RateCardVersionDBID()
	_, err = q.CreateChargeSnapshot(ctx, dbgen.CreateChargeSnapshotParams{
		OrganizationID: s.OrganizationID, ShipmentID: s.ID,
		RateCardID: &cardID, RateCardVersionID: &versionID,
		RateCardCode: quote.RateCardCode, RateCardVersionNo: quote.VersionNumber,
		PricingEngineVersion: quote.EngineVersion, Currency: quote.Currency,
		OriginZoneCode: optional(quote.OriginZoneCode), DestinationZoneCode: optional(quote.DestinationZoneCode),
		ChargeableWeightGrams: quote.Weight.ChargeableGrams,
		FreightMinor:          quote.FreightMinor,
		SurchargeTotalMinor:   quote.SurchargeTotalMinor,
		DiscountTotalMinor:    quote.DiscountTotalMinor,
		TaxableMinor:          quote.TaxableMinor,
		TaxTotalMinor:         quote.TaxTotalMinor,
		RoundingMinor:         quote.RoundingMinor,
		TotalMinor:            quote.TotalMinor,
		LineItems:             lineItems,
		CalculationInputs:     inputs,
	})
	if err != nil {
		return apierr.Internal(fmt.Errorf("snapshot charges: %w", err))
	}
	return nil
}

// writeRouteSnapshot persists the resolved path and the full decision trace.
func (b *Booker) writeRouteSnapshot(ctx context.Context, q *dbgen.Queries, prep *prepared, s dbgen.Shipment) error {
	route := prep.routing
	legs, err := json.Marshal(route.Legs)
	if err != nil {
		return apierr.Internal(err)
	}
	explanation := []byte("{}")
	if route.Explanation != nil {
		if explanation, err = json.Marshal(route.Explanation); err != nil {
			return apierr.Internal(err)
		}
	}
	params := dbgen.CreateRouteSnapshotParams{
		OrganizationID: s.OrganizationID, ShipmentID: s.ID,
		RouteDefinitionID: route.RouteDefinitionID(),
		RouteCode:         optional(route.RouteCode),
		ResolutionSource:  route.ResolutionSource,
		Legs:              legs, TransitHours: route.TransitHours, SlaHours: route.SLAHours,
		PromisedDeliveryAt: route.PromisedDeliveryAt,
		Explanation:        explanation, ExplanationID: optional(route.ExplanationID),
	}
	if route.OriginBranch != nil {
		params.OriginBranchCode = &route.OriginBranch.Code
	}
	if route.OriginHub != nil {
		params.OriginHubCode = &route.OriginHub.Code
	}
	if route.DestinationHub != nil {
		params.DestinationHubCode = &route.DestinationHub.Code
	}
	if route.DestinationBranch != nil {
		params.DestinationBranchCode = &route.DestinationBranch.Code
	}
	if _, err := q.CreateRouteSnapshot(ctx, params); err != nil {
		return apierr.Internal(fmt.Errorf("snapshot route: %w", err))
	}
	return nil
}

func eventMetadata(prep *prepared, awb string) []byte {
	raw, err := json.Marshal(map[string]any{
		"awb":              awb,
		"serviceCode":      prep.service.Code,
		"routeCode":        prep.routing.RouteCode,
		"resolutionSource": prep.routing.ResolutionSource,
		"totalAmountMinor": prep.quote.TotalMinor,
		"pieceCount":       len(prep.packages),
	})
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func toPricingPackages(in []PackageInput) []pricing.Package {
	out := make([]pricing.Package, 0, len(in))
	for _, p := range in {
		out = append(out, pricing.Package{
			ActualWeightGrams: int32(p.ActualWeightGrams),
			LengthMM:          int32(p.LengthMM),
			WidthMM:           int32(p.WidthMM),
			HeightMM:          int32(p.HeightMM),
		})
	}
	return out
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func encodeJSON(m map[string]any) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return raw
}
