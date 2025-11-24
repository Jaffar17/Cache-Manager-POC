package cache_manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	// L1DefaultTTL is used when CacheOptions do not specify an L1 TTL.
	L1DefaultTTL time.Duration
	// L2DefaultTTL is used when CacheOptions do not specify an L2 TTL.
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
			slog.Warn("cache mode mismatch",
				"mode", "ModeL1Only",
				"l1_configured", true,
				"l2_configured", true,
				"message", "L2 will be ignored by default")
		}
	case ModeL2Only:
		if l2 == nil {
			return nil, errors.New("ModeL2Only requires L2 cache to be configured")
		}
		// Ensure mode matches configuration
		if l1 != nil {
			slog.Warn("cache mode mismatch",
				"mode", "ModeL2Only",
				"l1_configured", true,
				"l2_configured", true,
				"message", "L1 will be ignored by default")
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
// It checks endpoint-level options first (via opts), then falls back to service-level mode.
func (m *MultiLevelCache) Get(ctx context.Context, key string, dest any, opts CacheOptions) (bool, error) {
	if m == nil {
		return false, errors.New("cache not initialized")
	}

	// Check if user is trying to override levels when not allowed
	if !m.allowOverrides && (opts.TargetL1 != nil || opts.TargetL2 != nil) {
		return false, errors.New("level overrides not allowed: both L1 and L2 must be configured to use TargetL1/TargetL2 options")
	}

	// Determine which levels to check based on mode (service-level default)
	var checkL1, checkL2 bool
	checkL1, checkL2 = m.determineCacheLevel()

	// Apply per-call overrides if provided (endpoint-level takes precedence)
	checkL1, checkL2 = m.applyEndpointLevelOverrides(opts, checkL1, checkL2)

	// Validate that at least one level is targeted
	if !checkL1 && !checkL2 {
		return false, errors.New("Get operation requires at least one cache level to be checked")
	}

	// Validate that targeted levels are configured
	if checkL1 && m.l1 == nil {
		return false, errors.New("L1 target requested but L1 cache not configured")
	}
	if checkL2 && m.l2 == nil {
		return false, errors.New("L2 target requested but L2 cache not configured")
	}

	// Check L1 if mode/options allow it
	if checkL1 && m.l1 != nil {
		fmt.Printf("🔍 [GET] Checking L1 cache for key: %s\n", key)
		if data, ok, err := m.l1.Get(ctx, key); err != nil {
			fmt.Printf("❌ [GET] L1 error for key %s: %v\n", key, err)
			return false, err
		} else if ok {
			fmt.Printf("✅ [GET] L1 HIT! Key: %s | Data size: %d bytes | Preview: %s\n", key, len(data), previewData(data))
			if err := m.serializer.Unmarshal(data, dest); err != nil {
				fmt.Printf("❌ [GET] L1 unmarshal error for key %s: %v\n", key, err)
				return false, err
			}
			fmt.Printf("✨ [GET] Successfully returned value from L1\n")
			return true, nil
		} else {
			fmt.Printf("❌ [GET] L1 MISS for key: %s\n", key)
		}
	}

	// Check L2 if mode/options allow it
	if !checkL2 || m.l2 == nil {
		fmt.Printf("❌ [GET] OVERALL MISS for key: %s (L2 not checked)\n", key)
		return false, nil
	}

	fmt.Printf("🔍 [GET] Checking L2 cache for key: %s\n", key)
	data, ok, err := m.l2.Get(ctx, key)
	if err != nil {
		fmt.Printf("❌ [GET] L2 error for key %s: %v\n", key, err)
		return false, err
	}
	if !ok {
		fmt.Printf("❌ [GET] L2 MISS for key: %s\n", key)
		fmt.Printf("❌ [GET] OVERALL MISS - key not found in any cache level\n")
		return false, nil
	}

	fmt.Printf("✅ [GET] L2 HIT! Key: %s | Data size: %d bytes | Preview: %s\n", key, len(data), previewData(data))
	if err := m.serializer.Unmarshal(data, dest); err != nil {
		fmt.Printf("❌ [GET] L2 unmarshal error for key %s: %v\n", key, err)
		return false, err
	}

	// Only warm L1 if:
	// 1. L1 checking was enabled (either by mode or override)
	// 2. L1 is configured
	// 3. Mode is ModeBothLevels and no explicit L1 override was provided
	//    (we don't warm L1 if user explicitly chose to skip it)
	if checkL1 && m.l1 != nil && m.mode == ModeBothLevels && opts.TargetL1 == nil {
		fmt.Printf("🔥 [GET] Warming L1 from L2 hit | Key: %s | TTL: %v | Data size: %d bytes\n", key, m.warmupTTL, len(data))
		// best-effort warmup; ignore errors to avoid failing the request.
		if err := m.l1.Set(ctx, key, data, m.warmupTTL); err != nil {
			fmt.Printf("⚠️  [GET] L1 warmup failed (continuing): %v\n", err)
		} else {
			fmt.Printf("✨ [GET] L1 warmup successful!\n")
		}
	}

	fmt.Printf("✨ [GET] Successfully returned value from L2\n")
	return true, nil
}

func (m *MultiLevelCache) applyEndpointLevelOverrides(opts CacheOptions, checkL1 bool, checkL2 bool) (bool, bool) {
	if opts.TargetL1 != nil {
		checkL1 = *opts.TargetL1
	}
	if opts.TargetL2 != nil {
		checkL2 = *opts.TargetL2
	}
	return checkL1, checkL2
}

func (m *MultiLevelCache) determineCacheLevel() (bool, bool) {
	var checkL1, checkL2 bool
	switch m.mode {
	case ModeBothLevels:
		checkL1 = true
		checkL2 = true
	case ModeL1Only:
		checkL1 = true
		checkL2 = false
	case ModeL2Only:
		checkL1 = false
		checkL2 = true
	}
	return checkL1, checkL2
}

// Set serializes value and persists to cache levels based on mode and options.
// It checks endpoint-level options first (via opts), then falls back to service-level mode.
func (m *MultiLevelCache) Set(ctx context.Context, key string, value any, opts CacheOptions) error {
	if m == nil {
		return errors.New("cache not initialized")
	}

	// Check if user is trying to override levels when not allowed
	if !m.allowOverrides && (opts.TargetL1 != nil || opts.TargetL2 != nil) {
		return errors.New("level overrides not allowed: both L1 and L2 must be configured to use TargetL1/TargetL2 options")
	}

	data, err := m.serializer.Marshal(value)
	if err != nil {
		fmt.Printf("❌ [SET] Marshal error for key %s: %v\n", key, err)
		return err
	}

	fmt.Printf("📦 [SET] Serialized value | Key: %s | Data size: %d bytes | Preview: %s\n", key, len(data), previewData(data))

	l1TTL, l2TTL := opts.normalize(m.l1DefaultTTL, m.l2DefaultTTL)

	// Determine target levels based on mode
	var targetL1, targetL2 bool
	targetL1, targetL2 = m.determineCacheLevel()

	// Apply per-call overrides if provided (endpoint-level takes precedence)
	targetL1, targetL2 = m.applyEndpointLevelOverrides(opts, targetL1, targetL2)

	// Validate that at least one level is targeted
	if !targetL1 && !targetL2 {
		return errors.New("Set operation requires at least one cache level to be targeted")
	}

	// Validate that targeted levels are configured
	if targetL1 && m.l1 == nil {
		return errors.New("L1 target requested but L1 cache not configured")
	}
	if targetL2 && m.l2 == nil {
		return errors.New("L2 target requested but L2 cache not configured")
	}

	// Write to targeted levels with best-effort semantics
	// Attempt both writes regardless of individual failures to maximize cache availability
	var l1Err, l2Err error

	if targetL1 {
		fmt.Printf("💾 [SET] Writing to L1 | Key: %s | TTL: %v | Size: %d bytes\n", key, l1TTL, len(data))
		if err := m.l1.Set(ctx, key, data, l1TTL); err != nil {
			l1Err = err
			fmt.Printf("❌ [SET] L1 write FAILED | Key: %s | Error: %v\n", key, err)
		} else {
			fmt.Printf("✅ [SET] L1 write SUCCESS | Key: %s\n", key)
		}
	}

	if targetL2 {
		fmt.Printf("💾 [SET] Writing to L2 | Key: %s | TTL: %v | Size: %d bytes\n", key, l2TTL, len(data))
		if err := m.l2.Set(ctx, key, data, l2TTL); err != nil {
			l2Err = err
			fmt.Printf("❌ [SET] L2 write FAILED | Key: %s | Error: %v\n", key, err)
		} else {
			fmt.Printf("✅ [SET] L2 write SUCCESS | Key: %s\n", key)
		}
	}

	// Only return error if all targeted levels failed
	if targetL1 && targetL2 {
		if l1Err != nil && l2Err != nil {
			return fmt.Errorf("both cache levels failed: L1=%w, L2=%v", l1Err, l2Err)
		}
		return nil
	}

	// For single-level operations, return the error
	if l1Err != nil {
		return l1Err
	}
	if l2Err != nil {
		return l2Err
	}

	return nil
}

// Delete removes the key from both levels.
func (m *MultiLevelCache) Delete(ctx context.Context, key string) error {
	if m == nil {
		return errors.New("cache not initialized")
	}

	fmt.Printf("🗑️  [DELETE] Deleting key: %s\n", key)
	var firstErr error

	if m.l1 != nil {
		fmt.Printf("🗑️  [DELETE] Deleting from L1 | Key: %s\n", key)
		if err := m.l1.Delete(ctx, key); err != nil {
			firstErr = err
			fmt.Printf("❌ [DELETE] L1 delete FAILED | Key: %s | Error: %v\n", key, err)
		} else {
			fmt.Printf("✅ [DELETE] L1 delete SUCCESS | Key: %s\n", key)
		}
	}

	if m.l2 != nil {
		fmt.Printf("🗑️  [DELETE] Deleting from L2 | Key: %s\n", key)
		if err := m.l2.Delete(ctx, key); err != nil && firstErr == nil {
			firstErr = err
			fmt.Printf("❌ [DELETE] L2 delete FAILED | Key: %s | Error: %v\n", key, err)
		} else if err == nil {
			fmt.Printf("✅ [DELETE] L2 delete SUCCESS | Key: %s\n", key)
		}
	}

	if firstErr == nil {
		fmt.Printf("✨ [DELETE] Successfully deleted from all cache levels\n")
	}

	return firstErr
}

// previewData returns a preview of the data for logging (max 100 chars)
func previewData(data []byte) string {
	if len(data) == 0 {
		return "<empty>"
	}
	preview := string(data)
	if len(preview) > 100 {
		preview = preview[:100] + "..."
	}
	return preview
}

