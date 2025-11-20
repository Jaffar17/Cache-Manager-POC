package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type memoryRawCache struct {
	data map[string][]byte
}

func newMemoryRawCache() *memoryRawCache {
	return &memoryRawCache{data: make(map[string][]byte)}
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

func (m *memoryRawCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[key] = cp
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

	ml, err := NewMultiLevelCache(l1, l2, serializer, MultiLevelConfig{WarmupTTL: time.Minute})
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

	ml, err := NewMultiLevelCache(newMemoryRawCache(), newMemoryRawCache(), JSONSerializer{}, MultiLevelConfig{})
	require.NoError(t, err)

	found, err := ml.Get(context.Background(), "missing", &struct{}{})
	require.NoError(t, err)
	require.False(t, found)
}

func TestMultiLevelCacheSetWritesBoth(t *testing.T) {
	t.Parallel()

	l1 := newMemoryRawCache()
	l2 := newMemoryRawCache()

	ml, err := NewMultiLevelCache(l1, l2, JSONSerializer{}, MultiLevelConfig{})
	require.NoError(t, err)

	value := map[string]string{"value": "cached"}
	err = ml.Set(context.Background(), "key", value, time.Minute)
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

	ml, err := NewMultiLevelCache(l1, l2, JSONSerializer{}, MultiLevelConfig{})
	require.NoError(t, err)

	require.NoError(t, ml.Delete(context.Background(), "key"))
	require.NotContains(t, l1.data, "key")
	require.NotContains(t, l2.data, "key")
}
