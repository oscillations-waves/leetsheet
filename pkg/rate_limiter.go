package pkg

// Rate Limiter - System Design Implementation
//
// Three classic algorithms:
//   1. Token Bucket   - tokens refill at a steady rate; allows controlled bursting
//   2. Fixed Window   - count requests per fixed time window; simple but has boundary spike issues
//   3. Sliding Window - tracks individual request timestamps; accurate, no boundary spikes

import (
	"fmt"
	"sync"
	"time"
)

// ─── 1. Token Bucket ────────────────────────────────────────────────────────

// TokenBucket allows up to `capacity` tokens. Tokens refill at `refillRate`
// per `refillInterval`. A request consumes one token; if empty, it is rejected.
type TokenBucket struct {
	mu             sync.Mutex
	capacity       int
	tokens         int
	refillRate     int           // tokens added per interval
	refillInterval time.Duration
	lastRefill     time.Time
}

func NewTokenBucket(capacity, refillRate int, refillInterval time.Duration) *TokenBucket {
	return &TokenBucket{
		capacity:       capacity,
		tokens:         capacity,
		refillRate:     refillRate,
		refillInterval: refillInterval,
		lastRefill:     time.Now(),
	}
}

func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	if tb.tokens > 0 {
		tb.tokens--
		return true
	}
	return false
}

func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)
	intervals := int(elapsed / tb.refillInterval)
	if intervals > 0 {
		tb.tokens = min(tb.capacity, tb.tokens+intervals*tb.refillRate)
		tb.lastRefill = tb.lastRefill.Add(time.Duration(intervals) * tb.refillInterval)
	}
}

// ─── 2. Fixed Window Counter ─────────────────────────────────────────────────

// FixedWindow resets its counter every `windowSize`. Simple but can allow
// 2× the limit at window boundaries (burst across two windows).
type FixedWindow struct {
	mu         sync.Mutex
	limit      int
	count      int
	windowSize time.Duration
	windowStart time.Time
}

func NewFixedWindow(limit int, windowSize time.Duration) *FixedWindow {
	return &FixedWindow{
		limit:       limit,
		windowSize:  windowSize,
		windowStart: time.Now(),
	}
}

func (fw *FixedWindow) Allow() bool {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	now := time.Now()
	if now.Sub(fw.windowStart) >= fw.windowSize {
		fw.count = 0
		fw.windowStart = now
	}

	if fw.count < fw.limit {
		fw.count++
		return true
	}
	return false
}

// ─── 3. Sliding Window Log ───────────────────────────────────────────────────

// SlidingWindow keeps a log of request timestamps. Any request older than
// `windowSize` is evicted before checking the limit. More accurate than
// fixed window but uses more memory under high traffic.
type SlidingWindow struct {
	mu         sync.Mutex
	limit      int
	windowSize time.Duration
	timestamps []time.Time
}

func NewSlidingWindow(limit int, windowSize time.Duration) *SlidingWindow {
	return &SlidingWindow{
		limit:      limit,
		windowSize: windowSize,
	}
}

func (sw *SlidingWindow) Allow() bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-sw.windowSize)

	// Evict timestamps outside the window
	i := 0
	for i < len(sw.timestamps) && sw.timestamps[i].Before(cutoff) {
		i++
	}
	sw.timestamps = sw.timestamps[i:]

	if len(sw.timestamps) < sw.limit {
		sw.timestamps = append(sw.timestamps, now)
		return true
	}
	return false
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ─── Demo ────────────────────────────────────────────────────────────────────

type rateLimiter interface {
	Allow() bool
}

func simulateLimiter(name string, limiter rateLimiter, requests int, delay time.Duration) {
	fmt.Printf("\n--- %s ---\n", name)
	allowed, rejected := 0, 0
	for i := 1; i <= requests; i++ {
		if limiter.Allow() {
			fmt.Printf("  req %-2d → ✓ allowed\n", i)
			allowed++
		} else {
			fmt.Printf("  req %-2d → ✗ rejected\n", i)
			rejected++
		}
		if delay > 0 {
			time.Sleep(delay)
		}
	}
	fmt.Printf("  Result: %d allowed, %d rejected\n", allowed, rejected)
}

func RunRateLimiter() {
	fmt.Println("\n=== Rate Limiter ===")
	fmt.Println("Settings: limit=3 requests per second, 10 rapid requests then a pause")

	// Token Bucket: capacity=3, refills 3 tokens/sec
	tb := NewTokenBucket(3, 3, time.Second)
	simulateLimiter("Token Bucket (burst=3, refill=3/s)", tb, 6, 0)
	time.Sleep(time.Second) // let bucket refill
	fmt.Println("  [1s pause — bucket refills]")
	simulateLimiter("Token Bucket (after refill)", tb, 4, 0)

	// Fixed Window: 3 requests per second
	fw := NewFixedWindow(3, time.Second)
	simulateLimiter("Fixed Window (3 req/s)", fw, 6, 0)
	time.Sleep(time.Second)
	fmt.Println("  [1s pause — window resets]")
	simulateLimiter("Fixed Window (after reset)", fw, 4, 0)

	// Sliding Window: 3 requests per second
	sw := NewSlidingWindow(3, time.Second)
	simulateLimiter("Sliding Window (3 req/s)", sw, 6, 0)
	time.Sleep(time.Second)
	fmt.Println("  [1s pause — old timestamps expire]")
	simulateLimiter("Sliding Window (after expiry)", sw, 4, 0)
}
