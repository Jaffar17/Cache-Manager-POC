## Cache Manager POC

Multi-level caching proof of concept written in Go, combining an in-memory L1 (BigCache) with Redis L2, plus a sample HTTP service backed by PostgreSQL.

### Features
- Cache-aside workflow with automatic L1→L2 fallback and warm-up.
- JSON serialization, per-layer TTL configuration, and optional per-call overrides.
- Redis + RedisInsight + PostgreSQL + pgAdmin via `docker-compose`.
- Sample Gin-based API (`GET /users/:id`, `POST /users/refresh/:id`) demonstrating cache usage with a mock DB replaced by Postgres.
- Unit tests for each cache layer and integration test against real Redis.

### Getting Started
```bash
git clone https://github.com/your-org/cache-manager-poc
cd cache-manager-poc

# Start infra
docker-compose up -d

# Run API (requires Go 1.22+)
go run cmd/app/main.go
```

### Configuration
Environment variables:
| Variable | Description | Default |
|----------|-------------|---------|
| `REDIS_ADDR` | Redis connection string | `localhost:6379` |
| `POSTGRES_DSN` | PostgreSQL DSN | `postgres://app:app@localhost:5432/app?sslmode=disable` |
| `CACHE_L1_TTL` | Default L1 TTL (e.g., `1m`) | `1m` |
| `CACHE_L2_TTL` | Default L2 TTL | `5m` |
| `CACHE_WARM_TTL` | TTL to use when warming L1 from L2 | `CACHE_L1_TTL` |

### API
- `GET /users/:id`
  - Cache-aside lookup: BigCache → Redis → Postgres.
- `POST /users/refresh/:id`
  - Updates the user in Postgres and invalidates both cache layers.

### Testing
```bash
go test ./...
# or focus on cache package
go test ./internal/cache
```

### Observability
- BigCache emits log snapshots on hits/misses (`[bigcache] action=...`).
- MultiLevel cache logs which layer served each request (`[cache] hit level=...`).
- RedisInsight (`http://localhost:5540`) and pgAdmin (`http://localhost:8081`) available via docker-compose.

### Roadmap Ideas
- Redis pub/sub invalidation across instances.
- Prometheus / OpenTelemetry metrics for hit/miss tracking.
- Alternative serializers or compression support.

