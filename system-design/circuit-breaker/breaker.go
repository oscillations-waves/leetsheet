// Package circuitbreaker teaches the Circuit Breaker resilience pattern.
//
// A circuit breaker wraps calls to an upstream dependency and stops forwarding
// requests when the upstream is unhealthy, giving it time to recover and
// preventing cascading failures.
//
// State machine:
//
//	              failure rate ≥ threshold
//	┌─────────┐  ─────────────────────────►  ┌──────────┐
//	│  Closed │                               │   Open   │
//	└─────────┘  ◄─────────────────────────  └──────────┘
//	     ▲              probe succeeds              │
//	     │                                          │ OpenTimeout elapsed
//	     │        ┌──────────────┐                  │
//	     └────────│  Half-Open   │◄─────────────────┘
//	probe success └──────────────┘
//	(window reset)      │ probe fails
//	                    ▼
//	                 Open (timer restarted)
//
// Key design decisions:
//   - Sliding call window (ring buffer): failure rate is computed over the last
//     N calls, not just consecutive ones, to tolerate brief transient errors.
//   - MinimumCalls guard: the circuit will not trip until at least MinimumCalls
//     have been recorded, preventing false positives on startup.
//   - Single probe in HalfOpen: concurrent callers are shed with ErrCircuitOpen
//     while the probe is in-flight, keeping recovery traffic minimal.
//   - OnStateChange hook: fire-and-forget goroutine so the mutex is never held
//     during user code, preventing deadlocks.
//   - Context propagation: Execute accepts a context so callers can set
//     per-call deadlines that combine with the breaker's own logic.
package circuitbreaker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ── State ─────────────────────────────────────────────────────────────────────

// State represents the circuit breaker's current operating mode.
type State int

const (
	// StateClosed is normal operation — all requests pass through to the
	// upstream. Outcomes are recorded in the sliding window; once the failure
	// rate reaches FailureRateThreshold the circuit trips to StateOpen.
	StateClosed State = iota

	// StateOpen means the circuit is tripped. Execute returns ErrCircuitOpen
	// immediately without calling the upstream, giving it time to recover.
	// After OpenTimeout the breaker moves to StateHalfOpen.
	StateOpen

	// StateHalfOpen is the probe state. Exactly one request is allowed through.
	// Success → StateClosed (window cleared). Failure → StateOpen (timer reset).
	// All other concurrent callers receive ErrCircuitOpen.
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// ErrCircuitOpen is returned by Execute when the breaker is StateOpen or when
// another goroutine is already probing in StateHalfOpen.
var ErrCircuitOpen = errors.New("circuit breaker: circuit is open")

// ── Config ────────────────────────────────────────────────────────────────────

// Config holds all tunable parameters for a CircuitBreaker.
type Config struct {
	// WindowSize is the number of most-recent calls tracked for failure-rate
	// calculation. Defaults to 10 when ≤ 0.
	WindowSize int

	// FailureRateThreshold is the fraction [0,1] of failures in the window that
	// trips the circuit. E.g. 0.5 means ≥50% failures → open. Defaults to 0.5.
	FailureRateThreshold float64

	// MinimumCalls is the minimum number of calls that must be in the window
	// before the failure rate is evaluated. Guards against false-tripping on the
	// very first call. Defaults to WindowSize.
	MinimumCalls int

	// OpenTimeout is how long the circuit stays in StateOpen before probing.
	// Defaults to 60 s.
	OpenTimeout time.Duration

	// OnStateChange is an optional callback invoked (in its own goroutine) on
	// every state transition. Use it for metrics counters, alerting, and logs.
	OnStateChange func(name string, from, to State)
}

func (c *Config) applyDefaults() {
	if c.WindowSize <= 0 {
		c.WindowSize = 10
	}
	if c.FailureRateThreshold <= 0 {
		c.FailureRateThreshold = 0.5
	}
	if c.MinimumCalls <= 0 {
		c.MinimumCalls = c.WindowSize
	}
	if c.OpenTimeout <= 0 {
		c.OpenTimeout = 60 * time.Second
	}
}

// ── CircuitBreaker ────────────────────────────────────────────────────────────

// CircuitBreaker wraps calls to an upstream dependency with fault-tolerance
// logic. It is safe for concurrent use by multiple goroutines.
type CircuitBreaker struct {
	name string
	cfg  Config

	mu       sync.Mutex
	state    State
	win      *window
	openedAt time.Time // when the circuit last entered StateOpen
	probing  bool      // true while a half-open probe is in-flight
}

// New creates a CircuitBreaker with the given name and config.
// name is typically the downstream service name (e.g. "payments-svc").
// Zero-value Config fields are replaced with sensible defaults.
func New(name string, cfg Config) *CircuitBreaker {
	cfg.applyDefaults()
	return &CircuitBreaker{
		name:  name,
		cfg:   cfg,
		state: StateClosed,
		win:   newWindow(cfg.WindowSize),
	}
}

// Execute calls f if the circuit permits the request.
//
//   - StateClosed  → always calls f; records outcome in the window.
//   - StateOpen    → returns ErrCircuitOpen immediately (f is never called).
//   - StateHalfOpen → allows exactly one probe; all concurrent callers get
//     ErrCircuitOpen while the probe is in-flight.
//
// The error returned by f is propagated to the caller unchanged so that
// business logic can still inspect it even though the breaker recorded a failure.
func (cb *CircuitBreaker) Execute(ctx context.Context, f func(context.Context) error) error {
	if err := cb.beforeCall(); err != nil {
		return err
	}
	err := f(ctx)
	cb.afterCall(err == nil)
	return err
}

// State returns the current state. Useful for health-check endpoints.
// This also advances an open circuit to half-open if the timeout has elapsed.
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.maybeAdvanceToHalfOpen()
	return cb.state
}

// Name returns the breaker's name.
func (cb *CircuitBreaker) Name() string { return cb.name }

// Metrics returns a point-in-time snapshot of the breaker's counters.
func (cb *CircuitBreaker) Metrics() Metrics {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return Metrics{
		State:       cb.state,
		TotalCalls:  cb.win.count(),
		Failures:    cb.win.failures,
		FailureRate: cb.win.failureRate(),
	}
}

// Metrics is a read-only snapshot of CircuitBreaker counters.
type Metrics struct {
	State       State
	TotalCalls  int
	Failures    int
	FailureRate float64
}

func (m Metrics) String() string {
	return fmt.Sprintf("state=%s calls=%d failures=%d rate=%.2f",
		m.State, m.TotalCalls, m.Failures, m.FailureRate)
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// beforeCall checks the current state and decides whether to allow the call.
// Returns nil to permit the call, or ErrCircuitOpen to reject it.
func (cb *CircuitBreaker) beforeCall() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.maybeAdvanceToHalfOpen()

	switch cb.state {
	case StateClosed:
		return nil
	case StateOpen:
		return ErrCircuitOpen
	case StateHalfOpen:
		if cb.probing {
			// A probe is already in-flight; shed this caller.
			return ErrCircuitOpen
		}
		cb.probing = true
		return nil
	}
	return nil
}

// afterCall records the outcome of a completed call and drives state transitions.
func (cb *CircuitBreaker) afterCall(success bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		cb.win.record(success)
		if cb.shouldTrip() {
			cb.transition(StateOpen)
		}
	case StateHalfOpen:
		cb.probing = false
		if success {
			cb.win.reset() // clear stale failure data before re-opening to traffic
			cb.transition(StateClosed)
		} else {
			cb.transition(StateOpen)
		}
	}
	// StateOpen: calls are rejected in beforeCall so afterCall is never reached.
}

// maybeAdvanceToHalfOpen transitions an open circuit to half-open if the
// configured OpenTimeout has elapsed. Must be called with cb.mu held.
func (cb *CircuitBreaker) maybeAdvanceToHalfOpen() {
	if cb.state == StateOpen && time.Since(cb.openedAt) >= cb.cfg.OpenTimeout {
		cb.transition(StateHalfOpen)
	}
}

// shouldTrip reports whether the window has accumulated enough data and the
// failure rate meets or exceeds the configured threshold.
// Must be called with cb.mu held.
func (cb *CircuitBreaker) shouldTrip() bool {
	return cb.win.count() >= cb.cfg.MinimumCalls &&
		cb.win.failureRate() >= cb.cfg.FailureRateThreshold
}

// transition moves the breaker to newState and fires OnStateChange if set.
// The hook is called in its own goroutine so the mutex is released before
// user code runs, preventing re-entrant lock attempts.
// Must be called with cb.mu held.
func (cb *CircuitBreaker) transition(newState State) {
	if cb.state == newState {
		return
	}
	from := cb.state
	cb.state = newState
	if newState == StateOpen {
		cb.openedAt = time.Now()
	}
	if cb.cfg.OnStateChange != nil {
		go cb.cfg.OnStateChange(cb.name, from, newState)
	}
}
