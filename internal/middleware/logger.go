// Package middleware provides HTTP middleware components for the SSP ad server,
// including structured logging and panic recovery.
package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// Logger returns a Fiber middleware that logs structured information about
// every HTTP request, including the request ID, method, path, status code,
// user ID (if present in the request body context), and latency.
func Logger(log *zap.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		// Generate or retrieve a request ID for correlation.
		requestID := c.Get("X-Request-ID")
		if requestID == "" {
			requestID = c.GetRespHeader("X-Request-Id")
		}

		// Store logger with request ID in Locals for downstream handlers.
		reqLogger := log.With(zap.String("request_id", requestID))
		c.Locals("logger", reqLogger)

		// Process the request.
		err := c.Next()

		latency := time.Since(start)
		statusCode := c.Response().StatusCode()

		// Extract user ID from Locals if the bid handler set it.
		userID, _ := c.Locals("user_id").(string)

		fields := []zap.Field{
			zap.String("method", c.Method()),
			zap.String("path", c.Path()),
			zap.Int("status", statusCode),
			zap.Duration("latency", latency),
			zap.String("ip", c.IP()),
			zap.String("user_agent", c.Get("User-Agent")),
		}

		if userID != "" {
			fields = append(fields, zap.String("user_id", userID))
		}

		if requestID != "" {
			fields = append(fields, zap.String("request_id", requestID))
		}

		// Log at appropriate level based on status code.
		switch {
		case statusCode >= 500:
			reqLogger.Error("request completed", fields...)
		case statusCode >= 400:
			reqLogger.Warn("request completed", fields...)
		default:
			reqLogger.Info("request completed", fields...)
		}

		return err
	}
}
