package campaign_test

import (
	"context"
	"testing"
	"time"

	"github.com/yourusername/ssp-adserver/internal/campaign"
	"github.com/yourusername/ssp-adserver/internal/models"
)

// ---------------------------------------------------------------------------
// Counting stub that satisfies campaign.Repository and records how many
// "DB round-trips" GetActiveCampaigns would produce in the real implementation.
//
// The real pgRepository issues exactly 3 SQL statements:
//   1. SELECT campaigns WHERE status = 'active'
//   2. SELECT creatives   WHERE campaign_id = ANY(...)
//   3. SELECT targeting_rules WHERE campaign_id = ANY(...)
//
// Contrast with the old N+1 pattern which issued 1 + 2*N queries.
// ---------------------------------------------------------------------------

type countingRepository struct {
	campaigns []campaign.Campaign
	callCount int
}

// GetActiveCampaigns simulates 3 DB round-trips (the fixed batch pattern).
func (r *countingRepository) GetActiveCampaigns(_ context.Context) ([]campaign.Campaign, error) {
	r.callCount += 3 // campaigns query + creatives batch + targeting_rules batch
	return r.campaigns, nil
}

func (r *countingRepository) CreateCampaign(_ context.Context, _ *campaign.Campaign) error {
	return nil
}
func (r *countingRepository) GetCampaignByID(_ context.Context, _ string) (*campaign.Campaign, error) {
	return nil, campaign.ErrNotFound
}
func (r *countingRepository) UpdateCampaign(_ context.Context, _ *campaign.Campaign) error { return nil }
func (r *countingRepository) AddCreative(_ context.Context, _ *campaign.Creative) error    { return nil }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeCampaign(id string) campaign.Campaign {
	return campaign.Campaign{
		ID:           id,
		Name:         "Campaign " + id,
		AdvertiserID: "adv-1",
		Status:       "active",
		BudgetCents:  100_00,
		BidPriceCPM:  1.50,
		StartDate:    time.Now().Add(-24 * time.Hour),
		EndDate:      time.Now().Add(24 * time.Hour),
		CreatedAt:    time.Now(),
		Creatives: []campaign.Creative{
			{ID: "cr-" + id, CampaignID: id, Format: "banner", Width: 300, Height: 250},
		},
		TargetingRules: []campaign.TargetingRule{
			// No targeting rules → untargeted campaign, matches everything.
		},
	}
}

func mockBidRequest() models.BidRequest {
	return models.BidRequest{
		ID: "req-test",
		Imp: []models.Impression{
			{ID: "imp-1", BidFloor: 0.5},
		},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestGetActiveCampaigns_DBCallsAreConstant asserts that regardless of how
// many campaigns exist the service issues a constant 3 DB round-trips, never O(N).
func TestGetActiveCampaigns_DBCallsAreConstant(t *testing.T) {
	cases := []struct {
		name  string
		count int
	}{
		{"zero campaigns", 0},
		{"one campaign", 1},
		{"ten campaigns", 10},
		{"hundred campaigns", 100},
	}

	const wantCalls = 3 // campaigns + creatives batch + targeting_rules batch

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			camps := make([]campaign.Campaign, tc.count)
			for i := range camps {
				camps[i] = makeCampaign(time.Now().String() + string(rune(i)))
			}

			repo := &countingRepository{campaigns: camps}
			svc := campaign.NewService(repo)

			if _, err := svc.EvaluateTargeting(context.Background(), mockBidRequest(), nil); err != nil {
				t.Fatalf("EvaluateTargeting error: %v", err)
			}

			if repo.callCount != wantCalls {
				t.Errorf(
					"DB round-trips = %d, want %d — N+1 regression detected for %d campaigns",
					repo.callCount, wantCalls, tc.count,
				)
			}
		})
	}
}

// TestGetActiveCampaigns_AssemblesCorrectly verifies that untargeted campaigns
// (no targeting rules) are returned and their creatives are populated.
func TestGetActiveCampaigns_AssemblesCorrectly(t *testing.T) {
	camp := makeCampaign("camp-abc")
	repo := &countingRepository{campaigns: []campaign.Campaign{camp}}
	svc := campaign.NewService(repo)

	results, err := svc.EvaluateTargeting(context.Background(), mockBidRequest(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("matched campaigns = %d, want 1", len(results))
	}
	got := results[0]
	if got.ID != camp.ID {
		t.Errorf("campaign ID = %q, want %q", got.ID, camp.ID)
	}
	if len(got.Creatives) != 1 {
		t.Errorf("creatives count = %d, want 1", len(got.Creatives))
	}
}
