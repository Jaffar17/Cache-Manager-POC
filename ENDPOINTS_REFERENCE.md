# Cache Manager POC - Endpoints Quick Reference

## 📋 All Available Endpoints

### 🎯 Mode-Specific Endpoints

Test each cache mode independently:

| Endpoint | Method | Description | Cache Mode |
|----------|--------|-------------|------------|
| `/users/both-levels/:id` | GET | Fetch user with L1+L2, warmup enabled | ModeBothLevels |
| `/users/l1-only/:id` | GET | Fetch user with L1 only (in-memory) | ModeL1Only |
| `/users/l2-only/:id` | GET | Fetch user with L2 only (Redis) | ModeL2Only |

### 🔧 Per-Call Override Endpoints

Use both-levels cache with per-call targeting:

| Endpoint | Method | Description | Behavior |
|----------|--------|-------------|----------|
| `/users/override-l1/:id` | GET | Fetch & cache ONLY in L1 | TargetL1=true, TargetL2=false |
| `/users/override-l2/:id` | GET | Fetch & cache ONLY in L2 | TargetL1=false, TargetL2=true |
| `/users/set-l1-only/:id` | POST | Force set user in L1 only | TargetL1=true, TargetL2=false |
| `/users/set-l2-only/:id` | POST | Force set user in L2 only | TargetL1=false, TargetL2=true |

### 📊 Cache Inspection Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/cache/stats/:id` | GET | View cache status across all modes |
| `/cache/clear/:id` | DELETE | Clear cache for user from all instances |

### 📌 Standard Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/users/:id` | GET | Get user (uses both-levels cache) |
| `/users/refresh/:id` | POST | Refresh user data from DB, clear all caches |

---

## 🚀 Quick Test Commands

### Test All Modes
```bash
# Both levels mode
curl http://localhost:8080/users/both-levels/1 | jq

# L1 only mode
curl http://localhost:8080/users/l1-only/1 | jq

# L2 only mode
curl http://localhost:8080/users/l2-only/1 | jq
```

### Test Overrides
```bash
# Override to L1 only
curl http://localhost:8080/users/override-l1/1 | jq

# Override to L2 only
curl http://localhost:8080/users/override-l2/1 | jq

# Force set in L1
curl -X POST http://localhost:8080/users/set-l1-only/1 | jq

# Force set in L2
curl -X POST http://localhost:8080/users/set-l2-only/1 | jq
```

### Inspect Cache
```bash
# View cache status
curl http://localhost:8080/cache/stats/1 | jq

# Clear cache
curl -X DELETE http://localhost:8080/cache/clear/1 | jq
```

---

## 📝 Response Format

### User Endpoints Response
```json
{
  "user": {
    "id": 1,
    "name": "User Name",
    "email": "user@example.com",
    "last_login": "2024-01-01T00:00:00Z"
  },
  "cache_mode": "both-levels",
  "from_cache": true
}
```

### Cache Stats Response
```json
{
  "cache_key": "user:1",
  "both_levels": { "cached": true },
  "l1_only": { "cached": false },
  "l2_only": { "cached": true }
}
```

### Cache Clear Response
```json
{
  "message": "Cache cleared",
  "cache_key": "user:1",
  "both_levels": true,
  "l1_only": true,
  "l2_only": true
}
```

---

## 🧪 Test Scenarios

### Scenario 1: Verify L1 Warmup
```bash
# 1. Clear cache
curl -X DELETE http://localhost:8080/cache/clear/1

# 2. Set in L2 only
curl -X POST http://localhost:8080/users/set-l2-only/1

# 3. Check stats (L1 should be empty)
curl http://localhost:8080/cache/stats/1 | jq

# 4. Fetch with both-levels (triggers L1 warmup)
curl http://localhost:8080/users/both-levels/1

# 5. Check stats again (L1 should now be populated)
curl http://localhost:8080/cache/stats/1 | jq
```

### Scenario 2: Verify L2-Only Doesn't Warm L1
```bash
# 1. Clear cache
curl -X DELETE http://localhost:8080/cache/clear/1

# 2. Fetch with L2-only mode
curl http://localhost:8080/users/l2-only/1

# 3. Check stats (L1 should still be empty)
curl http://localhost:8080/cache/stats/1 | jq
# Expected: l1_only.cached = false
```

### Scenario 3: Test Override Isolation
```bash
# 1. Clear cache
curl -X DELETE http://localhost:8080/cache/clear/1

# 2. Override to L1 only
curl http://localhost:8080/users/override-l1/1

# 3. Check stats
curl http://localhost:8080/cache/stats/1 | jq
# Expected: both_levels.cached = true, l2_only.cached = false
```

---

## ⚙️ Configuration

### Environment Variables
```bash
CACHE_L1_TTL=1m          # L1 cache TTL (default: 1 minute)
CACHE_L2_TTL=5m          # L2 cache TTL (default: 5 minutes)
CACHE_WARM_TTL=1m        # L1 warmup TTL (default: same as L1)
REDIS_ADDR=localhost:6379
POSTGRES_DSN=postgres://app:app@localhost:5432/app?sslmode=disable
```

---

## 🔍 Cache Mode Behavior

| Mode | L1 (BigCache) | L2 (Redis) | Warmup | Use Case |
|------|---------------|------------|---------|----------|
| **ModeBothLevels** | ✅ Read/Write | ✅ Read/Write | ✅ Yes | Production (fastest + persistent) |
| **ModeL1Only** | ✅ Read/Write | ❌ No | ❌ No | Testing, ephemeral data |
| **ModeL2Only** | ❌ No | ✅ Read/Write | ❌ No | Shared cache across instances |

### Per-Call Overrides
- Only available when **both** L1 and L2 are configured
- Use `TargetL1` and `TargetL2` options to override default mode
- Useful for selective caching strategies

---

## 🎬 Automated Testing

Run the complete test suite:
```bash
./test_endpoints.sh
```

This tests:
- ✅ All mode-specific endpoints
- ✅ Per-call override functionality
- ✅ Cache warmup behavior
- ✅ Cache hit/miss rates
- ✅ Cache inspection and clearing

---

## 📚 Additional Resources

- Full testing guide: `TESTING_GUIDE.md`
- Implementation details: `internal/cache/README.md`
- Usage examples: See test script `test_endpoints.sh`

