package middleware

import (
	"sync"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// RateLimiter manages keyed token-bucket rate limiters in memory.
type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*clientLimiter
	rate     rate.Limit
	burst    int
	ttl      time.Duration
}

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter creates a new RateLimiter instance.
func NewRateLimiter(r rate.Limit, burst int, ttl time.Duration) *RateLimiter {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &RateLimiter{
		limiters: make(map[string]*clientLimiter),
		rate:     r,
		burst:    burst,
		ttl:      ttl,
	}
}

func (rl *RateLimiter) getLimiter(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	// Periodic cleanup of idle limiters when map grows
	if len(rl.limiters) > 1000 {
		for k, cl := range rl.limiters {
			if now.Sub(cl.lastSeen) > rl.ttl {
				delete(rl.limiters, k)
			}
		}
	}

	cl, exists := rl.limiters[key]
	if !exists {
		cl = &clientLimiter{
			limiter: rate.NewLimiter(rl.rate, rl.burst),
		}
		rl.limiters[key] = cl
	}
	cl.lastSeen = now
	return cl.limiter
}

// RateLimitByTenant returns a Gin middleware that enforces token-bucket rate limiting
// keyed by tenant ID and environment ID (falling back to client IP if tenant ID is empty).
func RateLimitByTenant(r rate.Limit, burst int) gin.HandlerFunc {
	rl := NewRateLimiter(r, burst, 10*time.Minute)
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		tenantID := types.GetTenantID(ctx)
		envID := types.GetEnvironmentID(ctx)

		var key string
		if tenantID != "" {
			if envID != "" {
				key = tenantID + ":" + envID
			} else {
				key = tenantID
			}
		} else {
			key = c.ClientIP()
		}

		limiter := rl.getLimiter(key)
		if !limiter.Allow() {
			_ = c.Error(ierr.NewError("rate limit exceeded").
				WithHint("Too many requests, please slow down.").
				Mark(ierr.ErrTooManyRequests))
			c.Abort()
			return
		}

		c.Next()
	}
}
