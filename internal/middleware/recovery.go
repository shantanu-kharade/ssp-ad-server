package middleware

import (
	"fmt"
	"runtime/debug"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"

	apperrors "github.com/yourusername/ssp-adserver/pkg/errors"
)

// Recovery returns a Fiber middleware that catches panics in downstream handlers,
// logs the panic value and stack trace, and returns a structured 500 JSON response
// instead of crashing the server process.
func Recovery(log *zap.Logger) fiber.Handler {
	return func(c *fiber.Ctx) (err error) {
		// Deferred function to recover from panics.
		defer func() {
			if r := recover(); r != nil {
				// Capture the full stack trace for debugging.
				stack := debug.Stack()

				log.Error("panic recovered",
					zap.Any("panic", r),
					zap.String("method", c.Method()),
					zap.String("path", c.Path()),
					zap.String("stack", string(stack)),
				)

				// Build a structured error response.
				apiErr := apperrors.NewInternalError(
					"an unexpected error occurred",
					fmt.Errorf("panic: %v", r),
				)

				err = c.Status(apiErr.StatusCode).JSON(apiErr)
			}
		}()

		return c.Next()
	}
}
