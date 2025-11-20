package cache

import (
	"context"
	"time"
)

// Cache represents the multi-level cache facade exposed to callers.
type Cache interface {
	Get(ctx context.Context, key string, dest any) (bool, error)
	Set(ctx context.Context, key string, value any, ttlOptions SetTTLOptions) error
	Delete(ctx context.Context, key string) error
}

// SetTTLOptions controls TTL behavior for cache writes.
type SetTTLOptions struct {
	L1TTL time.Duration
	L2TTL time.Duration
}

// This function takes the per-call options and makes sure both layers end up with a valid duration
func (o SetTTLOptions) normalize(defaultL1, defaultL2 time.Duration) (time.Duration, time.Duration) {
	l1 := o.L1TTL
	if l1 <= 0 {
		l1 = defaultL1
	}
	l2 := o.L2TTL
	if l2 <= 0 {
		l2 = defaultL2
	}
	return l1, l2
}
