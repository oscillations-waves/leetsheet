# leetsheet

A Go reference sheet of data structures, algorithms, concurrency patterns, and system design implementations — written for clarity and depth.

---

## Structure

```
pkg/                    # Algorithms & concurrency primitives
system-design/          # Full system design implementations
  api-gateway/          # Multi-tenant SaaS API Gateway
```

---

## `pkg/` — Algorithms & Patterns

| File | Topic |
|---|---|
| `two_sum.go` | Hash map two-pass lookup — O(n) |
| `add_two_numbers.go` | Linked list addition with carry |
| `linked_list.go` | Singly linked list (insert, delete, reverse) |
| `length_of_longest_substring.go` | Sliding window — no-repeat substring |
| `subarray_sum_k.go` | Prefix sum — subarrays summing to k |
| `binary_gap.go` | Bit manipulation — longest gap between 1s |
| `reverse_int.go` | Integer reversal with overflow guard |
| `reverse_mid.go` | In-place array reversal around midpoint |
| `zigzag.go` | Zigzag level-order traversal |
| `stepwise_structure.go` | Stepwise matrix construction |
| `min_deletions_sorted.go` | Minimum deletions to make array sorted |
| `case_diff.go` | Case-insensitive character diff |
| `prediction_diff.go` | Prediction accuracy diff |
| `fee_waiver.go` | Fee waiver eligibility logic |
| `rate_limiter.go` | Token bucket rate limiter |
| `worker_pool.go` | Worker pool with job/result channels |
| `worker.go` | Worker pool usage example |
| `pubsub.go` | Thread-safe publish-subscribe broker |

---

## `pkg/worker_pool.go` — Worker Pool

A bounded concurrency worker pool with job queuing and result collection.

```go
pool := pkg.NewWorkerPool(3, 10)
pool.Start()
pool.Submit(pkg.Job{Id: 1, Input: 9})
pool.Done()
for r := range pool.Results() {
    fmt.Println(r.Output) // 81
}
```

---

## `pkg/rate_limiter.go` — Token Bucket Rate Limiter

Thread-safe token bucket with configurable capacity and refill rate.

---

## `system-design/pub-sub/` — Publish-Subscribe System

A thread-safe, in-process pub-sub broker with full system design documentation covering HLD, data flow diagrams, LLD, concurrency model, and scalability considerations.

See [`system-design/pub-sub/README.md`](system-design/pub-sub/README.md) for the full design document.

---

## `system-design/api-gateway/` — Multi-Tenant SaaS API Gateway

A full system design covering HLD, LLD, data flow, schema design, and database selection for a production API gateway with:

- Per-tenant authentication (JWT / API keys)
- Role-based authorisation (RBAC)
- Per-tenant rate limiting
- Observability (metrics, tracing, structured logging)

See [`system-design/api-gateway/README.md`](system-design/api-gateway/README.md) for the full design document.

---

## Running

```bash
go build ./...
go vet ./...
```

No external dependencies — stdlib only.
