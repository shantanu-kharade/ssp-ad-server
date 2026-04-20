package campaign

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/yourusername/ssp-adserver/internal/cache"
	"go.uber.org/zap"
)

// RedisCache is a read-through caching decorator for the campaign Repository.
// It caches campaign lookups in Redis and delegates writes to the underlying repo.
type RedisCache struct {
	repo  Repository
	redis *cache.RedisClient
	log   *zap.Logger
}

// NewRedisCache creates a new caching Repository decorator wrapping the given repo.
func NewRedisCache(repo Repository, redis *cache.RedisClient, log *zap.Logger) Repository {
	return &RedisCache{
		repo:  repo,
		redis: redis,
		log:   log,
	}
}

func (c *RedisCache) CreateCampaign(ctx context.Context, camp *Campaign) error {
	err := c.repo.CreateCampaign(ctx, camp)
	if err != nil {
		return err
	}
	// Invalidate active campaigns list
	_ = c.redis.Delete(ctx, "campaigns:active")
	// Cache the new campaign snapshot
	c.setCampaignCache(ctx, camp)
	return nil
}

func (c *RedisCache) GetActiveCampaigns(ctx context.Context) ([]Campaign, error) {
	key := "campaigns:active"
	val, err := c.redis.Get(ctx, key)
	if err == nil && val != "" {
		var campaigns []Campaign
		if err := json.Unmarshal([]byte(val), &campaigns); err == nil {
			return campaigns, nil
		}
	}

	// Cache miss or unmarshal error, fetch from DB
	campaigns, err := c.repo.GetActiveCampaigns(ctx)
	if err != nil {
		return nil, err
	}

	// Update cache
	if data, err := json.Marshal(campaigns); err == nil {
		_ = c.redis.SetEX(ctx, key, string(data), 30*time.Second)
	}

	return campaigns, nil
}

func (c *RedisCache) GetCampaignByID(ctx context.Context, id string) (*Campaign, error) {
	key := fmt.Sprintf("campaign:%s", id)
	val, err := c.redis.Get(ctx, key)
	if err == nil && val != "" {
		var camp Campaign
		if err := json.Unmarshal([]byte(val), &camp); err == nil {
			return &camp, nil
		}
	}

	// Cache miss, fetch from DB
	camp, err := c.repo.GetCampaignByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update cache
	c.setCampaignCache(ctx, camp)
	return camp, nil
}

func (c *RedisCache) UpdateCampaign(ctx context.Context, camp *Campaign) error {
	err := c.repo.UpdateCampaign(ctx, camp)
	if err != nil {
		return err
	}
	// Invalidate active campaigns list
	_ = c.redis.Delete(ctx, "campaigns:active")

	// Update cache
	updatedCamp, _ := c.repo.GetCampaignByID(ctx, camp.ID)
	if updatedCamp != nil {
		c.setCampaignCache(ctx, updatedCamp)
	}
	return nil
}

func (c *RedisCache) AddCreative(ctx context.Context, cr *Creative) error {
	err := c.repo.AddCreative(ctx, cr)
	if err != nil {
		return err
	}

	// Update cache
	updatedCamp, _ := c.repo.GetCampaignByID(ctx, cr.CampaignID)
	if updatedCamp != nil {
		c.setCampaignCache(ctx, updatedCamp)
		_ = c.redis.Delete(ctx, "campaigns:active") // Invalidate list
	}
	return nil
}

func (c *RedisCache) setCampaignCache(ctx context.Context, camp *Campaign) {
	key := fmt.Sprintf("campaign:%s", camp.ID)
	if data, err := json.Marshal(camp); err == nil {
		_ = c.redis.SetEX(ctx, key, string(data), 60*time.Second)
	}
}
