package dsp

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/yourusername/ssp-adserver/internal/auction"
	"github.com/yourusername/ssp-adserver/internal/metrics"
	"github.com/yourusername/ssp-adserver/internal/models"
	"github.com/yourusername/ssp-adserver/internal/resilience"
)

// FanoutCoordinator manages parallel bid requests to multiple DSPs.
type FanoutCoordinator struct {
	clients []*Client
	log     *zap.Logger
}

// NewFanoutCoordinator creates a new coordinator from configured clients.
func NewFanoutCoordinator(clients []*Client, log *zap.Logger) *FanoutCoordinator {
	return &FanoutCoordinator{
		clients: clients,
		log:     log,
	}
}

// Clients returns the list of DSP clients managed by this coordinator.
func (f *FanoutCoordinator) Clients() []*Client {
	return f.clients
}

// FetchBids sends the bid request to all configured DSPs in parallel.
// It enforces a 50ms hard timeout and collects all valid responses.
func (f *FanoutCoordinator) FetchBids(ctx context.Context, req models.BidRequest) []auction.Bid {
	publisherID := publisherFromRequest(req)
	auctionID := req.ID

	// Enforce hard 50ms timeout for the entire fanout operation.
	timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	results := make(chan []auction.Bid, len(f.clients))

	for _, client := range f.clients {
		wg.Add(1)
		go func(c *Client) {
			defer wg.Done()

			start := time.Now()
			bids, err := c.FetchBid(timeoutCtx, req)
			duration := time.Since(start)
			latency := duration.Milliseconds()

			metrics.DSPBidLatencySeconds.WithLabelValues(c.Name()).Observe(duration.Seconds())

			if err != nil {
				if errors.Is(err, ErrTimeout) {
					f.log.Warn("dsp timeout",
						zap.String("dsp_id", c.Name()),
						zap.Int64("elapsed_ms", latency),
						zap.String("request_id", req.ID),
						zap.String("publisher_id", publisherID),
						zap.String("auction_id", auctionID),
					)
					return
				}

				f.log.Error("dsp request failed",
					zap.String("request_id", req.ID),
					zap.String("publisher_id", publisherID),
					zap.String("auction_id", auctionID),
					zap.Int64("elapsed_ms", latency),
					zap.String("dsp_id", c.Name()),
					zap.Error(err),
				)
				if errors.Is(err, resilience.ErrCircuitOpen) {
					f.log.Error("dsp circuit breaker open",
						zap.String("request_id", req.ID),
						zap.String("publisher_id", publisherID),
						zap.String("auction_id", auctionID),
						zap.Int64("elapsed_ms", latency),
						zap.String("dsp_id", c.Name()),
					)
				}
				return
			}

			if len(bids) > 0 {
				results <- bids
			}
		}(client)
	}

	// Close results channel when all goroutines finish
	go func() {
		wg.Wait()
		close(results)
	}()

	var allBids []auction.Bid
	for dspBids := range results {
		allBids = append(allBids, dspBids...)
	}

	return allBids
}

func publisherFromRequest(req models.BidRequest) string {
	if req.Site != nil && req.Site.Publisher != nil && req.Site.Publisher.ID != "" {
		return req.Site.Publisher.ID
	}
	if req.App != nil && req.App.Publisher != nil && req.App.Publisher.ID != "" {
		return req.App.Publisher.ID
	}
	return "unknown"
}
