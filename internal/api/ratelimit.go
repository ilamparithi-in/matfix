package api

import (
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

// defaultRateLimitRPS is the per-key request rate used when a key's
// Permissions.RateLimitRPS is 0 (unset).
const defaultRateLimitRPS = 100

// # RateLimiter

// rateLimiter maintains a per-key token bucket using golang.org/x/time/rate.
// Buckets are created lazily on first use.
type rateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

// newRateLimiter returns an empty rateLimiter.
func newRateLimiter() *rateLimiter {
	return &rateLimiter{limiters: make(map[string]*rate.Limiter)}
}

// allow reports whether keyID may proceed under its rate limit.
// rps is the per-key limit from Permissions; 0 means use defaultRateLimitRPS.
// When the key's configured RPS changes, the limiter is replaced.
func (rl *rateLimiter) allow(keyID string, rps int) bool {
	if rps <= 0 {
		rps = defaultRateLimitRPS
	}
	rl.mu.Lock()
	l, ok := rl.limiters[keyID]
	if !ok || int(l.Limit()) != rps {
		// Burst is set to 2× RPS to absorb small bursts without penalising
		// bursty-but-compliant callers.
		l = rate.NewLimiter(rate.Limit(rps), rps*2)
		rl.limiters[keyID] = l
	}
	rl.mu.Unlock()
	return l.Allow()
}

// middleware returns an http.Handler middleware that enforces per-key rate
// limiting. Requests that exceed the limit receive 429 Too Many Requests.
func (rl *rateLimiter) middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			keyID := keyIDFromCtx(r.Context())
			if keyID == "" {
				// No key in context — auth middleware should have rejected first.
				next.ServeHTTP(w, r)
				return
			}
			perms := permissionsFromCtx(r.Context())
			rps := 0
			if perms != nil {
				rps = perms.RateLimitRPS
			}
			if !rl.allow(keyID, rps) {
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded", "rate_limited")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
