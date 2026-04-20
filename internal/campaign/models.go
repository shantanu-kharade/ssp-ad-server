package campaign

import (
	"encoding/json"
	"time"
)

// Campaign represents an advertising campaign.
type Campaign struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	AdvertiserID  string          `json:"advertiser_id"`
	Status        string          `json:"status"` // "active", "paused", "completed"
	BudgetCents   int64           `json:"budget_cents"`
	SpentCents    int64           `json:"spent_cents"`
	StartDate     time.Time       `json:"start_date"`
	EndDate       time.Time       `json:"end_date"`
	CreatedAt     time.Time       `json:"created_at"`
	Creatives     []Creative      `json:"creatives"`
	TargetingRules []TargetingRule `json:"targeting_rules"`
}

// Creative represents an ad creative associated with a campaign.
type Creative struct {
	ID         string    `json:"id"`
	CampaignID string    `json:"campaign_id"`
	Format     string    `json:"format"` // "banner", "video", "native"
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	AdMarkup   string    `json:"ad_markup"`
	ClickURL   string    `json:"click_url"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// TargetingRule represents a targeting criteria for a campaign.
type TargetingRule struct {
	ID         string          `json:"id"`
	CampaignID string          `json:"campaign_id"`
	RuleType   string          `json:"rule_type"` // "geo", "segment", "device"
	RuleValue  json.RawMessage `json:"rule_value"`
	CreatedAt  time.Time       `json:"created_at"`
}

// RuleValueGeo represents the value for a "geo" rule.
type RuleValueGeo struct {
	IPPrefix string `json:"ip_prefix"`
}

// RuleValueSegment represents the value for a "segment" rule.
type RuleValueSegment struct {
	SegmentIDs []string `json:"segment_ids"`
}

// RuleValueDevice represents the value for a "device" rule.
type RuleValueDevice struct {
	UASubstring string `json:"ua_substring"`
}
