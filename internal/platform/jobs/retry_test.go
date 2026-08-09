package jobs

import (
	"errors"
	"testing"
	"time"
)

// The queue's generic backoff is right for most work and wrong for work that
// knows its own schedule. These pin down the override, because the failure it
// prevents is silent: a row saying "next attempt in 30 minutes" while the queue
// retries in 40 seconds.

func TestRetryAfterCarriesTheHandlersInterval(t *testing.T) {
	cause := errors.New("endpoint returned 502")
	err := RetryAfter(9*time.Minute, cause)

	got, ok := retryIntervalOf(err)
	if !ok {
		t.Fatal("the interval was not carried")
	}
	if got != 9*time.Minute {
		t.Fatalf("interval = %v, want 9m", got)
	}
	// The cause must survive, or a log line loses the reason.
	if !errors.Is(err, cause) {
		t.Fatal("the underlying error was swallowed")
	}
	if err.Error() != cause.Error() {
		t.Fatalf("message = %q, want %q", err.Error(), cause.Error())
	}
}

func TestPlainErrorFallsBackToTheQueueBackoff(t *testing.T) {
	if _, ok := retryIntervalOf(errors.New("boom")); ok {
		t.Fatal("a plain error claimed an interval")
	}
	// A non-positive interval is a caller bug, not an instruction to retry
	// immediately: fall back rather than hammering a failing endpoint.
	if _, ok := retryIntervalOf(RetryAfter(0, errors.New("boom"))); ok {
		t.Fatal("a zero interval was honoured")
	}
	if _, ok := retryIntervalOf(RetryAfter(-time.Second, errors.New("boom"))); ok {
		t.Fatal("a negative interval was honoured")
	}
}

func TestRetryAfterOfNilIsNil(t *testing.T) {
	if err := RetryAfter(time.Minute, nil); err != nil {
		t.Fatalf("wrapping no error produced %v", err)
	}
}

func TestPermanentStillWinsThroughTheWrapper(t *testing.T) {
	// A permanent failure must dead-letter even when a handler also asked for a
	// retry interval, or a bad address would be retried on a nine-minute cycle
	// until the budget ran out.
	err := RetryAfter(9*time.Minute, ErrPermanent)
	if !errors.Is(err, ErrPermanent) {
		t.Fatal("ErrPermanent did not survive the wrapper")
	}
}

func TestQueueBackoffIsBounded(t *testing.T) {
	if got := retryBackoff(1); got != 10*time.Second {
		t.Fatalf("first retry after %v, want 10s", got)
	}
	if got := retryBackoff(50); got != 300*time.Second {
		t.Fatalf("retryBackoff(50) = %v, want the 5m cap", got)
	}
	if retryBackoff(2) <= retryBackoff(1) {
		t.Fatal("backoff does not grow")
	}
}
