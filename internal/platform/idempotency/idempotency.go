// Package idempotency makes mutating operations safe to retry.
//
// Constitution §21: correctness rests on a database uniqueness constraint, not
// on Redis. The ledger lives in idempotency_keys with
// UNIQUE(organization_id, endpoint, idempotency_key).
//
// The protocol is:
//
//  1. Claim the key with INSERT ... ON CONFLICT DO NOTHING, committed
//     immediately so concurrent callers can see it.
//  2. Run the business work in one transaction and mark the key COMPLETED
//     *inside that same transaction*.
//  3. On failure, delete the reservation so the client may retry.
//
// Because step 2 is atomic, a key left IN_PROGRESS proves the work did not
// commit — which is what makes reclaiming an abandoned reservation safe rather
// than a route to double execution.
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
)

// Defaults.
const (
	DefaultTTL        = 24 * time.Hour
	DefaultStaleAfter = 2 * time.Minute
)

// Request identifies one idempotent call.
type Request struct {
	OrganizationID int64
	UserID         *int64
	Endpoint       string
	Key            string
	// Body is the raw request payload; its digest detects a client reusing one
	// key for two different requests.
	Body      []byte
	RequestID string
}

// Outcome is what the guarded operation produced.
type Outcome struct {
	Status           int
	Body             any
	ResourceType     string
	ResourcePublicID string
}

// Executor runs operations under idempotency protection.
type Executor struct {
	db         *database.DB
	q          *dbgen.Queries
	ttl        time.Duration
	staleAfter time.Duration
}

// NewExecutor builds an Executor.
func NewExecutor(db *database.DB, q *dbgen.Queries) *Executor {
	return &Executor{db: db, q: q, ttl: DefaultTTL, staleAfter: DefaultStaleAfter}
}

// WithTTL overrides how long completed keys are retained.
func (e *Executor) WithTTL(ttl time.Duration) *Executor {
	c := *e
	c.ttl = ttl
	return &c
}

// Operation is the guarded business work. It receives the transaction it must
// use for every write, so that its effects and the idempotency completion
// commit together.
//
// An Operation must not acquire a second pooled connection: doing so while
// holding a transaction is a pool deadlock waiting to happen once concurrency
// exceeds the pool size. Read-only preparation belongs in a Preparer.
type Operation func(ctx context.Context, tx pgx.Tx) (Outcome, error)

// Preparer runs read-only work after the idempotency key is claimed but before
// the transaction opens.
//
// Putting preparation here rather than inside the Operation keeps the
// transaction short and, critically, keeps every pooled query outside it. It
// also means a replayed request skips the work entirely.
type Preparer func(ctx context.Context) error

// Run executes op at most once for the supplied key.
//
// The second return value reports whether the response was replayed from a
// previous call rather than freshly computed; handlers surface it as the
// Idempotent-Replay header so clients can tell the difference.
func (e *Executor) Run(ctx context.Context, req Request, prepare Preparer, op Operation) (Outcome, bool, error) {
	if req.Key == "" {
		// No key supplied: run once, unprotected. The caller decided the
		// endpoint tolerates that.
		if prepare != nil {
			if err := prepare(ctx); err != nil {
				return Outcome{}, false, err
			}
		}
		out, err := e.runOnce(ctx, nil, op)
		return out, false, err
	}

	digest := hashRequest(req.Endpoint, req.Body)
	claimed, err := e.claim(ctx, req, digest)
	if err != nil {
		return Outcome{}, false, err
	}
	if !claimed {
		return e.replay(ctx, req, digest)
	}

	if prepare != nil {
		if err := prepare(ctx); err != nil {
			_ = e.q.FailIdempotencyKey(context.WithoutCancel(ctx), dbgen.FailIdempotencyKeyParams{
				OrganizationID: req.OrganizationID,
				Endpoint:       req.Endpoint,
				IdempotencyKey: req.Key,
			})
			return Outcome{}, false, err
		}
	}

	out, err := e.runOnce(ctx, &req, op)
	if err != nil {
		// Release the reservation so a corrected retry with the same key can
		// proceed. Best effort: if this fails the key simply expires.
		_ = e.q.FailIdempotencyKey(context.WithoutCancel(ctx), dbgen.FailIdempotencyKeyParams{
			OrganizationID: req.OrganizationID,
			Endpoint:       req.Endpoint,
			IdempotencyKey: req.Key,
		})
		return Outcome{}, false, err
	}
	return out, false, nil
}

// claim reserves the key, taking over a reservation abandoned by a crashed
// process. It reports whether this caller owns the key.
func (e *Executor) claim(ctx context.Context, req Request, digest []byte) (bool, error) {
	params := dbgen.ClaimIdempotencyKeyParams{
		OrganizationID: req.OrganizationID,
		Endpoint:       req.Endpoint,
		IdempotencyKey: req.Key,
		RequestHash:    digest,
		UserID:         req.UserID,
		ExpiresAt:      time.Now().Add(e.ttl),
	}
	if req.RequestID != "" {
		params.RequestID = &req.RequestID
	}
	_, err := e.q.ClaimIdempotencyKey(ctx, params)
	if err == nil {
		return true, nil
	}
	if !database.IsNoRows(err) {
		return false, apierr.Internal(fmt.Errorf("claim idempotency key: %w", err))
	}

	// A row already exists. If it is a stale IN_PROGRESS reservation, take it.
	_, takeErr := e.q.TakeOverStaleIdempotencyKey(ctx, dbgen.TakeOverStaleIdempotencyKeyParams{
		OrganizationID:    req.OrganizationID,
		Endpoint:          req.Endpoint,
		IdempotencyKey:    req.Key,
		StaleAfterSeconds: int32(e.staleAfter.Seconds()),
		UserID:            req.UserID,
		RequestID:         optionalString(req.RequestID),
	})
	if takeErr == nil {
		return true, nil
	}
	if !database.IsNoRows(takeErr) {
		return false, apierr.Internal(fmt.Errorf("reclaim idempotency key: %w", takeErr))
	}
	return false, nil
}

// replay returns the stored response for a key someone else owns or completed.
func (e *Executor) replay(ctx context.Context, req Request, digest []byte) (Outcome, bool, error) {
	rec, err := e.q.GetIdempotencyKey(ctx, dbgen.GetIdempotencyKeyParams{
		OrganizationID: req.OrganizationID,
		Endpoint:       req.Endpoint,
		IdempotencyKey: req.Key,
	})
	if err != nil {
		if database.IsNoRows(err) {
			// The row vanished between the claim attempt and this read, which
			// means a concurrent call failed and released it. Asking the client
			// to retry is correct and cheap.
			return Outcome{}, false, apierr.Conflict(apierr.CodeIdempotencyInProgress,
				"A concurrent request with this Idempotency-Key is still being processed. Please retry.")
		}
		return Outcome{}, false, apierr.Internal(fmt.Errorf("read idempotency key: %w", err))
	}

	// Same key, different payload: a client bug that must never be answered
	// with the first request's response.
	if !equalDigest(rec.RequestHash, digest) {
		return Outcome{}, false, apierr.Conflict(apierr.CodeIdempotencyKeyReuse,
			"This Idempotency-Key was already used with a different request payload.")
	}

	switch rec.Status {
	case "COMPLETED":
		out := Outcome{Status: http.StatusOK}
		if rec.ResponseStatus != nil {
			out.Status = int(*rec.ResponseStatus)
		}
		if len(rec.ResponseBody) > 0 {
			out.Body = json.RawMessage(rec.ResponseBody)
		}
		if rec.ResourceType != nil {
			out.ResourceType = *rec.ResourceType
		}
		if rec.ResourcePublicID != nil {
			out.ResourcePublicID = *rec.ResourcePublicID
		}
		return out, true, nil
	default:
		return Outcome{}, false, apierr.Conflict(apierr.CodeIdempotencyInProgress,
			"A request with this Idempotency-Key is still being processed. Please retry shortly.")
	}
}

// runOnce executes op in a transaction, completing the idempotency record in
// the same transaction when a key is in play.
func (e *Executor) runOnce(ctx context.Context, req *Request, op Operation) (Outcome, error) {
	var out Outcome
	err := e.db.InTx(ctx, func(tx pgx.Tx) error {
		var opErr error
		out, opErr = op(ctx, tx)
		if opErr != nil {
			return opErr
		}
		if req == nil || req.Key == "" {
			return nil
		}
		body, mErr := json.Marshal(out.Body)
		if mErr != nil {
			return apierr.Internal(fmt.Errorf("marshal idempotent response: %w", mErr))
		}
		status := int32(out.Status)
		params := dbgen.CompleteIdempotencyKeyParams{
			OrganizationID: req.OrganizationID,
			Endpoint:       req.Endpoint,
			IdempotencyKey: req.Key,
			ResponseStatus: &status,
			ResponseBody:   body,
		}
		if out.ResourceType != "" {
			params.ResourceType = &out.ResourceType
		}
		if out.ResourcePublicID != "" {
			params.ResourcePublicID = &out.ResourcePublicID
		}
		return e.q.WithTx(tx).CompleteIdempotencyKey(ctx, params)
	})
	if err != nil {
		var ae *apierr.Error
		if errors.As(err, &ae) {
			return Outcome{}, ae
		}
		return Outcome{}, err
	}
	return out, nil
}

// PurgeExpired removes completed keys past their retention window.
func (e *Executor) PurgeExpired(ctx context.Context) (int64, error) {
	return e.q.PurgeExpiredIdempotencyKeys(ctx)
}

func hashRequest(endpoint string, body []byte) []byte {
	h := sha256.New()
	h.Write([]byte(endpoint))
	h.Write([]byte{0})
	h.Write(body)
	return h.Sum(nil)
}

func equalDigest(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
