package resilience

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/yourusername/ssp-adserver/internal/cache"
)

// GlobalRateLimiter enforces a per-minute sliding window rate limit backed by Redis.
//
// Why Redis instead of in-memory?
// An in-memory limiter (e.g. golang.org/x/time/rate) is local to a single process.
// When the server runs as multiple pods behind a load balancer, each pod maintains
// its own independent counter. A client that hits 3 pods can effectively get 3×
// the configured limit — the rate limiter becomes useless for distributed deployments.
//
// This implementation uses a simple Redis INCR + EXPIRE sliding window:
//   - Key: "ratelimit:<clientKey>:<unix-minute>"  (a new bucket every 60 seconds)
//   - On each request: INCR the key (atomic); if it's the first hit, set EXPIRE 60s.
//   - If the counter exceeds rps*60 (requests per minute), deny the request.
//
// Fail-open policy: if Redis is unavailable, the request is allowed and a warning
// is logged. A Redis outage must never take down the bidding path.
type GlobalRateLimiter struct {
	redis      *cache.RedisClient
	log        *zap.Logger
	// rpm is the maximum number of requests allowed per 60-second window.
	rpm        int64
}

// NewGlobalRateLimiter creates an in-memory-only stub kept for backward compatibility
// with unit tests and local development without Redis. Prefer NewRedisRateLimiter for
// production deployments.
func NewGlobalRateLimiter(rps int) *GlobalRateLimiter {
	return &GlobalRateLimiter{
		redis: nil, // nil signals fail-open (no Redis, allow everything)
		rpm:   int64(rps) * 60,
	}
}

// NewRedisRateLimiter creates a distributed rate limiter backed by the provided
// Redis client. rps is the sustained requests-per-second limit; internally this
// is converted to a per-minute window (rps × 60) to keep the Redis key granularity
// coarse enough to avoid thundering-herd key expiry issues.
func NewRedisRateLimiter(rps int, redis *cache.RedisClient, log *zap.Logger) *GlobalRateLimiter {
	return &GlobalRateLimiter{
		redis: redis,
		log:   log,
		rpm:   int64(rps) * 60,
	}
}

// Allow checks whether the request identified by the context's remote address
// is within the configured rate limit.
//
// The client key is derived from the context value "client_ip" if present,
// falling back to a global bucket. The time bucket is the current Unix minute,
// so each key expires naturally after 60 seconds.
//
// Concurrency: this method is safe to call from multiple goroutines simultaneously.
// Redis INCR is atomic at the server side.
func (l *GlobalRateLimiter) Allow(ctx context.Context) bool {
	// Fail-open: if no Redis is configured, always allow.
	if l.redis == nil {
		return true
	}

	// Derive a stable client key. The middleware stores the IP in context via
	// Fiber's c.Locals; fall back to a shared global bucket if not set.
	clientKey := "global"
	if ip, ok := ctx.Value("client_ip").(string); ok && ip != "" {
		clientKey = ip
	}

	// Current 60-second time bucket (Unix timestamp / 60 → changes once per minute).
	bucket := time.Now().Unix() / 60
	redisKey := fmt.Sprintf("ratelimit:%s:%d", clientKey, bucket)

	// Use a tight timeout so a slow Redis never stalls the bid path.
	opCtx, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
	defer cancel()

	count, err := l.redis.Incr(opCtx, redisKey)
	if err != nil {
		// Fail open: Redis unavailable. Log once per call; metrics/alerting
		// should catch sustained Redis degradation.
		l.log.Warn("redis rate limiter unavailable — failing open",
			zap.String("key", redisKey),
			zap.Error(err),
		)
		return true
	}

	// On the very first increment, set the expiry. The key will auto-delete
	// after 120 seconds (2× the window) to handle clock skew between pods.
	if count == 1 {
		// Best-effort; ignore error — the key will eventually expire anyway.
		_ = l.redis.Expire(opCtx, redisKey, 120*time.Second)
	}

	return count <= l.rpm
}
