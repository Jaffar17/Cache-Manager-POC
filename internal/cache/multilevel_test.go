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
	require.True(t, ok, "expected L1 warm after L2 hit")
}

func TestMultiLevelCacheMiss(t *testing.T) {
	t.Parallel()

	ml, err := NewMultiLevelCache(
		newMemoryRawCache(),
		newMemoryRawCache(),
		JSONSerializer{},
		MultiLevelConfig{L1DefaultTTL: time.Minute, L2DefaultTTL: time.Minute},
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
