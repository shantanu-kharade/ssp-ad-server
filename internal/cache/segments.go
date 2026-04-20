package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// SegmentFetcher retrieves and stores user audience segments in Redis.
type SegmentFetcher struct {
	redis *RedisClient
	log   *zap.Logger
}

// NewSegmentFetcher creates a new SegmentFetcher backed by the given RedisClient.
func NewSegmentFetcher(redis *RedisClient, log *zap.Logger) *SegmentFetcher {
	return &SegmentFetcher{
		redis: redis,
		log:   log,
	}
}

// FetchUserSegments returns the audience segments for the given user from Redis.
// It enforces a 10ms timeout and returns an empty slice on cache miss or error.
func (f *SegmentFetcher) FetchUserSegments(ctx context.Context, userID string) []string {
	if userID == "" {
		return []string{}
	}

	key := fmt.Sprintf("seg:%s", userID)

	// Enforce 10ms timeout max
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	val, err := f.redis.Get(timeoutCtx, key)
	if err != nil {
		if err != ErrCacheMiss {
			f.log.Warn("failed to fetch segments from cache", zap.String("user_id", userID), zap.Error(err))
		}
		return []string{}
	}

	var segments []string
	if err := json.Unmarshal([]byte(val), &segments); err != nil {
		f.log.Warn("failed to unmarshal segments", zap.String("user_id", userID), zap.Error(err))
		return []string{}
	}

	return segments
}

// SetUserSegments stores the given audience segments for a user in Redis with a 300s TTL.
func (f *SegmentFetcher) SetUserSegments(ctx context.Context, userID string, segments []string) error {
	if userID == "" {
		return nil
	}

	key := fmt.Sprintf("seg:%s", userID)

	data, err := json.Marshal(segments)
	if err != nil {
		return fmt.Errorf("failed to marshal segments: %w", err)
	}

	// Set with 300s TTL and a 10ms timeout context for writing
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	err = f.redis.SetEX(timeoutCtx, key, string(data), 300*time.Second)
	if err != nil {
		f.log.Warn("failed to save segments to cache", zap.String("user_id", userID), zap.Error(err))
		return err
	}

	return nil
}
