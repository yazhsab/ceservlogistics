package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Termii — SMS and WhatsApp for the Nigerian market.
//
// # Why this provider
//
// Nigeria has no equivalent of India's DLT template registry: the NCC requires a
// registered alphanumeric *sender id*, but not per-template pre-approval. That
// makes SMS integration far lighter than it would be in India, and it is why
// this adapter needs `ProviderRef` only as a sender id rather than as a
// mandatory per-message template handle.
//
// WhatsApp is the more important channel here. Nigerian business communication
// runs on it, delivery rates are better than SMS, and utility-category
// conversations are cheaper per message. The same Termii account serves both,
// which is the reason to prefer it over wiring two providers.
//
// # What this adapter is careful about
//
// Termii answers HTTP 200 for a rejected message and puts the real outcome in
// the body, so status code alone is not the answer. Classifying wrongly is
// expensive in both directions: retrying an unroutable number burns the attempt
// budget on something that can never succeed, and giving up on a gateway blip
// loses a message that would have gone through a second later. `classifyTermii`
// is where that judgement lives and is the part worth reviewing.
//
// # What it deliberately does not do
//
// No balance checks, no delivery-report polling, no sender-id management. A
// delivery report arrives by webhook and belongs to the partner webhook module,
// not here; an adapter's job is one call.

// Termii channels.
const (
	// termiiChannelDND routes through the Do-Not-Disturb-exempt corporate
	// route. Nigerian networks let subscribers block promotional SMS, and a
	// transactional courier notification must still arrive — a delivery
	// notification is not marketing.
	termiiChannelDND      = "dnd"
	termiiChannelGeneric  = "generic"
	termiiChannelWhatsApp = "whatsapp"
)

// TermiiConfig configures the adapter.
type TermiiConfig struct {
	APIKey  string
	BaseURL string
	// SenderID is the registered alphanumeric header, used when a template does
	// not carry its own in provider_ref.
	SenderID string
	// Channel is "dnd" or "generic" for SMS. Default "dnd": a transactional
	// notification must reach a subscriber who has blocked promotional traffic.
	Channel string
	Timeout time.Duration
	// For SMS, this is the SMS channel; for WhatsApp, set Kind to ChannelWhatsApp.
	Kind string
}

// TermiiSender delivers over one Termii channel.
type TermiiSender struct {
	cfg    TermiiConfig
	client *http.Client
}

// NewTermiiSender builds an adapter for one channel.
//
// Returns an error rather than a half-configured sender: a sender with no API
// key would register successfully and then fail every message, which reads as
// "the provider is down" rather than "nobody set the key".
func NewTermiiSender(cfg TermiiConfig) (*TermiiSender, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("notification: termii requires an API key")
	}
	if cfg.Kind == "" {
		cfg.Kind = ChannelSMS
	}
	if cfg.Kind != ChannelSMS && cfg.Kind != ChannelWhatsApp {
		return nil, fmt.Errorf("notification: termii cannot serve channel %s", cfg.Kind)
	}
	if cfg.Kind == ChannelSMS && strings.TrimSpace(cfg.SenderID) == "" {
		return nil, errors.New("notification: termii SMS requires a registered sender id")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.ng.termii.com"
	}
	if cfg.Channel == "" {
		cfg.Channel = termiiChannelDND
	}
	if cfg.Timeout <= 0 {
		// Bounded well inside the worker's per-attempt budget: a hung provider
		// must not hold a queue slot.
		cfg.Timeout = 15 * time.Second
	}
	return &TermiiSender{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (t *TermiiSender) Name() string { return "termii" }

func (t *TermiiSender) Channel() string { return t.cfg.Kind }

type termiiRequest struct {
	To      string `json:"to"`
	From    string `json:"from"`
	SMS     string `json:"sms"`
	Type    string `json:"type"`
	Channel string `json:"channel"`
	APIKey  string `json:"api_key"`
}

type termiiResponse struct {
	MessageID string `json:"message_id"`
	Message   string `json:"message"`
	Balance   any    `json:"balance"`
	// Termii reports a rejection in the body with HTTP 200, so this is not
	// decoration — it is where most failures actually appear.
	Code   string `json:"code"`
	Status any    `json:"status"`
}

// Send delivers one message.
func (t *TermiiSender) Send(ctx context.Context, msg Message) (*Result, error) {
	to := normaliseNigerianMSISDN(msg.To)
	if to == "" {
		// Never worth a retry: the number is not a number.
		return nil, Permanent(OutcomeRejected, "invalid_msisdn",
			fmt.Errorf("termii: %q is not a usable phone number", msg.To))
	}

	// A template may carry its own registered sender id; otherwise the
	// account default applies.
	from := t.cfg.SenderID
	if msg.ProviderRef != "" {
		from = msg.ProviderRef
	}

	channel := t.cfg.Channel
	if t.cfg.Kind == ChannelWhatsApp {
		channel = termiiChannelWhatsApp
		// WhatsApp has no sender id; the account's registered number is used.
		if from == "" {
			from = t.cfg.SenderID
		}
	}

	payload, err := json.Marshal(termiiRequest{
		To: to, From: from, SMS: msg.Body,
		Type: "plain", Channel: channel, APIKey: t.cfg.APIKey,
	})
	if err != nil {
		return nil, Permanent(OutcomeFailed, "", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(t.cfg.BaseURL, "/")+"/api/sms/send", bytes.NewReader(payload))
	if err != nil {
		return nil, Permanent(OutcomeFailed, "", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		// Transport failure: a timeout or a reset is exactly the case worth
		// retrying.
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, Transient(OutcomeTimeout, "", err)
		}
		return nil, Transient(OutcomeFailed, "", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Bounded read: a proxy returning an HTML error page must not be able to
	// put a megabyte into the attempt record.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))

	var body termiiResponse
	_ = json.Unmarshal(raw, &body)

	if sendErr := classifyTermii(resp.StatusCode, body, raw); sendErr != nil {
		return nil, sendErr
	}
	return &Result{
		ProviderMessageID: body.MessageID,
		ProviderStatus:    firstNonEmpty(body.Code, fmt.Sprint(body.Status), "sent"),
	}, nil
}

// classifyTermii decides whether a response is a success, a permanent refusal
// or something worth retrying.
//
// Termii answers 200 with a rejection in the body, so the status code is only
// half the signal. The rule is conservative in the direction that matters: an
// unrecognised failure is treated as retryable, because an unknown provider
// error is more often a blip than a permanent refusal, and the attempt budget
// bounds the cost of being wrong.
func classifyTermii(status int, body termiiResponse, raw []byte) *SendError {
	code := strings.ToLower(strings.TrimSpace(body.Code))
	message := strings.ToLower(strings.TrimSpace(body.Message))
	verbatim := firstNonEmpty(body.Code, body.Message, truncateBody(raw))

	switch {
	case status == http.StatusTooManyRequests:
		return Transient(OutcomeRateLimited, verbatim,
			errors.New("termii: rate limited"))
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		// A bad API key will not fix itself, and retrying it four more times
		// only delays somebody noticing.
		return Permanent(OutcomeRejected, verbatim,
			errors.New("termii: credentials rejected"))
	case status >= 500:
		return Transient(OutcomeFailed, verbatim,
			fmt.Errorf("termii: server error %d", status))
	}

	// A 200 that is not a success.
	if code == "ok" || strings.Contains(message, "successfully sent") {
		return nil
	}
	if body.MessageID != "" {
		return nil
	}

	// Refusals a retry cannot fix.
	for _, permanent := range []string{
		"insufficient", "invalid sender", "sender id",
		"invalid phone", "invalid number", "not found",
		"blacklist", "forbidden", "unsubscribed",
	} {
		if strings.Contains(message, permanent) || strings.Contains(code, permanent) {
			return Permanent(OutcomeRejected, verbatim,
				fmt.Errorf("termii: %s", firstNonEmpty(body.Message, body.Code, "rejected")))
		}
	}

	if status >= 400 {
		return Permanent(OutcomeRejected, verbatim,
			fmt.Errorf("termii: rejected with %d: %s", status, truncateBody(raw)))
	}
	// Unrecognised. Retry, and record what the provider actually said so a
	// support engineer can add it to the list above.
	return Transient(OutcomeFailed, verbatim,
		fmt.Errorf("termii: unrecognised response: %s", truncateBody(raw)))
}

// normaliseNigerianMSISDN puts a Nigerian number into the international form
// Termii expects, and returns "" for anything that cannot be one.
//
// Senders type these four ways — 08031234567, 8031234567, +2348031234567,
// 2348031234567 — and a provider that receives the wrong one silently fails to
// deliver rather than complaining.
func normaliseNigerianMSISDN(raw string) string {
	var digits strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	n := digits.String()

	switch {
	case strings.HasPrefix(n, "234") && len(n) == 13:
		// Already international.
	case strings.HasPrefix(n, "0") && len(n) == 11:
		// Local trunk form: 0803… -> 234803…
		n = "234" + n[1:]
	case len(n) == 10:
		// Bare subscriber number: 803… -> 234803…
		n = "234" + n
	default:
		// Not obviously Nigerian. Pass through anything of plausible
		// international length rather than refusing — the platform serves more
		// than one market, and Termii will reject a genuinely bad number.
		if len(n) >= 10 && len(n) <= 15 {
			return n
		}
		return ""
	}
	return n
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" && t != "<nil>" {
			return t
		}
	}
	return ""
}

func truncateBody(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 300 {
		return s[:300]
	}
	return s
}
