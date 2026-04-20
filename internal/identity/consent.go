// Package identity provides user identity resolution and consent validation
// for the SSP ad server bid processing pipeline.
package identity

import (
	"github.com/gofiber/fiber/v2"
)

// Validator checks whether a user has given consent for ad personalization.
type Validator struct{}

// NewValidator creates a new Validator instance.
func NewValidator() *Validator {
	return &Validator{}
}

// Validate checks the X-Consent header and returns true if consent is present.
func (v *Validator) Validate(c *fiber.Ctx) bool {
	// Stub: check if X-Consent header is non-empty
	consent := c.Get("X-Consent")
	return consent != ""
}
