package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"

	"github.com/yourusername/ssp-adserver/internal/events"
)

// TrackHandler handles event tracking endpoints.
type TrackHandler struct {
	producer *events.EventProducer
	log      *zap.Logger
}

// NewTrackHandler creates a new TrackHandler.
func NewTrackHandler(producer *events.EventProducer, log *zap.Logger) *TrackHandler {
	return &TrackHandler{
		producer: producer,
		log:      log,
	}
}

// HandleClick processes a GET /track/click request.
func (h *TrackHandler) HandleClick(c *fiber.Ctx) error {
	bidID := c.Query("bid_id")
	impID := c.Query("imp_id")
	campaignID := c.Query("campaign_id")
	creativeID := c.Query("creative_id")
	userID := c.Query("user_id")

	// Create the event
	event := events.ClickEvent{
		RequestID:    bidID, // Using bid_id as the correlation request ID
		ImpressionID: impID,
		CampaignID:   campaignID,
		CreativeID:   creativeID,
		UserID:       userID,
		Timestamp:    time.Now().UTC(),
	}

	// Publish the event
	if err := h.producer.PublishClickEvent(c.Context(), event); err != nil {
		h.log.Error("failed to publish click event", zap.Error(err), zap.String("imp_id", impID))
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to track click",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"tracked": true,
	})
}
