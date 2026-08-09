package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

// Interrupting a request in flight.
//
// # What this simulates, and what it does not
//
// A SIGKILL during a write severs the connection with the transaction still
// open. PostgreSQL sees the backend disappear and rolls back; the client sees a
// broken connection and never learns whether the work committed. That ambiguity
// is the hard part, and it is what a retry has to survive.
//
// InterruptedPost reproduces exactly that from the client's side: the request is
// abandoned mid-flight, the connection closes, and the server's handler context
// is cancelled with its transaction open. What it does *not* reproduce is the
// process dying — the server keeps running. That half is covered by
// deploy/scripts/restart-drill.sh, which really does send SIGKILL.
//
// Both halves matter and they answer different questions. This one asks "is the
// operation atomic and exactly-once under a retry"; the drill asks "does the
// process come back and can a client still make progress". Neither substitutes
// for the other.

// InterruptedPost fires a request and abandons it after the given delay.
//
// It returns true if the request was genuinely cut off, and false if the server
// answered first. A false is not a test failure — it means the operation was
// faster than the delay — but a caller that needs the interruption should
// shorten the delay and try again rather than assert on a race it did not win.
func (e *Env) InterruptedPost(
	t *testing.T, path, token string, body any, after time.Duration, headers ...[2]string,
) bool {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), after)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.URL(path), bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}

	resp, err := e.Server.Client().Do(req)
	if err != nil {
		// Deadline exceeded is the intended outcome: the connection was torn
		// down with the server still working.
		if errors.Is(err, context.DeadlineExceeded) {
			return true
		}
		// Any other transport error is also an interruption from the server's
		// point of view, so it counts.
		return true
	}
	defer func() { _ = resp.Body.Close() }()
	return false
}
