package cache

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/allegro/bigcache/v3"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestIntegrationMultiLevelCacheWithRedis(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}

	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("skipping integration test, redis unreachable at %s: %v", addr, err)
	}
	defer func() {
		_ = client.FlushAll(ctx).Err()
		_ = client.Close()
	}()

	l1, err := NewBigCache(BigCacheConfig{Config: bigcache.DefaultConfig(time.Minute)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = l1.Close() })

	l2, err := NewRedisCache(client)
	require.NoError(t, err)

	ml, err := NewMultiLevelCache(l1, l2, JSONSerializer{}, MultiLevelConfig{WarmupTTL: time.Second})
	require.NoError(t, err)

	key := "integration:user"
	_ = ml.Delete(ctx, key)

	type user struct {
		Name string `json:"name"`
	}

	value := user{Name: "cached"}
	require.NoError(t, ml.Set(ctx, key, value, 200*time.Millisecond))

	var out user
	found, err := ml.Get(ctx, key, &out)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, value, out)

	time.Sleep(300 * time.Millisecond)

	var expired user
	found, err = ml.Get(ctx, key, &expired)
	require.NoError(t, err)
	require.False(t, found)
}
