package cache

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrSerializerMissing indicates serializer dependency absent.
	ErrSerializerMissing = errors.New("serializer is required")
)

// RawCache represents a low-level cache storing raw bytes.
type RawCache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// MultiLevelConfig exposes optional tuning knobs.
type MultiLevelConfig struct {
	// WarmupTTL is the TTL applied when populating L1 from an L2 hit.
	// Defaults to 5 minutes when zero.
	WarmupTTL time.Duration
}

// MultiLevelCache composes an L1 and L2 cache with cache-aside semantics.
type MultiLevelCache struct {
	l1         RawCache
	l2         RawCache
	serializer Serializer
	warmupTTL  time.Duration
}

// NewMultiLevelCache builds a MultiLevelCache with sensible defaults.
func NewMultiLevelCache(l1 RawCache, l2 RawCache, serializer Serializer, cfg MultiLevelConfig) (*MultiLevelCache, error) {
	if serializer == nil {
		return nil, ErrSerializerMissing
	}

	warmTTL := cfg.WarmupTTL
	if warmTTL <= 0 {
		warmTTL = 5 * time.Minute
	}

	return &MultiLevelCache{
		l1:         l1,
		l2:         l2,
		serializer: serializer,
		warmupTTL:  warmTTL,
	}, nil
}

// Get implements Cache.Get with cache-aside semantics.
func (m *MultiLevelCache) Get(ctx context.Context, key string, dest any) (bool, error) {
	if m == nil {
		return false, errors.New("cache not initialized")
	}

	if m.l1 != nil {
		if data, ok, err := m.l1.Get(ctx, key); err != nil {
			return false, err
		} else if ok {
			return true, m.serializer.Unmarshal(data, dest)
		}
	}

	if m.l2 == nil {
		return false, nil
	}

	data, ok, err := m.l2.Get(ctx, key)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	if err := m.serializer.Unmarshal(data, dest); err != nil {
		return false, err
	}

	if m.l1 != nil {
		// best-effort warmup; ignore errors to avoid failing the request.
		_ = m.l1.Set(ctx, key, data, m.warmupTTL)
	}

	return true, nil
}

// Set serializes value and persists to both cache levels.
func (m *MultiLevelCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if m == nil {
		return errors.New("cache not initialized")
	}

	data, err := m.serializer.Marshal(value)
	if err != nil {
		return err
	}

	if m.l1 != nil {
		if err := m.l1.Set(ctx, key, data, ttl); err != nil {
			return err
		}
	}

	if m.l2 != nil {
		if err := m.l2.Set(ctx, key, data, ttl); err != nil {
			return err
		}
	}

	return nil
}

// Delete removes the key from both levels.
func (m *MultiLevelCache) Delete(ctx context.Context, key string) error {
	if m == nil {
		return errors.New("cache not initialized")
	}

	var firstErr error

	if m.l1 != nil {
		if err := m.l1.Delete(ctx, key); err != nil {
			firstErr = err
		}
	}

	if m.l2 != nil {
		if err := m.l2.Delete(ctx, key); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (m *MultiLevelCache) warmTTL() time.Duration {
	if m == nil {
		return 0
	}
	return m.warmupTTL
}
