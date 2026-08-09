package shipment

import (
	"strings"
	"testing"
)

func TestValidAWB(t *testing.T) {
	valid := []string{
		"QYN260808000001", // the format documented in the constitution
		"CSV260808999999",
		"AB260808000001", // two-character prefix
		"ABCD260808000001",
	}
	for _, awb := range valid {
		if !ValidAWB(awb) {
			t.Errorf("expected %q to be a valid AWB", awb)
		}
	}
	invalid := []string{
		"", "QYN", "qyn260808000001", // lower case
		"QYNXX0808000001",   // non-numeric period
		"Q260808000001",     // one-character prefix
		"ABCDE260808000001", // five-character prefix
		"QYN26080800000",    // short sequence
		"QYN260808000001X",  // trailing junk
	}
	for _, awb := range invalid {
		if ValidAWB(awb) {
			t.Errorf("expected %q to be rejected", awb)
		}
	}
}

func TestPieceBarcodeDerivesFromAWB(t *testing.T) {
	awb := "CSV260808000042"
	got := PieceBarcode(awb, 3)
	if got != "CSV260808000042-03" {
		t.Errorf("PieceBarcode = %q", got)
	}
	if !strings.HasPrefix(got, awb) {
		t.Error("a piece barcode must read back to its parent AWB")
	}
}

// TestTransitionTableIsDeterministic guards the state machine: every declared
// transition must be unique, and nothing may leave a terminal state.
func TestTransitionTableIsDeterministic(t *testing.T) {
	seen := map[string]bool{}
	for _, tr := range transitions {
		key := string(tr.From) + "->" + string(tr.To)
		if seen[key] {
			t.Errorf("duplicate transition %s", key)
		}
		seen[key] = true
		if tr.From == tr.To {
			t.Errorf("self-transition declared for %s", tr.From)
		}
		if IsTerminal(tr.From) {
			t.Errorf("transition declared out of terminal state %s", tr.From)
		}
	}
}

func TestValidateTransition(t *testing.T) {
	t.Run("booked to cancelled is allowed", func(t *testing.T) {
		if _, err := Validate(StatusBooked, StatusCancelled); err != nil {
			t.Fatalf("expected BOOKED -> CANCELLED to be allowed, got %v", err)
		}
	})
	t.Run("cancelled is terminal", func(t *testing.T) {
		if _, err := Validate(StatusCancelled, StatusBooked); err == nil {
			t.Fatal("expected a transition out of CANCELLED to be refused")
		}
	})
	t.Run("release 2 operational transitions are available", func(t *testing.T) {
		if _, err := Validate(StatusBooked, StatusPickupScheduled); err != nil {
			t.Fatalf("BOOKED -> PICKUP_SCHEDULED should execute in a Release 2 build: %v", err)
		}
	})
	t.Run("undeclared transitions are refused", func(t *testing.T) {
		if _, err := Validate(StatusBooked, StatusDelivered); err == nil {
			t.Fatal("BOOKED -> DELIVERED must not be reachable directly")
		}
	})
}

func TestAllowedFromOnlyListsCurrentRelease(t *testing.T) {
	// In Release 2 a booked shipment can be scheduled for pickup, taken in over
	// the counter, or cancelled. Anything else is a defect in the table.
	allowed := AllowedFrom(StatusBooked)
	want := []Status{StatusCancelled, StatusOriginBranchReceived, StatusPickupScheduled}
	if len(allowed) != len(want) {
		t.Fatalf("BOOKED allows %v, want %v", allowed, want)
	}
	for i := range want {
		if allowed[i] != want[i] {
			t.Errorf("allowed[%d] = %s, want %s", i, allowed[i], want[i])
		}
	}
	if got := AllowedFrom(StatusDelivered); len(got) != 0 {
		t.Errorf("a terminal state must allow nothing, got %v", got)
	}
}
