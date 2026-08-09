// Package shipment owns booking, AWB allocation and the shipment state machine
// (M08), and the operational transition engine every Release 2 module drives
// state changes through.
package shipment

import (
	"fmt"
	"slices"

	"github.com/ceserve/courier-os/internal/platform/apierr"
)

// Status is a shipment lifecycle state.
type Status string

const (
	StatusBooked                    Status = "BOOKED"
	StatusPickupScheduled           Status = "PICKUP_SCHEDULED"
	StatusPickupAssigned            Status = "PICKUP_ASSIGNED"
	StatusPickedUp                  Status = "PICKED_UP"
	StatusOriginBranchReceived      Status = "ORIGIN_BRANCH_RECEIVED"
	StatusOriginBagged              Status = "ORIGIN_BAGGED"
	StatusOriginDispatched          Status = "ORIGIN_DISPATCHED"
	StatusInTransit                 Status = "IN_TRANSIT"
	StatusTransitHubReceived        Status = "TRANSIT_HUB_RECEIVED"
	StatusTransitHubDispatched      Status = "TRANSIT_HUB_DISPATCHED"
	StatusDestinationHubReceived    Status = "DESTINATION_HUB_RECEIVED"
	StatusDestinationBranchReceived Status = "DESTINATION_BRANCH_RECEIVED"
	StatusOutForDelivery            Status = "OUT_FOR_DELIVERY"
	StatusDelivered                 Status = "DELIVERED"
	StatusDeliveryFailed            Status = "DELIVERY_FAILED"
	StatusNDR                       Status = "NDR"
	StatusRTOInitiated              Status = "RTO_INITIATED"
	StatusRTOInTransit              Status = "RTO_IN_TRANSIT"
	StatusRTODelivered              Status = "RTO_DELIVERED"
	StatusCancelled                 Status = "CANCELLED"
	StatusLost                      Status = "LOST"
	StatusDamaged                   Status = "DAMAGED"
)

// AllStatuses is the published enum, in lifecycle order.
var AllStatuses = []Status{
	StatusBooked, StatusPickupScheduled, StatusPickupAssigned, StatusPickedUp,
	StatusOriginBranchReceived, StatusOriginBagged, StatusOriginDispatched, StatusInTransit,
	StatusTransitHubReceived, StatusTransitHubDispatched, StatusDestinationHubReceived,
	StatusDestinationBranchReceived, StatusOutForDelivery, StatusDelivered, StatusDeliveryFailed,
	StatusNDR, StatusRTOInitiated, StatusRTOInTransit, StatusRTODelivered,
	StatusCancelled, StatusLost, StatusDamaged,
}

// StatusStrings returns the enum as strings for query validation and OpenAPI.
func StatusStrings() []string {
	out := make([]string, 0, len(AllStatuses))
	for _, s := range AllStatuses {
		out = append(out, string(s))
	}
	return out
}

// terminal states admit no further transition.
var terminalStatuses = []Status{StatusDelivered, StatusRTODelivered, StatusCancelled, StatusLost}

// IsTerminal reports whether a shipment has reached a final state.
func IsTerminal(s Status) bool { return slices.Contains(terminalStatuses, s) }

// Direction is which way along the network the parcel is travelling.
type Direction string

const (
	DirectionForward Direction = "FORWARD"
	DirectionReverse Direction = "REVERSE"
)

// CustodyRule says which facility must hold the parcel for a transition to be
// legal.
//
// Custody is the difference between a courier system and a status tracker: a
// branch must not be able to mark a parcel delivered while it is sitting in a
// hub two states away. Each rule names the facility the actor must be scanning
// at, and the engine compares it against the shipment's own routing.
type CustodyRule string

const (
	// CustodyNone imposes no facility requirement; the permission and the
	// current state are the whole check. Used for planning steps that do not
	// touch the parcel.
	CustodyNone CustodyRule = "NONE"
	// CustodyHolder requires the actor's facility to be the one currently
	// holding the parcel.
	CustodyHolder CustodyRule = "HOLDER"
	// CustodyOriginBranch, and the rest below, require the actor's facility to
	// be that specific point on the shipment's resolved route.
	CustodyOriginBranch      CustodyRule = "ORIGIN_BRANCH"
	CustodyDestinationBranch CustodyRule = "DESTINATION_BRANCH"
	// CustodyAgent requires the acting user to be the agent holding the parcel.
	CustodyAgent CustodyRule = "AGENT"
	// CustodyAnyFacility accepts any facility in the actor's scope, which is
	// how a misrouted parcel can be received somewhere unexpected and then
	// corrected rather than being stuck.
	CustodyAnyFacility CustodyRule = "ANY_FACILITY"
)

// Transition describes one legal state change.
type Transition struct {
	From Status
	To   Status
	// Permission the actor must hold.
	Permission string
	// Release records which release enables this transition.
	Release int
	// RequiresReason forces an explicit justification, copied into the shipment
	// event and the audit trail.
	RequiresReason bool
	// Custody is the facility rule enforced before the change is applied.
	Custody CustodyRule
	// Direction restricts the transition to forward or reverse movement. Empty
	// means it is legal in either.
	Direction Direction
	// EventType is the shipment_events.event_type written for this change.
	EventType string
	// Description is the customer-facing wording when the caller supplies none.
	Description string
	// BlockedByHold refuses the transition while the parcel is on hold, which is
	// what makes a HOLD scan actually stop the parcel rather than merely
	// annotate it.
	BlockedByHold bool
}

// transitions is the authoritative table. Anything not listed here is illegal.
//
// Read it as the physical journey: collection, origin processing, line haul,
// destination processing, delivery, and the two exception paths (NDR/RTO and
// loss/damage). Where the same pair appears twice it is because forward and
// reverse movement need different permissions or custody.
var transitions = []Transition{
	// --- Booking and cancellation --------------------------------------------
	{From: StatusBooked, To: StatusCancelled, Permission: "shipment.cancel", Release: 1,
		RequiresReason: true, Custody: CustodyNone, EventType: "CANCELLED", Description: "Shipment cancelled"},

	// --- Pickup (M09) ---------------------------------------------------------
	{From: StatusBooked, To: StatusPickupScheduled, Permission: "pickup.schedule", Release: 2,
		Custody: CustodyNone, EventType: "PICKUP", Description: "Pickup scheduled"},
	{From: StatusPickupScheduled, To: StatusPickupAssigned, Permission: "pickup.assign", Release: 2,
		Custody: CustodyNone, EventType: "PICKUP", Description: "Pickup assigned to an agent"},
	{From: StatusPickupAssigned, To: StatusPickupScheduled, Permission: "pickup.assign", Release: 2,
		Custody: CustodyNone, EventType: "PICKUP", Description: "Pickup reassigned"},
	{From: StatusPickupAssigned, To: StatusPickedUp, Permission: "pickup.complete", Release: 2,
		Custody: CustodyNone, EventType: "PICKUP", Description: "Shipment picked up"},
	// A failed visit returns the shipment to the scheduling pool.
	{From: StatusPickupAssigned, To: StatusBooked, Permission: "pickup.complete", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "PICKUP",
		Description: "Pickup attempt failed"},
	{From: StatusPickupScheduled, To: StatusCancelled, Permission: "shipment.cancel", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "CANCELLED", Description: "Shipment cancelled"},
	{From: StatusPickupAssigned, To: StatusCancelled, Permission: "shipment.cancel", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "CANCELLED", Description: "Shipment cancelled"},

	// --- Origin branch (M10) --------------------------------------------------
	{From: StatusPickedUp, To: StatusOriginBranchReceived, Permission: "scan.inbound", Release: 2,
		Custody: CustodyAnyFacility, EventType: "SCAN", Description: "Received at origin facility"},
	// Counter booking: the customer walks in, so the parcel is received without
	// ever having been picked up.
	{From: StatusBooked, To: StatusOriginBranchReceived, Permission: "scan.inbound", Release: 2,
		Custody: CustodyAnyFacility, EventType: "SCAN", Description: "Received at origin facility"},
	{From: StatusPickupScheduled, To: StatusOriginBranchReceived, Permission: "scan.inbound", Release: 2,
		Custody: CustodyAnyFacility, EventType: "SCAN", Description: "Received at origin facility"},
	{From: StatusPickupAssigned, To: StatusOriginBranchReceived, Permission: "scan.inbound", Release: 2,
		Custody: CustodyAnyFacility, EventType: "SCAN", Description: "Received at origin facility"},

	// --- Bagging (M11) --------------------------------------------------------
	{From: StatusOriginBranchReceived, To: StatusOriginBagged, Permission: "bag.manage", Release: 2,
		Custody: CustodyHolder, Direction: DirectionForward, EventType: "BAG",
		Description: "Added to a dispatch bag", BlockedByHold: true},
	// Removing a shipment from an open bag puts it back on the floor.
	{From: StatusOriginBagged, To: StatusOriginBranchReceived, Permission: "bag.manage", Release: 2,
		Custody: CustodyHolder, EventType: "BAG", Description: "Removed from the dispatch bag"},

	// --- Outbound dispatch (M12, M13) ----------------------------------------
	{From: StatusOriginBagged, To: StatusOriginDispatched, Permission: "manifest.dispatch", Release: 2,
		Custody: CustodyHolder, Direction: DirectionForward, EventType: "MANIFEST",
		Description: "Dispatched from origin facility", BlockedByHold: true},
	// Loose shipments travel on a manifest without a bag.
	{From: StatusOriginBranchReceived, To: StatusOriginDispatched, Permission: "manifest.dispatch", Release: 2,
		Custody: CustodyHolder, Direction: DirectionForward, EventType: "MANIFEST",
		Description: "Dispatched from origin facility", BlockedByHold: true},
	{From: StatusOriginDispatched, To: StatusInTransit, Permission: "linehaul.depart", Release: 2,
		Custody: CustodyNone, EventType: "TRIP", Description: "In transit"},

	// --- Transit and destination hubs ----------------------------------------
	{From: StatusInTransit, To: StatusTransitHubReceived, Permission: "scan.inbound", Release: 2,
		Custody: CustodyAnyFacility, EventType: "SCAN", Description: "Received at transit facility"},
	{From: StatusTransitHubReceived, To: StatusTransitHubDispatched, Permission: "manifest.dispatch", Release: 2,
		Custody: CustodyHolder, EventType: "MANIFEST", Description: "Dispatched from transit facility",
		BlockedByHold: true},
	{From: StatusTransitHubDispatched, To: StatusInTransit, Permission: "linehaul.depart", Release: 2,
		Custody: CustodyNone, EventType: "TRIP", Description: "In transit"},
	{From: StatusInTransit, To: StatusDestinationHubReceived, Permission: "scan.inbound", Release: 2,
		Custody: CustodyAnyFacility, EventType: "SCAN", Description: "Arrived at destination city"},
	{From: StatusDestinationHubReceived, To: StatusDestinationBranchReceived, Permission: "scan.inbound", Release: 2,
		Custody: CustodyAnyFacility, EventType: "SCAN", Description: "Received at delivery facility"},
	// Direct branch-to-branch line haul skips the hubs entirely.
	{From: StatusInTransit, To: StatusDestinationBranchReceived, Permission: "scan.inbound", Release: 2,
		Custody: CustodyAnyFacility, EventType: "SCAN", Description: "Received at delivery facility"},
	// A misroute correction sends the parcel back out from the destination hub.
	{From: StatusDestinationHubReceived, To: StatusTransitHubDispatched, Permission: "manifest.dispatch", Release: 2,
		RequiresReason: true, Custody: CustodyHolder, EventType: "MANIFEST",
		Description: "Redirected to the correct facility"},
	{From: StatusDestinationBranchReceived, To: StatusTransitHubDispatched, Permission: "manifest.dispatch", Release: 2,
		RequiresReason: true, Custody: CustodyHolder, EventType: "MANIFEST",
		Description: "Redirected to the correct facility"},
	// Same-branch delivery: origin and destination are the same facility, so the
	// parcel never enters line haul.
	{From: StatusOriginBranchReceived, To: StatusDestinationBranchReceived, Permission: "scan.sort", Release: 2,
		Custody: CustodyHolder, Direction: DirectionForward, EventType: "SCAN",
		Description: "Ready for local delivery"},

	// --- Delivery (M16) -------------------------------------------------------
	{From: StatusDestinationBranchReceived, To: StatusOutForDelivery, Permission: "delivery.dispatch", Release: 2,
		Custody: CustodyDestinationBranch, EventType: "DELIVERY", Description: "Out for delivery",
		BlockedByHold: true},
	{From: StatusOutForDelivery, To: StatusDelivered, Permission: "delivery.complete", Release: 2,
		Custody: CustodyAgent, Direction: DirectionForward, EventType: "DELIVERY",
		Description: "Delivered"},
	{From: StatusOutForDelivery, To: StatusDeliveryFailed, Permission: "delivery.complete", Release: 2,
		RequiresReason: true, Custody: CustodyAgent, EventType: "DELIVERY",
		Description: "Delivery attempted but not completed"},
	// The agent brings the parcel back to the branch at the end of the run.
	{From: StatusDeliveryFailed, To: StatusDestinationBranchReceived, Permission: "scan.inbound", Release: 2,
		Custody: CustodyAnyFacility, EventType: "SCAN", Description: "Returned to the delivery facility"},
	{From: StatusOutForDelivery, To: StatusDestinationBranchReceived, Permission: "scan.inbound", Release: 2,
		RequiresReason: true, Custody: CustodyAnyFacility, EventType: "SCAN",
		Description: "Returned to the delivery facility"},

	// --- NDR (M17) ------------------------------------------------------------
	{From: StatusDeliveryFailed, To: StatusNDR, Permission: "ndr.manage", Release: 2,
		Custody: CustodyNone, EventType: "NDR", Description: "Delivery exception raised"},
	// A branch can raise an NDR before ever dispatching, e.g. an address the
	// delivery supervisor knows is wrong.
	{From: StatusDestinationBranchReceived, To: StatusNDR, Permission: "ndr.manage", Release: 2,
		RequiresReason: true, Custody: CustodyDestinationBranch, EventType: "NDR",
		Description: "Delivery exception raised"},
	{From: StatusNDR, To: StatusDestinationBranchReceived, Permission: "ndr.manage", Release: 2,
		Custody: CustodyNone, EventType: "NDR", Description: "Scheduled for another delivery attempt"},
	{From: StatusNDR, To: StatusOutForDelivery, Permission: "delivery.dispatch", Release: 2,
		Custody: CustodyDestinationBranch, EventType: "DELIVERY", Description: "Out for delivery",
		BlockedByHold: true},
	// Customer collects from the branch counter instead of a redelivery.
	{From: StatusNDR, To: StatusDelivered, Permission: "delivery.complete", Release: 2,
		Custody: CustodyDestinationBranch, EventType: "DELIVERY",
		Description: "Collected by the customer"},
	{From: StatusDestinationBranchReceived, To: StatusDelivered, Permission: "delivery.complete", Release: 2,
		Custody: CustodyDestinationBranch, Direction: DirectionForward, EventType: "DELIVERY",
		Description: "Collected by the customer", BlockedByHold: true},

	// --- RTO (M18) ------------------------------------------------------------
	// RTO can start from any state where the parcel is still in the network on
	// the delivery side.
	{From: StatusNDR, To: StatusRTOInitiated, Permission: "rto.manage", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "RTO", Description: "Return to sender started"},
	{From: StatusDeliveryFailed, To: StatusRTOInitiated, Permission: "rto.manage", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "RTO", Description: "Return to sender started"},
	{From: StatusDestinationBranchReceived, To: StatusRTOInitiated, Permission: "rto.manage", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "RTO", Description: "Return to sender started"},
	{From: StatusRTOInitiated, To: StatusRTOInTransit, Permission: "rto.manage", Release: 2,
		Custody: CustodyNone, Direction: DirectionReverse, EventType: "RTO",
		Description: "Return in transit"},
	// The reverse journey reuses bags, manifests and trips. Those operations do
	// not change the status — the parcel stays RTO_IN_TRANSIT and each facility
	// appends a location event — so there is exactly one reverse in-flight
	// status rather than a mirrored copy of the forward ladder. See ADR 0009.
	{From: StatusRTOInTransit, To: StatusRTODelivered, Permission: "rto.manage", Release: 2,
		Custody: CustodyAnyFacility, Direction: DirectionReverse, EventType: "RTO",
		Description: "Returned to sender"},
	// A damaged parcel can still be sent back rather than written off.
	{From: StatusDamaged, To: StatusRTOInitiated, Permission: "rto.manage", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "RTO", Description: "Return to sender started"},

	// --- Exceptions -----------------------------------------------------------
	// Loss and damage can be declared from any state where the network still
	// holds the parcel. Listing them explicitly rather than using a wildcard
	// keeps the table the single source of truth.
	{From: StatusPickedUp, To: StatusLost, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusOriginBranchReceived, To: StatusLost, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusOriginBagged, To: StatusLost, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusOriginDispatched, To: StatusLost, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusInTransit, To: StatusLost, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusTransitHubReceived, To: StatusLost, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusTransitHubDispatched, To: StatusLost, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusDestinationHubReceived, To: StatusLost, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusDestinationBranchReceived, To: StatusLost, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusOutForDelivery, To: StatusLost, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusRTOInTransit, To: StatusLost, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "EXCEPTION", Description: "Under investigation"},

	{From: StatusOriginBranchReceived, To: StatusDamaged, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyHolder, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusOriginBagged, To: StatusDamaged, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyHolder, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusInTransit, To: StatusDamaged, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusTransitHubReceived, To: StatusDamaged, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyHolder, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusDestinationHubReceived, To: StatusDamaged, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyHolder, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusDestinationBranchReceived, To: StatusDamaged, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyHolder, EventType: "EXCEPTION", Description: "Under investigation"},
	{From: StatusOutForDelivery, To: StatusDamaged, Permission: "shipment.declare_exception", Release: 2,
		RequiresReason: true, Custody: CustodyNone, EventType: "EXCEPTION", Description: "Under investigation"},
	// A damaged parcel that the recipient still accepts.
	{From: StatusDamaged, To: StatusDelivered, Permission: "delivery.complete", Release: 2,
		RequiresReason: true, Custody: CustodyAgent, EventType: "DELIVERY",
		Description: "Delivered"},
	{From: StatusDamaged, To: StatusDestinationBranchReceived, Permission: "scan.inbound", Release: 2,
		Custody: CustodyAnyFacility, EventType: "SCAN", Description: "Received at delivery facility"},
}

// CurrentRelease gates which transitions the running build may execute.
const CurrentRelease = 2

// index for O(1) lookup. Built once; the table is immutable after init.
var transitionIndex = func() map[[2]Status]*Transition {
	m := make(map[[2]Status]*Transition, len(transitions))
	for i := range transitions {
		key := [2]Status{transitions[i].From, transitions[i].To}
		if _, dup := m[key]; dup {
			// A duplicate pair would make behaviour depend on table order.
			panic(fmt.Sprintf("shipment: duplicate transition %s -> %s", key[0], key[1]))
		}
		m[key] = &transitions[i]
	}
	return m
}()

// Find returns the transition definition for a state change.
func Find(from, to Status) (*Transition, error) {
	if t, ok := transitionIndex[[2]Status{from, to}]; ok {
		return t, nil
	}
	return nil, newIllegalTransition(from, to)
}

// Validate checks that a transition is legal and available in this release.
func Validate(from, to Status) (*Transition, error) {
	t, err := Find(from, to)
	if err != nil {
		return nil, err
	}
	if t.Release > CurrentRelease {
		return nil, apierr.Conflict(apierr.CodeConflict,
			fmt.Sprintf("The transition from %s to %s is part of a later release and is not yet available.", from, to)).
			WithDetail("fromStatus", string(from)).
			WithDetail("toStatus", string(to)).
			WithDetail("availableInRelease", t.Release)
	}
	return t, nil
}

// ValidateDirection additionally checks the transition against the shipment's
// direction of travel, so a reverse-moving parcel cannot re-enter the forward
// ladder and vice versa.
func ValidateDirection(from, to Status, dir Direction) (*Transition, error) {
	t, err := Validate(from, to)
	if err != nil {
		return nil, err
	}
	if t.Direction != "" && dir != "" && t.Direction != dir {
		return nil, apierr.Conflict("SHIPMENT_INVALID_STATE",
			fmt.Sprintf("This shipment is moving %s and cannot make the %s to %s transition.",
				lowerDirection(dir), from, to)).
			WithDetail("fromStatus", string(from)).
			WithDetail("toStatus", string(to)).
			WithDetail("movementDirection", string(dir)).
			WithDetail("requiredDirection", string(t.Direction))
	}
	return t, nil
}

// AllowedFrom lists the states reachable from a state in this release.
func AllowedFrom(from Status) []Status {
	var out []Status
	for _, t := range transitions {
		if t.From == from && t.Release <= CurrentRelease {
			out = append(out, t.To)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// TransitionsFor returns the full definitions reachable from a state, which the
// operations console uses to render only the buttons an actor may press.
func TransitionsFor(from Status) []Transition {
	var out []Transition
	for _, t := range transitions {
		if t.From == from && t.Release <= CurrentRelease {
			out = append(out, t)
		}
	}
	return out
}

// AllTransitions returns the whole table, for documentation generation and the
// contract test that keeps OpenAPI honest.
func AllTransitions() []Transition {
	out := make([]Transition, len(transitions))
	copy(out, transitions)
	return out
}

func lowerDirection(d Direction) string {
	if d == DirectionReverse {
		return "in reverse (RTO)"
	}
	return "forward"
}

func newIllegalTransition(from, to Status) error {
	allowed := AllowedFrom(from)
	strs := make([]string, 0, len(allowed))
	for _, s := range allowed {
		strs = append(strs, string(s))
	}
	e := apierr.Conflict("SHIPMENT_INVALID_STATE",
		fmt.Sprintf("Shipment cannot transition from %s to %s.", from, to)).
		WithDetail("fromStatus", string(from)).
		WithDetail("toStatus", string(to)).
		WithDetail("allowedTransitions", strs)
	if IsTerminal(from) {
		return e.WithDetail("reason", "The shipment has reached a terminal state.")
	}
	return e
}
