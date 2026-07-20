# Circuit Breaker — System Design

---

## Table of Contents
1. [High-Level Design](#1-high-level-design)
2. [State Machine](#2-state-machine)
3. [Data Flow](#3-data-flow)
4. [Low-Level Design](#4-low-level-design)
5. [Concurrency Model](#5-concurrency-model)
6. [Trade-offs & Design Decisions](#6-trade-offs--design-decisions)
7. [Scalability Considerations](#7-scalability-considerations)

---

## 1. High-Level Design

Without a circuit breaker, a slow or failing downstream causes callers to block indefinitely, exhausting thread/goroutine pools and propagating failure upstream — a **cascading failure**.

```
Without circuit breaker:

[Service A] ──► [Service B] ──► [DB — DOWN]
                    │
              all goroutines
              blocked, waiting
                    │
[Service A] ──► timeout / OOM / crash
```

```
With circuit breaker:

[Service A] ──► [CircuitBreaker] ──► [Service B] ──► [DB — DOWN]
                       │
                 trips open after
                 N% failure rate
                       │
[Service A] ◄── ErrCircuitOpen (instant)
             └─► fallback / cached response
```

**Core components:**

| Component | Responsibility |
|---|---|
| **CircuitBreaker** | State machine; allows or rejects calls based on observed failure rate |
| **window** | Fixed-size ring buffer; O(1) failure-rate calculation over last N calls |
| **Config** | Tunable thresholds: window size, failure rate, open timeout |
| **Metrics** | Point-in-time snapshot for health checks and dashboards |

---

## 2. State Machine

```
                   failure rate ≥ threshold
                   AND calls ≥ MinimumCalls
  ┌─────────┐  ─────────────────────────────►  ┌──────────┐
  │  Closed │                                   │   Open   │
  │ (normal)│  ◄─────────────────────────────  │(tripped) │
  └─────────┘       probe succeeds             └──────────┘
       ▲             + window reset                  │
       │                                             │ OpenTimeout elapsed
       │         ┌──────────────┐                    │
       └─────────│  Half-Open   │◄───────────────────┘
  probe success  │  (probing)   │
  (window reset) └──────────────┘
                       │ probe fails
                       ▼
                    Open (timer restarted)
```

### State Semantics

| State | Behaviour | Transition trigger |
|---|---|---|
| **Closed** | All calls go through; outcomes recorded | Failure rate ≥ threshold AND calls ≥ minimum → **Open** |
| **Open** | All calls return `ErrCircuitOpen` immediately | `OpenTimeout` elapses → **HalfOpen** |
| **HalfOpen** | One probe call allowed; others get `ErrCircuitOpen` | Probe success → **Closed** · Probe failure → **Open** |

---

## 3. Data Flow

### Normal operation (Closed)
```
caller → Execute(ctx, f)
    └─► beforeCall: state=Closed → permit
    └─► f(ctx) executes
    └─► afterCall(success=true)
            └─► window.record(true)
            └─► shouldTrip? → no → remain Closed
```

### Circuit trips (Closed → Open)
```
afterCall(success=false)
    └─► window.record(false)
    └─► shouldTrip?
            └─► count=10 ≥ MinimumCalls=10
            └─► failureRate=0.60 ≥ threshold=0.50
            └─► YES → transition(Open)
                        └─► openedAt = now
                        └─► OnStateChange("svc", Closed, Open)  [goroutine]
```

### Circuit open — fast fail
```
caller → Execute(ctx, f)
    └─► beforeCall: state=Open
            └─► maybeAdvanceToHalfOpen: time.Since(openedAt) < OpenTimeout → stay Open
    └─► return ErrCircuitOpen  ← f is never called
```

### Recovery probe (Open → HalfOpen → Closed)
```
OpenTimeout elapses
    └─► next Execute call
            └─► beforeCall: maybeAdvanceToHalfOpen → transition(HalfOpen)
            └─► probing=false → set probing=true → permit probe
    └─► f(ctx) executes
    └─► afterCall(success=true)
            └─► probing=false
            └─► window.reset()  ← clear stale failure data
            └─► transition(Closed)
```

### Failed probe (HalfOpen → Open)
```
afterCall(success=false)
    └─► probing=false
    └─► transition(Open)  ← openedAt reset; timer restarts
```

---

## 4. Low-Level Design

### 4.1 Sliding Window (Ring Buffer)

The window tracks the last **N** call outcomes using a fixed-size ring buffer, giving O(1) write and O(1) failure-rate read without unbounded memory growth.

```
WindowSize = 5, after 7 calls [F F F T T T F]:

ring: [ T  T  F  F  F ]
        ↑           ↑
      oldest      newest

head = 3  (next write position)
total = 5 (ring is full)
failures = 3
failureRate = 3/5 = 0.60
```

**Eviction on write:**
```go
if total == cap {
    if ring[head] == false { failures-- }  // subtract evicted entry
}
ring[head] = success
if !success { failures++ }
head = (head + 1) % cap
```

### 4.2 MinimumCalls Guard

The circuit will **not trip** until at least `MinimumCalls` have been recorded in the current window. Without this guard, a single failure on startup (0→1 calls, 100% failure rate) would immediately open the circuit.

```
MinimumCalls = 10, WindowSize = 10

Calls 1–9:  failure rate computed but shouldTrip → false
Call 10:    failure rate = 0.60 ≥ 0.50 AND count=10 ≥ 10 → TRIP
```

### 4.3 Half-Open Probe Serialisation

Only **one** probe is allowed at a time. The `probing` flag is set to `true` before the probe call and cleared in `afterCall` regardless of outcome. Concurrent callers see `probing=true` and receive `ErrCircuitOpen`.

```
goroutine 1: beforeCall → probing=false → set probing=true → permit
goroutine 2: beforeCall → probing=true  → return ErrCircuitOpen
goroutine 3: beforeCall → probing=true  → return ErrCircuitOpen
goroutine 1: afterCall(success) → probing=false → transition(Closed)
```

### 4.4 OnStateChange Hook

The callback is dispatched in a **fire-and-forget goroutine** so the mutex is never held while user code executes. This prevents:
- Deadlocks if the callback calls `cb.Execute` or `cb.State`
- Priority inversion if the callback is slow (e.g., sends a Slack alert)

---

## 5. Concurrency Model

All mutable state (`state`, `win`, `openedAt`, `probing`) is protected by a single `sync.Mutex`.

```
Execute goroutines          mutex        internal state
───────────────────────     ─────        ──────────────
goroutine 1: beforeCall ──► lock ──► check state, maybe set probing
                        ◄── unlock
goroutine 1: f(ctx)         (lock released during call — upstream not blocked)
goroutine 1: afterCall  ──► lock ──► record outcome, maybe transition state
                        ◄── unlock
```

**Lock is released during `f(ctx)`** — this is critical. Holding the lock across the upstream call would serialize all goroutines through the breaker, defeating the purpose of a worker pool.

---

## 6. Trade-offs & Design Decisions

| Decision | Rationale |
|---|---|
| **Count-based window vs. time-based window** | Count-based (last N calls) is simpler and sufficient for request-heavy services. Time-based (last N seconds) is better for low-traffic services where N calls could span many minutes. |
| **Failure rate vs. consecutive failures** | Rate-based tolerates isolated transient errors (a single blip does not trip the circuit) but requires MinimumCalls to avoid false positives. Consecutive-failure counting is simpler but fragile under intermittent errors. |
| **Single mutex vs. atomic operations** | A mutex is easier to reason about when multiple fields change atomically (state + openedAt + probing). Lock contention is negligible because the lock is only held for microseconds — never across the upstream call. |
| **Single probe in HalfOpen** | Sending full traffic during recovery risks re-tripping the circuit immediately. One probe is conservative; once closed, the full window must degrade again to re-trip. |
| **No built-in fallback** | The breaker returns `ErrCircuitOpen`; callers decide their own fallback (cached response, default value, graceful degradation). Coupling fallback logic into the breaker would violate single responsibility. |
| **OnStateChange is async** | Prevents deadlock and ensures the hot path is never slowed by alerting infrastructure. |

---

## 7. Scalability Considerations

### Per-Instance vs. Shared State

This implementation is **per-process**. In a horizontally scaled service, each pod has its own breaker with its own window. A pod that happens to receive fewer failing requests may stay closed while others open.

**Production solution:** centralise failure counters in Redis.

```
[Pod 1] ──► Redis INCR failures:payments-svc
[Pod 2] ──► Redis INCR failures:payments-svc
[Pod N] ──► Redis GET  failures:payments-svc → trip all pods atomically
```

### Half-Open at Scale

With N pods all probing independently at `OpenTimeout`, you get N probe requests hitting the recovering upstream simultaneously.

**Solutions:**
- **Leader election** — only one pod probes; others wait for the leader's verdict.
- **Jitter** — randomise `OpenTimeout ± 20%` so pods don't all advance to HalfOpen at the same moment.

### Observability Checklist

| Metric | Alert threshold |
|---|---|
| `circuit_state` (0=closed, 1=open, 2=half-open) | Alert if any breaker is open > 5 min |
| `circuit_failure_rate` | Alert if approaching threshold (e.g., >40% when threshold=50%) |
| `circuit_open_total` (counter) | Sudden spike → upstream incident |
| `circuit_rejected_total` | Calls shed by open circuit — quantifies blast radius |
