package api

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"go-press/core/user"
)

// RateLimiter implements a simple token bucket rate limiter per IP.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    int           // tokens per interval
	burst   int           // max burst
	window  time.Duration // time window
}

type bucket struct {
	tokens    int
	lastReset time.Time
}

// NewRateLimiter creates a rate limiter allowing `rate` requests per `window`.
func NewRateLimiter(rate int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		burst:   rate * 2,
		window:  window,
	}
	// Cleanup old buckets periodically
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[ip]
	now := time.Now()
	if !ok {
		rl.buckets[ip] = &bucket{tokens: rl.burst - 1, lastReset: now}
		return true
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(b.lastReset)
	if elapsed >= rl.window {
		b.tokens = rl.burst
		b.lastReset = now
	}

	if b.tokens > 0 {
		b.tokens--
		return true
	}
	return false
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-10 * time.Minute)
		for ip, b := range rl.buckets {
			if b.lastReset.Before(cutoff) {
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware returns a Gin middleware that enforces rate limiting.
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !rl.allow(ip) {
			c.JSON(http.StatusTooManyRequests, response{
				Success: false,
				Error:   &apiError{Code: "rate_limited", Message: "Too many requests"},
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// CORSMiddleware adds CORS headers for API endpoints.
func CORSMiddleware(allowedOrigins string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if allowedOrigins == "" {
			allowedOrigins = "*"
		}
		c.Header("Access-Control-Allow-Origin", allowedOrigins)
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-API-Key")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// APIKeyAuth returns a Gin middleware that validates API key from header or query.
// If no keys are configured (empty map), all requests are allowed (public API).
func APIKeyAuth(validKeys map[string]bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// If no keys configured, skip auth (public API mode)
		if len(validKeys) == 0 {
			c.Next()
			return
		}

		key := c.GetHeader("X-API-Key")
		if key == "" {
			key = c.Query("api_key")
		}
		if key == "" || !validKeys[key] {
			respondError(c, http.StatusUnauthorized, "unauthorized", "Invalid or missing API key")
			c.Abort()
			return
		}
		c.Next()
	}
}

// JWTAuth returns a Gin middleware that validates JWT Bearer tokens for API.
func JWTAuth(auth *user.Auth) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			// No auth header — continue as anonymous
			c.Next()
			return
		}

		token := strings.TrimPrefix(header, "Bearer ")
		claims, err := auth.ParseToken(token)
		if err != nil {
			respondError(c, http.StatusUnauthorized, "invalid_token", "Invalid or expired token")
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Next()
	}
}
