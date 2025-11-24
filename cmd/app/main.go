package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/allegro/bigcache/v3"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"go-cache-poc/internal/db"
	cache_manager "go-cache-poc/pkg/cache-manager"
)

func main() {
	ctx := context.Background()

	bcConfig := bigcache.DefaultConfig(10 * time.Minute)
	bcConfig.CleanWindow = time.Minute
	bcConfig.Shards = 128

	bigCache, err := cache_manager.NewBigCache(ctx, cache_manager.BigCacheConfig{Config: bcConfig})
	if err != nil {
		log.Fatalf("failed creating bigcache: %v", err)
	}
	defer bigCache.Close()

	l1TTL := getenvDuration("CACHE_L1_TTL", 40*time.Second)
	l2TTL := getenvDuration("CACHE_L2_TTL", 2*time.Minute)
	warmTTL := getenvDuration("CACHE_WARM_TTL", l1TTL)

	redisAddr := getenv("REDIS_ADDR", "localhost:6379")
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed connecting to redis at %s: %v", redisAddr, err)
	}
	defer redisClient.Close()

	redisCache, err := cache_manager.NewRedisCache(redisClient)
	if err != nil {
		log.Fatalf("failed creating redis cache: %v", err)
	}

	serializer := cache_manager.JSONSerializer{}

	// Create cache instances with different modes for testing
	cacheBothLevels, err := cache_manager.NewMultiLevelCache(bigCache, redisCache, serializer, cache_manager.MultiLevelConfig{
		Mode:         cache_manager.ModeBothLevels,
		WarmupTTL:    warmTTL,
		L1DefaultTTL: l1TTL,
		L2DefaultTTL: l2TTL,
	})
	if err != nil {
		log.Fatalf("failed constructing both-levels cache: %v", err)
	}

	cacheL1Only, err := cache_manager.NewMultiLevelCache(bigCache, nil, serializer, cache_manager.MultiLevelConfig{
		Mode:         cache_manager.ModeL1Only,
		L1DefaultTTL: l1TTL,
	})
	if err != nil {
		log.Fatalf("failed constructing L1-only cache: %v", err)
	}

	cacheL2Only, err := cache_manager.NewMultiLevelCache(nil, redisCache, serializer, cache_manager.MultiLevelConfig{
		Mode:         cache_manager.ModeL2Only,
		L2DefaultTTL: l2TTL,
	})
	if err != nil {
		log.Fatalf("failed constructing L2-only cache: %v", err)
	}

	log.Println("✓ Configured 3 cache instances: both-levels, L1-only, L2-only")

	postgresDSN := getenv("POSTGRES_DSN", "postgres://app:app@localhost:5432/app?sslmode=disable")
	store, err := db.NewStore(ctx, postgresDSN)
	if err != nil {
		log.Fatalf("failed connecting to postgres: %v", err)
	}
	defer store.Close()

	if err := store.Init(ctx); err != nil {
		log.Fatalf("failed initializing database: %v", err)
	}

	srv := &server{
		cacheBothLevels: cacheBothLevels,
		cacheL1Only:     cacheL1Only,
		cacheL2Only:     cacheL2Only,
		db:              store,
		l1TTL:           l1TTL,
		l2TTL:           l2TTL,
	}

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	// Standard endpoints (both levels)
	router.GET("/users/:id", srv.handleGetUser)
	router.POST("/users/refresh/:id", srv.handleRefreshUser)

	// Mode-specific endpoints
	router.GET("/users/l1-only/:id", srv.handleGetUserL1Only)
	router.GET("/users/l2-only/:id", srv.handleGetUserL2Only)
	router.GET("/users/both-levels/:id", srv.handleGetUserBothLevels)

	// Per-call override endpoints (using cacheBothLevels with overrides)
	router.GET("/users/override-l1/:id", srv.handleGetUserOverrideL1)
	router.GET("/users/override-l2/:id", srv.handleGetUserOverrideL2)
	router.POST("/users/set-l1-only/:id", srv.handleSetUserL1Only)
	router.POST("/users/set-l2-only/:id", srv.handleSetUserL2Only)

	// Cache inspection endpoints
	router.GET("/cache/stats/:id", srv.handleCacheStats)
	router.DELETE("/cache/clear/:id", srv.handleClearCache)

	log.Println("✓ Server configured with multiple cache mode endpoints")
	log.Println("  Standard: GET /users/:id, POST /users/refresh/:id")
	log.Println("  Mode-specific: GET /users/{l1-only,l2-only,both-levels}/:id")
	log.Println("  Overrides: GET /users/override-{l1,l2}/:id, POST /users/set-{l1,l2}-only/:id")
	log.Println("  Inspection: GET /cache/stats/:id, DELETE /cache/clear/:id")
	log.Println("server listening on :8080")
	if err := router.Run(":8080"); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

type server struct {
	cacheBothLevels cache_manager.Cache
	cacheL1Only     cache_manager.Cache
	cacheL2Only     cache_manager.Cache
	db              *db.Store
	l1TTL           time.Duration
	l2TTL           time.Duration
}

// Standard endpoint - uses both levels cache
func (s *server) handleGetUser(c *gin.Context) {
	s.getUserWithCache(c, s.cacheBothLevels, "both-levels", cache_manager.CacheOptions{
		L1TTL: s.l1TTL,
		L2TTL: s.l2TTL,
	})
}

// L1 only mode endpoint
func (s *server) handleGetUserL1Only(c *gin.Context) {
	s.getUserWithCache(c, s.cacheL1Only, "L1-only", cache_manager.CacheOptions{
		L1TTL: s.l1TTL,
	})
}

// L2 only mode endpoint
func (s *server) handleGetUserL2Only(c *gin.Context) {
	s.getUserWithCache(c, s.cacheL2Only, "L2-only", cache_manager.CacheOptions{
		L2TTL: s.l2TTL,
	})
}

// Both levels mode endpoint (explicit)
func (s *server) handleGetUserBothLevels(c *gin.Context) {
	s.getUserWithCache(c, s.cacheBothLevels, "both-levels-explicit", cache_manager.CacheOptions{
		L1TTL: 20 * time.Second,
		L2TTL: 40 * time.Second,
	})
}

// Override to L1 only (using both-levels cache with per-call override)
func (s *server) handleGetUserOverrideL1(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := parseID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	cacheKey := userCacheKey(id)
	var user db.User
	found, err := s.cacheBothLevels.Get(ctx, cacheKey, &user, cache_manager.CacheOptions{})
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if !found {
		user, err = s.db.GetUser(ctx, id)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, db.ErrUserNotFound) {
				status = http.StatusNotFound
			}
			writeError(c, status, err)
			return
		}

		// Override: write only to L1
		if err := s.cacheBothLevels.Set(ctx, cacheKey, user, cache_manager.CacheOptions{
			L1TTL:    s.l1TTL,
			TargetL1: cache_manager.BoolPtr(true),
			TargetL2: cache_manager.BoolPtr(false),
		}); err != nil {
			log.Printf("warn: failed setting cache (L1 override): %v", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"user":       user,
		"cache_mode": "override-L1-only",
		"from_cache": found,
	})
}

// Override to L2 only (using both-levels cache with per-call override)
func (s *server) handleGetUserOverrideL2(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := parseID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	cacheKey := userCacheKey(id)
	var user db.User
	found, err := s.cacheBothLevels.Get(ctx, cacheKey, &user, cache_manager.CacheOptions{})
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if !found {
		user, err = s.db.GetUser(ctx, id)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, db.ErrUserNotFound) {
				status = http.StatusNotFound
			}
			writeError(c, status, err)
			return
		}

		// Override: write only to L2
		if err := s.cacheBothLevels.Set(ctx, cacheKey, user, cache_manager.CacheOptions{
			L2TTL:    s.l2TTL,
			TargetL1: cache_manager.BoolPtr(false),
			TargetL2: cache_manager.BoolPtr(true),
		}); err != nil {
			log.Printf("warn: failed setting cache (L2 override): %v", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"user":       user,
		"cache_mode": "override-L2-only",
		"from_cache": found,
	})
}

// Helper function for standard get operations
func (s *server) getUserWithCache(c *gin.Context, cacheInstance cache_manager.Cache, mode string, opts cache_manager.CacheOptions) {
	ctx := c.Request.Context()
	id, err := parseID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	cacheKey := userCacheKey(id)
	var user db.User
	found, err := cacheInstance.Get(ctx, cacheKey, &user, cache_manager.CacheOptions{})
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if !found {
		user, err = s.db.GetUser(ctx, id)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, db.ErrUserNotFound) {
				status = http.StatusNotFound
			}
			writeError(c, status, err)
			return
		}

		if err := cacheInstance.Set(ctx, cacheKey, user, opts); err != nil {
			log.Printf("warn: failed setting cache (%s): %v", mode, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"user":       user,
		"cache_mode": mode,
		"from_cache": found,
	})
}

func (s *server) handleRefreshUser(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := parseID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	user, err := s.db.RefreshUser(ctx, id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, db.ErrUserNotFound) {
			status = http.StatusNotFound
		}
		writeError(c, status, err)
		return
	}

	// Clear from all cache instances
	cacheKey := userCacheKey(id)
	if err := s.cacheBothLevels.Delete(ctx, cacheKey); err != nil {
		log.Printf("warn: failed deleting from both-levels cache: %v", err)
	}
	if err := s.cacheL1Only.Delete(ctx, cacheKey); err != nil {
		log.Printf("warn: failed deleting from L1-only cache: %v", err)
	}
	if err := s.cacheL2Only.Delete(ctx, cacheKey); err != nil {
		log.Printf("warn: failed deleting from L2-only cache: %v", err)
	}

	c.JSON(http.StatusOK, user)
}

// Set user in L1 only
func (s *server) handleSetUserL1Only(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := parseID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	user, err := s.db.GetUser(ctx, id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, db.ErrUserNotFound) {
			status = http.StatusNotFound
		}
		writeError(c, status, err)
		return
	}

	cacheKey := userCacheKey(id)
	if err := s.cacheBothLevels.Set(ctx, cacheKey, user, cache_manager.CacheOptions{
		L1TTL:    s.l1TTL,
		TargetL1: cache_manager.BoolPtr(true),
		TargetL2: cache_manager.BoolPtr(false),
	}); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "User cached in L1 only",
		"user":    user,
	})
}

// Set user in L2 only
func (s *server) handleSetUserL2Only(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := parseID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	user, err := s.db.GetUser(ctx, id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, db.ErrUserNotFound) {
			status = http.StatusNotFound
		}
		writeError(c, status, err)
		return
	}

	cacheKey := userCacheKey(id)
	if err := s.cacheBothLevels.Set(ctx, cacheKey, user, cache_manager.CacheOptions{
		L2TTL:    s.l2TTL,
		TargetL1: cache_manager.BoolPtr(false),
		TargetL2: cache_manager.BoolPtr(true),
	}); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "User cached in L2 only",
		"user":    user,
	})
}

// Get cache stats for a user
func (s *server) handleCacheStats(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := parseID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	cacheKey := userCacheKey(id)

	var userBoth, userL1, userL2 db.User
	foundBoth, _ := s.cacheBothLevels.Get(ctx, cacheKey, &userBoth, cache_manager.CacheOptions{})
	foundL1, _ := s.cacheL1Only.Get(ctx, cacheKey, &userL1, cache_manager.CacheOptions{})
	foundL2, _ := s.cacheL2Only.Get(ctx, cacheKey, &userL2, cache_manager.CacheOptions{})

	c.JSON(http.StatusOK, gin.H{
		"cache_key":   cacheKey,
		"both_levels": gin.H{"cached": foundBoth},
		"l1_only":     gin.H{"cached": foundL1},
		"l2_only":     gin.H{"cached": foundL2},
	})
}

// Clear cache for a user from all instances
func (s *server) handleClearCache(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := parseID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	cacheKey := userCacheKey(id)

	errBoth := s.cacheBothLevels.Delete(ctx, cacheKey)
	errL1 := s.cacheL1Only.Delete(ctx, cacheKey)
	errL2 := s.cacheL2Only.Delete(ctx, cacheKey)

	c.JSON(http.StatusOK, gin.H{
		"message":     "Cache cleared",
		"cache_key":   cacheKey,
		"both_levels": errBoth == nil,
		"l1_only":     errL1 == nil,
		"l2_only":     errL2 == nil,
	})
}

func parseID(idParam string) (int, error) {
	return strconv.Atoi(idParam)
}

func userCacheKey(id int) string {
	return fmt.Sprintf("user:%d", id)
}

func writeError(c *gin.Context, status int, err error) {
	c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
}

func getenv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
		log.Printf("warn: invalid duration for %s=%s, using fallback %s", key, val, fallback)
	}
	return fallback
}
