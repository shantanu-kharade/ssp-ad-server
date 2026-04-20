package dsp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/yourusername/ssp-adserver/internal/auction"
	"github.com/yourusername/ssp-adserver/internal/models"
	"github.com/yourusername/ssp-adserver/internal/resilience"
	"go.uber.org/zap"
)

var (
	ErrNoBid           = errors.New("no bid (204)")
	ErrTimeout         = errors.New("timeout")
	ErrInvalidResponse = errors.New("invalid response")
)

// Client represents a single DSP endpoint client.
type Client struct {
	config Config
	http   *http.Client
	cb     *resilience.DSPCircuitBreaker
}

// NewClient creates a new DSP client.
func NewClient(cfg Config, log *zap.Logger) *Client {
	return &Client{
		config: cfg,
		http: &http.Client{
			// The overall timeout is enforced by the context in FetchBid,
			// but we can set a fallback timeout on the client itself.
			Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond,
		},
		cb: resilience.NewDSPCircuitBreaker(cfg.Name, log),
	}
}

// Name returns the DSP's configured name.
func (c *Client) Name() string {
	return c.config.Name
}

// CircuitBreaker returns the client's internal circuit breaker.
func (c *Client) CircuitBreaker() *resilience.DSPCircuitBreaker {
	return c.cb
}

// FetchBid sends a BidRequest to the DSP and parses the response.
func (c *Client) FetchBid(ctx context.Context, req models.BidRequest) ([]auction.Bid, error) {
	res, err := c.cb.Execute(ctx, func() (interface{}, error) {
		bids, fetchErr := c.doFetch(ctx, req)
		// ErrNoBid is a valid RTB outcome, not a system failure.
		if errors.Is(fetchErr, ErrNoBid) {
			return []auction.Bid{}, nil // Treat as success for circuit breaker
		}
		return bids, fetchErr
	})

	if err != nil {
		if errors.Is(err, resilience.ErrCircuitOpen) {
			return nil, resilience.ErrCircuitOpen
		}
		return nil, err
	}

	bids := res.([]auction.Bid)
	if len(bids) == 0 {
		return nil, ErrNoBid
	}

	return bids, nil
}

func (c *Client) doFetch(ctx context.Context, req models.BidRequest) ([]auction.Bid, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.URL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "SSP-AdServer/1.0")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		// Context deadlines and client timeouts both produce timeout errors.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, ErrNoBid
	}

	if resp.StatusCode != http.StatusOK {
		return nil, ErrInvalidResponse
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ErrInvalidResponse
	}

	var bidResp models.BidResponse
	if err := json.Unmarshal(respBody, &bidResp); err != nil {
		return nil, ErrInvalidResponse
	}

	var parsedBids []auction.Bid
	for _, seatBid := range bidResp.SeatBid {
		for _, bid := range seatBid.Bid {
			parsedBids = append(parsedBids, auction.Bid{
				DealID:     "open-deal", // Mocked deal mapping 
				DealType:   auction.Open,
				Price:      bid.Price,
				AdID:       bid.AdID,
				DSPName:    c.config.Name,
				CreativeID: bid.CrID,
			})
		}
	}
	return parsedBids, nil
}
