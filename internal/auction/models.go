package auction

// DealType represents the type of deal for a bid.
type DealType string

const (
	PG   DealType = "PG"   // Programmatic Guaranteed
	PMP  DealType = "PMP"  // Private Marketplace
	Open DealType = "Open" // Open Auction
)

// Bid represents a single bid submitted to the auction engine.
type Bid struct {
	DealID     string
	DealType   DealType
	Price      float64
	AdID       string
	DSPName    string
	CreativeID string
	ImpID      string
	// NURL is the DSP's win notice URL. The SSP must fire a GET to this URL
	// (with ${AUCTION_PRICE} substituted) when this bid wins the auction.
	NURL string
}

// AuctionResult holds the outcome of an auction.
type AuctionResult struct {
	Winner            *Bid
	ClearingPrice     float64
	TotalBidsReceived int
	ValidBidsCount    int
	AuctionDurationMs float64
	NoBidReason       string
}
