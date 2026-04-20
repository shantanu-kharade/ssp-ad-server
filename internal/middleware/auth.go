package middleware

import (
	"github.com/gofiber/fiber/v2"
	apperrors "github.com/yourusername/ssp-adserver/pkg/errors"
)

// APIKeyAuth returns a Fiber middleware that validates the X-API-Key header
// against the expected key. Returns 401 if the key is missing or incorrect.
func APIKeyAuth(expectedKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key := c.Get("X-API-Key")
		
		if key == "" || key != expectedKey {
			apiErr := apperrors.NewUnauthorizedError("invalid or missing API key")
			return c.Status(apiErr.StatusCode).JSON(apiErr)
		}
		
		return c.Next()
	}
}
