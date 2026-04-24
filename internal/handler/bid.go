// Package handler implements the HTTP handlers for the SSP ad server,
// including bid request processing and health checks.
package handler

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/yourusername/ssp-adserver/internal/ads"
	"github.com/yourusername/ssp-adserver/internal/auction"
	"github.com/yourusername/ssp-adserver/internal/cache"
	"github.com/yourusername/ssp-adserver/internal/campaign"
	"github.com/yourusername/ssp-adserver/internal/dsp"
	"github.com/yourusername/ssp-adserver/internal/events"
	"github.com/yourusername/ssp-adserver/internal/identity"
	"github.com/yourusername/ssp-adserver/internal/metrics"
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
	// nurlQueue carries win-notice fire requests to the bounded nurlWorker pool.
	// Capacity of 1000 absorbs short bursts without blocking the hot path.
	nurlQueue chan nurlFireEvent
}

// nurlFireEvent is the payload sent to nurlWorker for asynchronous NURL firing.
type nurlFireEvent struct {
	NURL          string
	ClearingPrice float64
	RequestID     string
	ImpID         string
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
		nurlQueue:   make(chan nurlFireEvent, 1000),
	}

	// Start bounded worker pool for impression events
	for i := 0; i < 16; i++ {
		go h.eventWorker()
	}

	// Start bounded worker pool for NURL win-notice HTTP fires.
	// 4 workers: each fire is a single outbound HTTP GET (200ms timeout),
	// so 4 concurrent workers handles ~20 fires/s before queuing.
	for i := 0; i < 4; i++ {
		go h.nurlWorker()
	}

	return h
}

func (h *BidHandler) eventWorker() {
	for event := range h.eventQueue {
		metrics.ActiveGoroutines.Set(float64(len(h.eventQueue)))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := h.producer.PublishImpressionEvent(ctx, event); err != nil {
			h.log.Error("failed to publish impression event asynchronously", zap.Error(err), zap.String("request_id", event.RequestID))
		}
		cancel()
	}
}

// nurlWorker drains nurlQueue and fires HTTP GET win notices to DSPs.
// Each call uses a 200ms timeout per OpenRTB best-practice for win notices.
// A single http.Client is shared across all fires in this worker — its
// transport-level connection pool reuses keep-alive connections to the same DSP.
func (h *BidHandler) nurlWorker() {
	client := &http.Client{Timeout: 200 * time.Millisecond}
	for evt := range h.nurlQueue {
		// Substitute the clearing price macro before firing.
		firedURL := strings.ReplaceAll(evt.NURL, "${AUCTION_PRICE}", fmt.Sprintf("%.4f", evt.ClearingPrice))

		h.log.Debug("firing win notice (NURL)",
			zap.String("request_id", evt.RequestID),
			zap.String("imp_id", evt.ImpID),
			zap.String("nurl", firedURL),
			zap.Float64("clearing_price", evt.ClearingPrice),
		)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, firedURL, nil)
		if err != nil {
			h.log.Warn("failed to construct NURL request",
				zap.String("request_id", evt.RequestID),
				zap.String("imp_id", evt.ImpID),
				zap.String("nurl", evt.NURL),
				zap.Error(err),
			)
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			h.log.Warn("NURL win notice fire failed",
				zap.String("request_id", evt.RequestID),
				zap.String("imp_id", evt.ImpID),
				zap.String("nurl", firedURL),
				zap.Error(err),
			)
			continue
		}
		resp.Body.Close()
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
	start := time.Now()
	publisherID := "unknown"
	status := "error" // default to error
	tracer := otel.Tracer("ssp-adserver/bid-handler")
	ctx, span := tracer.Start(c.UserContext(), "bid.handle")
	defer span.End()
	c.SetUserContext(ctx)

	defer func() {
		metrics.BidLatencySeconds.Observe(time.Since(start).Seconds())
		metrics.BidRequestsTotal.WithLabelValues(publisherID, status).Inc()
	}()

	var req models.BidRequest

	if err := c.BodyParser(&req); err != nil {
		apiErr := apperrors.NewValidationError("malformed JSON body", map[string]string{"body": err.Error()})
		span.RecordError(apiErr)
		span.SetStatus(codes.Error, "malformed JSON body")
		h.log.Error("failed to parse bid request body",
			zap.Error(err),
			zap.Int64("elapsed_ms", time.Since(start).Milliseconds()),
		)
		return c.Status(apiErr.StatusCode).JSON(apiErr)
	}

	// Extract publisher ID for metrics
	if req.Site != nil && req.Site.Publisher != nil && req.Site.Publisher.ID != "" {
		publisherID = req.Site.Publisher.ID
	} else if req.App != nil && req.App.Publisher != nil && req.App.Publisher.ID != "" {
		publisherID = req.App.Publisher.ID
	}

	// Resolve user ID
	userID := h.resolver.Resolve(c, req)
	// Store the user ID in Locals so the logger middleware can pick it up.
	c.Locals("user_id", userID)

	// Validate consent
	if !h.consent.Validate(c) {
		h.log.Warn("bid request rejected: no consent",
			zap.String("publisher_id", publisherID),
			zap.String("auction_id", req.ID),
			zap.Int64("elapsed_ms", time.Since(start).Milliseconds()),
			zap.String("request_id", req.ID),
			zap.String("user_id", userID),
		)
		// Return empty bid response with NBR=0
		status = "no_bid"
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
			span.RecordError(apiErr)
			span.SetStatus(codes.Error, "unexpected validation error type")
			h.log.Error("unexpected validation error type",
				zap.Error(apiErr),
				zap.String("request_id", req.ID),
				zap.String("publisher_id", publisherID),
				zap.String("auction_id", req.ID),
				zap.Int64("elapsed_ms", time.Since(start).Milliseconds()),
			)
			return c.Status(apiErr.StatusCode).JSON(apiErr)
		}

		details := make(map[string]string, len(validationErrors))
		for _, fe := range validationErrors {
			details[fe.Field()] = fmt.Sprintf("failed on '%s' tag (value: '%v')", fe.Tag(), fe.Value())
		}

		apiErr := apperrors.NewValidationError("bid request validation failed", details)
		span.RecordError(apiErr)
		span.SetStatus(codes.Error, "bid request validation failed")
		h.log.Error("bid request validation failure",
			zap.String("request_id", req.ID),
			zap.String("publisher_id", publisherID),
			zap.String("auction_id", req.ID),
			zap.Int64("elapsed_ms", time.Since(start).Milliseconds()),
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
		userSegments     []string
		matchedCampaigns []campaign.Campaign
		dspBids          []auction.Bid
	)

	g, gCtx := errgroup.WithContext(ctx)

	// Goroutine A: Redis segment lookup → campaign targeting evaluation.
	// Failures are non-fatal: we degrade to empty segments / no internal bids.
	g.Go(func() error {
		segmentsCtx, segmentsSpan := tracer.Start(gCtx, "pipeline.fetch_segments")
		defer segmentsSpan.End()
		userSegments = h.segments.FetchUserSegments(segmentsCtx, userID)

		evalCtx, evalSpan := tracer.Start(gCtx, "pipeline.evaluate_campaigns")
		defer evalSpan.End()
		var evalErr error
		matchedCampaigns, evalErr = h.campaignSvc.EvaluateTargeting(evalCtx, req, userSegments)
		if evalErr != nil {
			evalSpan.RecordError(evalErr)
			evalSpan.SetStatus(codes.Error, "campaign evaluation failed")
			h.log.Error("targeting evaluation failed — continuing with no internal bids",
				zap.Error(evalErr),
				zap.String("publisher_id", publisherID),
				zap.String("auction_id", req.ID),
				zap.Int64("elapsed_ms", time.Since(start).Milliseconds()),
				zap.String("request_id", req.ID),
			)
		}
		return nil // always nil — errors are soft
	})

	// Goroutine B: DSP fanout. FetchBids handles its own error/timeout logic
	// and returns an empty slice on failure, so we never return a hard error.
	g.Go(func() error {
		fanoutCtx, fanoutSpan := tracer.Start(gCtx, "pipeline.dsp_fanout")
		defer fanoutSpan.End()
		dspBids = h.fanout.FetchBids(fanoutCtx, req)
		return nil // always nil — FetchBids degrades gracefully
	})

	// Wait for both goroutines. Because both always return nil this can only
	// fail if the parent context is cancelled.
	if err := g.Wait(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "parallel pipeline failed")
	}

	// Map matched internal campaigns to auction bids.
	var internalBids []auction.Bid
	for _, camp := range matchedCampaigns {
		if len(camp.Creatives) == 0 {
			continue
		}
		if camp.BidPriceCPM <= 0 {
			h.log.Warn("skipping internal campaign with no configured bid price",
				zap.String("publisher_id", publisherID),
				zap.String("auction_id", req.ID),
				zap.Int64("elapsed_ms", time.Since(start).Milliseconds()),
				zap.String("request_id", req.ID),
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

	enableMultiImp := os.Getenv("ENABLE_MULTI_IMP") == "true"
	impsToProcess := req.Imp
	if !enableMultiImp && len(req.Imp) > 0 {
		impsToProcess = []models.Impression{req.Imp[0]}
	}

	seatBidsMap := make(map[string][]models.Bid)

	for _, imp := range impsToProcess {
		floor := imp.BidFloor
		impID := imp.ID
		var bannerW, bannerH int
		if imp.Banner != nil {
			bannerW = imp.Banner.W
			bannerH = imp.Banner.H
		}

		// Filter DSP bids for this impression
		var bidsForImp []auction.Bid
		for _, b := range dspBids {
			if b.ImpID == impID {
				bidsForImp = append(bidsForImp, b)
			}
		}
		// Internal bids apply to the request generally, so include them for each imp
		bidsForImp = append(bidsForImp, internalBids...)

		// Run Auction
		_, auctionSpan := tracer.Start(ctx, "auction.run")
		result := auction.RunAuction(bidsForImp, floor, h.log, req.AuctionType)
		auctionSpan.End()

		if result.Winner != nil {
			bidID := "bid-" + req.ID
			if enableMultiImp {
				bidID += "-" + imp.ID
			}
			dealID := result.Winner.DealID
			creativeID := result.Winner.CreativeID
			winPrice := result.ClearingPrice

			bid := models.Bid{
				ID:    bidID,
				ImpID: impID,
				Price: winPrice,
				AdID:  result.Winner.AdID,
				CrID:  creativeID,
				W:     bannerW,
				H:     bannerH,
			}

			seatBidsMap[result.Winner.DSPName] = append(seatBidsMap[result.Winner.DSPName], bid)
			metrics.AuctionClearingPrice.Observe(winPrice)

			// Publish ImpressionEvent asynchronously
			h.publishImpressionEvent(ctx, req, bidID, impID, dealID, creativeID, winPrice, userID, floor, start, publisherID, span)

			// Fire NURL win notice for external DSP wins only.
			// Internal (house / direct campaign) wins have no upstream DSP to notify.
			if result.Winner.DSPName != "internal" && result.Winner.NURL != "" {
				h.fireNURL(result.Winner.NURL, winPrice, req.ID, impID)
			}
		} else {
			// Fallback logic
			houseAd := h.houseAds.GetFallbackAd(imp)
			if houseAd != nil {
				h.log.Warn("serving house ad as fallback",
					zap.String("publisher_id", publisherID),
					zap.String("auction_id", req.ID),
					zap.Int64("elapsed_ms", time.Since(start).Milliseconds()),
					zap.String("request_id", req.ID),
					zap.String("imp_id", imp.ID),
				)
				
				bidID := houseAd.ID
				if enableMultiImp {
					bidID += "-" + imp.ID
				}
				
				// Make sure we set the proper ImpID in the response
				houseBid := *houseAd
				houseBid.ID = bidID
				houseBid.ImpID = imp.ID

				seatBidsMap["house-ad"] = append(seatBidsMap["house-ad"], houseBid)
				
				// Publish ImpressionEvent for house ad
				h.publishImpressionEvent(ctx, req, bidID, imp.ID, "house-deal", houseBid.CrID, houseBid.Price, userID, floor, start, publisherID, span)
			}
		}
	}

	if len(seatBidsMap) == 0 {
		status = "no_bid"
		return c.SendStatus(fiber.StatusNoContent)
	}

	var seatBids []models.SeatBid
	for seat, bids := range seatBidsMap {
		seatBids = append(seatBids, models.SeatBid{
			Bid:  bids,
			Seat: seat,
		})
	}

	response := models.BidResponse{
		ID:      req.ID,
		SeatBid: seatBids,
		Cur:     "USD",
	}

	status = "success"
	if rand.Intn(100) == 0 {
		h.log.Debug("sampled successful bid response",
			zap.String("request_id", req.ID),
			zap.String("publisher_id", publisherID),
			zap.String("auction_id", req.ID),
			zap.Int64("elapsed_ms", time.Since(start).Milliseconds()),
			zap.String("winner_seat", response.SeatBid[0].Seat),
		)
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

// publishImpressionEvent queues an impression event for async processing.
func (h *BidHandler) publishImpressionEvent(
	ctx context.Context,
	req models.BidRequest,
	bidID, impID, dealID, creativeID string,
	winPrice float64,
	userID string,
	floor float64,
	start time.Time,
	publisherID string,
	span trace.Span,
) {
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
		metrics.ActiveGoroutines.Set(float64(len(h.eventQueue)))
	default:
		span.SetStatus(codes.Error, "event queue full")
		h.log.Error("event queue full, dropping impression event",
			zap.String("request_id", req.ID),
			zap.String("publisher_id", publisherID),
			zap.String("auction_id", req.ID),
			zap.Int64("elapsed_ms", time.Since(start).Milliseconds()),
		)
	}
}

// fireNURL enqueues a NURL win-notice fire event onto the bounded nurlQueue.
// It is non-blocking: if the queue is full it logs a warning and drops the
// event rather than stalling the bid response.
func (h *BidHandler) fireNURL(nurl string, clearingPrice float64, requestID, impID string) {
	evt := nurlFireEvent{
		NURL:          nurl,
		ClearingPrice: clearingPrice,
		RequestID:     requestID,
		ImpID:         impID,
	}
	select {
	case h.nurlQueue <- evt:
		// queued successfully — nurlWorker will fire the HTTP GET
	default:
		h.log.Warn("NURL queue full, dropping win notice",
			zap.String("request_id", requestID),
			zap.String("imp_id", impID),
			zap.String("nurl", nurl),
		)
	}
}
