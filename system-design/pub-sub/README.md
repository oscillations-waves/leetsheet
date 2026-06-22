# Publish-Subscribe System — System Design

---

## Table of Contents
1. [High-Level Design](#1-high-level-design)
2. [Data Flow](#2-data-flow)
3. [Low-Level Design](#3-low-level-design)
4. [Concurrency Model](#4-concurrency-model)
5. [Trade-offs & Design Decisions](#5-trade-offs--design-decisions)
6. [Scalability Considerations](#6-scalability-considerations)

---

## 1. High-Level Design

A Pub-Sub system decouples producers of events from consumers of events. Neither side knows the other exists — they communicate only through a shared broker.

```
Publisher A ──►
Publisher B ──►  [ Broker ]  ──► Subscriber 1
Publisher C ──►     │        ──► Subscriber 2
                    │        ──► Subscriber 3
              topic registry
```

**Core components:**

| Component | Responsibility |
|---|---|
| **Publisher** | Calls `Publish(topic, payload)` — no knowledge of subscribers |
| **Broker** | Maintains topic → subscriber registry; routes messages |
| **Subscriber** | Registers interest in a topic; receives messages on a channel |
| **Message** | Envelope carrying topic, payload, and timestamp |

---

## 2. Data Flow

### Subscribe
```
Subscriber calls Subscribe("orders")
    └─► Broker acquires write lock
    └─► Creates buffered channel (mailbox)
    └─► Appends to subscribers["orders"]
    └─► Releases write lock
    └─► Returns <-chan Message to subscriber
```

### Publish
```
Publisher calls Publish("orders", data)
    └─► Broker builds Message{topic, payload, timestamp}
    └─► Acquires read lock
    └─► Copies subscriber slice (snapshot)
    └─► Releases read lock   ← lock dropped before any I/O
    └─► For each subscriber:
            select {
              case ch <- msg:   ← delivered
              default:          ← buffer full, message dropped (non-blocking)
            }
```

### Unsubscribe
```
Subscriber calls Unsubscribe("orders", ch)
    └─► Broker acquires write lock
    └─► Finds subscriber by channel identity
    └─► Swap-with-last removal (O(1))
    └─► Closes channel → subscriber's range loop exits
    └─► Releases write lock
```

---

## 3. Low-Level Design

### Data Structures

```
PubSub {
    mu:          sync.RWMutex
    subscribers: map[string][]*subscriber   // topic → mailboxes
    bufSize:     int
}

subscriber {
    ch: chan Message   // buffered mailbox
}

Message {
    Topic:     string
    Payload:   any
    Timestamp: time.Time
}
```

### API

```go
broker := NewPubSub(bufSize int) *PubSub

ch := broker.Subscribe(topic string) <-chan Message
broker.Unsubscribe(topic string, ch <-chan Message)
broker.Publish(topic string, payload any)
broker.Close()
```

### Complexity

| Operation | Time | Notes |
|---|---|---|
| `Subscribe` | O(1) amortised | append to slice |
| `Unsubscribe` | O(n) scan + O(1) removal | n = subscribers on topic |
| `Publish` | O(n) | n = subscribers on topic |
| `Close` | O(T × n) | T = topics, n = subscribers per topic |

---

## 4. Concurrency Model

### Why `sync.RWMutex` over `sync.Mutex`

A plain mutex serialises all operations — even concurrent reads. Since `Publish` only reads the subscriber map (to find recipients), multiple publishers can safely hold `RLock` simultaneously.

```
Plain Mutex:    P1 ████ P2 ████ P3 ████   (serialised)
RWMutex:        P1 ████
                P2 ████  ← all three publish concurrently
                P3 ████
```

Write operations (`Subscribe`, `Unsubscribe`, `Close`) take the exclusive `Lock()`.

### Deadlock Prevention — The Snapshot Pattern

Holding a lock while sending to a channel creates a deadlock risk:

```
Publish holds RLock → blocks on full channel
Subscriber calls Unsubscribe → needs Lock()
Lock() waits for RLock → RLock waits for channel drain → deadlock
```

Solution: copy the subscriber slice under `RLock`, release the lock, *then* do all channel I/O on the local copy.

### Non-Blocking Fan-Out

```go
select {
case sub.ch <- msg:  // delivered
default:             // buffer full — drop, never block
}
```

This ensures one slow subscriber cannot stall delivery to all others or block publishers. The buffer size is a tuneable slack window for bursty consumers.

---

## 5. Trade-offs & Design Decisions

| Decision | Chosen | Alternative | Reason |
|---|---|---|---|
| Message delivery | At-most-once (drop on full buffer) | Block until delivered | Prevents publisher stalls |
| Subscriber storage | Slice per topic | Linked list / set | Cache-friendly, O(1) append |
| Removal algorithm | Swap-with-last | Shift elements | O(1) vs O(n); order irrelevant |
| Subscriber identity | Channel pointer equality | UUID / handle | Zero allocation, idiomatic Go |
| Shutdown signal | `close(ch)` | Sentinel value | Native Go broadcast mechanism |
| Payload type | `any` | Generics `[T any]` | Keeps broker topic-agnostic |

---

## 6. Scalability Considerations

This implementation is **in-process** — suitable for a single service. For distributed systems:

| Requirement | Solution |
|---|---|
| Cross-service messaging | Replace channels with a message queue (Kafka, NATS, RabbitMQ) |
| Persistence / replay | Kafka topics with consumer group offsets |
| At-least-once delivery | Acknowledgement protocol + dead-letter queue |
| Fan-out at scale | Partitioned topics with consumer groups |
| Back-pressure | Flow control / consumer lag monitoring |

The interface (`Subscribe`, `Publish`, `Unsubscribe`) is stable enough that the channel-based implementation could be swapped for a network-backed one without changing callers.
