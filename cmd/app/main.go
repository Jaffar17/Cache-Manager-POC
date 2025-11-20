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

	"go-cache-poc/internal/cache"
	"go-cache-poc/internal/db"
)

const defaultTTL = 5 * time.Minute

func main() {
	ctx := context.Background()

	bcConfig := bigcache.DefaultConfig(10 * time.Minute)
	bcConfig.CleanWindow = time.Minute
	bcConfig.Shards = 128

	bigCache, err := cache.NewBigCache(cache.BigCacheConfig{Config: bcConfig})
	if err != nil {
		log.Fatalf("failed creating bigcache: %v", err)
	}
	defer bigCache.Close()

	redisAddr := getenv("REDIS_ADDR", "localhost:6379")
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed connecting to redis at %s: %v", redisAddr, err)
	}
	defer redisClient.Close()

	redisCache, err := cache.NewRedisCache(redisClient)
	if err != nil {
		log.Fatalf("failed creating redis cache: %v", err)
	}

	serializer := cache.JSONSerializer{}
	multiLevel, err := NewAppCache(bigCache, redisCache, serializer)
	if err != nil {
		log.Fatalf("failed constructing cache: %v", err)
	}

	postgresDSN := getenv("POSTGRES_DSN", "postgres://app:app@localhost:5432/app?sslmode=disable")
	store, err := db.NewStore(ctx, postgresDSN)
	if err != nil {
		log.Fatalf("failed connecting to postgres: %v", err)
	}
	defer store.Close()

	if err := store.Init(ctx); err != nil {
		log.Fatalf("failed initializing database: %v", err)
	}

	srv := &server{cache: multiLevel, db: store}

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/users/:id", srv.handleGetUser)
	router.POST("/users/refresh/:id", srv.handleRefreshUser)

	log.Println("server listening on :8080")
	if err := router.Run(":8080"); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func NewAppCache(l1 *cache.BigCache, l2 *cache.RedisCache, serializer cache.Serializer) (cache.Cache, error) {
	var l1Raw cache.RawCache
	var l2Raw cache.RawCache
	if l1 != nil {
		l1Raw = l1
	}
	if l2 != nil {
		l2Raw = l2
	}
	return cache.NewMultiLevelCache(l1Raw, l2Raw, serializer, cache.MultiLevelConfig{WarmupTTL: defaultTTL})
}

type server struct {
	cache cache.Cache
	db    *db.Store
}

func (s *server) handleGetUser(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := parseID(c.Param("id"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	cacheKey := userCacheKey(id)
	var user db.User
	found, err := s.cache.Get(ctx, cacheKey, &user)
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

		if err := s.cache.Set(ctx, cacheKey, user, defaultTTL); err != nil {
			log.Printf("warn: failed setting cache: %v", err)
		}
	}

	c.JSON(http.StatusOK, user)
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

	if err := s.cache.Delete(ctx, userCacheKey(id)); err != nil {
		log.Printf("warn: failed deleting cache: %v", err)
	}

	c.JSON(http.StatusOK, user)
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
