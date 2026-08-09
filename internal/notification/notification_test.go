package notification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRenderSubstitutesPlaceholders(t *testing.T) {
	out := Render("Your shipment {{awb}} is out for delivery to {{city}}.",
		map[string]string{"awb": "DMO260808000042", "city": "New Delhi"})
	want := "Your shipment DMO260808000042 is out for delivery to New Delhi."
	if out != want {
		t.Fatalf("rendered %q, want %q", out, want)
	}
}

func TestRenderToleratesWhitespaceInPlaceholders(t *testing.T) {
	// Template editors will produce both forms; a customer must not see a raw
	// placeholder because someone typed a space.
	for _, tmpl := range []string{"{{awb}}", "{{ awb }}", "{{  awb  }}"} {
		if got := Render(tmpl, map[string]string{"awb": "X1"}); got != "X1" {
			t.Fatalf("%q rendered as %q, want X1", tmpl, got)
		}
	}
}

func TestRenderLeavesNoTemplateArtefactForAMissingVariable(t *testing.T) {
	// A gap in a message is bad. "{{recipientName}}" in a customer's SMS is
	// worse, because it looks broken rather than merely terse.
	out := Render("Hello {{recipientName}}, your parcel {{awb}} is here.",
		map[string]string{"awb": "X1"})
	if strings.Contains(out, "{{") || strings.Contains(out, "}}") {
		t.Fatalf("rendered output still contains a placeholder: %q", out)
	}
	if !strings.Contains(out, "X1") {
		t.Fatalf("known variable was not substituted: %q", out)
	}
}

func TestMissingVariablesReportsGaps(t *testing.T) {
	missing := MissingVariables(
		"{{awb}} to {{city}} for {{customerName}}",
		map[string]string{"awb": "X1"})
	if len(missing) != 2 || missing[0] != "city" || missing[1] != "customerName" {
		t.Fatalf("missing = %v, want [city customerName]", missing)
	}
	if len(MissingVariables("{{awb}}", map[string]string{"awb": "X1"})) != 0 {
		t.Fatal("a fully-supplied template reported gaps")
	}
}

func TestPlaceholdersAreDeduplicatedAndSorted(t *testing.T) {
	got := Placeholders("{{b}} {{a}} {{b}} {{c}}")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("placeholders = %v, want [a b c]", got)
	}
}

func TestValidAddress(t *testing.T) {
	cases := []struct {
		channel, address string
		want             bool
	}{
		{ChannelEmail, "someone@example.com", true},
		{ChannelEmail, "someone@example", false},
		{ChannelEmail, "no-at-sign", false},
		{ChannelEmail, "", false},
		{ChannelSMS, "+919876543210", true},
		{ChannelSMS, "9876543210", true},
		{ChannelSMS, "12345", false},
		{ChannelWhatsApp, "+919876543210", true},
		{ChannelPush, "fcm-token-abcdefgh", true},
		{ChannelPush, "short", false},
		{"CARRIER_PIGEON", "anything", false},
	}
	for _, tc := range cases {
		if got := ValidAddress(tc.channel, tc.address); got != tc.want {
			t.Errorf("ValidAddress(%s, %q) = %v, want %v",
				tc.channel, tc.address, got, tc.want)
		}
	}
}

func TestClassifyDefaultsToRetryable(t *testing.T) {
	// An adapter returning a bare error must not cause a message to be
	// dead-lettered: a message sent twice is a nuisance, a message never sent
	// is a support ticket.
	se := classify(errors.New("connection reset"))
	if !se.Retryable {
		t.Fatal("a plain error should be treated as retryable")
	}
	if se.Outcome != OutcomeFailed {
		t.Fatalf("outcome = %s, want FAILED", se.Outcome)
	}
}

func TestClassifyRecognisesTimeouts(t *testing.T) {
	se := classify(context.DeadlineExceeded)
	if !se.Retryable || se.Outcome != OutcomeTimeout {
		t.Fatalf("a deadline should be a retryable TIMEOUT, got %s retryable=%v",
			se.Outcome, se.Retryable)
	}
}

func TestClassifyPreservesAnAdapterDecision(t *testing.T) {
	// A rejected address never becomes valid; retrying it burns the budget.
	se := classify(Permanent(OutcomeRejected, "21211", errors.New("invalid 'To' number")))
	if se.Retryable {
		t.Fatal("a permanent failure was reclassified as retryable")
	}
	if se.ProviderStatus != "21211" {
		t.Fatalf("provider status = %q, want it preserved verbatim", se.ProviderStatus)
	}

	tr := classify(Transient(OutcomeRateLimited, "429", errors.New("slow down")))
	if !tr.Retryable {
		t.Fatal("a transient failure was reclassified as permanent")
	}
}

func TestClassifyUnwrapsAWrappedSendError(t *testing.T) {
	inner := Permanent(OutcomeRejected, "BAD", errors.New("nope"))
	se := classify(&wrapped{inner})
	if se.Retryable {
		t.Fatal("a wrapped permanent failure lost its retry decision")
	}
}

type wrapped struct{ err error }

func (w *wrapped) Error() string { return "wrapped: " + w.err.Error() }
func (w *wrapped) Unwrap() error { return w.err }

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	first := backoff(1)
	second := backoff(2)
	if second <= first {
		t.Fatalf("backoff did not grow: %v then %v", first, second)
	}
	// Capped, so a long outage does not push a retry a day into the future.
	if got := backoff(20); got != 30*time.Minute {
		t.Fatalf("backoff(20) = %v, want the 30m cap", got)
	}
	if got := backoff(0); got != time.Minute {
		t.Fatalf("backoff(0) = %v, want 1m", got)
	}
}

func TestRegistryReportsAnUnconfiguredChannel(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.For(ChannelSMS); ok {
		t.Fatal("an empty registry reported a sender")
	}
	r.Register(NewLogSender(ChannelSMS, discardLogger{}))
	if _, ok := r.For(ChannelSMS); !ok {
		t.Fatal("a registered sender was not found")
	}
	if _, ok := r.For(ChannelWhatsApp); ok {
		t.Fatal("an unregistered channel reported a sender")
	}
	if len(r.Channels()) != 1 {
		t.Fatalf("channels = %v, want one", r.Channels())
	}
}

func TestLogSenderRespectsAContextDeadline(t *testing.T) {
	// The worker bounds every attempt so one hung provider cannot stall the
	// queue; a sender that ignores the deadline breaks that.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := NewLogSender(ChannelSMS, discardLogger{})
	if _, err := s.Send(ctx, Message{To: "+919876543210", Body: "hi"}); err == nil {
		t.Fatal("the sender ignored a cancelled context")
	}
}

func TestLogSenderSucceedsAndReportsAMessageID(t *testing.T) {
	s := NewLogSender(ChannelEmail, discardLogger{})
	res, err := s.Send(context.Background(), Message{
		To: "a@example.com", Subject: "hi", Body: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderMessageID == "" {
		t.Fatal("a successful send must report a provider message id")
	}
	if s.Name() != "log" || s.Channel() != ChannelEmail {
		t.Fatalf("sender identity = %s/%s", s.Name(), s.Channel())
	}
}

type discardLogger struct{}

func (discardLogger) Info(string, ...any) {}
