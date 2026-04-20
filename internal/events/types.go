package events

import (
	"encoding/json"
	"time"
)

// ImpressionEvent represents a won ad impression.
type ImpressionEvent struct {
	RequestID    string    `json:"request_id"`
	BidID        string    `json:"bid_id"`
	ImpressionID string    `json:"impression_id"`
	CampaignID   string    `json:"campaign_id"`
	CreativeID   string    `json:"creative_id"`
	WinPrice     float64   `json:"win_price"`
	UserID       string    `json:"user_id"`
	GeoCountry   string    `json:"geo_country"`
	GeoCity      string    `json:"geo_city"`
	DeviceType   int       `json:"device_type"`
	Timestamp    time.Time `json:"timestamp"`
	FloorPrice   float64   `json:"floor_price"`
}

// ToJSON serializes the event to a JSON byte slice.
func (e ImpressionEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// ClickEvent represents an ad click.
type ClickEvent struct {
	RequestID    string    `json:"request_id"`
	ImpressionID string    `json:"impression_id"`
	CampaignID   string    `json:"campaign_id"`
	CreativeID   string    `json:"creative_id"`
	UserID       string    `json:"user_id"`
	Timestamp    time.Time `json:"timestamp"`
}

// ToJSON serializes the event to a JSON byte slice.
func (e ClickEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}
