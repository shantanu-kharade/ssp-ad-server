// Package cache provides Redis-backed caching utilities for the SSP ad server.
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Cache sentinel errors for distinguishing failure modes.
var (
	// ErrCacheMiss indicates the requested key does not exist in Redis.
	ErrCacheMiss = errors.New("cache miss")
	// ErrCacheTimeout indicates a Redis operation exceeded its deadline.
	ErrCacheTimeout = errors.New("cache timeout")
	// ErrCacheUnavailable indicates Redis is unreachable.
	ErrCacheUnavailable = errors.New("cache unavailable")
)

// RedisClient wraps a go-redis client with structured logging and
// connection pooling configured for the ad server hot path.
type RedisClient struct {
	client *redis.Client
	log    *zap.Logger
}

// NewRedisClient creates a new RedisClient from the given URL string.
// It configures connection pooling and logs a warning if Redis is unreachable.
func NewRedisClient(url string, log *zap.Logger) *RedisClient {
	if url == "" {
		url = "redis://localhost:6379"
	}

	opt, err := redis.ParseURL(url)
	if err != nil {
		log.Warn("failed to parse redis url, falling back to default", zap.Error(err))
		opt = &redis.Options{
			Addr: "localhost:6379",
		}
	}

	opt.PoolSize = 1000
	opt.MinIdleConns = 100

	client := redis.NewClient(opt)

	// TODO: context.Background() is used here because this runs during initialization,
	// before any request context is available.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Warn("redis is unreachable on startup (cache will be bypassed)", zap.Error(err))
	} else {
		log.Info("connected to redis successfully")
	}

	return &RedisClient{
		client: client,
		log:    log,
	}
}

// Get retrieves the value for the given key from Redis.
// Returns ErrCacheMiss if the key does not exist.
func (r *RedisClient) Get(ctx context.Context, key string) (string, error) {
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", ErrCacheMiss
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "", ErrCacheTimeout
		}
		return "", ErrCacheUnavailable
	}
	return val, nil
}

// Set stores a key-value pair in Redis with no expiration.
func (r *RedisClient) Set(ctx context.Context, key string, value interface{}) error {
	err := r.client.Set(ctx, key, value, 0).Err()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ErrCacheTimeout
		}
		return ErrCacheUnavailable
	}
	return nil
}

// SetEX stores a key-value pair in Redis with the given expiration duration.
func (r *RedisClient) SetEX(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	err := r.client.Set(ctx, key, value, expiration).Err()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ErrCacheTimeout
		}
		return ErrCacheUnavailable
	}
	return nil
}

// Delete removes the given key from Redis.
func (r *RedisClient) Delete(ctx context.Context, key string) error {
	err := r.client.Del(ctx, key).Err()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ErrCacheTimeout
		}
		return ErrCacheUnavailable
	}
	return nil
}

// Incr atomically increments the integer value stored at key by 1 and returns
// the new value. If the key does not exist it is created with value 1.
func (r *RedisClient) Incr(ctx context.Context, key string) (int64, error) {
	val, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, ErrCacheTimeout
		}
		return 0, ErrCacheUnavailable
	}
	return val, nil
}

// Expire sets a TTL on the given key. It is a no-op if the key does not exist.
func (r *RedisClient) Expire(ctx context.Context, key string, ttl time.Duration) error {
	err := r.client.Expire(ctx, key, ttl).Err()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ErrCacheTimeout
		}
		return ErrCacheUnavailable
	}
	return nil
}

// Close shuts down the Redis connection pool.
func (r *RedisClient) Close() error {
	return r.client.Close()
}
