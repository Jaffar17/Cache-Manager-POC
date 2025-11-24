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
	// Mode defines the default caching strategy. Defaults to ModeBothLevels.
	Mode CacheMode
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
	l1             RawCache
	l2             RawCache
	serializer     Serializer
	mode           CacheMode
	allowOverrides bool // true only when both L1 and L2 are configured
	warmupTTL      time.Duration
	l1DefaultTTL   time.Duration
	l2DefaultTTL   time.Duration
}

// NewMultiLevelCache builds a MultiLevelCache with sensible defaults.
func NewMultiLevelCache(l1 RawCache, l2 RawCache, serializer Serializer, cfg MultiLevelConfig) (*MultiLevelCache, error) {
	if serializer == nil {
		return nil, ErrSerializerMissing
	}

	// Validate mode against provided caches
	mode := cfg.Mode
	switch mode {
	case ModeL1Only:
		if l1 == nil {
			return nil, errors.New("ModeL1Only requires L1 cache to be configured")
		}
		// Ensure mode matches configuration
		if l2 != nil {
			log.Printf("[cache] Warning: Both L1 and L2 configured but mode is ModeL1Only. L2 will be ignored by default.")
		}
	case ModeL2Only:
		if l2 == nil {
			return nil, errors.New("ModeL2Only requires L2 cache to be configured")
		}
		// Ensure mode matches configuration
		if l1 != nil {
			log.Printf("[cache] Warning: Both L1 and L2 configured but mode is ModeL2Only. L1 will be ignored by default.")
		}
	case ModeBothLevels:
		if l1 == nil || l2 == nil {
			return nil, errors.New("ModeBothLevels requires both L1 and L2 caches to be configured")
		}
	default:
		// Default to ModeBothLevels if not specified
		mode = ModeBothLevels
		if l1 == nil || l2 == nil {
			return nil, errors.New("ModeBothLevels (default) requires both L1 and L2 caches to be configured")
		}
	}

	// Strict validation: if only one level configured, verify mode matches exactly
	if l1 != nil && l2 == nil && mode != ModeL1Only {
		return nil, errors.New("only L1 configured but mode is not ModeL1Only; set mode to ModeL1Only or configure L2")
	}
	if l1 == nil && l2 != nil && mode != ModeL2Only {
		return nil, errors.New("only L2 configured but mode is not ModeL2Only; set mode to ModeL2Only or configure L1")
	}

	// Per-call overrides are only allowed when both levels are configured
	allowOverrides := (l1 != nil && l2 != nil)

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
		l1:             l1,
		l2:             l2,
		serializer:     serializer,
		mode:           mode,
		allowOverrides: allowOverrides,
		warmupTTL:      warmTTL,
		l1DefaultTTL:   l1TTL,
		l2DefaultTTL:   l2TTL,
	}, nil
}

// Get implements Cache.Get with cache-aside semantics and mode-aware warmup.
func (m *MultiLevelCache) Get(ctx context.Context, key string, dest any) (bool, error) {
	if m == nil {
		return false, errors.New("cache not initialized")
	}

	// Check L1 first if available
	if m.l1 != nil {
		if data, ok, err := m.l1.Get(ctx, key); err != nil {
			return false, err
		} else if ok {
			log.Printf("[cache] hit level=L1 key=%s", key)
			return true, m.serializer.Unmarshal(data, dest)
		}
	}

	// Check L2 if available
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

	log.Printf("[cache] hit level=L2 key=%s", key)

	// Only warm L1 if mode is ModeBothLevels (cache-aside semantics)
	// If mode is ModeL2Only, user explicitly chose not to use L1
	if m.l1 != nil && m.mode == ModeBothLevels {
		log.Printf("[cache] warming L1 for key=%s", key)
		// best-effort warmup; ignore errors to avoid failing the request.
		_ = m.l1.Set(ctx, key, data, m.warmupTTL)
	}

	return true, nil
}

// Set serializes value and persists to cache levels based on mode and options.
func (m *MultiLevelCache) Set(ctx context.Context, key string, value any, opts SetTTLOptions) error {
	if m == nil {
		return errors.New("cache not initialized")
	}

	// Check if user is trying to override levels when not allowed
	if !m.allowOverrides && (opts.TargetL1 != nil || opts.TargetL2 != nil) {
		return errors.New("level overrides not allowed: both L1 and L2 must be configured to use TargetL1/TargetL2 options")
	}

	data, err := m.serializer.Marshal(value)
	if err != nil {
		return err
	}

	l1TTL, l2TTL := opts.normalize(m.l1DefaultTTL, m.l2DefaultTTL)

	// Determine target levels based on mode
	var targetL1, targetL2 bool
	switch m.mode {
	case ModeBothLevels:
		targetL1 = true
		targetL2 = true
	case ModeL1Only:
		targetL1 = true
		targetL2 = false
	case ModeL2Only:
		targetL1 = false
		targetL2 = true
	}

	// Apply per-call overrides if provided (only when allowOverrides is true)
	if opts.TargetL1 != nil {
		targetL1 = *opts.TargetL1
	}
	if opts.TargetL2 != nil {
		targetL2 = *opts.TargetL2
	}

	// Validate that targeted levels are configured
	if targetL1 && m.l1 == nil {
		return errors.New("L1 target requested but L1 cache not configured")
	}
	if targetL2 && m.l2 == nil {
		return errors.New("L2 target requested but L2 cache not configured")
	}

	// Write to targeted levels
	if targetL1 {
		if err := m.l1.Set(ctx, key, data, l1TTL); err != nil {
			return err
		}
		log.Printf("[cache] set level=L1 key=%s", key)
	}

	if targetL2 {
		if err := m.l2.Set(ctx, key, data, l2TTL); err != nil {
			return err
		}
		log.Printf("[cache] set level=L2 key=%s", key)
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
