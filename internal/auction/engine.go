package auction

import (
	"sort"
	"time"

	"go.uber.org/zap"
)

// RunAuction executes the auction logic to determine the winning bid and clearing price.
// It filters bids below the floor, sorts them by priority and price, and calculates
// the second price.
func RunAuction(bids []Bid, floor float64, log *zap.Logger) AuctionResult {
	start := time.Now()

	totalBids := len(bids)

	// Filter bids below floor
	validBids := EnforceFloor(bids, floor, log)
	validCount := len(validBids)

	if validCount == 0 {
		return AuctionResult{
			Winner:            nil,
			TotalBidsReceived: totalBids,
			ValidBidsCount:    0,
			AuctionDurationMs: float64(time.Since(start).Microseconds()) / 1000.0,
			NoBidReason:       "no_valid_bids",
		}
	}

	// Sort valid bids: highest priority first, then highest price
	sort.Slice(validBids, func(i, j int) bool {
		pi := Priority(validBids[i].DealType)
		pj := Priority(validBids[j].DealType)
		if pi != pj {
			return pi > pj
		}
		return validBids[i].Price > validBids[j].Price
	})

	winner := validBids[0]
	clearingPrice := floor

	if validCount > 1 {
		secondPrice := validBids[1].Price
		if secondPrice > floor {
			clearingPrice = secondPrice
		}
	}

	return AuctionResult{
		Winner:            &winner,
		ClearingPrice:     clearingPrice,
		TotalBidsReceived: totalBids,
		ValidBidsCount:    validCount,
		AuctionDurationMs: float64(time.Since(start).Microseconds()) / 1000.0,
		NoBidReason:       "",
	}
}
