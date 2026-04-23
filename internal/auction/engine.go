package auction

import (
	"os"
	"sort"
	"time"

	"go.uber.org/zap"

	"github.com/yourusername/ssp-adserver/internal/metrics"
)

const (
	// AuctionTypeFirstPrice is the OpenRTB at=1 value.
	AuctionTypeFirstPrice = 1
	// AuctionTypeSecondPrice is the OpenRTB at=2 value (also the default when at=0/unset).
	AuctionTypeSecondPrice = 2

	// minIncrement is the $0.01 CPM added to the second-highest bid in a
	// second-price auction, per standard SSP practice.
	minIncrement = 0.01
)

// RunAuction executes the auction logic to determine the winning bid and clearing price.
//
// auctionType mirrors OpenRTB's req.at field:
//   - 1 (first-price):  clearing price == winner's own bid price.
//   - 2 (second-price): clearing price == second-highest bid + $0.01 (or floor if only one bid).
//   - 0 / anything else: treated as second-price for backwards compatibility.
//
// First-price mode is only active when the ENABLE_FIRST_PRICE environment variable
// is set to "true". If not set, second-price is always used regardless of auctionType,
// preserving the existing behaviour exactly.
func RunAuction(bids []Bid, floor float64, log *zap.Logger, auctionType int) AuctionResult {
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

	// Sort valid bids: highest priority first, then highest price.
	// Winner selection is identical for both auction types.
	sort.Slice(validBids, func(i, j int) bool {
		pi := Priority(validBids[i].DealType)
		pj := Priority(validBids[j].DealType)
		if pi != pj {
			return pi > pj
		}
		return validBids[i].Price > validBids[j].Price
	})

	winner := validBids[0]

	// Determine clearing price based on auction type.
	// First-price is only enabled when the feature flag is set.
	enableFirstPrice := os.Getenv("ENABLE_FIRST_PRICE") == "true"

	var clearingPrice float64
	var auctionTypeLabel string

	if enableFirstPrice && auctionType == AuctionTypeFirstPrice {
		// First-price: winner pays their own submitted bid price.
		clearingPrice = winner.Price
		auctionTypeLabel = "first_price"
	} else {
		// Second-price (default): winner pays the second-highest bid price,
		// or the floor if there is only one valid bid.
		clearingPrice = floor
		if validCount > 1 {
			secondPrice := validBids[1].Price
			if secondPrice+minIncrement > floor {
				clearingPrice = secondPrice + minIncrement
			}
		}
		auctionTypeLabel = "second_price"
	}

	metrics.AuctionTypeTotal.WithLabelValues(auctionTypeLabel).Inc()

	return AuctionResult{
		Winner:            &winner,
		ClearingPrice:     clearingPrice,
		TotalBidsReceived: totalBids,
		ValidBidsCount:    validCount,
		AuctionDurationMs: float64(time.Since(start).Microseconds()) / 1000.0,
		NoBidReason:       "",
	}
}
