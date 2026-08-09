package shipment

import (
	"slices"
	"strings"
	"testing"

	"github.com/ceserve/courier-os/internal/platform/apierr"
)

func TestAllStatusesCoverConstitution(t *testing.T) {
	// The Constitution §13 list, verbatim. If someone adds a status to the code
	// without adding it here the test fails, and vice versa, so the enum cannot
	// drift from the specification.
	want := []string{
		"BOOKED", "PICKUP_SCHEDULED", "PICKUP_ASSIGNED", "PICKED_UP",
		"ORIGIN_BRANCH_RECEIVED", "ORIGIN_BAGGED", "ORIGIN_DISPATCHED", "IN_TRANSIT",
		"TRANSIT_HUB_RECEIVED", "TRANSIT_HUB_DISPATCHED", "DESTINATION_HUB_RECEIVED",
		"DESTINATION_BRANCH_RECEIVED", "OUT_FOR_DELIVERY", "DELIVERED", "DELIVERY_FAILED",
		"NDR", "RTO_INITIATED", "RTO_IN_TRANSIT", "RTO_DELIVERED",
		"CANCELLED", "LOST", "DAMAGED",
	}
	got := StatusStrings()
	if len(got) != len(want) {
		t.Fatalf("status count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("status[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTransitionTableIsWellFormed(t *testing.T) {
	statuses := make(map[Status]bool, len(AllStatuses))
	for _, s := range AllStatuses {
		statuses[s] = true
	}
	for _, tr := range AllTransitions() {
		label := string(tr.From) + "->" + string(tr.To)
		if !statuses[tr.From] {
			t.Errorf("%s: unknown from-status", label)
		}
		if !statuses[tr.To] {
			t.Errorf("%s: unknown to-status", label)
		}
		if tr.From == tr.To {
			t.Errorf("%s: self-transition", label)
		}
		if tr.Permission == "" {
			t.Errorf("%s: no permission", label)
		}
		if tr.EventType == "" {
			t.Errorf("%s: no event type", label)
		}
		if tr.Description == "" {
			t.Errorf("%s: no description", label)
		}
		if tr.Custody == "" {
			t.Errorf("%s: no custody rule (use CustodyNone to mean none)", label)
		}
		if IsTerminal(tr.From) {
			t.Errorf("%s: leaves a terminal state", label)
		}
	}
}

func TestEveryNonTerminalStatusIsReachableAndProgressable(t *testing.T) {
	// A status nothing can reach is dead configuration; a non-terminal status
	// nothing can leave is a parcel stuck forever. Both are bugs worth failing
	// the build over.
	reachable := map[Status]bool{StatusBooked: true}
	hasExit := map[Status]bool{}
	for _, tr := range AllTransitions() {
		reachable[tr.To] = true
		hasExit[tr.From] = true
	}
	for _, s := range AllStatuses {
		if !reachable[s] {
			t.Errorf("status %s is unreachable", s)
		}
		if !IsTerminal(s) && !hasExit[s] {
			t.Errorf("status %s is non-terminal but has no outgoing transition", s)
		}
	}
}

func TestTerminalStatesRefuseEverything(t *testing.T) {
	for _, s := range terminalStatuses {
		if got := AllowedFrom(s); len(got) != 0 {
			t.Errorf("terminal %s allows %v", s, got)
		}
		_, err := Validate(s, StatusInTransit)
		if err == nil {
			t.Fatalf("expected %s -> IN_TRANSIT to fail", s)
		}
		var ae *apierr.Error
		if !asAPIError(err, &ae) || ae.Code != "SHIPMENT_INVALID_STATE" {
			t.Errorf("%s: unexpected error %v", s, err)
		}
		if !strings.Contains(ae.Message, string(s)) {
			t.Errorf("%s: message should name the state, got %q", s, ae.Message)
		}
	}
}

func TestHappyPathIsWalkable(t *testing.T) {
	// The full physical journey from the release brief, walked one legal
	// transition at a time. If any link is missing the release cannot work.
	path := []Status{
		StatusBooked, StatusPickupScheduled, StatusPickupAssigned, StatusPickedUp,
		StatusOriginBranchReceived, StatusOriginBagged, StatusOriginDispatched,
		StatusInTransit, StatusTransitHubReceived, StatusTransitHubDispatched,
		StatusInTransit, StatusDestinationHubReceived, StatusDestinationBranchReceived,
		StatusOutForDelivery, StatusDelivered,
	}
	walk(t, path, DirectionForward)
}

func TestNDRReattemptPathIsWalkable(t *testing.T) {
	walk(t, []Status{
		StatusDestinationBranchReceived, StatusOutForDelivery, StatusDeliveryFailed,
		StatusNDR, StatusOutForDelivery, StatusDelivered,
	}, DirectionForward)
}

func TestRTOPathIsWalkable(t *testing.T) {
	// Forward up to the failure, then reverse. The direction flip happens at
	// RTO_INITIATED -> RTO_IN_TRANSIT, which is the transition that marks the
	// parcel as travelling backwards.
	walk(t, []Status{
		StatusOutForDelivery, StatusDeliveryFailed, StatusNDR, StatusRTOInitiated,
	}, DirectionForward)
	walk(t, []Status{
		StatusRTOInitiated, StatusRTOInTransit, StatusRTODelivered,
	}, DirectionReverse)
}

func TestDirectionGating(t *testing.T) {
	// A reverse-moving parcel must not be able to re-enter forward delivery.
	if _, err := ValidateDirection(StatusOutForDelivery, StatusDelivered, DirectionReverse); err == nil {
		t.Error("expected a reverse parcel to be refused a forward DELIVERED transition")
	}
	// And the same transition is fine going forward.
	if _, err := ValidateDirection(StatusOutForDelivery, StatusDelivered, DirectionForward); err != nil {
		t.Errorf("forward delivery should be legal: %v", err)
	}
	// Transitions with no direction restriction work either way.
	for _, d := range []Direction{DirectionForward, DirectionReverse} {
		if _, err := ValidateDirection(StatusOutForDelivery, StatusDeliveryFailed, d); err != nil {
			t.Errorf("direction %s: %v", d, err)
		}
	}
}

func TestIllegalTransitionsAreRefused(t *testing.T) {
	cases := []struct{ from, to Status }{
		// Skipping the physical journey entirely.
		{StatusBooked, StatusDelivered},
		{StatusBooked, StatusOutForDelivery},
		{StatusPickedUp, StatusDelivered},
		// Delivering before the parcel reached the destination branch.
		{StatusDestinationHubReceived, StatusOutForDelivery},
		{StatusInTransit, StatusDelivered},
		// Going backwards up the ladder.
		{StatusDelivered, StatusOutForDelivery},
		{StatusOriginDispatched, StatusBooked},
		// Cancelling a parcel already in the network.
		{StatusInTransit, StatusCancelled},
		{StatusOutForDelivery, StatusCancelled},
	}
	for _, c := range cases {
		if _, err := Validate(c.from, c.to); err == nil {
			t.Errorf("%s -> %s should be illegal", c.from, c.to)
		}
	}
}

func TestIllegalTransitionErrorListsAlternatives(t *testing.T) {
	_, err := Validate(StatusBooked, StatusDelivered)
	var ae *apierr.Error
	if !asAPIError(err, &ae) {
		t.Fatalf("expected an api error, got %T", err)
	}
	allowed, ok := ae.Details["allowedTransitions"].([]string)
	if !ok || len(allowed) == 0 {
		t.Fatalf("error should list the legal alternatives, got %#v", ae.Details)
	}
	if !slices.Contains(allowed, "PICKUP_SCHEDULED") {
		t.Errorf("BOOKED should be able to reach PICKUP_SCHEDULED, got %v", allowed)
	}
}

func TestCustodySensitiveTransitionsDeclareARule(t *testing.T) {
	// Anything that physically moves a parcel out of a facility, or completes
	// it, must name a custody rule. This is the check that would have caught
	// "delivery from any branch in the network".
	mustHaveCustody := map[Status]bool{
		StatusOutForDelivery: true, StatusDelivered: true,
		StatusOriginBagged: true, StatusOriginDispatched: true,
		StatusTransitHubDispatched: true,
	}
	for _, tr := range AllTransitions() {
		if mustHaveCustody[tr.To] && tr.Custody == CustodyNone {
			t.Errorf("%s -> %s moves or completes the parcel but declares no custody rule",
				tr.From, tr.To)
		}
	}
}

func TestHoldBlocksMovementTransitions(t *testing.T) {
	// A HOLD must actually stop the parcel. Every transition that dispatches it
	// onward has to be marked BlockedByHold, or the hold is decorative.
	blocking := map[Status]bool{
		StatusOutForDelivery: true, StatusOriginDispatched: true,
		StatusTransitHubDispatched: true, StatusOriginBagged: true,
	}
	for _, tr := range AllTransitions() {
		if !blocking[tr.To] {
			continue
		}
		// Redirect-after-misroute carries a reason and is a correction, not a
		// normal dispatch, so it is allowed to proceed on a held parcel.
		if tr.RequiresReason && tr.To == StatusTransitHubDispatched {
			continue
		}
		if !tr.BlockedByHold {
			t.Errorf("%s -> %s dispatches the parcel but is not blocked by a hold", tr.From, tr.To)
		}
	}
}

func TestReleaseGating(t *testing.T) {
	if CurrentRelease != 2 {
		t.Fatalf("CurrentRelease = %d, want 2", CurrentRelease)
	}
	for _, tr := range AllTransitions() {
		if tr.Release > CurrentRelease {
			if _, err := Validate(tr.From, tr.To); err == nil {
				t.Errorf("%s -> %s is release %d but was permitted", tr.From, tr.To, tr.Release)
			}
		}
	}
}

// walk asserts every consecutive pair in a path is a legal transition.
func walk(t *testing.T, path []Status, dir Direction) {
	t.Helper()
	for i := 0; i < len(path)-1; i++ {
		if _, err := ValidateDirection(path[i], path[i+1], dir); err != nil {
			t.Fatalf("step %d: %s -> %s: %v", i, path[i], path[i+1], err)
		}
	}
}

func asAPIError(err error, target **apierr.Error) bool {
	e, ok := err.(*apierr.Error)
	if ok {
		*target = e
	}
	return ok
}
