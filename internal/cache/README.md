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
- `SetTTLOptions` allows per-layer TTLs at both service and endpoint levels.
- Service-level defaults come from `MultiLevelConfig` (set via env in sample app).
- Endpoint-level overrides can be specified per `Set` call.
- If `L1TTL` or `L2TTL` in `SetTTLOptions` is zero or negative, the service-level default is used.
- L1 warmup uses `WarmupTTL` (defaults to L1 TTL) to keep L1 hot after L2 hits.

#### Service-Level Configuration
Set default TTLs when constructing the cache:
```go
ml, _ := cache.NewMultiLevelCache(
    bc, rc, serializer,
    cache.MultiLevelConfig{
        WarmupTTL:    30 * time.Second,
        L1DefaultTTL: time.Minute,        // Default L1 TTL
        L2DefaultTTL: 5 * time.Minute,    // Default L2 TTL
    })
```

#### Endpoint-Level Configuration
Override TTLs per endpoint by passing specific `SetTTLOptions`:

**Use service defaults:**
```go
// Uses L1DefaultTTL and L2DefaultTTL from MultiLevelConfig
ml.Set(ctx, "user:42", user, cache.SetTTLOptions{})
```

**Override both layers:**
```go
// Custom TTLs for this specific endpoint
ml.Set(ctx, "user:42", user, cache.SetTTLOptions{
    L1TTL: 30 * time.Second,   // Override L1 TTL
    L2TTL: 10 * time.Minute,   // Override L2 TTL
})
```

**Override only one layer:**
```go
// Override L1, use default for L2
ml.Set(ctx, "user:42", user, cache.SetTTLOptions{
    L1TTL: 15 * time.Second,
    // L2TTL: 0 (zero value) → uses L2DefaultTTL
})

// Override L2, use default for L1
ml.Set(ctx, "user:42", user, cache.SetTTLOptions{
    L2TTL: 20 * time.Minute,
    // L1TTL: 0 (zero value) → uses L1DefaultTTL
})
```

**Example: Different endpoints with different TTLs**
```go
func (s *server) handleGetUser(c *gin.Context) {
    // ... load user from cache or DB ...
    
    // Use endpoint-specific TTLs (e.g., short-lived user data)
    ttlOpts := cache.SetTTLOptions{
        L1TTL: 30 * time.Second,
        L2TTL: 2 * time.Minute,
    }
    ml.Set(ctx, cacheKey, user, ttlOpts)
}

func (s *server) handleGetProduct(c *gin.Context) {
    // ... load product from cache or DB ...
    
    // Use longer TTLs for product data
    ttlOpts := cache.SetTTLOptions{
        L1TTL: 5 * time.Minute,
        L2TTL: 1 * time.Hour,
    }
    ml.Set(ctx, cacheKey, product, ttlOpts)
}
```

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

