package cache_manager

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache is the L2 cache backed by Redis.
type RedisCache struct {
	client *redis.Client
}

// NewRedisCache builds a Redis-backed cache.
func NewRedisCache(client *redis.Client) (*RedisCache, error) {
	if client == nil {
		return nil, errors.New("redis client is required")
	}
	return &RedisCache{client: client}, nil
}

// Get fetches a key returning raw bytes when present.
func (r *RedisCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if r == nil || r.client == nil {
		return nil, false, errors.New("redis cache not initialized")
	}

	cmd := r.client.Get(ctx, key)
	if err := cmd.Err(); err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, err
	}

	data, err := cmd.Bytes()
	if err != nil {
		return nil, false, err
	}

	return data, true, nil
}

// Set stores the payload with the provided TTL.
func (r *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if r == nil || r.client == nil {
		return errors.New("redis cache not initialized")
	}
	return r.client.Set(ctx, key, value, ttl).Err()
}

// Delete removes key from Redis.
func (r *RedisCache) Delete(ctx context.Context, key string) error {
	if r == nil || r.client == nil {
		return errors.New("redis cache not initialized")
	}
	return r.client.Del(ctx, key).Err()
}

// SubscribeInvalidations is a placeholder for future pub/sub invalidation support.
func (r *RedisCache) SubscribeInvalidations(ctx context.Context, channel string, handler func(context.Context, string)) error {
	return errors.New("pub/sub invalidation not implemented")
}
