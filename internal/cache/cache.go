package cache

import (
	"context"
	"time"
)

// Cache represents the multi-level cache facade exposed to callers.
type Cache interface {
	Get(ctx context.Context, key string, dest any) (bool, error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}
