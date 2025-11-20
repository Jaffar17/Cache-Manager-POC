package cache

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func setupRedisCache(t *testing.T) (*RedisCache, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache, err := NewRedisCache(client)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = client.Close()
	})
	return cache, mr
}

func TestRedisCacheSetGetDelete(t *testing.T) {
	t.Parallel()

	cache, _ := setupRedisCache(t)
	ctx := context.Background()

	require.NoError(t, cache.Set(ctx, "foo", []byte("bar"), time.Minute))

	data, ok, err := cache.Get(ctx, "foo")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("bar"), data)

	require.NoError(t, cache.Delete(ctx, "foo"))

	_, ok, err = cache.Get(ctx, "foo")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRedisCacheTTL(t *testing.T) {
	t.Parallel()

	cache, mr := setupRedisCache(t)
	ctx := context.Background()

	require.NoError(t, cache.Set(ctx, "ttl", []byte("value"), 50*time.Millisecond))
	mr.FastForward(100 * time.Millisecond)

	_, ok, err := cache.Get(ctx, "ttl")
	require.NoError(t, err)
	require.False(t, ok)
}
