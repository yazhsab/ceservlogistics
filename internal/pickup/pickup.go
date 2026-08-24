// Package pickup implements M09: pickup requests, agent assignment, runs and
// attempts.
//
// A pickup is the first physical touch of a shipment's life and the first place
// the system can be wrong in an expensive way — sending an agent to an address
// the customer has since changed, or accepting a completion for parcels that
// were never handed over. Two decisions follow from that:
//
//   - The address is snapshotted onto the request. Editing the customer's
//     address record afterwards does not redirect a pickup already dispatched.
//   - Completion names the shipments actually collected. Anything on the request
//     that was not handed over stays behind with a recorded reason rather than
//     being silently marked collected.
package pickup

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/geography"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/serviceability"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Types of pickup a customer can ask for.
var pickupTypes = []string{"SCHEDULED", "ON_DEMAND", "BULK", "RECURRING"}

// Failure reasons an agent may report. Kept as a fixed vocabulary so that
// pickup failure analysis is possible; NDR-style configurability is a
// deliberate non-goal here because the set is small and stable.
var pickupFailureReasons = []string{
	"CUSTOMER_NOT_AVAILABLE", "PREMISES_CLOSED", "SHIPMENT_NOT_READY",
	"ADDRESS_NOT_FOUND", "CUSTOMER_CANCELLED", "VEHICLE_CAPACITY",
	"RESTRICTED_ACCESS", "WEATHER", "OTHER",
}

// Service implements the pickup workflow.
type Service struct {
	db                 *database.DB
	q                  *dbgen.Queries
	geo                *geography.Service
	routing            *serviceability.Resolver
	units              *ops.Resolver
	codes              *ops.CodeAllocator
	trans              *shipment.Transitioner
	audit              *audit.Recorder
	log                *slog.Logger
	maxDays            int
	maxItems           int
	completedObservers []CompletionObserver
}

// CompletionEvent is the durable business fact raised when a pickup visit
// finishes successfully or partially successfully. Observers run inside the
// pickup transaction and may enqueue work, but must not perform network I/O.
type CompletionEvent struct {
	Request   dbgen.PickupRequest
	Attempt   dbgen.PickupAttempt
	Status    string
	Shipments []dbgen.Shipment
}

type CompletionObserver func(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, event CompletionEvent,
) error

// ObserveCompletion registers a pickup completion outbox observer at startup.
func (s *Service) ObserveCompletion(observer CompletionObserver) {
	if observer != nil {
		s.completedObservers = append(s.completedObservers, observer)
	}
}

func (s *Service) runCompletionObservers(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, event CompletionEvent,
) error {
	for _, observer := range s.completedObservers {
		if err := observer(ctx, tx, p, event); err != nil {
			return err
		}
	}
	return nil
}

// NewService builds the pickup service.
func NewService(
	db *database.DB, q *dbgen.Queries, geo *geography.Service, routing *serviceability.Resolver,
	units *ops.Resolver, codes *ops.CodeAllocator, trans *shipment.Transitioner,
	rec *audit.Recorder, log *slog.Logger,
) *Service {
	return &Service{
		db: db, q: q, geo: geo, routing: routing, units: units, codes: codes,
		trans: trans, audit: rec, log: log,
		maxDays: 30, maxItems: 500,
	}
}

// CreateInput is a new pickup request.
type CreateInput struct {
	CustomerID    string
	PickupType    string
	ContactName   string
	ContactPhone  string
	AltPhone      string
	Line1         string
	Line2         string
	Landmark      string
	City          string
	State         string
	Pincode       string
	Latitude      *float64
	Longitude     *float64
	AddressID     string
	ScheduledDate time.Time
	WindowStart   time.Time
	WindowEnd     time.Time
	ExpectedPiece int
	ExpectedGrams *int
	Instructions  string
	MaxAttempts   int
	ShipmentIDs   []string
	Metadata      map[string]any
}

// Create raises a pickup request and, when shipments are named, links them.
//
// The branch is resolved from the pickup PIN code through the same routing
// engine that books shipments, so a pickup lands in the queue of the facility
// that actually serves that area rather than wherever the caller happens to be.
func (s *Service) Create(ctx context.Context, p *tenant.Principal, in CreateInput) (*Detail, error) {
	cust, err := s.q.GetCustomerByPublicID(ctx, dbgen.GetCustomerByPublicIDParams{
		PublicID: in.CustomerID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.NotFound("Customer")
		}
		return nil, apierr.Internal(err)
	}
	if !p.IsCustomerInScope(cust.ID) {
		return nil, apierr.NotFound("Customer")
	}
	if cust.Status != "ACTIVE" {
		return nil, apierr.Conflict(apierr.CodeConflict,
			"This customer is not active and cannot raise pickups.").
			WithDetail("customerStatus", cust.Status)
	}

	// A saved address may be referenced only if it belongs to this customer,
	// the same rule booking applies.
	var sourceAddressID *int64
	if in.AddressID != "" {
		saved, aErr := s.q.GetCustomerAddressByPublicID(ctx, dbgen.GetCustomerAddressByPublicIDParams{
			PublicID: in.AddressID, OrganizationID: p.OrganizationID,
		})
		if aErr != nil {
			if database.IsNoRows(aErr) {
				return nil, apierr.Validation("The saved address does not exist.",
					map[string]any{"field": "addressId"})
			}
			return nil, apierr.Internal(aErr)
		}
		if saved.CustomerID != cust.ID {
			return nil, apierr.Validation("The saved address does not belong to this customer.",
				map[string]any{"field": "addressId"})
		}
		sourceAddressID = &saved.ID
		fillFromSavedAddress(&in, saved)
	}

	pin, err := s.geo.RequireActivePincode(ctx, in.Pincode, geography.DefaultCountry)
	if err != nil {
		return nil, err
	}

	branch, err := s.resolvePickupBranch(ctx, p, pin.Code, in.ScheduledDate)
	if err != nil {
		return nil, err
	}

	// Shipments are validated before anything is written, so a request is never
	// created half-linked.
	shipments, err := s.loadLinkableShipments(ctx, p, cust.ID, in.ShipmentIDs)
	if err != nil {
		return nil, err
	}

	code, err := s.codes.Allocate(ctx, p.OrganizationID, ops.KindPickupRequest, branch.Code, time.Now())
	if err != nil {
		return nil, err
	}

	var detail *Detail
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		params := dbgen.CreatePickupRequestParams{
			PublicID: publicid.New(publicid.PrefixPickupRequest), OrganizationID: p.OrganizationID,
			ReferenceCode: code, CustomerID: cust.ID, BranchID: branch.ID,
			PickupType: in.PickupType, Status: initialStatus(in.PickupType),
			ContactName: in.ContactName, ContactPhone: in.ContactPhone,
			AltPhone: ops.Optional(in.AltPhone),
			Line1:    in.Line1, Line2: ops.Optional(in.Line2), Landmark: ops.Optional(in.Landmark),
			CityName: in.City, StateName: in.State, Pincode: pin.Code,
			Latitude: in.Latitude, Longitude: in.Longitude, SourceAddressID: sourceAddressID,
			ScheduledDate: in.ScheduledDate, WindowStart: in.WindowStart, WindowEnd: in.WindowEnd,
			ExpectedPieceCount:  int32(in.ExpectedPiece),
			SpecialInstructions: ops.Optional(in.Instructions),
			MaxAttempts:         int32(in.MaxAttempts),
			RequestedByUserID:   p.ActorUserID(),
			Metadata:            encodeJSON(in.Metadata),
		}
		if in.ExpectedGrams != nil {
			g := int32(*in.ExpectedGrams)
			params.ExpectedWeightGrams = &g
		}
		created, cErr := q.CreatePickupRequest(ctx, params)
		if cErr != nil {
			return apierr.Internal(fmt.Errorf("create pickup request: %w", cErr))
		}

		for _, sh := range shipments {
			if _, lErr := q.AddShipmentToPickupRequest(ctx, dbgen.AddShipmentToPickupRequestParams{
				OrganizationID: p.OrganizationID, PickupRequestID: created.ID, ShipmentID: sh.ID,
			}); lErr != nil {
				if database.IsUniqueViolation(lErr, "pickup_request_shipments_active_idx") {
					return apierr.Conflict(apierr.CodeConflict,
						"One of these shipments is already awaiting collection on another pickup request.").
						WithDetail("awb", sh.Awb)
				}
				return apierr.Internal(fmt.Errorf("link shipment: %w", lErr))
			}
			// The shipment follows the request into PICKUP_SCHEDULED so its own
			// timeline reflects that collection is arranged.
			if sh.CurrentStatus == string(shipment.StatusBooked) {
				if _, tErr := s.trans.Apply(ctx, tx,
					ops.Device{Source: "API"}.Actor(p, branch),
					shipment.Request{
						Shipment: sh, To: shipment.StatusPickupScheduled,
						Description: "Pickup scheduled",
						Metadata:    map[string]any{"pickupRequest": code},
					}); tErr != nil {
					return tErr
				}
			}
		}

		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionPickupCreated, ResourceType: "pickup_request",
			ResourceID: &created.ID, ResourcePublicID: created.PublicID,
			OperatingUnitID: &branch.ID,
			After: map[string]any{
				"referenceCode": code, "customerCode": cust.Code, "pickupType": in.PickupType,
				"branchCode": branch.Code, "scheduledDate": in.ScheduledDate.Format("2006-01-02"),
				"shipmentCount": len(shipments),
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}

		var dErr error
		detail, dErr = s.loadDetail(ctx, q, p, created.PublicID)
		return dErr
	})
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// resolvePickupBranch finds the facility that serves a PIN code.
func (s *Service) resolvePickupBranch(
	ctx context.Context, p *tenant.Principal, pincode string, at time.Time,
) (*ops.Facility, error) {
	branchID, err := s.routing.ResolvePickupBranch(ctx, p.OrganizationID, pincode, at)
	if err != nil {
		return nil, err
	}
	if branchID == nil {
		return nil, apierr.Conflict("PICKUP_NOT_SERVICEABLE",
			"No branch serves pickups at this PIN code.").
			WithDetail("pincode", pincode)
	}
	return s.units.FacilityByID(ctx, p.OrganizationID, *branchID)
}

// loadLinkableShipments validates the shipments a request names.
func (s *Service) loadLinkableShipments(
	ctx context.Context, p *tenant.Principal, customerID int64, ids []string,
) ([]dbgen.Shipment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > s.maxItems {
		return nil, apierr.Validation(
			fmt.Sprintf("A pickup request can reference at most %d shipments.", s.maxItems),
			map[string]any{"field": "shipmentIds", "max": s.maxItems})
	}
	out := make([]dbgen.Shipment, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return nil, apierr.Validation("The same shipment is listed twice.",
				map[string]any{"field": "shipmentIds", "shipmentId": id})
		}
		seen[id] = true

		sh, err := s.q.LockShipmentForUpdate(ctx, dbgen.LockShipmentForUpdateParams{
			PublicID: id, OrganizationID: p.OrganizationID,
		})
		if err != nil {
			if database.IsNoRows(err) {
				return nil, apierr.NotFound("Shipment")
			}
			return nil, apierr.Internal(err)
		}
		if sh.CustomerID != customerID {
			return nil, apierr.Validation("This shipment belongs to a different customer.",
				map[string]any{"field": "shipmentIds", "shipmentId": id})
		}
		switch shipment.Status(sh.CurrentStatus) {
		case shipment.StatusBooked, shipment.StatusPickupScheduled:
		default:
			return nil, apierr.Conflict("SHIPMENT_INVALID_STATE",
				"This shipment is past the pickup stage and cannot be added to a pickup request.").
				WithDetail("awb", sh.Awb).
				WithDetail("currentStatus", sh.CurrentStatus)
		}
		out = append(out, sh)
	}
	return out, nil
}

func initialStatus(pickupType string) string {
	// An on-demand pickup is live the moment it is raised; a scheduled one waits
	// for the dispatcher to slot it.
	if pickupType == "ON_DEMAND" {
		return "REQUESTED"
	}
	return "SCHEDULED"
}

func fillFromSavedAddress(in *CreateInput, saved dbgen.CustomerAddress) {
	if in.ContactName == "" {
		in.ContactName = saved.ContactName
	}
	if in.ContactPhone == "" {
		in.ContactPhone = saved.ContactPhone
	}
	if in.AltPhone == "" && saved.AltPhone != nil {
		in.AltPhone = *saved.AltPhone
	}
	if in.Line1 == "" {
		in.Line1 = saved.Line1
	}
	if in.Line2 == "" && saved.Line2 != nil {
		in.Line2 = *saved.Line2
	}
	if in.Landmark == "" && saved.Landmark != nil {
		in.Landmark = *saved.Landmark
	}
	if in.City == "" {
		in.City = saved.CityName
	}
	if in.State == "" {
		in.State = saved.StateName
	}
	if in.Pincode == "" {
		in.Pincode = saved.Pincode
	}
	if in.Latitude == nil {
		in.Latitude = saved.Latitude
	}
	if in.Longitude == nil {
		in.Longitude = saved.Longitude
	}
}
