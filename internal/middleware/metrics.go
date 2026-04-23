package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// Metrics returns a Fiber middleware that logs structured metrics
// for every HTTP request after it completes.
func Metrics(log *zap.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		latencyMs := time.Since(start).Milliseconds()
		statusCode := c.Response().StatusCode()

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

