package dsp

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/yourusername/ssp-adserver/internal/auction"
	"github.com/yourusername/ssp-adserver/internal/models"
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
			latency := time.Since(start).Milliseconds()

			if err != nil {
				f.log.Warn("dsp failed",
					zap.String("dsp", c.Name()),
					zap.Error(err),
					zap.Int64("latency_ms", latency),
					zap.String("request_id", req.ID),
				)
				return
			}

			if len(bids) > 0 {
				f.log.Info("dsp responded with bids",
					zap.String("dsp", c.Name()),
					zap.Int("bid_count", len(bids)),
					zap.Int64("latency_ms", latency),
					zap.String("request_id", req.ID),
				)
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
