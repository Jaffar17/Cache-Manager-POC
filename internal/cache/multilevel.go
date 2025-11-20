package cache

import (
	"context"
	"errors"
	"log"
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
	// L1DefaultTTL is used when SetTTLOptions do not specify an L1 TTL.
	L1DefaultTTL time.Duration
	// L2DefaultTTL is used when SetTTLOptions do not specify an L2 TTL.
	L2DefaultTTL time.Duration
}

// MultiLevelCache composes an L1 and L2 cache with cache-aside semantics.
type MultiLevelCache struct {
	l1           RawCache
	l2           RawCache
	serializer   Serializer
	warmupTTL    time.Duration
	l1DefaultTTL time.Duration
	l2DefaultTTL time.Duration
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

	l1TTL := cfg.L1DefaultTTL
	if l1TTL <= 0 {
		l1TTL = 5 * time.Minute
	}

	l2TTL := cfg.L2DefaultTTL
	if l2TTL <= 0 {
		l2TTL = 5 * time.Minute
	}

	return &MultiLevelCache{
		l1:           l1,
		l2:           l2,
		serializer:   serializer,
		warmupTTL:    warmTTL,
		l1DefaultTTL: l1TTL,
		l2DefaultTTL: l2TTL,
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
			log.Printf("[cache] hit level=L1 key=%s", key)
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
		log.Printf("[cache] miss key=%s", key)
		return false, nil
	}

	if err := m.serializer.Unmarshal(data, dest); err != nil {
		return false, err
	}

	log.Printf("[cache] hit level=L2 key=%s (warming L1)", key)
	if m.l1 != nil {
		// best-effort warmup; ignore errors to avoid failing the request.
		_ = m.l1.Set(ctx, key, data, m.warmupTTL)
	}

	return true, nil
}

// Set serializes value and persists to both cache levels.
func (m *MultiLevelCache) Set(ctx context.Context, key string, value any, opts SetTTLOptions) error {
	if m == nil {
		return errors.New("cache not initialized")
	}

	data, err := m.serializer.Marshal(value)
	if err != nil {
		return err
	}

	l1TTL, l2TTL := opts.normalize(m.l1DefaultTTL, m.l2DefaultTTL)

	if m.l1 != nil {
		if err := m.l1.Set(ctx, key, data, l1TTL); err != nil {
			return err
		}
	}

	if m.l2 != nil {
		if err := m.l2.Set(ctx, key, data, l2TTL); err != nil {
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
		} else {
			log.Printf("[cache] delete level=L1 key=%s", key)
		}
	}

	if m.l2 != nil {
		if err := m.l2.Delete(ctx, key); err != nil && firstErr == nil {
			firstErr = err
		} else if err == nil {
			log.Printf("[cache] delete level=L2 key=%s", key)
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
