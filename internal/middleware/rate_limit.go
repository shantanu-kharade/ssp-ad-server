package middleware

import (
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"

	"github.com/yourusername/ssp-adserver/internal/resilience"
)

// RateLimit returns a Fiber middleware that restricts the number of incoming requests
// using the provided global rate limiter.
func RateLimit(limiter *resilience.GlobalRateLimiter, log *zap.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !limiter.Allow(c.Context()) {
			requestID := c.Get("X-Request-ID")
			if requestID == "" {
				requestID = c.GetRespHeader("X-Request-Id")
			}

			log.Warn("rate limit exceeded",
				zap.String("request_id", requestID),
				zap.String("ip", c.IP()),
				zap.String("path", c.Path()),
			)

			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error":          "rate limit exceeded",
				"retry_after_ms": 100,
			})
		}
		return c.Next()
	}
}
