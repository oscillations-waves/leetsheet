package circuitbreaker

// window is a fixed-capacity ring buffer that tracks the outcomes of the last
// N calls. It gives O(1) record and O(1) failureRate without storing a full
// history slice that grows unboundedly.
//
// Layout (size=5, after 7 calls):
//
//	ring: [F T F T T]
//	            ↑
//	           head (next write)
//
// When the ring is full each new write evicts the oldest entry by
// overwriting the slot at head and subtracting its contribution from
// the failure counter before incrementing.
type window struct {
	ring     []bool // true = success, false = failure
	head     int    // index of the next slot to overwrite
	total    int    // number of valid entries currently in the ring (≤ cap)
	failures int    // running count of false entries
}

// newWindow allocates a ring buffer of the given capacity.
func newWindow(size int) *window {
	return &window{ring: make([]bool, size)}
}

// record appends a new outcome, evicting the oldest entry once the ring is full.
func (w *window) record(success bool) {
	if w.total == len(w.ring) {
		// Ring is full: the slot at head holds the entry we are about to evict.
		if !w.ring[w.head] {
			w.failures-- // outgoing entry was a failure; remove its contribution
		}
	} else {
		w.total++
	}

	w.ring[w.head] = success
	if !success {
		w.failures++
	}
	w.head = (w.head + 1) % len(w.ring)
}

// failureRate returns failures/total, or 0 when the window is empty.
func (w *window) failureRate() float64 {
	if w.total == 0 {
		return 0
	}
	return float64(w.failures) / float64(w.total)
}

// count returns the number of recorded entries (saturates at window capacity).
func (w *window) count() int { return w.total }

// reset clears all recorded outcomes. Called when the circuit resets to Closed
// after a successful half-open probe so stale failure data does not linger.
func (w *window) reset() {
	w.head = 0
	w.total = 0
	w.failures = 0
}
