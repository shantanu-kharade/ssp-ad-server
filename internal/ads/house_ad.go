package ads

import (
	"github.com/yourusername/ssp-adserver/internal/config"
	"github.com/yourusername/ssp-adserver/internal/models"
)

// HouseAdProvider provides fallback house ads when no DSP bids clear the auction.
type HouseAdProvider struct {
	config config.ServerConfig
}

// NewHouseAdProvider creates a new HouseAdProvider.
func NewHouseAdProvider(cfg config.ServerConfig) *HouseAdProvider {
	return &HouseAdProvider{
		config: cfg,
	}
}

// GetFallbackAd returns a pre-configured house ad bid if house ads are enabled.
// Returns nil if house ads are disabled.
func (p *HouseAdProvider) GetFallbackAd(imp models.Impression) *models.Bid {
	if !p.config.HouseAdsEnabled {
		return nil
	}

	w, h := 0, 0
	if imp.Banner != nil {
		w = imp.Banner.W
		h = imp.Banner.H
	}

	return &models.Bid{
		ID:       "house-bid-" + imp.ID,
		ImpID:    imp.ID,
		Price:    p.config.HouseAdPrice,
		AdID:     p.config.HouseAdCampaignID,
		CrID:     p.config.HouseAdCreativeID,
		AdMarkup: p.config.HouseAdMarkup,
		W:        w,
		H:        h,
		Ext: map[string]interface{}{
			"house_ad": true,
		},
	}
}
