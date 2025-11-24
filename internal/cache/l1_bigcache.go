package cache

import (
	"context"
	"encoding/binary"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/allegro/bigcache/v3"
)

// BigCache wraps github.com/allegro/bigcache for L1 caching.
type BigCache struct {
	cache *bigcache.BigCache
	stats *hitTracker
}

// BigCacheConfig allows customizing the underlying cache.
type BigCacheConfig struct {
	Config bigcache.Config
}

// NewBigCache constructs a BigCache instance.
func NewBigCache(cfg BigCacheConfig) (*BigCache, error) {
	config := cfg.Config
	if config.LifeWindow == 0 {
		config = bigcache.DefaultConfig(10 * time.Minute)
		config.CleanWindow = time.Minute
	}

	bc, err := bigcache.NewBigCache(config)
	if err != nil {
		return nil, err
	}

	return &BigCache{cache: bc, stats: newHitTracker()}, nil
}

// Close shuts down the cache.
func (b *BigCache) Close() error {
	if b == nil || b.cache == nil {
		return nil
	}
	return b.cache.Close()
}

// Get returns payload if present and not expired.
func (b *BigCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if b == nil || b.cache == nil {
		return nil, false, errors.New("bigcache not initialized")
	}

	data, err := b.cache.Get(key)
	if err != nil {
		if errors.Is(err, bigcache.ErrEntryNotFound) {
			b.stats.recordMiss(key)
			b.logSnapshot("miss", key)
			return nil, false, nil
		}
		return nil, false, err
	}

	payload, ok := decodeEntry(data)
	if !ok {
		_ = b.cache.Delete(key)
		b.stats.recordMiss(key)
		b.logSnapshot("miss", key)
		return nil, false, nil
	}

	b.stats.recordHit(key)
	b.logSnapshot("hit", key)
	return payload, true, nil
}

// Set stores payload with TTL metadata.
func (b *BigCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if b == nil || b.cache == nil {
		return errors.New("bigcache not initialized")
	}

	entry := encodeEntry(value, ttl)
	if err := b.cache.Set(key, entry); err != nil {
		return err
	}
	b.logSnapshot("set", key)
	return nil
}

// Delete removes an entry.
func (b *BigCache) Delete(ctx context.Context, key string) error {
	if b == nil || b.cache == nil {
		return errors.New("bigcache not initialized")
	}
	b.stats.delete(key)
	if err := b.cache.Delete(key); err != nil {
		return err
	}
	b.logSnapshot("delete", key)
	return nil
}

// Snapshot returns a shallow copy of the cached keys with their hit counts.
func (b *BigCache) Snapshot() map[string]int {
	if b == nil || b.stats == nil {
		return map[string]int{}
	}
	return b.stats.snapshot()
}

type hitTracker struct {
	mu   sync.RWMutex
	data map[string]int
}

func newHitTracker() *hitTracker {
	return &hitTracker{data: make(map[string]int)}
}

func (h *hitTracker) recordHit(key string) {
	if key == "" {
		return
	}
	h.mu.Lock()
	h.data[key]++
	h.mu.Unlock()
}

func (h *hitTracker) recordMiss(key string) {
	if key == "" {
		return
	}
	h.mu.Lock()
	if _, ok := h.data[key]; !ok {
		h.data[key] = 0
	}
	h.mu.Unlock()
}

func (h *hitTracker) delete(key string) {
	h.mu.Lock()
	delete(h.data, key)
	h.mu.Unlock()
}

func (h *hitTracker) snapshot() map[string]int {
	h.mu.RLock()
	copy := make(map[string]int, len(h.data))
	for k, v := range h.data {
		copy[k] = v
	}
	h.mu.RUnlock()
	return copy
}

func (b *BigCache) logSnapshot(action, key string) {
	if b == nil {
		return
	}
	log.Printf("[bigcache] action=%s key=%s snapshot=%v", action, key, b.Snapshot())
}

func encodeEntry(payload []byte, ttl time.Duration) []byte {
	expiry := int64(0)
	if ttl > 0 {
		expiry = time.Now().Add(ttl).UnixNano()
	}

	out := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint64(out[:8], uint64(expiry))
	copy(out[8:], payload)
	return out
}

func decodeEntry(raw []byte) ([]byte, bool) {
	if len(raw) < 8 {
		return nil, false
	}
	expiry := int64(binary.LittleEndian.Uint64(raw[:8]))
	if expiry > 0 && time.Now().UnixNano() > expiry {
		return nil, false
	}
	cp := make([]byte, len(raw)-8)
	copy(cp, raw[8:])
	return cp, true
}
