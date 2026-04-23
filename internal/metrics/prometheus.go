package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	BidRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ssp_bid_requests_total",
			Help: "Total number of bid requests processed",
		},
		[]string{"publisher_id", "status"},
	)

	BidLatencySeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "ssp_bid_latency_seconds",
			Help:    "Latency of bid requests",
			Buckets: []float64{0.01, 0.025, 0.05, 0.075, 0.1, 0.15, 0.2},
		},
	)

	DSPBidLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ssp_dsp_bid_latency_seconds",
			Help:    "Latency of bid requests to DSPs",
			Buckets: []float64{0.01, 0.025, 0.05, 0.075, 0.1, 0.15, 0.2},
		},
		[]string{"dsp_id"},
	)

	AuctionClearingPrice = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "ssp_auction_clearing_price",
			Help:    "Clearing price of auctions in CPM $",
			Buckets: []float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0},
		},
	)

	ActiveGoroutines = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ssp_active_goroutines",
			Help: "Current depth of the impression worker pool queue",
		},
	)

	// AuctionTypeTotal tracks how many auctions ran under each clearing-price
	// regime. Label "type" is either "first_price" or "second_price".
	AuctionTypeTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ssp_auction_type_total",
			Help: "Total number of auctions run per auction type (first_price or second_price)",
		},
		[]string{"type"},
	)
)

func init() {
	prometheus.MustRegister(
		BidRequestsTotal,
		BidLatencySeconds,
		DSPBidLatencySeconds,
		AuctionClearingPrice,
		ActiveGoroutines,
		AuctionTypeTotal,
	)
}
