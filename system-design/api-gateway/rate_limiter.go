package apigateway

// Rate limiting: per-tenant sliding-window counter
//
// ── Why sliding window instead of token bucket? ──────────────────────────────
//
//  Token bucket    good for bursty traffic (credits accumulate).
//                  Risk: a tenant can fire 2× quota in ~2× window boundary.
//
//  Fixed window    simplest. Bad: a burst at t=59s and t=61s lets 2× quota
//                  through at the boundary.
//
//  Sliding window  counts requests in the rolling last N seconds.
//                  Smoothest limit, no boundary burst. Slightly more memory.
//                  Used by Stripe, GitHub, and most production API gateways.
//
// ── Redis-backed sliding window (production) ─────────────────────────────────
//
//  ZADD tenant:{id}:rl {now_ms} {uuid}        -- record request
//  ZREMRANGEBYSCORE tenant:{id}:rl 0 {now_ms - window_ms}  -- evict old
//  ZCARD tenant:{id}:rl                        -- count in window
//  EXPIRE tenant:{id}:rl {window_sec+1}        -- auto-clean
//
//  All four commands run in a single MULTI/EXEC transaction (atomic).
//  The sorted set key is the timestamp; values are unique request IDs.
//  TTL ensures keys are cleaned up even if the tenant stops sending traffic.
//
//  Why Redis, not in-process memory?
//    - Gateway runs as N replicas behind a load balancer.
//    - In-process counters are per-replica, so a tenant sees N× the quota.
//    - Redis is the shared, consistent counter store across replicas.
//
// ── In-process implementation below (for teaching) ───────────────────────────

import (
	"net/http"
	"sync"
	"time"
)

// window is the time bucket used by the sliding-window counter.
const defaultWindow = time.Minute

// requestTimestamps holds the ring of recent request times for one tenant.
type requestTimestamps struct {
	mu   sync.Mutex
	ring []time.Time // treated as FIFO; append new, evict old
}

// allowed reports true and records the request if it is within quota.
// limit is the max requests permitted in the sliding window.
func (rt *requestTimestamps) allowed(limit int, window time.Duration) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)

	// Evict timestamps older than the window — find first valid index.
	i := 0
	for i < len(rt.ring) && rt.ring[i].Before(cutoff) {
		i++
	}
	rt.ring = rt.ring[i:] // drop the stale prefix (O(n) but n is small for /min)

	if len(rt.ring) >= limit {
		return false // quota exhausted
	}
	rt.ring = append(rt.ring, now)
	return true
}

// ---- RateLimiter -----------------------------------------------------------

// RateLimiter enforces per-tenant sliding-window rate limits.
//
// Key insight: limits come from the Tenant record (loaded by tenantMiddleware),
// so each tenant can have a different quota based on its Plan or a custom
// override — all without restarting the gateway.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*requestTimestamps
	window  time.Duration
	store   TenantStore // to read current quota per tenant
}

func NewRateLimiter(store TenantStore) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*requestTimestamps),
		window:  defaultWindow,
		store:   store,
	}
}

func (rl *RateLimiter) bucket(tenantID string) *requestTimestamps {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.buckets[tenantID]
	if !ok {
		b = &requestTimestamps{}
		rl.buckets[tenantID] = b
	}
	return b
}

// Middleware is the http.Handler middleware for rate limiting.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := TenantFromCtx(r.Context())
		if tenantID == "" {
			// AuthN should have rejected this already; belt-and-suspenders.
			writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing tenant")
			return
		}

		// Read the tenant's current quota. If the tenant was just upgraded,
		// the new limit is effective immediately on the next request.
		tenant, err := rl.store.Get(r.Context(), tenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", "rate limiter store error")
			return
		}

		b := rl.bucket(tenantID)
		if !b.allowed(tenant.RateLimit(), rl.window) {
			// 429 Too Many Requests — include retry guidance.
			w.Header().Set("X-RateLimit-Limit", itoa(tenant.RateLimit()))
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED",
				"quota exceeded; see X-RateLimit-Limit header")
			return
		}

		// Expose quota info to callers on successful requests too — good DX.
		b.mu.Lock()
		remaining := tenant.RateLimit() - len(b.ring)
		b.mu.Unlock()

		w.Header().Set("X-RateLimit-Limit", itoa(tenant.RateLimit()))
		w.Header().Set("X-RateLimit-Remaining", itoa(remaining))
		w.Header().Set("X-RateLimit-Window", "60s")

		next.ServeHTTP(w, r)
	})
}

// itoa converts int to string without importing strconv in this snippet.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

// ── Advanced: Token Bucket (for comparison) ──────────────────────────────────
//
// A token bucket refills at `rate` tokens/sec up to `capacity`.
// Each request consumes one token. Requests that find 0 tokens are rejected.
//
//  type tokenBucket struct {
//      mu       sync.Mutex
//      tokens   float64
//      capacity float64
//      rate     float64    // tokens per second
//      last     time.Time
//  }
//
//  func (tb *tokenBucket) Allow() bool {
//      tb.mu.Lock()
//      defer tb.mu.Unlock()
//      now := time.Now()
//      elapsed := now.Sub(tb.last).Seconds()
//      tb.last = now
//      tb.tokens = min(tb.capacity, tb.tokens + elapsed*tb.rate)
//      if tb.tokens < 1 {
//          return false
//      }
//      tb.tokens--
//      return true
//  }
//
// Token bucket is better when you want to allow short bursts (e.g. a batch
// import) without permanently raising the steady-state quota.
