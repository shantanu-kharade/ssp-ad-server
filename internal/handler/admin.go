package handler

import (
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"

	"github.com/yourusername/ssp-adserver/internal/cache"
	"github.com/yourusername/ssp-adserver/internal/dsp"
	apperrors "github.com/yourusername/ssp-adserver/pkg/errors"
)

// AdminHandler handles admin-only endpoints for managing segments and
// inspecting circuit breaker state.
type AdminHandler struct {
	segments *cache.SegmentFetcher
	fanout   *dsp.FanoutCoordinator
	log      *zap.Logger
}

// NewAdminHandler creates a new AdminHandler with the given dependencies.
func NewAdminHandler(segments *cache.SegmentFetcher, fanout *dsp.FanoutCoordinator, log *zap.Logger) *AdminHandler {
	return &AdminHandler{
		segments: segments,
		fanout:   fanout,
		log:      log,
	}
}

// SetSegmentsRequest is the JSON body for the POST /admin/segments endpoint.
type SetSegmentsRequest struct {
	UserID   string   `json:"user_id"`
	Segments []string `json:"segments"`
}

// HandleSetSegments processes a POST /admin/segments request to update
// a user's audience segment data in Redis.
func (h *AdminHandler) HandleSetSegments(c *fiber.Ctx) error {
	var req SetSegmentsRequest

	if err := c.BodyParser(&req); err != nil {
		apiErr := apperrors.NewBadRequestError("invalid request body", err)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	if req.UserID == "" {
		apiErr := apperrors.NewBadRequestError("user_id is required", nil)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	if req.Segments == nil {
		req.Segments = []string{}
	}

	// Write to Redis with 300s TTL
	if err := h.segments.SetUserSegments(c.Context(), req.UserID, req.Segments); err != nil {
		h.log.Error("failed to set segments in redis", zap.Error(err), zap.String("user_id", req.UserID))
		apiErr := apperrors.NewInternalError("failed to update segments", err)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	h.log.Info("segments updated successfully", zap.String("user_id", req.UserID), zap.Int("segment_count", len(req.Segments)))

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status":  "success",
		"message": "segments updated",
	})
}

// HandleCircuitBreakers processes a GET /admin/circuit-breakers request
// and returns the current state of each DSP's circuit breaker.
func (h *AdminHandler) HandleCircuitBreakers(c *fiber.Ctx) error {
	states := make(map[string]interface{})
	
	for _, client := range h.fanout.Clients() {
		state := client.CircuitBreaker().State()
		states[client.Name()] = fiber.Map{
			"state":    state,
		}
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status": "success",
		"data":   states,
	})
}
