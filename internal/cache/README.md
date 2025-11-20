## Cache Package Overview

### Architecture
- `Cache` interface exposes `Get`, `Set`, `Delete`.
- `MultiLevelCache` orchestrates two `RawCache` layers (L1 BigCache, L2 Redis) plus JSON serialization.
- `BigCache` stores serialized bytes with per-entry TTL metadata.
- `RedisCache` uses go-redis for persistent L2 storage, optionally future pub/sub invalidation.

### Data Flow (Cache-Aside Pattern)
```
        ┌──────────┐
        │ Caller   │
        └────┬─────┘
             │ Get(key)
             ▼
    ┌───────────────────┐
    │ MultiLevelCache   │
    └────┬──────────────┘
         │
   ┌─────▼─────┐               miss
   │ L1 BigCache│──────────────┐
   └─────┬─────┘               │
hit →    │                     ▼
  decode │             ┌────────────────┐
         │             │ L2 RedisCache  │
         │             └─────┬──────────┘
         └─────────────hit   │
                             │ (on hit)
                   ┌─────────▼─────────┐
                   │ JSON unmarshal    │
                   │ warm L1 (best-effort)
                   └─────────┬─────────┘
                             │
                           return
```

### TTL Strategy
- `SetTTLOptions` allows per-layer TTLs.
- Defaults come from `MultiLevelConfig` (set via env in sample app).
- L1 warmup uses `WarmupTTL` (defaults to L1 TTL) to keep L1 hot after L2 hits.

### Integrating in Another Go Service
1. **Add dependencies**
   ```bash
   go get github.com/allegro/bigcache/v3 \
          github.com/redis/go-redis/v9
   ```
2. **Construct layers**
   ```go
   bc, _ := cache.NewBigCache(cache.BigCacheConfig{Config: bigcache.DefaultConfig(10 * time.Minute)})
   rc, _ := cache.NewRedisCache(redisClient)
   serializer := cache.JSONSerializer{}
   ml, _ := cache.NewMultiLevelCache(
       bc, rc, serializer,
       cache.MultiLevelConfig{
           WarmupTTL:    30 * time.Second,
           L1DefaultTTL: time.Minute,
           L2DefaultTTL: 5 * time.Minute,
       })
   ```
3. **Use through `cache.Cache`**
   ```go
   var user User
   if ok, _ := ml.Get(ctx, "user:42", &user); !ok {
       user = loadFromDB(...)
       _ = ml.Set(ctx, "user:42", user, cache.SetTTLOptions{})
   }
   ```
4. **Inject into handlers/services** so all reads/writes go through the cache facade.

### Instrumentation & Visibility
- BigCache wrapper logs snapshots and maintains hit counts per key.
- MultiLevel cache logs which layer served each request and when misses occur.
- Integration tests demonstrate end-to-end behavior with Docker Redis.

### Future Enhancements
- Redis pub/sub invalidation hook (`SubscribeInvalidations`) ready for implementation.
- Swap serializer or add compression by providing different `Serializer`.

