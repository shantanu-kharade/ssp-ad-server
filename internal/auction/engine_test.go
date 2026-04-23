package auction

import (
	"os"
	"testing"

	"go.uber.org/zap"
)

// ── existing second-price tests (updated for +$0.01 increment and new signature) ──

func TestRunAuction(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name          string
		bids          []Bid
		floor         float64
		wantWinnerID  string
		wantPrice     float64
		wantNoBid     string
		wantValidBids int
	}{
		{
			name: "All bids below floor",
			bids: []Bid{
				{DealID: "1", DealType: Open, Price: 1.0, AdID: "ad1"},
				{DealID: "2", DealType: Open, Price: 1.5, AdID: "ad2"},
			},
			floor:         2.0,
			wantWinnerID:  "",
			wantNoBid:     "no_valid_bids",
			wantValidBids: 0,
		},
		{
			name: "PG beats higher-priced open bid",
			bids: []Bid{
				{DealID: "1", DealType: Open, Price: 5.0, AdID: "ad1"},
				{DealID: "2", DealType: PG, Price: 2.0, AdID: "ad2"},
			},
			floor:         1.0,
			wantWinnerID:  "2",
			wantPrice:     5.01, // second price (Open 5.0) + $0.01 increment
			wantValidBids: 2,
		},
		{
			name: "Second-price calculation with multiple Open bids",
			bids: []Bid{
				{DealID: "1", DealType: Open, Price: 3.0, AdID: "ad1"},
				{DealID: "2", DealType: Open, Price: 5.0, AdID: "ad2"},
				{DealID: "3", DealType: Open, Price: 4.0, AdID: "ad3"},
			},
			floor:         1.0,
			wantWinnerID:  "2",
			wantPrice:     4.01, // second price (4.0) + $0.01
			wantValidBids: 3,
		},
		{
			name: "Single bid clears at floor",
			bids: []Bid{
				{DealID: "1", DealType: Open, Price: 5.0, AdID: "ad1"},
			},
			floor:         2.0,
			wantWinnerID:  "1",
			wantPrice:     2.0,
			wantValidBids: 1,
		},
		{
			name: "Empty bids slice",
			bids: []Bid{},
			floor:         1.0,
			wantWinnerID:  "",
			wantNoBid:     "no_valid_bids",
			wantValidBids: 0,
		},
		{
			name: "PG and PMP compete",
			bids: []Bid{
				{DealID: "1", DealType: PMP, Price: 4.0, AdID: "ad1"},
				{DealID: "2", DealType: PG, Price: 3.0, AdID: "ad2"},
			},
			floor:         1.0,
			wantWinnerID:  "2",
			wantPrice:     4.01, // second price (PMP 4.0) + $0.01
			wantValidBids: 2,
		},
		{
			name: "Second price below floor defaults to floor",
			bids: []Bid{
				{DealID: "1", DealType: PG, Price: 5.0, AdID: "ad1"},
				{DealID: "2", DealType: Open, Price: 0.5, AdID: "ad2"}, // below floor, filtered
			},
			floor:         1.0,
			wantWinnerID:  "1",
			wantPrice:     1.0, // only one valid bid — clears at floor
			wantValidBids: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Always second-price mode (ENABLE_FIRST_PRICE unset)
			result := RunAuction(tt.bids, tt.floor, logger, AuctionTypeSecondPrice)

			if result.ValidBidsCount != tt.wantValidBids {
				t.Errorf("ValidBidsCount = %d, want %d", result.ValidBidsCount, tt.wantValidBids)
			}
			if result.NoBidReason != tt.wantNoBid {
				t.Errorf("NoBidReason = %v, want %v", result.NoBidReason, tt.wantNoBid)
			}
			if tt.wantWinnerID != "" {
				if result.Winner == nil {
					t.Fatalf("expected winner %s, got nil", tt.wantWinnerID)
				}
				if result.Winner.DealID != tt.wantWinnerID {
					t.Errorf("Winner ID = %s, want %s", result.Winner.DealID, tt.wantWinnerID)
				}
				if result.ClearingPrice != tt.wantPrice {
					t.Errorf("ClearingPrice = %v, want %v", result.ClearingPrice, tt.wantPrice)
				}
			} else if result.Winner != nil {
				t.Errorf("expected no winner, got %v", result.Winner.DealID)
			}
		})
	}
}

// ── first-price auction tests ──

func TestRunAuctionFirstPrice(t *testing.T) {
	logger := zap.NewNop()

	// Enable the feature flag for this test group.
	os.Setenv("ENABLE_FIRST_PRICE", "true")
	defer os.Unsetenv("ENABLE_FIRST_PRICE")

	tests := []struct {
		name         string
		bids         []Bid
		floor        float64
		wantWinnerID string
		wantPrice    float64
		wantNoBid    string
	}{
		{
			name: "Single bid — clears at winner's own price (not floor)",
			bids: []Bid{
				{DealID: "1", DealType: Open, Price: 5.0, AdID: "ad1"},
			},
			floor:        2.0,
			wantWinnerID: "1",
			wantPrice:    5.0, // first-price: pays own bid, not floor
		},
		{
			name: "Multiple bids — winner pays own bid, not second price",
			bids: []Bid{
				{DealID: "1", DealType: Open, Price: 3.0, AdID: "ad1"},
				{DealID: "2", DealType: Open, Price: 5.0, AdID: "ad2"},
				{DealID: "3", DealType: Open, Price: 4.0, AdID: "ad3"},
			},
			floor:        1.0,
			wantWinnerID: "2",
			wantPrice:    5.0, // first-price: winner pays 5.0, NOT 4.01
		},
		{
			name: "Floor enforcement still applies in first-price mode",
			bids: []Bid{
				{DealID: "1", DealType: Open, Price: 1.0, AdID: "ad1"}, // below floor
				{DealID: "2", DealType: Open, Price: 0.5, AdID: "ad2"}, // below floor
			},
			floor:     2.0,
			wantNoBid: "no_valid_bids",
		},
		{
			name: "Tie bids — first in priority order wins, pays own price",
			bids: []Bid{
				{DealID: "1", DealType: Open, Price: 4.0, AdID: "ad1"},
				{DealID: "2", DealType: Open, Price: 4.0, AdID: "ad2"},
			},
			floor:        1.0,
			wantWinnerID: "1", // stable sort keeps first encountered at same price
			wantPrice:    4.0,
		},
		{
			name: "PG wins regardless of price; pays own PG price (first-price)",
			bids: []Bid{
				{DealID: "1", DealType: Open, Price: 9.0, AdID: "ad1"},
				{DealID: "2", DealType: PG, Price: 2.0, AdID: "ad2"},
			},
			floor:        1.0,
			wantWinnerID: "2",
			wantPrice:    2.0, // first-price: PG winner pays their 2.0, not Open's 9.0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RunAuction(tt.bids, tt.floor, logger, AuctionTypeFirstPrice)

			if result.NoBidReason != tt.wantNoBid {
				t.Errorf("NoBidReason = %q, want %q", result.NoBidReason, tt.wantNoBid)
			}
			if tt.wantWinnerID != "" {
				if result.Winner == nil {
					t.Fatalf("expected winner %s, got nil", tt.wantWinnerID)
				}
				if result.Winner.DealID != tt.wantWinnerID {
					t.Errorf("Winner DealID = %s, want %s", result.Winner.DealID, tt.wantWinnerID)
				}
				if result.ClearingPrice != tt.wantPrice {
					t.Errorf("ClearingPrice = %v, want %v", result.ClearingPrice, tt.wantPrice)
				}
			} else if result.Winner != nil {
				t.Errorf("expected no winner, got %s", result.Winner.DealID)
			}
		})
	}
}

// TestRunAuctionFirstPriceGated verifies that when ENABLE_FIRST_PRICE is NOT set,
// passing auctionType=1 still falls through to second-price logic.
func TestRunAuctionFirstPriceGated(t *testing.T) {
	logger := zap.NewNop()

	// Feature flag NOT set — second-price must apply even if at=1.
	os.Unsetenv("ENABLE_FIRST_PRICE")

	bids := []Bid{
		{DealID: "1", DealType: Open, Price: 5.0, AdID: "ad1"},
		{DealID: "2", DealType: Open, Price: 3.0, AdID: "ad2"},
	}
	result := RunAuction(bids, 1.0, logger, AuctionTypeFirstPrice)

	if result.Winner == nil {
		t.Fatal("expected a winner")
	}
	// Without the flag, second-price applies: 3.0 + 0.01 = 3.01
	if result.ClearingPrice != 3.01 {
		t.Errorf("expected second-price 3.01 when flag unset, got %v", result.ClearingPrice)
	}
}
