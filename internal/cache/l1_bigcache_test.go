package cache

import (
	"context"
	"testing"
	"time"

	"github.com/allegro/bigcache/v3"
	"github.com/stretchr/testify/require"
)

func TestBigCacheSetGetDelete(t *testing.T) {
	t.Parallel()

	bc, err := NewBigCache(BigCacheConfig{Config: bigcache.DefaultConfig(time.Minute)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = bc.Close() })

	ctx := context.Background()
	err = bc.Set(ctx, "foo", []byte("bar"), time.Minute)
	require.NoError(t, err)

	data, ok, err := bc.Get(ctx, "foo")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("bar"), data)

	err = bc.Delete(ctx, "foo")
	require.NoError(t, err)

	_, ok, err = bc.Get(ctx, "foo")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestBigCacheTTL(t *testing.T) {
	t.Parallel()

	bc, err := NewBigCache(BigCacheConfig{Config: bigcache.DefaultConfig(time.Minute)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = bc.Close() })

	ctx := context.Background()
	require.NoError(t, bc.Set(ctx, "ttl", []byte("value"), 50*time.Millisecond))

	time.Sleep(70 * time.Millisecond)

	_, ok, err := bc.Get(ctx, "ttl")
	require.NoError(t, err)
	require.False(t, ok)
}
