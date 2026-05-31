package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type ipBucket struct {
	count    int
	resetAt  time.Time
	mu       sync.Mutex
}

type rateLimiter struct {
	buckets  map[string]*ipBucket
	mu       sync.RWMutex
	max      int
	window   time.Duration
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		buckets: make(map[string]*ipBucket),
		max:     max,
		window:  window,
	}
	// Cleanup goroutine — remove stale buckets every minute
	go func() {
		for range time.Tick(time.Minute) {
			rl.mu.Lock()
			now := time.Now()
			for ip, b := range rl.buckets {
				b.mu.Lock()
				if now.After(b.resetAt) {
					delete(rl.buckets, ip)
				}
				b.mu.Unlock()
			}
			rl.mu.Unlock()
		}
	}()
	return rl
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.RLock()
	b, ok := rl.buckets[ip]
	rl.mu.RUnlock()

	if !ok {
		rl.mu.Lock()
		b = &ipBucket{resetAt: time.Now().Add(rl.window)}
		rl.buckets[ip] = b
		rl.mu.Unlock()
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if time.Now().After(b.resetAt) {
		b.count = 0
		b.resetAt = time.Now().Add(rl.window)
	}

	if b.count >= rl.max {
		return false
	}
	b.count++
	return true
}

// AuthRateLimit returns a middleware that limits requests to max per window per IP.
func AuthRateLimit(max int, window time.Duration) gin.HandlerFunc {
	rl := newRateLimiter(max, window)
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !rl.allow(ip) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "คำขอมากเกินไป กรุณารอสักครู่แล้วลองใหม่",
			})
			return
		}
		c.Next()
	}
}
