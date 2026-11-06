package ratelimit

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrWaitTooLong is returned by Acquire when a token would not be available
// within the limiter's maxWait budget. It is a client-side throttle signal, not
// an upstream rejection; the transport maps it to a clear, non-retryable error.
var ErrWaitTooLong = errors.New("rate limiter: token wait exceeds the configured budget")

// Limiter is a token bucket. capacity is the burst allowance (tokens held when
// idle); refillRate is the steady-state tokens per second. maxWait caps how long
// Acquire will block for a token before giving up (0 = wait until ctx ends).
type Limiter struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens / second
	maxWait    time.Duration
	last       time.Time
}

// New builds a bucket that holds capacity tokens and refills refillPerSecond per
// second, refusing to wait longer than maxWait for a token. capacity and
// refillPerSecond are independent: the first bounds a burst, the second the
// steady rate. A maxWait of 0 waits indefinitely (until the caller's ctx ends).
func New(capacity int, refillPerSecond float64, maxWait time.Duration) *Limiter {
	// Guard against a bucket that can never open: a capacity below 1 never holds
	// the single token an Acquire needs (it would block forever), and a
	// non-positive refill rate divides by zero when computing the wait. Floor both
	// to a safe positive default. A fractional rate below 1 is deliberately kept —
	// a slow limiter (e.g. one token every few seconds) is a legitimate policy —
	// so only a non-positive rate, which is never meaningful, is corrected.
	if capacity < 1 {
		capacity = 1
	}
	if refillPerSecond <= 0 {
		refillPerSecond = 1
	}
	return &Limiter{
		tokens:     float64(capacity),
		maxTokens:  float64(capacity),
		refillRate: refillPerSecond,
		maxWait:    maxWait,
		last:       time.Now(),
	}
}

func (l *Limiter) refill() {
	now := time.Now()
	elapsed := now.Sub(l.last).Seconds()
	l.tokens = min(l.maxTokens, l.tokens+elapsed*l.refillRate)
	l.last = now
}

// Acquire takes one token; if the bucket is empty it blocks until a refill, until
// ctx is cancelled (returns ctx.Err()), or until the wait would exceed maxWait
// (returns ErrWaitTooLong). Safe for concurrent use: after waking from a wait it
// re-checks the bucket under the lock before consuming, so several goroutines
// released by one refill cannot all consume and drive the count negative.
func (l *Limiter) Acquire(ctx context.Context) error {
	var deadline time.Time
	if l.maxWait > 0 {
		deadline = time.Now().Add(l.maxWait)
	}
	for {
		l.mu.Lock()
		l.refill()
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		waitSec := (1 - l.tokens) / l.refillRate
		l.mu.Unlock()

		wait := time.Duration(waitSec * float64(time.Second))
		// Refuse before sleeping if this wait would blow the budget: a caller
		// stuck behind a drained bucket gets a clear error instead of an
		// unbounded hang.
		if !deadline.IsZero() && time.Now().Add(wait).After(deadline) {
			return ErrWaitTooLong
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
