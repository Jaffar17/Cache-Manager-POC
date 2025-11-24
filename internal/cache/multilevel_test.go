package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type memoryRawCache struct {
	data map[string][]byte
	ttl  map[string]time.Duration
}

func newMemoryRawCache() *memoryRawCache {
	return &memoryRawCache{
		data: make(map[string][]byte),
		ttl:  make(map[string]time.Duration),
	}
}

func (m *memoryRawCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	val, ok := m.data[key]
	if !ok {
		return nil, false, nil
	}
	cp := make([]byte, len(val))
	copy(cp, val)
	return cp, true, nil
}

func (m *memoryRawCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[key] = cp
	m.ttl[key] = ttl
	return nil
}

func (m *memoryRawCache) Delete(_ context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func TestMultiLevelCacheL1MissL2HitWarmsL1(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()
	payload := map[string]string{"value": "from-l2"}

	serializer := JSONSerializer{}
	bytes, err := serializer.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, l2.Set(context.Background(), "key", bytes, time.Minute))

	ml, err := NewMultiLevelCache(l1, l2, serializer, MultiLevelConfig{
		Mode:         ModeBothLevels, // Explicitly set mode
		WarmupTTL:    time.Minute,
		L1DefaultTTL: time.Minute,
		L2DefaultTTL: time.Minute,
	})
	require.NoError(t, err)

	var result map[string]string
	found, err := ml.Get(context.Background(), "key", &result)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, payload, result)

	_, ok := l1.data["key"]
	require.True(t, ok, "expected L1 warm after L2 hit in ModeBothLevels")
}

func TestMultiLevelCacheMiss(t *testing.T) {
	t.Parallel()

	ml, err := NewMultiLevelCache(
		newMemoryRawCache(),
		newMemoryRawCache(),
		JSONSerializer{},
		MultiLevelConfig{
			Mode:         ModeBothLevels, // Explicitly set mode
			L1DefaultTTL: time.Minute,
			L2DefaultTTL: time.Minute,
		},
	)
	require.NoError(t, err)

	found, err := ml.Get(context.Background(), "missing", &struct{}{})
	require.NoError(t, err)
	require.False(t, found)
}

func TestMultiLevelCacheSetWritesBoth(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	ml, err := NewMultiLevelCache(l1, l2, JSONSerializer{}, MultiLevelConfig{
		Mode:         ModeBothLevels, // Explicitly set mode
		L1DefaultTTL: time.Minute,
		L2DefaultTTL: time.Minute,
	})
	require.NoError(t, err)

	value := map[string]string{"value": "cached"}
	err = ml.Set(context.Background(), "key", value, SetTTLOptions{L1TTL: time.Minute, L2TTL: 2 * time.Minute})
	require.NoError(t, err)

	require.Contains(t, l1.data, "key")
	require.Contains(t, l2.data, "key")
}

func TestMultiLevelCacheDeleteEvictsBoth(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	data := []byte("value")
	require.NoError(t, l1.Set(context.Background(), "key", data, time.Minute))
	require.NoError(t, l2.Set(context.Background(), "key", data, time.Minute))

	ml, err := NewMultiLevelCache(l1, l2, JSONSerializer{}, MultiLevelConfig{
		Mode:         ModeBothLevels, // Explicitly set mode
		L1DefaultTTL: time.Minute,
		L2DefaultTTL: time.Minute,
	})
	require.NoError(t, err)

	require.NoError(t, ml.Delete(context.Background(), "key"))
	require.NotContains(t, l1.data, "key")
	require.NotContains(t, l2.data, "key")
}

func TestMultiLevelCacheSetHonorsLayerSpecificTTLs(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	ml, err := NewMultiLevelCache(l1, l2, JSONSerializer{}, MultiLevelConfig{
		Mode:         ModeBothLevels, // Explicitly set mode
		L1DefaultTTL: 30 * time.Second,
		L2DefaultTTL: 2 * time.Minute,
	})
	require.NoError(t, err)

	err = ml.Set(
		context.Background(),
		"key",
		map[string]string{"value": "ttl-test"},
		SetTTLOptions{L1TTL: 10 * time.Second, L2TTL: 5 * time.Minute},
	)
	require.NoError(t, err)

	require.Equal(t, 10*time.Second, l1.ttl["key"])
	require.Equal(t, 5*time.Minute, l2.ttl["key"])

	// when options omitted, fall back to defaults
	err = ml.Set(
		context.Background(),
		"key2",
		map[string]string{"value": "default"},
		SetTTLOptions{},
	)
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, l1.ttl["key2"])
	require.Equal(t, 2*time.Minute, l2.ttl["key2"])
}

// TestMultiLevelCacheModeL1Only_OnlyWritesToL1 tests that ModeL1Only only writes to L1
func TestMultiLevelCacheModeL1Only_OnlyWritesToL1(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	ml, err := NewMultiLevelCache(l1, l2, JSONSerializer{}, MultiLevelConfig{
		Mode:         ModeL1Only,
		L1DefaultTTL: time.Minute,
		L2DefaultTTL: time.Minute,
	})
	require.NoError(t, err)

	value := map[string]string{"value": "l1-only"}
	err = ml.Set(context.Background(), "key", value, SetTTLOptions{})
	require.NoError(t, err)

	require.Contains(t, l1.data, "key")
	require.NotContains(t, l2.data, "key", "L2 should not be written in ModeL1Only")
}

// TestMultiLevelCacheModeL2Only_OnlyWritesToL2 tests that ModeL2Only only writes to L2
func TestMultiLevelCacheModeL2Only_OnlyWritesToL2(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	ml, err := NewMultiLevelCache(l1, l2, JSONSerializer{}, MultiLevelConfig{
		Mode:         ModeL2Only,
		L1DefaultTTL: time.Minute,
		L2DefaultTTL: time.Minute,
	})
	require.NoError(t, err)

	value := map[string]string{"value": "l2-only"}
	err = ml.Set(context.Background(), "key", value, SetTTLOptions{})
	require.NoError(t, err)

	require.NotContains(t, l1.data, "key", "L1 should not be written in ModeL2Only")
	require.Contains(t, l2.data, "key")
}

// TestMultiLevelCacheModeL2Only_NoWarmup tests that ModeL2Only does not warm L1
func TestMultiLevelCacheModeL2Only_NoWarmup(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()
	payload := map[string]string{"value": "from-l2"}

	serializer := JSONSerializer{}
	bytes, err := serializer.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, l2.Set(context.Background(), "key", bytes, time.Minute))

	ml, err := NewMultiLevelCache(l1, l2, serializer, MultiLevelConfig{
		Mode:         ModeL2Only,
		WarmupTTL:    time.Minute,
		L1DefaultTTL: time.Minute,
		L2DefaultTTL: time.Minute,
	})
	require.NoError(t, err)

	var result map[string]string
	found, err := ml.Get(context.Background(), "key", &result)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, payload, result)

	// L1 should NOT be warmed in ModeL2Only
	require.NotContains(t, l1.data, "key", "L1 should not be warmed in ModeL2Only")
}

// TestMultiLevelCacheSet_InvalidTargetLevel tests error handling for invalid target level requests
// When only one level is configured, overrides are not allowed, so we get the "overrides not allowed" error
func TestMultiLevelCacheSet_InvalidTargetLevel(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()

	// Only L1 configured
	ml, err := NewMultiLevelCache(l1, nil, JSONSerializer{}, MultiLevelConfig{
		Mode:         ModeL1Only,
		L1DefaultTTL: time.Minute,
		L2DefaultTTL: time.Minute,
	})
	require.NoError(t, err)

	// Try to target L2 when it's not configured - should fail with "overrides not allowed"
	err = ml.Set(context.Background(), "key", "value", SetTTLOptions{
		TargetL2: BoolPtr(true),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "level overrides not allowed")
}

// TestMultiLevelCacheSet_InvalidTargetLevelWithBothConfigured tests error when targeting non-existent level
// This scenario can occur if overrides are used incorrectly with both levels configured
func TestMultiLevelCacheSet_InvalidTargetLevelWithBothConfigured(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	// Both levels configured
	ml, err := NewMultiLevelCache(l1, l2, JSONSerializer{}, MultiLevelConfig{
		Mode:         ModeBothLevels,
		L1DefaultTTL: time.Minute,
		L2DefaultTTL: time.Minute,
	})
	require.NoError(t, err)

	// This should work fine since both levels are configured
	err = ml.Set(context.Background(), "key", "value", SetTTLOptions{
		TargetL1: BoolPtr(true),
		TargetL2: BoolPtr(true),
	})
	require.NoError(t, err)
}

// TestMultiLevelCacheSet_OverrideInSingleLevelMode tests that overrides are not allowed in single-level mode
func TestMultiLevelCacheSet_OverrideInSingleLevelMode(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()

	ml, err := NewMultiLevelCache(l1, nil, JSONSerializer{}, MultiLevelConfig{
		Mode:         ModeL1Only,
		L1DefaultTTL: time.Minute,
		L2DefaultTTL: time.Minute,
	})
	require.NoError(t, err)

	// Try to use overrides when only one level is configured
	err = ml.Set(context.Background(), "key", "value", SetTTLOptions{
		TargetL1: BoolPtr(true),
		TargetL2: BoolPtr(false),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "level overrides not allowed")
}

// TestMultiLevelCacheGet_ModeL1Only_ChecksBothButNoWarmup tests that ModeL1Only checks both levels but doesn't warm L1
func TestMultiLevelCacheGet_ModeL1Only_ChecksBothButNoWarmup(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	// Put data in L2 only
	payload := map[string]string{"value": "in-l2"}
	serializer := JSONSerializer{}
	bytes, err := serializer.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, l2.Set(context.Background(), "key", bytes, time.Minute))

	ml, err := NewMultiLevelCache(l1, l2, serializer, MultiLevelConfig{
		Mode:         ModeL1Only,
		WarmupTTL:    time.Minute,
		L1DefaultTTL: time.Minute,
		L2DefaultTTL: time.Minute,
	})
	require.NoError(t, err)

	// Get should find it in L2 (Get checks both levels regardless of mode)
	var result map[string]string
	found, err := ml.Get(context.Background(), "key", &result)
	require.NoError(t, err)
	require.True(t, found, "should find data in L2 even in ModeL1Only")

	// But L1 should NOT be warmed because mode is ModeL1Only
	require.NotContains(t, l1.data, "key", "L1 should not be warmed in ModeL1Only")
}

// TestMultiLevelCacheGet_ModeL2Only_ChecksBothButNoWarmup tests that ModeL2Only checks both levels but doesn't warm L1
func TestMultiLevelCacheGet_ModeL2Only_ChecksBothButNoWarmup(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	// Put data in L1 only
	payload := map[string]string{"value": "in-l1"}
	serializer := JSONSerializer{}
	bytes, err := serializer.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, l1.Set(context.Background(), "key", bytes, time.Minute))

	ml, err := NewMultiLevelCache(l1, l2, serializer, MultiLevelConfig{
		Mode:         ModeL2Only,
		WarmupTTL:    time.Minute,
		L1DefaultTTL: time.Minute,
		L2DefaultTTL: time.Minute,
	})
	require.NoError(t, err)

	// Get should find it in L1 (Get checks both levels regardless of mode)
	var result map[string]string
	found, err := ml.Get(context.Background(), "key", &result)
	require.NoError(t, err)
	require.True(t, found, "should find data in L1 even in ModeL2Only")
	require.Equal(t, payload, result)
}

// TestMultiLevelCacheDelete_ModeL1Only_OnlyDeletesL1 tests that Delete only affects configured levels
func TestMultiLevelCacheDelete_ModeL1Only_OnlyDeletesL1(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	data := []byte("value")
	require.NoError(t, l1.Set(context.Background(), "key", data, time.Minute))
	require.NoError(t, l2.Set(context.Background(), "key", data, time.Minute))

	ml, err := NewMultiLevelCache(l1, nil, JSONSerializer{}, MultiLevelConfig{
		Mode:         ModeL1Only,
		L1DefaultTTL: time.Minute,
		L2DefaultTTL: time.Minute,
	})
	require.NoError(t, err)

	require.NoError(t, ml.Delete(context.Background(), "key"))
	require.NotContains(t, l1.data, "key")
	// L2 should still have the data since it's not managed by this cache instance
	require.Contains(t, l2.data, "key")
}

// TestMultiLevelCacheSet_OverrideTargetL1Only tests per-call override to write only to L1
func TestMultiLevelCacheSet_OverrideTargetL1Only(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	ml, err := NewMultiLevelCache(l1, l2, JSONSerializer{}, MultiLevelConfig{
		Mode:         ModeBothLevels,
		L1DefaultTTL: time.Minute,
		L2DefaultTTL: time.Minute,
	})
	require.NoError(t, err)

	value := map[string]string{"value": "l1-override"}
	err = ml.Set(context.Background(), "key", value, SetTTLOptions{
		TargetL1: BoolPtr(true),
		TargetL2: BoolPtr(false),
	})
	require.NoError(t, err)

	require.Contains(t, l1.data, "key")
	require.NotContains(t, l2.data, "key")
}

// TestMultiLevelCacheSet_OverrideTargetL2Only tests per-call override to write only to L2
func TestMultiLevelCacheSet_OverrideTargetL2Only(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	ml, err := NewMultiLevelCache(l1, l2, JSONSerializer{}, MultiLevelConfig{
		Mode:         ModeBothLevels,
		L1DefaultTTL: time.Minute,
		L2DefaultTTL: time.Minute,
	})
	require.NoError(t, err)

	value := map[string]string{"value": "l2-override"}
	err = ml.Set(context.Background(), "key", value, SetTTLOptions{
		TargetL1: BoolPtr(false),
		TargetL2: BoolPtr(true),
	})
	require.NoError(t, err)

	require.NotContains(t, l1.data, "key")
	require.Contains(t, l2.data, "key")
}
