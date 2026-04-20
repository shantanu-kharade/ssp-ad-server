package identity

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/gofiber/fiber/v2"
	"github.com/yourusername/ssp-adserver/internal/models"
)

// Resolver determines a stable user identifier from the request context.
// It uses a priority chain: X-User-ID header > uid cookie > BidRequest.User.ID >
// IP-based anonymous hash.
type Resolver struct{}

// NewResolver creates a new Resolver instance.
func NewResolver() *Resolver {
	return &Resolver{}
}

// Resolve returns a stable user identifier from the request. It checks
// the X-User-ID header, uid cookie, BidRequest.User.ID, and falls back
// to an IP-based anonymous hash.
func (r *Resolver) Resolve(c *fiber.Ctx, req models.BidRequest) string {
	// 1. X-User-ID header
	if headerID := c.Get("X-User-ID"); headerID != "" {
		return headerID
	}

	// 2. uid cookie
	if cookieID := c.Cookies("uid"); cookieID != "" {
		return cookieID
	}

	// 3. BidRequest.User.ID
	if req.User != nil && req.User.ID != "" {
		return req.User.ID
	}

	// Fallback: anonymous ID based on IP hash
	ip := c.IP()
	hash := sha256.Sum256([]byte(ip))
	hashHex := hex.EncodeToString(hash[:])
	return "anon-" + hashHex[:16] // Use first 16 chars for brevity
}
