package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache is an L2 distributed cache backed by Redis.
type RedisCache struct {
	client *redis.Client
	prefix string // key prefix to avoid collisions
}

// NewRedisCache creates a Redis-backed cache. Returns nil if connection fails.
// The caller should fall back to noop if this returns nil.
func NewRedisCache(addr, password string, db int) *RedisCache {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		DialTimeout:  3 * time.Second,
		PoolSize:     20,
	})

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil
	}

	return &RedisCache{
		client: client,
		prefix: "gopress:",
	}
}

func (r *RedisCache) key(k string) string {
	return r.prefix + k
}

func (r *RedisCache) Get(key string) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	val, err := r.client.Get(ctx, r.key(key)).Bytes()
	if err != nil {
		return nil, false
	}
	return val, true
}

func (r *RedisCache) Set(key string, value []byte, ttl time.Duration) {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = r.client.Set(ctx, r.key(key), value, ttl).Err()
}

func (r *RedisCache) Delete(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = r.client.Del(ctx, r.key(key)).Err()
}

func (r *RedisCache) DeleteByPrefix(prefix string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pattern := r.key(prefix) + "*"
	var cursor uint64
	for {
		keys, nextCursor, err := r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			_ = r.client.Del(ctx, keys...).Err()
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
}

func (r *RedisCache) Flush() {
	// Only flush keys with our prefix, not the entire Redis
	r.DeleteByPrefix("")
}

// Close gracefully closes the Redis connection.
func (r *RedisCache) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}
