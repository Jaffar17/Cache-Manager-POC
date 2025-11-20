package cache

import (
	"context"
	"encoding/binary"
	"errors"
	"time"

	"github.com/allegro/bigcache/v3"
)

// BigCache wraps github.com/allegro/bigcache for L1 caching.
type BigCache struct {
	cache *bigcache.BigCache
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

	return &BigCache{cache: bc}, nil
}

// Close shuts down the cache.
func (b *BigCache) Close() error {
	if b == nil || b.cache == nil {
		return nil
	}
	return b.cache.Close()
}

// Get returns payload if present and not expired.
func (b *BigCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	if b == nil || b.cache == nil {
		return nil, false, errors.New("bigcache not initialized")
	}

	data, err := b.cache.Get(key)
	if err != nil {
		if errors.Is(err, bigcache.ErrEntryNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}

	payload, ok := decodeEntry(data)
	if !ok {
		_ = b.cache.Delete(key)
		return nil, false, nil
	}

	return payload, true, nil
}

// Set stores payload with TTL metadata.
func (b *BigCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if b == nil || b.cache == nil {
		return errors.New("bigcache not initialized")
	}

	entry := encodeEntry(value, ttl)
	return b.cache.Set(key, entry)
}

// Delete removes an entry.
func (b *BigCache) Delete(_ context.Context, key string) error {
	if b == nil || b.cache == nil {
		return errors.New("bigcache not initialized")
	}
	return b.cache.Delete(key)
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
	// TODO: Rethink about this as if its nearing expiry, we have to think before serving
	if expiry > 0 && time.Now().UnixNano() > expiry {
		return nil, false
	}
	cp := make([]byte, len(raw)-8)
	copy(cp, raw[8:])
	return cp, true
}
