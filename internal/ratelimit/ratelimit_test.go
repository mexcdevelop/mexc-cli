package ratelimit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestAcquire_InitialBurstDoesNotBlock(t *testing.T) {
	l := New(5, 5, 0) // capacity 5
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 5; i++ {
		if err := l.Acquire(ctx); err != nil {
			t.Fatalf("Acquire %d: %v", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("initial burst should not block, took %v", elapsed)
	}
}

// Capacity and refill rate are independent: a bucket can hold a big burst yet
// refill slowly. Draining the burst is instant; the next token waits on refill.
func TestAcquire_BurstExceedsRate(t *testing.T) {
	l := New(8, 2, 0) // burst 8, but only 2/s refill
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 8; i++ {
		if err := l.Acquire(ctx); err != nil {
			t.Fatalf("burst drain %d: %v", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("the 8-token burst should not block, took %v", elapsed)
	}
	start = time.Now()
	if err := l.Acquire(ctx); err != nil {
		t.Fatalf("9th acquire: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("at 2/s the 9th token should wait ~500ms, only waited %v", elapsed)
	}
}

func TestAcquire_BlocksWhenEmpty(t *testing.T) {
	l := New(10, 10, 0) // 10/s -> refilling one token takes about 100ms
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if err := l.Acquire(ctx); err != nil {
			t.Fatalf("drain %d: %v", i, err)
		}
	}
	start := time.Now()
	if err := l.Acquire(ctx); err != nil {
		t.Fatalf("11th acquire: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("expected to block ~100ms refilling, only waited %v", elapsed)
	}
}

// A drained bucket whose next token would take longer than maxWait must return
// ErrWaitTooLong instead of blocking — the guard against an unbounded hang.
func TestAcquire_MaxWaitCapsTheHang(t *testing.T) {
	l := New(1, 1, 200*time.Millisecond) // 1/s refill, so a 2nd token needs ~1s
	ctx := context.Background()
	if err := l.Acquire(ctx); err != nil {
		t.Fatalf("first token: %v", err)
	}
	start := time.Now()
	err := l.Acquire(ctx)
	if !errors.Is(err, ErrWaitTooLong) {
		t.Fatalf("expected ErrWaitTooLong, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("should refuse quickly rather than wait out the budget, took %v", elapsed)
	}
}

// maxWait 0 means wait indefinitely (until ctx), preserving the old behavior for
// a product that wants no cap.
func TestAcquire_ZeroMaxWaitWaitsForRefill(t *testing.T) {
	l := New(1, 20, 0) // no cap; refill is fast so the wait is short
	ctx := context.Background()
	if err := l.Acquire(ctx); err != nil {
		t.Fatalf("first token: %v", err)
	}
	if err := l.Acquire(ctx); err != nil {
		t.Fatalf("second token should eventually succeed with no cap, got %v", err)
	}
}

func TestAcquire_ConcurrentDoesNotOversubscribe(t *testing.T) {
	l := New(10, 10, 0)
	var passed int
	var mu sync.Mutex
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.Acquire(ctx); err == nil {
				mu.Lock()
				passed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if passed > 15 {
		t.Errorf("released %d in ~100ms; a 10/s limiter should release ~11 — oversubscription", passed)
	}
	if l.tokens < 0 {
		t.Errorf("tokens went negative (%.3f): concurrent waiters over-consumed", l.tokens)
	}
}

func TestAcquire_RespectsContextCancel(t *testing.T) {
	l := New(1, 1, 0)
	ctx := context.Background()
	if err := l.Acquire(ctx); err != nil { // take the only token
		t.Fatalf("first acquire: %v", err)
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	if err := l.Acquire(cancelCtx); err == nil {
		t.Fatal("expected Acquire to return ctx error when cancelled while waiting")
	}
}

// A misconfigured bucket must not wedge: a capacity below 1 would hold no token,
// a non-positive rate would divide by zero. Both are floored to a safe default.
func TestNew_FloorsUnusableConfig(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		cap  int
		rate float64
	}{
		{"zero capacity", 0, 10},
		{"negative capacity", -5, 10},
		{"zero rate", 5, 0},
		{"negative rate", 5, -1},
	} {
		l := New(tc.cap, tc.rate, 0)
		if err := l.Acquire(ctx); err != nil {
			t.Errorf("%s: a floored bucket must still yield a token, got %v", tc.name, err)
		}
	}
}

// A deliberate sub-1 rate (a slow limiter) is preserved, not floored to 1.
func TestNew_KeepsFractionalRate(t *testing.T) {
	l := New(1, 0.5, 0) // one token every 2s
	ctx := context.Background()
	if err := l.Acquire(ctx); err != nil {
		t.Fatalf("first token: %v", err)
	}
	start := time.Now()
	if err := l.Acquire(ctx); err != nil {
		t.Fatalf("second token: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 1*time.Second {
		t.Fatalf("at 0.5/s the second token should wait ~2s; a floor-to-1 bug would wait ~1s, waited %v", elapsed)
	}
}
