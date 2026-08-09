// Package notification implements M26: provider-neutral notifications.
//
// # The rule that shapes everything
//
// "Shipment success must not depend on notification provider availability."
//
// So raising a notification is one INSERT plus a job enqueue, inside the caller's
// transaction. Nothing here opens a socket to a provider on the request path. If
// Twilio is down, a booking still books; the message sits PENDING and the worker
// retries it later.
//
// # Provider neutrality
//
// A Sender is an interface. The domain knows about channels — EMAIL, SMS,
// WHATSAPP, PUSH — and nothing about Twilio, SES or FCM. Adding a provider is
// implementing one method and registering it; no shipment, delivery or billing
// code changes, which is the point of §"Do not place provider API code in
// shipment domain".
//
// # Retry
//
// The platform job queue already does bounded retry, backoff, dedupe and
// dead-lettering (§30), so none of that is reimplemented. What this package adds
// is the decision an adapter is uniquely able to make: whether a given failure
// is worth retrying at all. A rejected phone number never becomes valid; a
// gateway timeout might.
package notification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Channels.
const (
	ChannelEmail    = "EMAIL"
	ChannelSMS      = "SMS"
	ChannelWhatsApp = "WHATSAPP"
	ChannelPush     = "PUSH"
)

// Statuses.
const (
	StatusPending    = "PENDING"
	StatusSending    = "SENDING"
	StatusSent       = "SENT"
	StatusDelivered  = "DELIVERED"
	StatusFailed     = "FAILED"
	StatusDeadLetter = "DEAD_LETTER"
	StatusSuppressed = "SUPPRESSED"
	StatusCancelled  = "CANCELLED"
)

// Attempt outcomes.
const (
	OutcomeSent        = "SENT"
	OutcomeFailed      = "FAILED"
	OutcomeRejected    = "REJECTED"
	OutcomeTimeout     = "TIMEOUT"
	OutcomeRateLimited = "RATE_LIMITED"
	OutcomeSkipped     = "SKIPPED"
)

// Event types raised by the platform. Templates key on these.
const (
	EventShipmentBooked         = "SHIPMENT_BOOKED"
	EventPickupScheduled        = "PICKUP_SCHEDULED"
	EventPickupCompleted        = "PICKUP_COMPLETED"
	EventShipmentInTransit      = "SHIPMENT_IN_TRANSIT"
	EventShipmentOutForDelivery = "SHIPMENT_OUT_FOR_DELIVERY"
	EventShipmentDelivered      = "SHIPMENT_DELIVERED"
	EventShipmentNDR            = "SHIPMENT_NDR"
	EventShipmentRTO            = "SHIPMENT_RTO"
	EventDeliveryOTP            = "DELIVERY_OTP"
	EventInvoiceIssued          = "INVOICE_ISSUED"
	EventSettlementApproved     = "SETTLEMENT_APPROVED"
)

// Message is what an adapter is asked to send. It is deliberately free of any
// database type: an adapter cannot reach back into the domain.
type Message struct {
	Channel string
	To      string
	ToName  string
	Subject string
	Body    string
	Locale  string
	// ProviderRef carries a provider-specific handle — an approved WhatsApp
	// template name, an SMS sender id — taken from the template's metadata.
	ProviderRef string
	// Variables are passed through for providers that render server-side
	// (WhatsApp template messages) rather than accepting a rendered body.
	Variables map[string]string
}

// Result is what an adapter reports back.
type Result struct {
	ProviderMessageID string
	ProviderStatus    string
}

// Sender delivers a message over one channel.
//
// An implementation must be safe to call concurrently and must respect the
// context deadline: the worker gives each attempt a bounded slice of time so one
// hung provider cannot stall the queue.
type Sender interface {
	// Name identifies the provider in logs and in notification_attempts.
	Name() string
	// Channel is the one channel this sender handles.
	Channel() string
	// Send delivers the message, or returns an error describing why not.
	Send(ctx context.Context, msg Message) (*Result, error)
}

// SendError lets an adapter say whether a failure is worth retrying.
//
// This is the judgement only the adapter can make, and getting it wrong is
// expensive in both directions: retrying a rejected address burns the attempt
// budget on something that can never succeed, while giving up on a timeout
// loses a message that would have gone through a second later.
type SendError struct {
	// Outcome is one of the OutcomeX constants.
	Outcome string
	// Retryable is false for anything a retry cannot fix — a malformed
	// address, an unknown recipient, a rejected template.
	Retryable bool
	// ProviderStatus is the provider's own code, kept verbatim. Never mapped
	// into a shared enum: providers disagree, and the raw value is what a
	// support engineer needs.
	ProviderStatus string
	Err            error
}

func (e *SendError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("%s (%s)", e.Outcome, e.ProviderStatus)
	}
	return fmt.Sprintf("%s: %v", e.Outcome, e.Err)
}

func (e *SendError) Unwrap() error { return e.Err }

// Permanent builds a non-retryable failure.
func Permanent(outcome, providerStatus string, err error) *SendError {
	return &SendError{Outcome: outcome, Retryable: false, ProviderStatus: providerStatus, Err: err}
}

// Transient builds a retryable failure.
func Transient(outcome, providerStatus string, err error) *SendError {
	return &SendError{Outcome: outcome, Retryable: true, ProviderStatus: providerStatus, Err: err}
}

// classify turns any error into a retry decision.
//
// An adapter that returns a plain error gets the safe default — retryable —
// because a message sent twice is a nuisance while a message never sent is a
// support ticket.
func classify(err error) *SendError {
	var se *SendError
	if errors.As(err, &se) {
		return se
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return Transient(OutcomeTimeout, "", err)
	}
	return Transient(OutcomeFailed, "", err)
}

// Registry holds one sender per channel.
//
// A channel with no registered sender suppresses rather than fails: an
// organization that has not configured WhatsApp should not accumulate
// dead-lettered WhatsApp messages.
type Registry struct {
	senders map[string]Sender
}

func NewRegistry() *Registry { return &Registry{senders: map[string]Sender{}} }

// Register installs a sender for its channel, replacing any existing one.
func (r *Registry) Register(s Sender) { r.senders[s.Channel()] = s }

// For returns the sender for a channel, and whether one is configured.
func (r *Registry) For(channel string) (Sender, bool) {
	s, ok := r.senders[channel]
	return s, ok
}

// Channels lists the configured channels, for the health endpoint.
func (r *Registry) Channels() []string {
	out := make([]string, 0, len(r.senders))
	for c := range r.senders {
		out = append(out, c)
	}
	return out
}

// ---------------------------------------------------------------------------
// LogSender
// ---------------------------------------------------------------------------

// LogSender writes a message to the log instead of sending it.
//
// This is the default in development and in tests, and it is deliberately a
// real Sender rather than a nil check scattered through the service: the code
// path exercised in development is the same one production uses, so a bug in
// rendering or preference resolution surfaces before a provider is ever wired.
//
// It is not a silent no-op — the message is logged in full, so a developer can
// see exactly what a customer would have received.
type LogSender struct {
	channel string
	log     Logger
}

// Logger is the minimal logging surface a sender needs, so this package does
// not depend on a particular logging library.
type Logger interface {
	Info(msg string, args ...any)
}

func NewLogSender(channel string, log Logger) *LogSender {
	return &LogSender{channel: channel, log: log}
}

func (s *LogSender) Name() string    { return "log" }
func (s *LogSender) Channel() string { return s.channel }

func (s *LogSender) Send(ctx context.Context, msg Message) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, Transient(OutcomeTimeout, "", err)
	}
	// Addresses are logged because this sender exists to show a developer what
	// would have been sent. A real adapter must not do this (§36).
	s.log.Info("notification (log sender)",
		"channel", s.channel, "to", msg.To,
		"subject", msg.Subject, "body", collapse(msg.Body))
	return &Result{ProviderMessageID: "log-" + fmt.Sprint(time.Now().UnixNano())}, nil
}

func collapse(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// ---------------------------------------------------------------------------
// Address validation
// ---------------------------------------------------------------------------

// ValidAddress reports whether an address is plausibly usable on a channel.
//
// Checked before a notification is created, so a missing or malformed address
// suppresses at raise time rather than burning five delivery attempts to
// discover the same thing.
func ValidAddress(channel, address string) bool {
	address = strings.TrimSpace(address)
	if address == "" {
		return false
	}
	switch channel {
	case ChannelEmail:
		at := strings.Index(address, "@")
		return at > 0 && at < len(address)-1 && strings.Contains(address[at:], ".")
	case ChannelSMS, ChannelWhatsApp:
		digits := 0
		for _, r := range address {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		return digits >= 8
	case ChannelPush:
		return len(address) >= 8
	}
	return false
}

// backoff returns the delay before attempt n.
//
// Exponential with a cap: 1m, 2m, 4m, 8m, 16m, then 30m. A provider outage is
// usually minutes rather than seconds, and retrying aggressively against a
// rate-limited gateway makes the outage worse.
func backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := time.Minute << uint(attempt-1)
	if d > 30*time.Minute {
		d = 30 * time.Minute
	}
	return d
}
