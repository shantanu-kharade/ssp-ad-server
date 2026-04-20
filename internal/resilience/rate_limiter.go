package resilience

import (
	"context"

	"golang.org/x/time/rate"
)

// GlobalRateLimiter provides rate limiting for the entire application.
type GlobalRateLimiter struct {
	limiter *rate.Limiter
}

// NewGlobalRateLimiter creates a new global rate limiter with the specified requests per second.
func NewGlobalRateLimiter(rps int) *GlobalRateLimiter {
	// Allow burst size equal to rps to handle sudden spikes.
	return &GlobalRateLimiter{
		limiter: rate.NewLimiter(rate.Limit(rps), rps),
	}
}

// Allow checks if a request is allowed to proceed.
// It returns false immediately if the rate limit is exceeded.
func (l *GlobalRateLimiter) Allow(ctx context.Context) bool {
	return l.limiter.Allow()
}
