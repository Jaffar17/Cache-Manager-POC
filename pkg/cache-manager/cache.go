package cache_manager

import (
	"context"
	"time"
)

// CacheMode defines the default caching strategy for the cache instance.
type CacheMode int

const (
	// ModeBothLevels writes to both L1 and L2 by default, with warmup enabled.
	ModeBothLevels CacheMode = iota
	// ModeL1Only writes only to L1 by default.
	ModeL1Only
	// ModeL2Only writes only to L2 by default, with warmup disabled.
	ModeL2Only
)

// Cache represents the multi-level cache facade exposed to callers.
type Cache interface {
	Get(ctx context.Context, key string, dest any, opts CacheOptions) (bool, error)
	Set(ctx context.Context, key string, value any, opts CacheOptions) error
	Delete(ctx context.Context, key string) error
}

// CacheOptions controls both read/write behavior and target levels for cache operations.
// This unified struct ensures consistency between Get and Set operations.
type CacheOptions struct {
	// Target levels (used by both Get and Set)
	TargetL1 *bool // nil = use mode default, true/false = override
	TargetL2 *bool // nil = use mode default, true/false = override

	// TTL options (only used by Set, ignored by Get)
	L1TTL time.Duration // TTL for L1 (0 = use default)
	L2TTL time.Duration // TTL for L2 (0 = use default)
}

// This function takes the per-call options and makes sure both layers end up with a valid duration
func (o CacheOptions) normalize(defaultL1, defaultL2 time.Duration) (time.Duration, time.Duration) {
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

