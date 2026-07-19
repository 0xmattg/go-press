package metamaskidentity

import (
	"sync"
	"time"
)

type limitBucket struct {
	count     int
	resetTime time.Time
}

type requestLimiter struct {
	mu      sync.Mutex
	buckets map[string]limitBucket
}

func newRequestLimiter() *requestLimiter {
	return &requestLimiter{buckets: make(map[string]limitBucket)}
}

func (l *requestLimiter) Allow(key string, limit int, window time.Duration, now time.Time) bool {
	if l == nil || key == "" || limit <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket, ok := l.buckets[key]
	if !ok && len(l.buckets) >= 8192 {
		l.cleanupExpired(now)
		if len(l.buckets) >= 8192 {
			return false
		}
	}
	if !ok || !now.Before(bucket.resetTime) {
		l.buckets[key] = limitBucket{count: 1, resetTime: now.Add(window)}
		return true
	}
	if bucket.count >= limit {
		return false
	}
	bucket.count++
	l.buckets[key] = bucket
	return true
}

func (l *requestLimiter) cleanupExpired(now time.Time) {
	for key, bucket := range l.buckets {
		if !now.Before(bucket.resetTime) {
			delete(l.buckets, key)
		}
	}
}
