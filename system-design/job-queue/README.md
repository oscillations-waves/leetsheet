# Distributed Job Queue — System Design

---

## Table of Contents
1. [High-Level Design](#1-high-level-design)
2. [Data Flow](#2-data-flow)
3. [Low-Level Design](#3-low-level-design)
4. [Concurrency Model](#4-concurrency-model)
5. [Retry & Dead-Letter Strategy](#5-retry--dead-letter-strategy)
6. [Trade-offs & Design Decisions](#6-trade-offs--design-decisions)
7. [Scalability Considerations](#7-scalability-considerations)

---

## 1. High-Level Design

A distributed job queue decouples **producers** (services that create work) from **consumers** (workers that execute it). Neither side is aware of the other — they communicate only through the queue broker.

```
[Producer A] ──►
[Producer B] ──►   [ Queue Broker ]   ──► [Worker 1]
[Producer C] ──►         │            ──► [Worker 2]
                          │            ──► [Worker N]
                   ┌──────┴──────┐
                   │  Pending    │  ← priority heap (ready now)
                   │  Scheduled  │  ← sorted by RunAt (future)
                   │  DLQ        │  ← permanently failed jobs
                   └─────────────┘
```

**Core components:**

| Component | Responsibility |
|---|---|
| **Job** | Unit of work: ID, type, payload, priority, retry config, scheduled time |
| **Queue** | Priority heap + scheduled list + DLQ; thread-safe broker |
| **Dispatcher** | Worker pool; routes jobs to registered handlers; owns retry logic |
| **Scheduler** | Helper for submitting delayed / time-triggered jobs |
| **HandlerFunc** | Business logic — one per job type, registered at startup |

---

## 2. Data Flow

### Enqueue (immediate)
```
Producer calls Enqueue(job)
    └─► Acquire mutex
    └─► Validate: non-empty ID & Type, no duplicate
    └─► Set EnqueuedAt, Status = Pending
    └─► RunAt ≤ now? → push to pending heap → ping notify channel
    └─► RunAt > now? → insert into scheduled list (sorted) → ping reschedule
    └─► Release mutex
```

### Scheduler goroutine (future jobs)
```
Loop:
    └─► Calculate time-until soonest scheduled job
    └─► Sleep with timer (up to 24 h if nothing scheduled)
    └─► Wake on: timer fires | reschedule ping | ctx cancel | close
    └─► promoteReady: move all scheduled jobs with RunAt ≤ now → pending heap
    └─► Ping notify so workers wake up
```

### Worker dequeue → execute → ack
```
Worker calls dequeue(ctx)
    └─► Lock → pop from pending heap if non-empty → mark StatusProcessing → return
    └─► Empty? → unlock → block on notify channel | ctx.Done | closeCh
    └─► On job received → Dispatcher.process(ctx, job, workerID)
            └─► Look up HandlerFunc by job.Type
            └─► Increment job.Attempt
            └─► Call HandlerFunc(ctx, job)
                    │ nil  ──► Queue.complete → StatusCompleted
                    │ err, attempts < max ──► Queue.requeue(delay) → StatusFailed → re-enters scheduled
                    └─► err, attempts == max ──► Queue.dead → StatusDead → DLQ
```

---

## 3. Low-Level Design

### 3.1 Priority Heap (`container/heap`)

Jobs in the **pending** list are stored in a max-heap ordered by:

1. **Priority descending** — `PriorityCritical (3) > PriorityHigh (2) > PriorityNormal (1) > PriorityLow (0)`
2. **RunAt ascending** (tie-breaker) — earlier enqueue time runs first within the same priority

```go
func (h jobHeap) Less(i, j int) bool {
    if h[i].Priority != h[j].Priority {
        return h[i].Priority > h[j].Priority
    }
    return h[i].RunAt.Before(h[j].RunAt)
}
```

**Heap operations:**
- `heap.Push` — O(log n)
- `heap.Pop` — O(log n)
- Peek at minimum — O(1)

### 3.2 Scheduled List

Future-RunAt jobs live in a plain sorted slice. Binary search insertion keeps it ordered by RunAt ascending.

```
scheduled = [job@T+5s, job@T+30s, job@T+2m, ...]
                 ↑
        next to promote
```

The scheduler goroutine sleeps exactly until `scheduled[0].RunAt`, then batch-promotes all jobs whose time has arrived.

### 3.3 Signalling Protocol

Two buffered-1 channels coordinate goroutines without polling:

| Channel | Direction | Meaning |
|---|---|---|
| `notify` | Queue → Workers | A job entered the pending heap |
| `reschedule` | Queue → Scheduler | The scheduled list changed; recalculate timer |

Sends are non-blocking (`select { case ch <- struct{}{}: default: }`). If the buffer already holds a signal, the pending wake-up is sufficient — the receiver will drain the loop.

### 3.4 Job Lifecycle State Machine

```
                    ┌──────────┐
                    │ Pending  │◄──────────────────────┐
                    └────┬─────┘                       │ requeue (with backoff)
                         │ worker picks up             │
                    ┌────▼──────────┐           ┌──────┴─────┐
                    │  Processing   │──► err ──►│   Failed   │
                    └────┬──────────┘           └──────┬─────┘
                         │ success                     │ attempts exhausted
                    ┌────▼──────────┐           ┌──────▼─────┐
                    │   Completed   │           │    Dead    │ (DLQ)
                    └───────────────┘           └────────────┘
```

---

## 4. Concurrency Model

### Worker Pool

```
Dispatcher.Start(ctx)
    ├─► goroutine worker-1: dequeue → process → loop
    ├─► goroutine worker-2: dequeue → process → loop
    └─► goroutine worker-N: dequeue → process → loop
```

- All workers share a single `Queue` protected by `sync.Mutex`.
- The handler registry uses `sync.RWMutex` — reads (per-job lookups) never block each other.
- `Dispatcher.Wait()` uses `sync.WaitGroup` for a clean drain on shutdown.

### Graceful Shutdown Sequence

```
1. ctx, cancel := context.WithCancel(context.Background())
2. q.Start(ctx)           // starts scheduler goroutine
3. d.Start(ctx)           // starts worker goroutines
4. ... produce jobs ...
5. cancel()               // signals scheduler + workers to stop accepting new work
6. q.Close()              // unblocks any workers blocked in dequeue
7. d.Wait()               // blocks until all in-flight jobs finish
```

---

## 5. Retry & Dead-Letter Strategy

### Exponential Backoff

Failed jobs are re-queued with an exponentially growing delay:

| Attempt | Delay |
|---|---|
| 1 | 1 s |
| 2 | 2 s |
| 3 | 4 s |
| 4 | 8 s |
| 5 | 16 s |
| … | … (capped at 5 min) |

```go
func backoff(attempt int) time.Duration {
    d := time.Second * time.Duration(math.Pow(2, float64(attempt-1)))
    if d > 5*time.Minute { return 5 * time.Minute }
    return d
}
```

> **Production note:** add ±10 % jitter to prevent retry storms when many jobs fail simultaneously (e.g., after a downstream outage).

### Dead-Letter Queue (DLQ)

When `job.Attempt == job.MaxAttempts` and the handler still returns an error:

- Job status → `StatusDead`
- Job appended to `q.dlq`
- DLQ is readable via `Queue.DLQ()` for offline inspection, alerting, or manual replay

---

## 6. Trade-offs & Design Decisions

| Decision | Rationale |
|---|---|
| **In-process queue (channels/heap) vs. external broker (Redis, Kafka)** | In-process is zero-dependency and teaches the fundamentals. In production replace `Queue` with a Redis Streams or Kafka consumer group adapter behind the same `HandlerFunc` interface. |
| **At-least-once delivery** | A job stays in `StatusProcessing` until the handler returns. If the process crashes mid-execution the job is lost (in-memory). Production systems persist job state to a database and use leases with heartbeats. |
| **Global mutex on Queue** | Simple and correct for moderate throughput. For very high throughput, shard the queue by priority tier or job type and use per-shard locks. |
| **Separate pending heap vs. scheduled slice** | Keeps the dequeue hot path free of timer/sleep logic. Workers never block inside the lock — they release it and park on a channel. |
| **`sync.Cond` rejected in favour of channels** | `sync.Cond` has no timeout support; channels integrate naturally with `context.Context` and `select`. |
| **No persistence** | In-memory for clarity. Bolt, Postgres (with `FOR UPDATE SKIP LOCKED`), or Redis would be the next step. |
| **Priority via max-heap** | O(log n) enqueue/dequeue; O(1) peek. For dynamic priority changes a Fibonacci heap would give O(1) decrease-key, but `container/heap` is idiomatic Go. |

---

## 7. Scalability Considerations

### Horizontal Scale

Replace the in-process `Queue` with a distributed backend:

```
[Producer N] ──►  [ Redis Streams / Kafka / SQS ]  ◄── [Worker Pod 1]
                                                    ◄── [Worker Pod 2]
                                                    ◄── [Worker Pod N]
```

Key concerns:
- **Competing consumers** — each worker pod reads from the same stream; the broker ensures a message is delivered to exactly one consumer at a time.
- **Job leasing** — workers acquire a time-bounded lease. If the worker dies, the lease expires and another worker reprocesses the job.
- **Idempotency** — at-least-once delivery means handlers must be idempotent (e.g., use a unique constraint in the DB on job ID).

### Partitioning

- **By job type** — high-volume types (e.g., `send-notification`) get their own queue and worker tier so they don't starve low-volume but critical types (e.g., `charge-payment`).
- **By tenant** — in multi-tenant SaaS, per-tenant queues prevent a noisy tenant from starving others.

### Observability

| Signal | What to track |
|---|---|
| Queue depth | Pending + scheduled counts per job type |
| Processing latency | Time from `EnqueuedAt` to handler start |
| Error rate | Handler failures / total attempts |
| DLQ size | Alert when non-zero; indicates systemic failures |
| Worker utilisation | Active workers / total workers (saturation signal) |
