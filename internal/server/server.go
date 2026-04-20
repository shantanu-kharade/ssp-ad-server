// Package server wires together the Fiber HTTP application with middleware,
// handlers, and configuration for the SSP ad server.
package server

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/gofiber/fiber/v2/middleware/timeout"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/yourusername/ssp-adserver/internal/ads"
	"github.com/yourusername/ssp-adserver/internal/cache"
	"github.com/yourusername/ssp-adserver/internal/campaign"
	"github.com/yourusername/ssp-adserver/internal/config"
	"github.com/yourusername/ssp-adserver/internal/dsp"
	"github.com/yourusername/ssp-adserver/internal/events"
	"github.com/yourusername/ssp-adserver/internal/handler"
	"github.com/yourusername/ssp-adserver/internal/identity"
	"github.com/yourusername/ssp-adserver/internal/metrics"
	"github.com/yourusername/ssp-adserver/internal/middleware"
	"github.com/yourusername/ssp-adserver/internal/resilience"
)

// New creates and configures a new Fiber application with all middleware
// and route handlers registered. The returned app is ready to be started
// with app.Listen().
func New(cfg *config.Config, log *zap.Logger, redisClient *cache.RedisClient, dbPool *pgxpool.Pool, eventProducer *events.EventProducer) *fiber.App {
	app := fiber.New(fiber.Config{
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		// Return structured JSON errors instead of Fiber's default text.
		ErrorHandler: customErrorHandler(log),
		// Disable the startup banner for cleaner log output.
		DisableStartupMessage: true,
	})

	// --- Global Middleware ---

	// Recovery must be first so it can catch panics from all downstream handlers.
	app.Use(middleware.Recovery(log))

	// Generate unique request IDs for every request.
	app.Use(requestid.New())

	// Structured request/response logging.
	app.Use(middleware.Logger(log))

	// Structured metrics logging
	app.Use(middleware.Metrics(log))

	// Global rate limiter middleware (e.g., 1000 RPS)
	globalLimiter := resilience.NewGlobalRateLimiter(2000)
	app.Use(middleware.RateLimit(globalLimiter, log))

	// Parse DSP Configuration
	dspConfigs, err := dsp.ParseConfig(cfg.Server.DSPEndpoints)
	if err != nil {
		log.Fatal("failed to parse DSP endpoints configuration", zap.Error(err))
	}

	var dspClients []*dsp.Client
	for _, c := range dspConfigs {
		dspClients = append(dspClients, dsp.NewClient(c, log))
	}

	fanout := dsp.NewFanoutCoordinator(dspClients, log)

	// --- Handlers ---
	segments := cache.NewSegmentFetcher(redisClient, log)
	resolver := identity.NewResolver()
	consent := identity.NewValidator()

	repo := campaign.NewPGRepository(dbPool)
	cachedRepo := campaign.NewRedisCache(repo, redisClient, log)
	campaignSvc := campaign.NewService(cachedRepo)

	houseAdsProvider := ads.NewHouseAdProvider(cfg.Server)

	bidHandler := handler.NewBidHandler(log, segments, resolver, consent, fanout, houseAdsProvider, campaignSvc, eventProducer)
	adminHandler := handler.NewAdminHandler(segments, fanout, log)
	campaignHandler := handler.NewCampaignHandler(cachedRepo, log)
	trackHandler := handler.NewTrackHandler(eventProducer, log)

	// --- Routes ---

	// Health check endpoint — lightweight, no timeout wrapper needed.
	app.Get("/health", bidHandler.HandleHealth)

	// Metrics endpoint — returns JSON with in-memory atomic counters.
	// Supports ?reset=true to zero all counters.
	app.Get("/metrics", handleMetrics())

	// Bid endpoint with a 100ms hard timeout to meet SLA requirements.
	app.Post("/bid", timeout.NewWithContext(bidHandler.HandleBid, 100*time.Millisecond))

	// Tracking endpoint
	app.Get("/track/click", trackHandler.HandleClick)

	// Admin routes
	admin := app.Group("/admin", middleware.APIKeyAuth(cfg.Server.AdminAPIKey))
	admin.Post("/segments", adminHandler.HandleSetSegments)
	admin.Get("/circuit-breakers", adminHandler.HandleCircuitBreakers)
	
	// Campaign REST API
	admin.Get("/campaigns", campaignHandler.HandleGetActiveCampaigns)
	admin.Post("/campaigns", campaignHandler.HandleCreateCampaign)
	admin.Get("/campaigns/:id", campaignHandler.HandleGetCampaign)
	admin.Patch("/campaigns/:id", campaignHandler.HandleUpdateCampaign)
	admin.Post("/campaigns/:id/creatives", campaignHandler.HandleAddCreative)

	return app
}

// handleMetrics returns a Fiber handler for the GET /metrics endpoint.
// It returns a JSON snapshot of all in-memory atomic counters. When the
// query parameter reset=true is provided, counters are zeroed after
// capturing the snapshot.
func handleMetrics() fiber.Handler {
	return func(c *fiber.Ctx) error {
		snapshot := metrics.GlobalCounters.Snapshot()

		if c.Query("reset") == "true" {
			metrics.GlobalCounters.Reset()
		}

		return c.Status(fiber.StatusOK).JSON(snapshot)
	}
}

// Start begins listening on the configured port and blocks until the server
// exits. It logs the listening address on startup.
func Start(app *fiber.App, cfg *config.Config, log *zap.Logger) error {
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Info("starting SSP ad server",
		zap.String("address", addr),
		zap.Duration("read_timeout", cfg.Server.ReadTimeout),
		zap.Duration("write_timeout", cfg.Server.WriteTimeout),
	)

	if err := app.Listen(addr); err != nil {
		return fmt.Errorf("server failed to listen on %s: %w", addr, err)
	}

	return nil
}

// customErrorHandler returns a Fiber error handler that logs unhandled errors
// and returns a consistent JSON error response.
func customErrorHandler(log *zap.Logger) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		// Default to 500 Internal Server Error.
		code := fiber.StatusInternalServerError

		// If Fiber provides a typed error with a code, use it.
		if e, ok := err.(*fiber.Error); ok {
			code = e.Code
		}

		log.Error("unhandled error",
			zap.Error(err),
			zap.Int("status", code),
			zap.String("method", c.Method()),
			zap.String("path", c.Path()),
		)

		return c.Status(code).JSON(fiber.Map{
			"type":        "INTERNAL_ERROR",
			"status_code": code,
			"message":     err.Error(),
		})
	}
}
