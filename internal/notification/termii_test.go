package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Two things decide whether this adapter works in production: whether a
// Nigerian number reaches the provider in a form it recognises, and whether a
// failure is classified as worth retrying. Everything else is plumbing.

func TestNigerianNumberNormalisation(t *testing.T) {
	// Senders type these four ways. A provider handed the wrong form does not
	// complain — it silently fails to deliver.
	for _, tc := range []struct{ in, want string }{
		{"08031234567", "2348031234567"},    // local trunk
		{"8031234567", "2348031234567"},     // bare subscriber
		{"+2348031234567", "2348031234567"}, // international
		{"2348031234567", "2348031234567"},  // already normalised
		{"0803 123 4567", "2348031234567"},  // spaced
		{"+234 803-123-4567", "2348031234567"},
	} {
		if got := normaliseNigerianMSISDN(tc.in); got != tc.want {
			t.Errorf("normalise(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// Not plausibly a phone number at all.
	for _, bad := range []string{"", "123", "abc", "0803"} {
		if got := normaliseNigerianMSISDN(bad); got != "" {
			t.Errorf("normalise(%q) = %q, want empty", bad, got)
		}
	}

	// A non-Nigerian international number passes through rather than being
	// refused: the platform serves more than one market.
	if got := normaliseNigerianMSISDN("+919876543210"); got != "919876543210" {
		t.Errorf("an Indian number was mangled to %q", got)
	}
}

func TestTermiiRequiresConfiguration(t *testing.T) {
	// A sender with no key would register successfully and then fail every
	// message, which reads as "the provider is down" rather than "nobody set
	// the key".
	if _, err := NewTermiiSender(TermiiConfig{}); err == nil {
		t.Fatal("a Termii sender with no API key was accepted")
	}
	if _, err := NewTermiiSender(TermiiConfig{APIKey: "k"}); err == nil {
		t.Fatal("an SMS sender with no registered sender id was accepted")
	}
	if _, err := NewTermiiSender(TermiiConfig{
		APIKey: "k", Kind: ChannelEmail,
	}); err == nil {
		t.Fatal("Termii was accepted for a channel it does not serve")
	}
	// WhatsApp needs no sender id.
	if _, err := NewTermiiSender(TermiiConfig{APIKey: "k", Kind: ChannelWhatsApp}); err != nil {
		t.Fatalf("a valid WhatsApp sender was refused: %v", err)
	}
}

// termiiStub answers with whatever a test asks for, and records the request.
func termiiStub(t *testing.T, status int, body map[string]any) (*httptest.Server, *termiiRequest) {
	t.Helper()
	captured := &termiiRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(captured)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

func newStubSender(t *testing.T, srv *httptest.Server) *TermiiSender {
	t.Helper()
	s, err := NewTermiiSender(TermiiConfig{
		APIKey: "test-key", SenderID: "CeServe", BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTermiiSendsNormalisedAndReturnsTheProviderID(t *testing.T) {
	srv, captured := termiiStub(t, http.StatusOK, map[string]any{
		"message_id": "msg-123", "message": "Successfully Sent", "code": "ok",
	})
	sender := newStubSender(t, srv)

	res, err := sender.Send(context.Background(), Message{
		Channel: ChannelSMS, To: "08031234567", Body: "Your parcel is out for delivery.",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.ProviderMessageID != "msg-123" {
		t.Fatalf("provider message id = %q", res.ProviderMessageID)
	}
	if captured.To != "2348031234567" {
		t.Fatalf("sent to %q, want the normalised form", captured.To)
	}
	if captured.From != "CeServe" {
		t.Fatalf("sender id = %q", captured.From)
	}
	// Transactional traffic must reach subscribers who blocked promotional SMS.
	if captured.Channel != termiiChannelDND {
		t.Fatalf("channel = %q, want the DND-exempt route for transactional SMS",
			captured.Channel)
	}
}

func TestTermiiTemplateSenderIDWins(t *testing.T) {
	srv, captured := termiiStub(t, http.StatusOK, map[string]any{
		"message_id": "m", "code": "ok",
	})
	sender := newStubSender(t, srv)

	// A template may carry its own registered header — this is what
	// provider_ref is for, and before the plumbing existed it never arrived.
	if _, err := sender.Send(context.Background(), Message{
		Channel: ChannelSMS, To: "08031234567", Body: "hi", ProviderRef: "CeServeOps",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if captured.From != "CeServeOps" {
		t.Fatalf("sender id = %q, want the template's provider_ref", captured.From)
	}
}

func TestTermiiRejectsAnUnusableNumberWithoutCallingOut(t *testing.T) {
	srv, captured := termiiStub(t, http.StatusOK, map[string]any{"code": "ok"})
	sender := newStubSender(t, srv)

	_, err := sender.Send(context.Background(), Message{
		Channel: ChannelSMS, To: "not-a-number", Body: "hi",
	})
	if err == nil {
		t.Fatal("an unusable number was sent")
	}
	var se *SendError
	if !asSendError(err, &se) {
		t.Fatalf("error is not a SendError: %T", err)
	}
	if se.Retryable {
		t.Fatal("an unusable number was marked retryable; a retry cannot fix it")
	}
	if captured.To != "" {
		t.Fatal("the provider was called with a number known to be unusable")
	}
}

func TestTermiiFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      map[string]any
		retryable bool
		outcome   string
	}{
		// A 200 carrying a rejection: the status code is only half the signal.
		{"insufficient balance", http.StatusOK,
			map[string]any{"message": "Insufficient balance"}, false, OutcomeRejected},
		{"unregistered sender id", http.StatusOK,
			map[string]any{"message": "Invalid Sender ID"}, false, OutcomeRejected},
		{"unroutable number", http.StatusOK,
			map[string]any{"message": "Invalid phone number"}, false, OutcomeRejected},
		// Worth retrying.
		{"rate limited", http.StatusTooManyRequests,
			map[string]any{"message": "Too many requests"}, true, OutcomeRateLimited},
		{"provider outage", http.StatusBadGateway,
			map[string]any{"message": "Bad gateway"}, true, OutcomeFailed},
		// A bad key will not fix itself; retrying only delays somebody noticing.
		{"bad credentials", http.StatusUnauthorized,
			map[string]any{"message": "Invalid API key"}, false, OutcomeRejected},
		// The conservative default: unknown failures retry.
		{"unrecognised", http.StatusOK,
			map[string]any{"message": "Some new error nobody has seen"}, true, OutcomeFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := termiiStub(t, tc.status, tc.body)
			sender := newStubSender(t, srv)

			_, err := sender.Send(context.Background(), Message{
				Channel: ChannelSMS, To: "08031234567", Body: "hi",
			})
			if err == nil {
				t.Fatal("the failure was reported as a success")
			}
			var se *SendError
			if !asSendError(err, &se) {
				t.Fatalf("error is not a SendError: %T", err)
			}
			if se.Retryable != tc.retryable {
				t.Errorf("retryable = %v, want %v (%v)", se.Retryable, tc.retryable, err)
			}
			if se.Outcome != tc.outcome {
				t.Errorf("outcome = %s, want %s", se.Outcome, tc.outcome)
			}
			// The provider's own words are kept, because that is what a support
			// engineer needs to add a new case to the classifier.
			if se.ProviderStatus == "" {
				t.Error("the provider's response was not recorded")
			}
		})
	}
}

func TestTermiiWhatsAppUsesTheWhatsAppChannel(t *testing.T) {
	srv, captured := termiiStub(t, http.StatusOK, map[string]any{
		"message_id": "m", "code": "ok",
	})
	sender, err := NewTermiiSender(TermiiConfig{
		APIKey: "k", SenderID: "2348000000000", BaseURL: srv.URL, Kind: ChannelWhatsApp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sender.Channel() != ChannelWhatsApp {
		t.Fatalf("Channel() = %s", sender.Channel())
	}
	if _, err := sender.Send(context.Background(), Message{
		Channel: ChannelWhatsApp, To: "08031234567", Body: "hi",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if captured.Channel != termiiChannelWhatsApp {
		t.Fatalf("channel = %q, want whatsapp", captured.Channel)
	}
}

func TestTermiiTruncatesAHugeErrorBody(t *testing.T) {
	// A proxy returning an HTML error page must not put a megabyte into the
	// attempt record.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(strings.Repeat("x", 500_000)))
	}))
	t.Cleanup(srv.Close)
	sender := newStubSender(t, srv)

	_, err := sender.Send(context.Background(), Message{
		Channel: ChannelSMS, To: "08031234567", Body: "hi",
	})
	if err == nil {
		t.Fatal("expected a failure")
	}
	var se *SendError
	if !asSendError(err, &se) {
		t.Fatalf("error is not a SendError: %T", err)
	}
	if len(se.ProviderStatus) > 400 {
		t.Fatalf("the recorded provider status is %d bytes", len(se.ProviderStatus))
	}
}

func asSendError(err error, target **SendError) bool {
	se, ok := err.(*SendError)
	if ok {
		*target = se
	}
	return ok
}
