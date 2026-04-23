package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"

	"github.com/yourusername/ssp-adserver/internal/ads"
	"github.com/yourusername/ssp-adserver/internal/cache"
	"github.com/yourusername/ssp-adserver/internal/campaign"
	"github.com/yourusername/ssp-adserver/internal/config"
	"github.com/yourusername/ssp-adserver/internal/dsp"
	"github.com/yourusername/ssp-adserver/internal/events"
	"github.com/yourusername/ssp-adserver/internal/handler"
	"github.com/yourusername/ssp-adserver/internal/identity"
	"github.com/yourusername/ssp-adserver/internal/models"
)

type mockRepo struct{}
func (m mockRepo) CreateCampaign(ctx context.Context, c *campaign.Campaign) error { return nil }
func (m mockRepo) GetActiveCampaigns(ctx context.Context) ([]campaign.Campaign, error) { return nil, nil }
func (m mockRepo) GetCampaignByID(ctx context.Context, id string) (*campaign.Campaign, error) { return nil, nil }
func (m mockRepo) UpdateCampaign(ctx context.Context, c *campaign.Campaign) error { return nil }
func (m mockRepo) AddCreative(ctx context.Context, cr *campaign.Creative) error { return nil }

func TestMultiImpBidHandler(t *testing.T) {
	// 1. Enable the multi-imp environment variable
	os.Setenv("ENABLE_MULTI_IMP", "true")
	defer os.Unsetenv("ENABLE_MULTI_IMP")

	logger, _ := zap.NewDevelopment()
	defer logger.Sync() //nolint:errcheck

	// 2. Create a Mock DSP server that returns bids for Imp 0 and Imp 2, but not Imp 1.
	mockDSPServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req models.BidRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		resp := models.BidResponse{
			ID: req.ID,
			SeatBid: []models.SeatBid{
				{
					Seat: "mock-dsp",
					Bid: []models.Bid{
						{
							ID:    "dsp-bid-0",
							ImpID: "imp-0", // Winner for Imp 0
							Price: 2.50,
							CrID:  "cr-123",
						},
						{
							ID:    "dsp-bid-2",
							ImpID: "imp-2", // Winner for Imp 2
							Price: 1.50,
							CrID:  "cr-456",
						},
					},
				},
			},
			Cur: "USD",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockDSPServer.Close()

	// 3. Setup Dependencies
	// - DSP Client & Fanout
	dspCfg := dsp.Config{
		Name:      "MockDSP",
		URL:       mockDSPServer.URL,
		TimeoutMs: 500, // plenty of time for test
	}
	client := dsp.NewClient(dspCfg, logger)
	fanout := dsp.NewFanoutCoordinator([]*dsp.Client{client}, logger)

	// - Segment Fetcher (mocked with an empty redis config to just fail fast)
	redisURL := "redis://127.0.0.1:9999" // dead port
	redisClient := cache.NewRedisClient(redisURL, logger)
	segments := cache.NewSegmentFetcher(redisClient, logger)

	// - DB/Campaign Service
	campaignSvc := campaign.NewService(mockRepo{})

	// - Identity
	resolver := identity.NewResolver()
	consent := identity.NewValidator()

	// - House Ads (empty so no fallback)
	houseAds := ads.NewHouseAdProvider(config.ServerConfig{})

	// - Events Producer: use a dead-broker config; NewEventProducer won't dial on
	//   construction, it only fails on first Write — which our test never reaches
	//   because the event worker runs async and the test exits cleanly.
	kafkaCfg := &config.Config{
		Kafka: config.KafkaConfig{
			Brokers:              []string{"127.0.0.1:9999"},
			ImpressionTopic:      "impressions",
			ClickTopic:           "clicks",
			WriterBatchSize:      1,
			WriterBatchTimeoutMs: 10,
		},
	}
	producer, err := events.NewEventProducer(kafkaCfg, logger)
	if err != nil {
		t.Fatalf("failed to create event producer: %v", err)
	}

	// 4. Instantiate Handler & Fiber App
	bidHandler := handler.NewBidHandler(logger, segments, resolver, consent, fanout, houseAds, campaignSvc, producer)
	app := fiber.New()
	app.Post("/bid", bidHandler.HandleBid)

	// 5. Construct a 3-Impression Bid Request
	reqBody := models.BidRequest{
		ID: "test-req-123",
		Imp: []models.Impression{
			{ID: "imp-0", BidFloor: 1.00},
			{ID: "imp-1", BidFloor: 1.00},
			{ID: "imp-2", BidFloor: 1.00},
		},
		Device: &models.Device{IP: "1.2.3.4", UA: "test-agent"},
		User:   &models.User{ID: "u-1"},
	}

	reqBytes, _ := json.Marshal(reqBody)
	httpReq := httptest.NewRequest(http.MethodPost, "/bid", bytes.NewReader(reqBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	// The consent validator checks for a non-empty X-Consent header.
	// Without this, the handler returns 200 with NBR=0 before reaching the auction.
	httpReq.Header.Set("X-Consent", "1")

	// 6. Execute Request
	resp, err := app.Test(httpReq, -1) // -1 disables timeout
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	// 7. Verify Outcome
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d", resp.StatusCode)
	}

	var bidResp models.BidResponse
	if err := json.NewDecoder(resp.Body).Decode(&bidResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// We expect SeatBids for imp-0 and imp-2
	var foundImp0, foundImp1, foundImp2 bool

	for _, sb := range bidResp.SeatBid {
		for _, b := range sb.Bid {
			if b.ImpID == "imp-0" {
				foundImp0 = true
			}
			if b.ImpID == "imp-1" {
				foundImp1 = true
			}
			if b.ImpID == "imp-2" {
				foundImp2 = true
			}
		}
	}

	if !foundImp0 {
		t.Errorf("Expected bid for imp-0, but none found")
	}
	if foundImp1 {
		t.Errorf("Did NOT expect bid for imp-1, but one was found")
	}
	if !foundImp2 {
		t.Errorf("Expected bid for imp-2, but none found")
	}

	// Wait briefly so async event worker can fail safely
	time.Sleep(100 * time.Millisecond)
}
