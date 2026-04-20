package auction

import (
	"go.uber.org/zap"
)

// EnforceFloor filters out bids that are below the specified floor price.
// Rejected bids are logged at the DEBUG level.
// This is a pure function and does not mutate the original slice.
func EnforceFloor(bids []Bid, floor float64, log *zap.Logger) []Bid {
	var validBids []Bid
	for _, bid := range bids {
		if bid.Price >= floor {
			validBids = append(validBids, bid)
		} else {
			if log != nil {
				log.Debug("bid rejected: below floor",
					zap.Float64("bid_price", bid.Price),
					zap.Float64("floor", floor),
					zap.String("dsp", bid.DSPName),
					zap.String("deal_id", bid.DealID),
				)
			}
		}
	}
	return validBids
}
