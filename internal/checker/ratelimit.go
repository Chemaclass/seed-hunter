package checker

import (
	"context"

	"golang.org/x/time/rate"
)

// rateLimitedChecker wraps a BalanceChecker with a token-bucket limiter so
// that no more than rps requests-per-second are issued upstream. The burst
// is fixed to 1 — we want a strict steady state, not a bursty fan-out, so
// the second consecutive call always waits ~1/rps before firing.
type rateLimitedChecker struct {
	inner   BalanceChecker
	limiter *rate.Limiter
}

// WithRateLimit wraps c so that no more than rps requests-per-second are
// issued. rps must be > 0; passing 0 or a negative value is treated as 1
// rps (a defensive default — callers should validate up-stream).
func WithRateLimit(c BalanceChecker, rps float64) BalanceChecker {
	if rps <= 0 {
		rps = 1
	}
	return &rateLimitedChecker{
		inner:   c,
		limiter: rate.NewLimiter(rate.Limit(rps), 1),
	}
}

// CheckAddresses blocks until the limiter releases a token (or ctx is
// cancelled), then delegates to the wrapped checker.
func (r *rateLimitedChecker) CheckAddresses(ctx context.Context, addresses []string) (int64, error) {
	if err := r.limiter.Wait(ctx); err != nil {
		return 0, err
	}
	return r.inner.CheckAddresses(ctx, addresses)
}
