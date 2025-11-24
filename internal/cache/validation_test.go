package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestStrictValidation_BothLevelsRequired tests that ModeBothLevels requires both caches
func TestStrictValidation_BothLevelsRequired(t *testing.T) {
	t.Parallel()

	serializer := &JSONSerializer{}
	l1 := newMemoryRawCache()

	// Should error: ModeBothLevels requires both
	_, err := NewMultiLevelCache(l1, nil, serializer, MultiLevelConfig{
		Mode: ModeBothLevels,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ModeBothLevels requires both L1 and L2 caches to be configured")
}

// TestStrictValidation_ModeMatchesConfig tests that mode must match configured levels
func TestStrictValidation_ModeMatchesConfig(t *testing.T) {
	t.Parallel()

	serializer := &JSONSerializer{}
	l1 := newMemoryRawCache()

	// Only L1 configured but mode is ModeL2Only
	_, err := NewMultiLevelCache(l1, nil, serializer, MultiLevelConfig{
		Mode: ModeL2Only,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ModeL2Only requires L2 cache to be configured")
}

// TestStrictValidation_OverridesNotAllowed tests that overrides fail when only one level configured
func TestStrictValidation_OverridesNotAllowed(t *testing.T) {
	t.Parallel()

	serializer := &JSONSerializer{}
	l1 := newMemoryRawCache()

	// Only L1 configured with ModeL1Only (valid)
	cache, err := NewMultiLevelCache(l1, nil, serializer, MultiLevelConfig{
		Mode: ModeL1Only,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Try to override to L2 - should fail
	err = cache.Set(ctx, "test", "value", SetTTLOptions{
		TargetL1: BoolPtr(false),
		TargetL2: BoolPtr(true),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "level overrides not allowed")
}

// TestStrictValidation_OverridesAllowed tests that overrides work when both levels configured
func TestStrictValidation_OverridesAllowed(t *testing.T) {
	t.Parallel()

	serializer := &JSONSerializer{}
	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	// Both levels configured
	cache, err := NewMultiLevelCache(l1, l2, serializer, MultiLevelConfig{
		Mode: ModeBothLevels,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Overrides should work - write only to L1
	err = cache.Set(ctx, "test", "value", SetTTLOptions{
		TargetL1: BoolPtr(true),
		TargetL2: BoolPtr(false),
	})
	require.NoError(t, err)

	// Verify only L1 has the data
	require.Contains(t, l1.data, "test")
	require.NotContains(t, l2.data, "test")
}

// TestModeL1Only tests that ModeL1Only only uses L1
func TestModeL1Only(t *testing.T) {
	t.Parallel()

	serializer := &JSONSerializer{}
	l1 := newMemoryRawCache()

	cache, err := NewMultiLevelCache(l1, nil, serializer, MultiLevelConfig{
		Mode: ModeL1Only,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Set should only write to L1
	err = cache.Set(ctx, "key", "value", SetTTLOptions{})
	require.NoError(t, err)
	require.Contains(t, l1.data, "key")

	// Get should only check L1
	var result string
	found, err := cache.Get(ctx, "key", &result)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "value", result)
}

// TestModeL2Only tests that ModeL2Only only uses L2
func TestModeL2Only(t *testing.T) {
	t.Parallel()

	serializer := &JSONSerializer{}
	l2 := newMemoryRawCache()

	cache, err := NewMultiLevelCache(nil, l2, serializer, MultiLevelConfig{
		Mode: ModeL2Only,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Set should only write to L2
	err = cache.Set(ctx, "key", "value", SetTTLOptions{})
	require.NoError(t, err)
	require.Contains(t, l2.data, "key")

	// Get should only check L2
	var result string
	found, err := cache.Get(ctx, "key", &result)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "value", result)
}

// TestModeBothLevels_NoWarmupInL2OnlyMode tests that warmup doesn't happen when mode is L2Only
func TestModeBothLevels_NoWarmupInL2OnlyMode(t *testing.T) {
	t.Parallel()

	serializer := &JSONSerializer{}
	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	// Both configured but mode is L2Only
	cache, err := NewMultiLevelCache(l1, l2, serializer, MultiLevelConfig{
		Mode: ModeL2Only,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Manually set in L2
	testData := []byte(`"test-value"`)
	require.NoError(t, l2.Set(ctx, "key", testData, time.Minute))

	// Get should not warm L1 when mode is ModeL2Only
	var result string
	found, err := cache.Get(ctx, "key", &result)
	require.NoError(t, err)
	require.True(t, found)

	// L1 should NOT be warmed because mode is ModeL2Only
	require.NotContains(t, l1.data, "key")
}

// TestModeBothLevels_WarmupEnabled tests that warmup happens when mode is ModeBothLevels
func TestModeBothLevels_WarmupEnabled(t *testing.T) {
	t.Parallel()

	serializer := &JSONSerializer{}
	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	cache, err := NewMultiLevelCache(l1, l2, serializer, MultiLevelConfig{
		Mode: ModeBothLevels,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Set in L2 only
	testData := []byte(`"test-value"`)
	require.NoError(t, l2.Set(ctx, "key", testData, time.Minute))

	// Get should warm L1 when mode is ModeBothLevels
	var result string
	found, err := cache.Get(ctx, "key", &result)
	require.NoError(t, err)
	require.True(t, found)

	// L1 should be warmed because mode is ModeBothLevels
	require.Contains(t, l1.data, "key")
}

// TestTargetOverrides tests per-call targeting of specific levels
func TestTargetOverrides(t *testing.T) {
	t.Parallel()

	serializer := &JSONSerializer{}
	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	cache, err := NewMultiLevelCache(l1, l2, serializer, MultiLevelConfig{
		Mode: ModeBothLevels,
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Write only to L2
	err = cache.Set(ctx, "key1", "value1", SetTTLOptions{
		TargetL1: BoolPtr(false),
		TargetL2: BoolPtr(true),
	})
	require.NoError(t, err)
	require.NotContains(t, l1.data, "key1")
	require.Contains(t, l2.data, "key1")

	// Write only to L1
	err = cache.Set(ctx, "key2", "value2", SetTTLOptions{
		TargetL1: BoolPtr(true),
		TargetL2: BoolPtr(false),
	})
	require.NoError(t, err)
	require.Contains(t, l1.data, "key2")
	require.NotContains(t, l2.data, "key2")

	// Write to both (explicit)
	err = cache.Set(ctx, "key3", "value3", SetTTLOptions{
		TargetL1: BoolPtr(true),
		TargetL2: BoolPtr(true),
	})
	require.NoError(t, err)
	require.Contains(t, l1.data, "key3")
	require.Contains(t, l2.data, "key3")
}

// TestDefaultModeIsBothLevels tests that mode defaults to ModeBothLevels
func TestDefaultModeIsBothLevels(t *testing.T) {
	t.Parallel()

	serializer := &JSONSerializer{}
	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	// Don't specify mode - should default to ModeBothLevels
	cache, err := NewMultiLevelCache(l1, l2, serializer, MultiLevelConfig{})
	require.NoError(t, err)

	ctx := context.Background()

	// Should write to both levels by default
	err = cache.Set(ctx, "key", "value", SetTTLOptions{})
	require.NoError(t, err)
	require.Contains(t, l1.data, "key")
	require.Contains(t, l2.data, "key")
}

