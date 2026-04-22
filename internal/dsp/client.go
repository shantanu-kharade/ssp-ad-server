package dsp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/yourusername/ssp-adserver/internal/auction"
	"github.com/yourusername/ssp-adserver/internal/models"
	"github.com/yourusername/ssp-adserver/internal/resilience"
	"go.uber.org/zap"
)

// bufPool recycles *bytes.Buffer instances across DSP bid request serializations.
// At high QPS (5,000+), json.Marshal allocates a new []byte on every call.
// Pooling the buffer eliminates those short-lived allocations, reducing GC pressure
// and CPU spikes from constant collection. Each buffer is Reset() before reuse
// so stale data is never forwarded.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

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
			// The overall per-request deadline is enforced by the context passed into
			// FetchBid. The transport-level timeout below acts as an absolute safety net
			// in case a context is ever constructed without a deadline.
			Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond,

			Transport: &http.Transport{
				// --- Connection Pool Tuning ---
				//
				// MaxIdleConnsPerHost: Go's default is 2.
				// At 5,000 QPS with 3 DSPs each pod sends ~1,667 req/s per DSP.
				// With only 2 idle connections, almost every request that arrives
				// while both connections are in-flight must open a new TCP socket:
				// that means a full TCP handshake (~1-5ms RTT) on each miss.
				// 100 idle connections per host ensures the pool is deep enough to
				// absorb realistic concurrency spikes without exhausting keep-alives.
				MaxIdleConnsPerHost: 100,

				// MaxIdleConns: total idle connections across ALL hosts.
				// With 3 DSP hosts × 100 per-host = 300 max needed. Setting this
				// equal to 3×MaxIdleConnsPerHost prevents the global pool from
				// silently capping per-host connections below our target.
				MaxIdleConns: 300,

				// IdleConnTimeout: how long an unused connection stays in the pool
				// before it is closed. 90s matches common server-side keepalive
				// timeouts (nginx default: 75s) with a small margin. Too short =
				// connections expire before they can be reused; too long = wasted
				// file descriptors and potential silent half-close issues.
				IdleConnTimeout: 90 * time.Second,

				// DisableKeepAlives: must be false (the zero value) so that TCP
				// connections are reused across requests. Setting this to true would
				// force a new handshake on every single DSP call, negating all of
				// the tuning above.
				DisableKeepAlives: false,
			},
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
	// Acquire a pooled buffer, encode the request JSON into it, then immediately
	// return the buffer to the pool once the HTTP request body is set up.
	// bytes.NewReader copies the buffer bytes, so it is safe to recycle the buffer
	// before the HTTP request is dispatched.
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	if err := json.NewEncoder(buf).Encode(req); err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.URL, bytes.NewReader(buf.Bytes()))
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
