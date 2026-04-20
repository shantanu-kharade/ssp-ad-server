package dsp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/yourusername/ssp-adserver/internal/models"
)

// TestFanoutCoordinator verifies that the fan-out coordinator sends requests
// to all DSPs in parallel and only returns valid bids within the timeout.
func TestFanoutCoordinator(t *testing.T) {
	// Create mock servers
	fastServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := models.BidResponse{
			ID: "test-id",
			SeatBid: []models.SeatBid{
				{Bid: []models.Bid{{Price: 2.0, AdID: "ad-fast"}}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer fastServer.Close()

	noBidServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer noBidServer.Close()

	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // Exceeds 50ms timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	// Setup clients
	logger := zap.NewNop()
	clients := []*Client{
		NewClient(Config{Name: "fast", URL: fastServer.URL, TimeoutMs: 50}, logger),
		NewClient(Config{Name: "nobid", URL: noBidServer.URL, TimeoutMs: 50}, logger),
		NewClient(Config{Name: "slow", URL: slowServer.URL, TimeoutMs: 50}, logger),
	}

	coordinator := NewFanoutCoordinator(clients, logger)

	req := models.BidRequest{ID: "test-id"}

	start := time.Now()
	bids := coordinator.FetchBids(context.Background(), req)
	duration := time.Since(start)

	// Should have exactly 1 valid bid from the fast server
	if len(bids) != 1 {
		t.Fatalf("expected 1 bid, got %d", len(bids))
	}
	if bids[0].AdID != "ad-fast" {
		t.Errorf("expected ad-fast, got %s", bids[0].AdID)
	}

	// Duration should be close to 50ms (timeout limit), not 100ms
	if duration > 75*time.Millisecond {
		t.Errorf("fanout took too long: %v", duration)
	}
}

