package partner

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/jobs"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// JobTypeWebhookSend delivers one webhook.
const JobTypeWebhookSend = "webhook.send"

// Headers a consumer reads. Named after the widely-used convention so a partner
// can reuse an existing verification library rather than writing one.
const (
	HeaderSignature = "Webhook-Signature"
	HeaderID        = "Webhook-Id"
	HeaderTimestamp = "Webhook-Timestamp"
	HeaderEvent     = "Webhook-Event"
	HeaderAttempt   = "Webhook-Attempt"
)

// signatureTolerance bounds how old a timestamp a consumer should accept. It is
// documented rather than enforced here — the consumer does the checking — but
// it is the value the contract tells them to use.
const signatureTolerance = 5 * time.Minute

// Delivery statuses.
const (
	DeliveryPending    = "PENDING"
	DeliverySending    = "SENDING"
	DeliveryDelivered  = "DELIVERED"
	DeliveryFailed     = "FAILED"
	DeliveryDeadLetter = "DEAD_LETTER"
	DeliveryCancelled  = "CANCELLED"
)

// pauseAfter is how many consecutive failures disable an endpoint. A partner
// whose server has been down for a day should not still be generating retries.
const pauseAfter = 20

// Error codes.
const (
	CodeEndpointPaused = "WEBHOOK_ENDPOINT_PAUSED"
	CodeNotReplayable  = "WEBHOOK_NOT_REPLAYABLE"
	CodeInvalidURL     = "WEBHOOK_INVALID_URL"
)

// WebhookService fans events out to partner endpoints.
type WebhookService struct {
	db     *database.DB
	q      *dbgen.Queries
	jobs   *jobs.Enqueuer
	client *http.Client
	audit  *audit.Recorder
	log    *slog.Logger
}

func NewWebhookService(
	db *database.DB, q *dbgen.Queries, enq *jobs.Enqueuer,
	rec *audit.Recorder, log *slog.Logger,
) *WebhookService {
	return &WebhookService{
		db: db, q: q, jobs: enq, audit: rec, log: log,
		// A bounded client. An endpoint that never responds must not hold a
		// worker slot indefinitely; the per-endpoint timeout narrows this
		// further at send time.
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetHTTPClient replaces the delivery client.
//
// Exists for tests, which stand up a TLS endpoint with a self-signed
// certificate: the alternative is disabling verification in production code so
// a test can pass, which is exactly the kind of shortcut that later turns into
// an incident.
func (s *WebhookService) SetHTTPClient(c *http.Client) {
	if c != nil {
		s.client = c
	}
}

// Sign computes the signature a consumer verifies.
//
// The signed string is "<timestamp>.<body>", not the body alone. Including the
// timestamp is what makes a captured request un-replayable by a third party: a
// consumer that rejects old timestamps cannot be fed yesterday's valid payload.
//
// The scheme is versioned ("v1=") so the algorithm can change without silently
// breaking every consumer.
func Sign(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", timestamp)
	mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature checks a signature in constant time.
//
// Exported because it is the reference implementation the contract points
// partners at, and because the tests use it to prove the header we send is the
// one a correct consumer would accept.
func VerifySignature(secret, signature string, timestamp int64, body []byte) bool {
	expected := Sign(secret, timestamp, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// Emit queues an event to every endpoint subscribed to it.
//
// Runs inside the caller's transaction, so emitting is atomic with the business
// event that caused it — and, like notifications, it never contacts anyone on
// the request path.
//
// eventID must be stable for a given business event: it is what both our dedupe
// index and the consumer's own dedupe key on.
func (s *WebhookService) Emit(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal,
	eventType, eventID string, payload any, shipmentID *int64,
) ([]dbgen.WebhookDelivery, error) {
	q := s.q.WithTx(tx)

	endpoints, err := q.ListEndpointsForEvent(ctx, dbgen.ListEndpointsForEventParams{
		OrganizationID: p.OrganizationID, EventType: eventType,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	if len(endpoints) == 0 {
		return nil, nil // nobody is listening; not an error
	}

	// Serialise once. Every endpoint gets byte-identical bytes, and a retry
	// three hours later sends what the event said *then* rather than what the
	// object looks like now.
	body, err := json.Marshal(map[string]any{
		"id":        eventID,
		"type":      eventType,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
		"data":      payload,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}

	out := make([]dbgen.WebhookDelivery, 0, len(endpoints))
	for _, ep := range endpoints {
		delivery, err := q.CreateWebhookDelivery(ctx, dbgen.CreateWebhookDeliveryParams{
			PublicID:       publicid.New("whd"),
			OrganizationID: p.OrganizationID,
			EndpointID:     ep.ID,
			EventType:      eventType,
			EventID:        eventID,
			Payload:        body,
			ShipmentID:     shipmentID,
		})
		if err != nil {
			// Already queued for this endpoint — the business event was raised
			// twice and the consumer must not be told twice.
			if ops.IsUnique(err, "webhook_deliveries_event_idx") {
				continue
			}
			return nil, apierr.Internal(err)
		}

		if _, err := s.jobs.EnqueueTx(ctx, tx, JobTypeWebhookSend,
			map[string]any{
				"organizationId": p.OrganizationID,
				"deliveryId":     delivery.ID,
			},
			jobs.EnqueueOptions{
				OrganizationID: &p.OrganizationID,
				Queue:          "webhooks",
				DedupeKey:      "whd:" + delivery.PublicID,
				MaxAttempts:    int32(ep.MaxAttempts),
			}); err != nil {
			s.log.Warn("webhook queued without a job",
				"deliveryId", delivery.PublicID, "error", err)
		}
		out = append(out, delivery)
	}
	return out, nil
}

// Deliver sends one webhook. Called by the worker.
func (s *WebhookService) Deliver(ctx context.Context, orgID, deliveryID int64) error {
	claimed, err := s.q.ClaimDeliveryForSend(ctx, dbgen.ClaimDeliveryForSendParams{
		OrganizationID: orgID, ID: deliveryID,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil // already delivered, cancelled, or claimed elsewhere
		}
		return err
	}

	endpoint, err := s.q.GetWebhookEndpointByID(ctx, claimed.EndpointID)
	if err != nil {
		return err
	}

	timestamp := time.Now().UTC().Unix()
	signature := Sign(endpoint.SigningSecret, timestamp, claimed.Payload)

	sendCtx, cancel := context.WithTimeout(ctx,
		time.Duration(endpoint.TimeoutSeconds)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(sendCtx, http.MethodPost, endpoint.Url,
		bytes.NewReader(claimed.Payload))
	if err != nil {
		return s.recordFailure(ctx, orgID, claimed, endpoint, nil, err.Error(), 0)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "CourierOS-Webhooks/1")
	req.Header.Set(HeaderSignature, signature)
	req.Header.Set(HeaderID, claimed.EventID)
	req.Header.Set(HeaderTimestamp, strconv.FormatInt(timestamp, 10))
	req.Header.Set(HeaderEvent, claimed.EventType)
	req.Header.Set(HeaderAttempt, strconv.Itoa(int(claimed.AttemptCount)))

	started := time.Now()
	resp, err := s.client.Do(req)
	elapsed := int32(time.Since(started).Milliseconds())

	if err != nil {
		return s.recordFailure(ctx, orgID, claimed, endpoint, nil, err.Error(), elapsed)
	}
	defer resp.Body.Close()

	// Read a bounded amount. A misconfigured endpoint returning a megabyte of
	// HTML must not be able to fill the database or the worker's memory.
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	code := int32(resp.StatusCode)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if _, aErr := s.q.CreateWebhookAttempt(ctx, dbgen.CreateWebhookAttemptParams{
			OrganizationID: orgID, DeliveryID: claimed.ID,
			AttemptNo: claimed.AttemptCount, StatusCode: &code,
			ResponseBody: ops.Optional(string(snippet)), DurationMs: &elapsed,
		}); aErr != nil {
			s.log.Warn("could not record a webhook attempt", "error", aErr)
		}
		if _, err := s.q.MarkDeliverySucceeded(ctx, dbgen.MarkDeliverySucceededParams{
			OrganizationID: orgID, ID: claimed.ID, StatusCode: &code,
		}); err != nil {
			return err
		}
		return s.q.RecordWebhookSuccess(ctx, endpoint.ID)
	}

	return s.recordFailure(ctx, orgID, claimed, endpoint, &code,
		fmt.Sprintf("endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet))),
		elapsed)
}

func (s *WebhookService) recordFailure(
	ctx context.Context, orgID int64, delivery dbgen.WebhookDelivery,
	endpoint dbgen.WebhookEndpoint, statusCode *int32, message string, elapsed int32,
) error {
	if _, err := s.q.CreateWebhookAttempt(ctx, dbgen.CreateWebhookAttemptParams{
		OrganizationID: orgID, DeliveryID: delivery.ID,
		AttemptNo: delivery.AttemptCount, StatusCode: statusCode,
		ErrorMessage: ops.Optional(message), DurationMs: &elapsed,
	}); err != nil {
		s.log.Warn("could not record a webhook attempt", "error", err)
	}

	// One schedule, used twice. The delay is stored on the row so an operator
	// can see when the next attempt is due, and the same delay is handed to the
	// queue below so that is genuinely when it happens. Writing a schedule the
	// queue then ignores would make next_attempt_at a decoration.
	//
	// The delay, not the wake-up time: the database adds it to its own clock,
	// which is not the application's.
	retryIn := webhookBackoff(int(delivery.AttemptCount))
	updated, err := s.q.MarkDeliveryFailed(ctx, dbgen.MarkDeliveryFailedParams{
		OrganizationID: orgID, ID: delivery.ID,
		MaxAttempts: int32(endpoint.MaxAttempts), StatusCode: statusCode,
		ErrorMessage: ops.Optional(message),
		RetryAfter:   interval(retryIn),
	})
	if err != nil {
		return err
	}

	if _, err := s.q.RecordWebhookFailure(ctx, dbgen.RecordWebhookFailureParams{
		ID: endpoint.ID, PauseAfter: pauseAfter,
	}); err != nil {
		s.log.Warn("could not record an endpoint failure", "error", err)
	}

	// A dead-lettered delivery has nothing left for the queue to retry, so the
	// job succeeds. Returning an error would make the job burn its own budget
	// repeating a send the delivery has already given up on.
	if updated.Status == DeliveryDeadLetter {
		s.log.Info("webhook dead-lettered",
			"deliveryId", delivery.PublicID, "endpoint", endpoint.Name,
			"attempts", delivery.AttemptCount, "error", message)
		return nil
	}
	return jobs.RetryAfter(retryIn,
		fmt.Errorf("webhook %s: %s", delivery.PublicID, message))
}

// Replay re-sends a delivery as a new one.
//
// A replay is a *new* delivery row pointing at the original, not a reset of it:
// the original's attempt history is evidence and must survive. The payload is
// copied byte-for-byte so the consumer sees exactly what it saw before, which
// is what makes their own dedupe work.
func (s *WebhookService) Replay(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.WebhookDelivery, error) {
	var out *dbgen.WebhookDelivery

	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		original, err := q.GetDeliveryByPublicID(ctx, dbgen.GetDeliveryByPublicIDParams{
			OrganizationID: p.OrganizationID, PublicID: publicID,
		})
		if err != nil {
			return ops.NotFoundOr(err, "Webhook delivery")
		}
		if original.Status == DeliverySending {
			return apierr.Conflict(CodeNotReplayable,
				"This delivery is in flight; wait for it to finish before replaying.")
		}

		replay, err := q.CreateWebhookDelivery(ctx, dbgen.CreateWebhookDeliveryParams{
			PublicID:       publicid.New("whd"),
			OrganizationID: p.OrganizationID,
			EndpointID:     original.EndpointID,
			EventType:      original.EventType,
			EventID:        original.EventID,
			Payload:        original.Payload,
			ShipmentID:     original.ShipmentID,
			ReplayOfID:     &original.ID,
		})
		if err != nil {
			return apierr.Internal(err)
		}

		if _, err := s.jobs.EnqueueTx(ctx, tx, JobTypeWebhookSend,
			map[string]any{"organizationId": p.OrganizationID, "deliveryId": replay.ID},
			jobs.EnqueueOptions{
				OrganizationID: &p.OrganizationID, Queue: "webhooks",
				DedupeKey: "whd:" + replay.PublicID, MaxAttempts: 6,
			}); err != nil {
			s.log.Warn("replay queued without a job", "error", err)
		}

		out = &replay
		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "webhook.replayed", ResourceType: "webhook_delivery",
			ResourceID: &original.ID, ResourcePublicID: original.PublicID,
			Metadata: map[string]any{
				"eventType": original.EventType, "eventId": original.EventID,
				"replayId": replay.PublicID,
			},
		}))
	})
	return out, err
}

// RegisterHandlers wires webhook delivery into a worker.
func (s *WebhookService) RegisterHandlers(w *jobs.Worker) {
	w.Register(JobTypeWebhookSend, func(ctx context.Context, job jobs.Job) error {
		var payload struct {
			OrganizationID int64 `json:"organizationId"`
			DeliveryID     int64 `json:"deliveryId"`
		}
		if err := job.Decode(&payload); err != nil {
			return err
		}
		return s.Deliver(ctx, payload.OrganizationID, payload.DeliveryID)
	})
}

// CreateEndpoint registers a partner endpoint and returns its signing secret
// once.
func (s *WebhookService) CreateEndpoint(
	ctx context.Context, p *tenant.Principal, name, url string, events []string,
) (*dbgen.WebhookEndpoint, string, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, "", apierr.Validation(
			"A webhook endpoint must be HTTPS: payloads carry customer data.",
			map[string]any{"url": "must start with https://"})
	}
	secret, err := randomToken(32)
	if err != nil {
		return nil, "", apierr.Internal(err)
	}

	var endpoint *dbgen.WebhookEndpoint
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		created, cErr := q.CreateWebhookEndpoint(ctx, dbgen.CreateWebhookEndpointParams{
			PublicID: publicid.New("whe"), OrganizationID: p.OrganizationID,
			Name: name, Url: url, SigningSecret: secret,
			MaxAttempts: 6, TimeoutSeconds: 10, CreatedBy: &p.UserID,
		})
		if cErr != nil {
			return apierr.Internal(cErr)
		}
		for _, event := range events {
			if _, sErr := q.CreateWebhookSubscription(ctx, dbgen.CreateWebhookSubscriptionParams{
				PublicID: publicid.New("whs"), OrganizationID: p.OrganizationID,
				EndpointID: created.ID, EventType: event, Filter: []byte("{}"),
			}); sErr != nil {
				return apierr.Internal(sErr)
			}
		}
		endpoint = &created
		return s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "webhook.endpoint.created", ResourceType: "webhook_endpoint",
			ResourceID: &created.ID, ResourcePublicID: created.PublicID,
			Metadata: map[string]any{"name": name, "url": url, "events": events},
		}))
	})
	if err != nil {
		return nil, "", err
	}
	return endpoint, secret, nil
}

// webhookBackoff spaces retries out.
//
// Faster than notifications at the start — a partner's transient 502 usually
// clears in seconds — and capped at 30 minutes so a long outage does not push a
// retry beyond the point the event is still useful.
func webhookBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := 10 * time.Second << uint(attempt-1)
	if d > 30*time.Minute {
		d = 30 * time.Minute
	}
	return d
}

// interval converts a Go duration to the pgtype form the queries take.
func interval(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}
