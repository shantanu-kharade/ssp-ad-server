// Package handler implements the HTTP handlers for the SSP ad server,
// including bid request processing and health checks.
package handler

import (
	"context"
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"

	"github.com/yourusername/ssp-adserver/internal/ads"
	"github.com/yourusername/ssp-adserver/internal/auction"
	"github.com/yourusername/ssp-adserver/internal/cache"
	"github.com/yourusername/ssp-adserver/internal/campaign"
	"github.com/yourusername/ssp-adserver/internal/dsp"
	"github.com/yourusername/ssp-adserver/internal/events"
	"github.com/yourusername/ssp-adserver/internal/identity"
	"github.com/yourusername/ssp-adserver/internal/models"
	apperrors "github.com/yourusername/ssp-adserver/pkg/errors"
)

// BidHandler handles OpenRTB 2.5 bid request processing.
// It holds a validator instance for request validation and
// a logger for structured logging.
type BidHandler struct {
	validate    *validator.Validate
	log         *zap.Logger
	segments    *cache.SegmentFetcher
	resolver    *identity.Resolver
	consent     *identity.Validator
	fanout      *dsp.FanoutCoordinator
	houseAds    *ads.HouseAdProvider
	campaignSvc *campaign.Service
	producer    *events.EventProducer
}

// NewBidHandler creates a new BidHandler with an initialised validator
// and the provided logger.
func NewBidHandler(log *zap.Logger, segments *cache.SegmentFetcher, resolver *identity.Resolver, consent *identity.Validator, fanout *dsp.FanoutCoordinator, houseAds *ads.HouseAdProvider, campaignSvc *campaign.Service, producer *events.EventProducer) *BidHandler {
	return &BidHandler{
		validate:    validator.New(),
		log:         log,
		segments:    segments,
		resolver:    resolver,
		consent:     consent,
		fanout:      fanout,
		houseAds:    houseAds,
		campaignSvc: campaignSvc,
		producer:    producer,
	}
}

// HandleBid processes a POST /bid request. It parses and validates the
// incoming OpenRTB 2.5 BidRequest JSON, then returns a hardcoded mock
// BidResponse for demonstration purposes.
//
// Error handling:
//   - Returns 400 with structured validation errors for invalid payloads.
//   - Returns 400 for malformed JSON that cannot be parsed.
func (h *BidHandler) HandleBid(c *fiber.Ctx) error {
	var req models.BidRequest

	// Parse the JSON request body.
	if err := c.BodyParser(&req); err != nil {
		apiErr := apperrors.NewBadRequestError(
			"failed to parse bid request body",
			fmt.Errorf("body parser error: %w", err),
		)
		h.log.Warn("bid request parse failure",
			zap.Error(apiErr),
			zap.String("raw_body", string(c.Body())),
		)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	// Resolve user ID
	userID := h.resolver.Resolve(c, req)
	// Store the user ID in Locals so the logger middleware can pick it up.
	c.Locals("user_id", userID)

	// Validate consent
	if !h.consent.Validate(c) {
		h.log.Info("bid request rejected: no consent", 
			zap.String("request_id", req.ID), 
			zap.String("user_id", userID),
		)
		// Return empty bid response with NBR=0
		return c.Status(fiber.StatusOK).JSON(models.BidResponse{
			ID:  req.ID,
			NBR: 0,
		})
	}

	// Validate the parsed request against OpenRTB struct tags.
	if err := h.validate.Struct(req); err != nil {
		validationErrors, ok := err.(validator.ValidationErrors)
		if !ok {
			apiErr := apperrors.NewInternalError(
				"unexpected validation error type",
				fmt.Errorf("validator returned non-ValidationErrors type: %w", err),
			)
			return c.Status(apiErr.StatusCode).JSON(apiErr)
		}

		details := make(map[string]string, len(validationErrors))
		for _, fe := range validationErrors {
			details[fe.Field()] = fmt.Sprintf("failed on '%s' tag (value: '%v')", fe.Tag(), fe.Value())
		}

		apiErr := apperrors.NewValidationError("bid request validation failed", details)
		h.log.Warn("bid request validation failure",
			zap.String("request_id", req.ID),
			zap.Any("details", details),
		)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	// Fetch user segments
	userSegments := h.segments.FetchUserSegments(c.Context(), userID)

	// Evaluate targeting
	matchedCampaigns, err := h.campaignSvc.EvaluateTargeting(c.Context(), req, userSegments)
	if err != nil {
		h.log.Warn("targeting evaluation failed", zap.Error(err))
	}

	// For demonstration, map internal matching campaigns to mock bids if no DSP responses
	// In a full implementation, you might pass these matched campaigns into the auction directly
	// or append them as a special seat.
	var internalBids []auction.Bid
	for _, camp := range matchedCampaigns {
		if len(camp.Creatives) > 0 {
			cr := camp.Creatives[0]
			internalBids = append(internalBids, auction.Bid{
				DealID:     camp.ID,
				DealType:   auction.PG, // Internal campaigns might have high priority
				Price:      float64(camp.BudgetCents) / 100.0, // Simplified price
				AdID:       cr.ID,
				DSPName:    "internal",
				CreativeID: cr.ID,
			})
		}
	}

	h.log.Info("processing bid request",
		zap.String("request_id", req.ID),
		zap.Int("impression_count", len(req.Imp)),
		zap.String("user_id", userID),
		zap.Strings("segments", userSegments),
		zap.Int("matched_campaigns", len(matchedCampaigns)),
	)

	// Use real DSP fan-out
	dspBids := h.fanout.FetchBids(c.Context(), req)
	
	// Combine internal bids and DSP bids
	bids := append(dspBids, internalBids...)

	// Extract BidFloor from the first impression
	var floor float64
	var impID string
	var bannerW, bannerH int
	if len(req.Imp) > 0 {
		floor = req.Imp[0].BidFloor
		impID = req.Imp[0].ID
		if req.Imp[0].Banner != nil {
			bannerW = req.Imp[0].Banner.W
			bannerH = req.Imp[0].Banner.H
		}
	}

	// Run Auction
	result := auction.RunAuction(bids, floor, h.log)

	var response models.BidResponse
	var bidID string
	var dealID string
	var creativeID string
	var winPrice float64

	if result.Winner != nil {
		bidID = "bid-" + req.ID
		dealID = result.Winner.DealID
		creativeID = result.Winner.CreativeID
		winPrice = result.ClearingPrice

		bid := models.Bid{
			ID:    bidID,
			ImpID: impID,
			Price: winPrice,
			AdID:  result.Winner.AdID,
			CrID:  creativeID,
			W:     bannerW,
			H:     bannerH,
		}

		response = models.BidResponse{
			ID: req.ID,
			SeatBid: []models.SeatBid{
				{
					Bid:  []models.Bid{bid},
					Seat: result.Winner.DSPName,
				},
			},
			Cur: "USD",
		}
	} else {
		// Fallback logic
		var houseAd *models.Bid
		if len(req.Imp) > 0 {
			houseAd = h.houseAds.GetFallbackAd(req.Imp[0])
		}

		if houseAd != nil {
			h.log.Warn("serving house ad as fallback", zap.String("request_id", req.ID))
			bidID = houseAd.ID
			dealID = "house-deal"
			creativeID = houseAd.CrID
			winPrice = houseAd.Price

			response = models.BidResponse{
				ID: req.ID,
				SeatBid: []models.SeatBid{
					{
						Bid:  []models.Bid{*houseAd},
						Seat: "house-ad",
					},
				},
				Cur: "USD",
			}
		} else {
			return c.SendStatus(fiber.StatusNoContent)
		}
	}

	// Publish ImpressionEvent asynchronously.
	// NOTE: This goroutine is intentionally fire-and-forget — we do not block the
	// bid response on event publishing. The goroutine is bounded by a 2s context
	// timeout, ensuring it will not leak.
	go func(eventReqID, bidID, impID, dealID, crID, userID string, winPrice, floorPrice float64) {
		// TODO: context.Background() is used because the Fiber request context
		// will be cancelled once the HTTP response is sent, but we need the
		// event publish to outlive the request lifecycle.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		event := events.ImpressionEvent{
			RequestID:    eventReqID,
			BidID:        bidID,
			ImpressionID: impID,
			CampaignID:   dealID,
			CreativeID:   crID,
			WinPrice:     winPrice,
			UserID:       userID,
			Timestamp:    time.Now().UTC(),
			FloorPrice:   floorPrice,
		}

		if req.Device != nil {
			event.DeviceType = req.Device.DeviceType
			if req.Device.Geo != nil {
				event.GeoCountry = req.Device.Geo.Country
				event.GeoCity = req.Device.Geo.City
			}
		}

		if err := h.producer.PublishImpressionEvent(ctx, event); err != nil {
			h.log.Error("failed to publish impression event asynchronously", zap.Error(err), zap.String("request_id", eventReqID))
		}
	}(req.ID, bidID, impID, dealID, creativeID, userID, winPrice, floor)

	return c.Status(fiber.StatusOK).JSON(response)
}

// HandleHealth returns a simple health check response with the server status
// and current version.
func (h *BidHandler) HandleHealth(c *fiber.Ctx) error {
	// TODO: context.Background() is used here because Fiber's c.Context() is tied
	// to the request lifecycle and may be reused. A standalone context ensures the
	// health check probe runs independently.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	kafkaStatus := "ok"
	if err := h.producer.Ping(ctx); err != nil {
		h.log.Warn("kafka health check failed", zap.Error(err))
		kafkaStatus = "unavailable"
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status":  "ok",
		"version": "1.0.0",
		"kafka":   kafkaStatus,
	})
}
