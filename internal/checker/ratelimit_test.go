package checker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// stubChecker is a deterministic in-memory BalanceChecker used to exercise the
// rate-limit wrapper.
type stubChecker struct {
	balance int64
	err     error
	calls   atomic.Int64
}

func (s *stubChecker) CheckAddresses(_ context.Context, _ []string) (int64, error) {
	s.calls.Add(1)
	if s.err != nil {
		return 0, s.err
	}
	return s.balance, nil
}

func TestWithRateLimitDelaysSecondCall(t *testing.T) {
	t.Parallel()

	stub := &stubChecker{balance: 42}
	wrapped := WithRateLimit(stub, 5) // 5 rps -> ~200ms gap between calls

	ctx := context.Background()

	// First call: should be immediate (token bucket has burst=1).
	if _, err := wrapped.CheckAddresses(ctx, []string{"a"}); err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}

	start := time.Now()
	if _, err := wrapped.CheckAddresses(ctx, []string{"a"}); err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 150*time.Millisecond {
		t.Errorf("second call returned after %v, expected at least 150ms", elapsed)
	}
	if stub.calls.Load() != 2 {
		t.Errorf("expected 2 underlying calls, got %d", stub.calls.Load())
	}
}

func TestWithRateLimitPropagatesError(t *testing.T) {
	t.Parallel()

	want := errors.New("boom")
	stub := &stubChecker{err: want}
	wrapped := WithRateLimit(stub, 100)

	_, err := wrapped.CheckAddresses(context.Background(), []string{"a"})
	if !errors.Is(err, want) {
		t.Errorf("expected error %v, got %v", want, err)
	}
}

func TestWithRateLimitContextCancellation(t *testing.T) {
	t.Parallel()

	stub := &stubChecker{balance: 42}
	wrapped := WithRateLimit(stub, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := wrapped.CheckAddresses(ctx, []string{"a"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if stub.calls.Load() != 0 {
		t.Errorf("expected 0 underlying calls, got %d", stub.calls.Load())
	}
}
