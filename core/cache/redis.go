// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache is an L2 distributed cache backed by Redis.
//
// Every key, including the ones removed by DeleteByPrefix and Flush, lives
// under "gopress:<namespace>:", so sites sharing a Redis database neither read
// each other's pages nor clear each other's caches.
type RedisCache struct {
	client *redis.Client
	prefix string // key prefix to avoid collisions
}

// NewRedisCache creates a Redis-backed cache with the legacy, site-unscoped
// "gopress:" prefix. Returns nil if connection fails. Prefer
// NewNamespacedRedisCache whenever more than one site may share the database.
func NewRedisCache(addr, password string, db int) *RedisCache {
	return NewNamespacedRedisCache(addr, password, db, "")
}

// NewNamespacedRedisCache creates a Redis-backed cache whose keys are scoped
// to namespace. Returns nil if connection fails; the caller should fall back
// to noop. An empty namespace keeps the legacy "gopress:" prefix.
func NewNamespacedRedisCache(addr, password string, db int, namespace string) *RedisCache {
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
		prefix: redisKeyPrefix(namespace),
	}
}

func redisKeyPrefix(namespace string) string {
	if namespace == "" {
		return "gopress:"
	}
	return "gopress:" + namespace + ":"
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
