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
	"golang.org/x/sync/errgroup"

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
	eventQueue  chan events.ImpressionEvent
}

// NewBidHandler creates a new BidHandler with an initialised validator
// and the provided logger.
func NewBidHandler(log *zap.Logger, segments *cache.SegmentFetcher, resolver *identity.Resolver, consent *identity.Validator, fanout *dsp.FanoutCoordinator, houseAds *ads.HouseAdProvider, campaignSvc *campaign.Service, producer *events.EventProducer) *BidHandler {
	h := &BidHandler{
		validate:    validator.New(),
		log:         log,
		segments:    segments,
		resolver:    resolver,
		consent:     consent,
		fanout:      fanout,
		houseAds:    houseAds,
		campaignSvc: campaignSvc,
		producer:    producer,
		eventQueue:  make(chan events.ImpressionEvent, 10000),
	}

	// Start bounded worker pool for impression events
	for  i := 0; i < 16; i++ {
		go h.eventWorker()
	}

	return h
}

func (h *BidHandler) eventWorker() {
	for event := range h.eventQueue {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := h.producer.PublishImpressionEvent(ctx, event); err != nil {
			h.log.Error("failed to publish impression event asynchronously", zap.Error(err), zap.String("request_id", event.RequestID))
		}
		cancel()
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

	// -------------------------------------------------------------------------
	// Parallel Bid Pipeline
	// -------------------------------------------------------------------------
	// The three data-fetching operations below are independent of each other
	// and are run concurrently to collapse their wall-clock time:
	//
	//   Goroutine A: segment fetch + campaign evaluation (sequential within A,
	//                because EvaluateTargeting needs the segment results).
	//                Typical cost: ~10ms Redis + ~5ms Redis/DB cache hit.
	//
	//   Goroutine B: DSP fanout — fires HTTP requests to all demand partners.
	//                Typical cost: 50-80ms (dominant latency).
	//
	// Before: total sequential cost ≈ 15ms + 70ms = 85ms before auction.
	// After:  total cost ≈ max(15ms, 70ms) = 70ms — saving ~15ms of SLA budget.
	//
	// errgroup.WithContext propagates cancellation if the parent context is
	// cancelled (e.g. the 100ms Fiber timeout fires). Neither goroutine returns
	// a fatal error — they each degrade gracefully on failure.
	// -------------------------------------------------------------------------

	var (
		userSegments    []string
		matchedCampaigns []campaign.Campaign
		dspBids         []auction.Bid
	)

	g, gCtx := errgroup.WithContext(c.Context())

	// Goroutine A: Redis segment lookup → campaign targeting evaluation.
	// Failures are non-fatal: we degrade to empty segments / no internal bids.
	g.Go(func() error {
		userSegments = h.segments.FetchUserSegments(gCtx, userID)

		var evalErr error
		matchedCampaigns, evalErr = h.campaignSvc.EvaluateTargeting(gCtx, req, userSegments)
		if evalErr != nil {
			h.log.Warn("targeting evaluation failed — continuing with no internal bids",
				zap.Error(evalErr),
				zap.String("request_id", req.ID),
			)
		}
		return nil // always nil — errors are soft
	})

	// Goroutine B: DSP fanout. FetchBids handles its own error/timeout logic
	// and returns an empty slice on failure, so we never return a hard error.
	g.Go(func() error {
		dspBids = h.fanout.FetchBids(gCtx, req)
		return nil // always nil — FetchBids degrades gracefully
	})

	// Wait for both goroutines. Because both always return nil this can only
	// fail if the parent context is cancelled.
	_ = g.Wait()

	// Map matched internal campaigns to auction bids.
	var internalBids []auction.Bid
	for _, camp := range matchedCampaigns {
		if len(camp.Creatives) == 0 {
			continue
		}
		if camp.BidPriceCPM <= 0 {
			h.log.Warn("skipping internal campaign with no configured bid price",
				zap.String("campaign_id", camp.ID),
				zap.String("campaign_name", camp.Name),
			)
			continue
		}
		cr := camp.Creatives[0]
		internalBids = append(internalBids, auction.Bid{
			DealID:     camp.ID,
			DealType:   auction.PG,
			Price:      camp.BidPriceCPM,
			AdID:       cr.ID,
			DSPName:    "internal",
			CreativeID: cr.ID,
		})
	}

	h.log.Info("processing bid request",
		zap.String("request_id", req.ID),
		zap.Int("impression_count", len(req.Imp)),
		zap.String("user_id", userID),
		zap.Strings("segments", userSegments),
		zap.Int("matched_campaigns", len(matchedCampaigns)),
	)

	// Combine internal bids and DSP bids for the auction.
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

	// Publish ImpressionEvent asynchronously via the bounded worker pool.
	// We construct the event synchronously and use a non-blocking send to
	// prevent the bid response from blocking if the Kafka publisher is slow.
	event := events.ImpressionEvent{
		RequestID:    req.ID,
		BidID:        bidID,
		ImpressionID: impID,
		CampaignID:   dealID,
		CreativeID:   creativeID,
		WinPrice:     winPrice,
		UserID:       userID,
		Timestamp:    time.Now().UTC(),
		FloorPrice:   floor,
	}

	if req.Device != nil {
		event.DeviceType = req.Device.DeviceType
		if req.Device.Geo != nil {
			event.GeoCountry = req.Device.Geo.Country
			event.GeoCity = req.Device.Geo.City
		}
	}

	select {
	case h.eventQueue <- event:
		// Successfully queued
	default:
		h.log.Warn("event queue full, dropping impression event", zap.String("request_id", req.ID))
	}

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
