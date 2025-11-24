# Cache Manager POC - Testing Guide

This guide explains how to test the multi-level cache implementation with different modes and per-call overrides.

## Setup

1. Start the services:
```bash
docker-compose up -d
```

2. Wait for services to be ready (PostgreSQL and Redis)

3. The server will start on port 8080

## Cache Modes

The application is configured with three separate cache instances:

### 1. **Both Levels Mode** (Default)
- Uses both L1 (BigCache) and L2 (Redis)
- Writes to both levels on Set
- Reads from L1 first, falls back to L2
- Warms L1 on L2 hit (cache-aside pattern)

### 2. **L1 Only Mode**
- Uses only L1 (in-memory BigCache)
- Fast but data is lost on restart
- No Redis interaction

### 3. **L2 Only Mode**
- Uses only L2 (Redis)
- Persistent across restarts
- No L1 warmup

## Endpoints

### Mode-Specific Endpoints

Test each cache mode independently:

```bash
# Both levels (L1 + L2 with warmup)
curl http://localhost:8080/users/both-levels/1 | jq

# L1 only (in-memory)
curl http://localhost:8080/users/l1-only/1 | jq

# L2 only (Redis)
curl http://localhost:8080/users/l2-only/1 | jq
```

**Response includes:**
- `cache_mode`: Which mode was used
- `from_cache`: Whether data came from cache (true) or database (false)
- `user`: The user data

### Per-Call Override Endpoints

These use the "both-levels" cache but override the target level per call:

```bash
# Fetch and cache ONLY in L1 (override)
curl http://localhost:8080/users/override-l1/1 | jq

# Fetch and cache ONLY in L2 (override)
curl http://localhost:8080/users/override-l2/1 | jq

# Force set in L1 only
curl -X POST http://localhost:8080/users/set-l1-only/1 | jq

# Force set in L2 only
curl -X POST http://localhost:8080/users/set-l2-only/1 | jq
```

### Cache Inspection Endpoints

```bash
# View cache status for user ID 1
curl http://localhost:8080/cache/stats/1 | jq

# Expected response:
# {
#   "cache_key": "user:1",
#   "both_levels": { "cached": true },
#   "l1_only": { "cached": false },
#   "l2_only": { "cached": true }
# }

# Clear cache for user ID 1
curl -X DELETE http://localhost:8080/cache/clear/1 | jq
```

### Standard Endpoints

```bash
# Get user (uses both-levels cache)
curl http://localhost:8080/users/1 | jq

# Refresh user (fetches from DB and clears all caches)
curl -X POST http://localhost:8080/users/refresh/1 | jq
```

## Test Scenarios

### Scenario 1: L1 Warmup Behavior

Test that L1 gets warmed on L2 hit when using both-levels mode:

```bash
# Clear all caches
curl -X DELETE http://localhost:8080/cache/clear/1

# Set in L2 only
curl -X POST http://localhost:8080/users/set-l2-only/1

# Verify only L2 has data
curl http://localhost:8080/cache/stats/1 | jq
# both_levels: false, l2_only: true

# Fetch with both-levels mode (should warm L1)
curl http://localhost:8080/users/both-levels/1

# Verify L1 is now warmed
curl http://localhost:8080/cache/stats/1 | jq
# both_levels: true (L1 is warmed)
```

### Scenario 2: L2-Only Mode Does NOT Warm L1

When using L2-only mode, L1 should never be populated:

```bash
# Clear all caches
curl -X DELETE http://localhost:8080/cache/clear/1

# Use L2-only mode
curl http://localhost:8080/users/l2-only/1

# Check stats - L1 should still be empty
curl http://localhost:8080/cache/stats/1 | jq
# l1_only: false (not warmed)
# l2_only: true
```

### Scenario 3: Per-Call Overrides

Test that you can override the default mode per call:

```bash
# Clear all caches
curl -X DELETE http://localhost:8080/cache/clear/1

# Override to L1 only
curl http://localhost:8080/users/override-l1/1

# Verify only L1 has data
curl http://localhost:8080/cache/stats/1 | jq
# Should show: both_levels.cached: true, l2_only.cached: false

# Clear again
curl -X DELETE http://localhost:8080/cache/clear/1

# Override to L2 only
curl http://localhost:8080/users/override-l2/1

# Verify only L2 has data
curl http://localhost:8080/cache/stats/1 | jq
# Should show: both_levels.cached: true (from L2), l1_only.cached: false
```

### Scenario 4: Cache Hit Rates

Test cache hit vs miss behavior:

```bash
# Clear cache
curl -X DELETE http://localhost:8080/cache/clear/1

# First call - cache miss
curl http://localhost:8080/users/both-levels/1 | jq
# from_cache: false

# Second call - cache hit from L1
curl http://localhost:8080/users/both-levels/1 | jq
# from_cache: true
```

## Automated Testing

Run the provided test script to automatically test all endpoints:

```bash
./test_endpoints.sh
```

This script will:
1. Test all mode-specific endpoints
2. Test per-call overrides
3. Verify cache warmup behavior
4. Test cache hit rates
5. Show cache statistics

## Monitoring

Watch the server logs to see cache operations:

```bash
docker-compose logs -f app
```

You'll see logs like:
```
[cache] set level=L1 key=user:1
[cache] set level=L2 key=user:1
[cache] hit level=L1 key=user:1
[cache] hit level=L2 key=user:1
[cache] warming L1 for key=user:1
[cache] delete level=L1 key=user:1
```

## Environment Variables

Configure cache behavior via environment variables:

```bash
# L1 TTL (default: 1m)
CACHE_L1_TTL=2m

# L2 TTL (default: 5m)
CACHE_L2_TTL=10m

# Warmup TTL (default: same as L1)
CACHE_WARM_TTL=1m30s

# Redis address
REDIS_ADDR=localhost:6379

# PostgreSQL DSN
POSTGRES_DSN=postgres://app:app@localhost:5432/app?sslmode=disable
```

## Key Concepts

### Cache Modes
- **ModeBothLevels**: Default behavior, uses both L1 and L2
- **ModeL1Only**: Only uses in-memory cache
- **ModeL2Only**: Only uses Redis, no L1 warmup

### Per-Call Overrides
When both L1 and L2 are configured, you can override the target level per call:
```go
cache.Set(ctx, key, value, cache.SetTTLOptions{
    TargetL1: cache.BoolPtr(true),   // Write to L1
    TargetL2: cache.BoolPtr(false),  // Skip L2
})
```

### Cache-Aside Pattern
- On Get: Check L1 → Check L2 → Check DB
- On L2 hit: Warm L1 (only in ModeBothLevels)
- On Set: Write to configured level(s)
- On Delete: Remove from all levels

## Troubleshooting

### Redis Connection Issues
```bash
# Check Redis is running
docker-compose ps redis

# Test Redis connection
docker-compose exec redis redis-cli ping
```

### PostgreSQL Connection Issues
```bash
# Check PostgreSQL is running
docker-compose ps postgres

# Test connection
docker-compose exec postgres psql -U app -d app -c "SELECT 1"
```

### Clear Everything
```bash
# Restart all services
docker-compose down -v
docker-compose up -d
```

