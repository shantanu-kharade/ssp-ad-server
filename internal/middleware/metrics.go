package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"

	"github.com/yourusername/ssp-adserver/internal/metrics"
)

// Metrics returns a Fiber middleware that logs structured metrics
// for every HTTP request after it completes. It also records atomic
// counters for the /metrics observability endpoint.
func Metrics(log *zap.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		latencyMs := time.Since(start).Milliseconds()
		statusCode := c.Response().StatusCode()

		// Record in-memory atomic counters for the /metrics endpoint.
		metrics.GlobalCounters.Record(statusCode, latencyMs)

		requestID := c.Get("X-Request-ID")
		if requestID == "" {
			requestID = c.GetRespHeader("X-Request-Id")
		}

		log.Info("request metrics",
			zap.String("endpoint", c.Path()),
			zap.String("method", c.Method()),
			zap.Int("status_code", statusCode),
			zap.Int64("latency_ms", latencyMs),
			zap.String("request_id", requestID),
		)

		return err
	}
}

