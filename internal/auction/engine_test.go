package auction

import (
	"testing"

	"go.uber.org/zap"
)

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
			wantPrice:     5.0, // second price is Open bid's 5.0
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
			wantPrice:     4.0,
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
			wantPrice:     4.0,
			wantValidBids: 2,
		},
		{
			name: "Second price below floor defaults to floor",
			bids: []Bid{
				{DealID: "1", DealType: PG, Price: 5.0, AdID: "ad1"},
				{DealID: "2", DealType: Open, Price: 0.5, AdID: "ad2"}, // below floor, will be filtered
			},
			floor:         1.0,
			wantWinnerID:  "1",
			wantPrice:     1.0, // second valid price doesn't exist, clears at floor
			wantValidBids: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RunAuction(tt.bids, tt.floor, logger)

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
