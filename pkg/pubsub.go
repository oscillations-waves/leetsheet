package pkg

import (
	"sync"
	"time"
)

// Message represents a published message on a topic.
type Message struct {
	Topic     string
	Payload   any
	Timestamp time.Time
}

// subscriber holds a buffered channel for receiving messages.
type subscriber struct {
	ch chan Message
}

// PubSub is a thread-safe publish-subscribe broker.
type PubSub struct {
	mu          sync.RWMutex
	subscribers map[string][]*subscriber
	bufSize     int
}

// NewPubSub creates a PubSub broker where each subscriber channel is
// buffered to bufSize messages. A larger buffer reduces the chance of
// dropped messages for slow consumers.
func NewPubSub(bufSize int) *PubSub {
	if bufSize <= 0 {
		bufSize = 64
	}
	return &PubSub{
		subscribers: make(map[string][]*subscriber),
		bufSize:     bufSize,
	}
}

// Subscribe registers interest in topic and returns a receive-only channel
// on which the caller will receive all future messages published to that topic.
// Call Unsubscribe with the same channel to stop receiving messages.
func (ps *PubSub) Subscribe(topic string) <-chan Message {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	sub := &subscriber{ch: make(chan Message, ps.bufSize)}
	ps.subscribers[topic] = append(ps.subscribers[topic], sub)
	return sub.ch
}

// Unsubscribe removes the subscription identified by ch from topic.
// The channel is closed so the caller's range loop (if any) terminates.
func (ps *PubSub) Unsubscribe(topic string, ch <-chan Message) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	subs := ps.subscribers[topic]
	for i, sub := range subs {
		if sub.ch == ch {
			// Remove by swapping with last element.
			subs[i] = subs[len(subs)-1]
			subs[len(subs)-1] = nil
			ps.subscribers[topic] = subs[:len(subs)-1]
			close(sub.ch)
			return
		}
	}
}

// Publish sends msg to every subscriber of topic. Delivery to each
// subscriber is non-blocking: if a subscriber's buffer is full the message
// is dropped for that subscriber only, keeping fast publishers from being
// stalled by slow consumers.
func (ps *PubSub) Publish(topic string, payload any) {
	msg := Message{
		Topic:     topic,
		Payload:   payload,
		Timestamp: time.Now(),
	}

	ps.mu.RLock()
	subs := make([]*subscriber, len(ps.subscribers[topic]))
	copy(subs, ps.subscribers[topic])
	ps.mu.RUnlock()

	for _, sub := range subs {
		select {
		case sub.ch <- msg:
		default:
			// subscriber is too slow; drop message rather than block
		}
	}
}

// Close unsubscribes all subscribers across all topics and closes their
// channels, signalling them that no more messages will arrive.
func (ps *PubSub) Close() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	for topic, subs := range ps.subscribers {
		for _, sub := range subs {
			close(sub.ch)
		}
		delete(ps.subscribers, topic)
	}
}
