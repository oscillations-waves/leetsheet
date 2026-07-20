// Package jobqueue teaches distributed job-queue design.
//
// Architecture: producers enqueue Jobs; a pool of worker goroutines dequeue
// and execute them via registered HandlerFuncs.
//
//	[Producer A] ──►
//	[Producer B] ──►  [ Queue ]  ──► [Worker 1]
//	[Producer C] ──►     │       ──► [Worker 2]
//	                     │       ──► [Worker N]
//	              priority heap
//	              scheduled list
//	              dead-letter queue
//
// Key design decisions:
//   - Priority heap: critical jobs preempt low-priority ones within the same
//     scheduling window.
//   - Scheduled jobs: a background goroutine promotes future-RunAt jobs into
//     the pending heap exactly when their time arrives.
//   - At-least-once delivery: a worker holds the job in-flight until the
//     handler returns; only then is it acked (completed) or nacked (requeued).
//   - Retry with exponential backoff: failed jobs re-enter the scheduled list
//     with a geometrically growing delay; see dispatcher.go/backoff.
//   - Dead-letter queue (DLQ): jobs that exhaust MaxAttempts are moved here
//     for offline inspection/replay rather than silently dropped.
//   - Context propagation: every blocking call accepts a context so
//     consumers can be cancelled cleanly on shutdown.
package jobqueue

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ── Status & Priority ─────────────────────────────────────────────────────────

// Status represents the lifecycle state of a Job.
type Status int

const (
	StatusPending    Status = iota // waiting to be picked up by a worker
	StatusProcessing               // a worker is currently executing this job
	StatusCompleted                // handler returned nil; job is done
	StatusFailed                   // handler returned an error; retries remain
	StatusDead                     // exhausted MaxAttempts; moved to DLQ
)

func (s Status) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusProcessing:
		return "processing"
	case StatusCompleted:
		return "completed"
	case StatusFailed:
		return "failed"
	case StatusDead:
		return "dead"
	default:
		return "unknown"
	}
}

// Priority controls ordering within the pending heap. Higher values run first.
type Priority int

const (
	PriorityLow      Priority = 0
	PriorityNormal   Priority = 1
	PriorityHigh     Priority = 2
	PriorityCritical Priority = 3
)

// ── Job ───────────────────────────────────────────────────────────────────────

// Job is the unit of work flowing through the queue.
type Job struct {
	ID          string    // unique identifier (caller-assigned)
	Type        string    // routes to a registered HandlerFunc
	Payload     any       // arbitrary data forwarded to the handler verbatim
	Priority    Priority  // higher value → processed before lower-priority jobs
	Status      Status    // updated in place as the job moves through the pipeline
	Attempt     int       // 1-based; incremented each time a worker picks it up
	MaxAttempts int       // total attempts before dead-lettering (0 → defaults to 1)
	RunAt       time.Time // earliest wall-clock time the job may execute
	EnqueuedAt  time.Time // set by Enqueue
	LastError   string    // most recent handler error message
}

// ready reports whether the job's RunAt wall-clock time has arrived.
func (j *Job) ready() bool { return !time.Now().Before(j.RunAt) }

// ── Priority Heap ─────────────────────────────────────────────────────────────
// jobHeap implements container/heap.Interface.
// Ordering: higher Priority first; ties broken by earlier RunAt (FIFO within
// the same priority level when jobs were enqueued at the same wall time).

type jobHeap []*Job

func (h jobHeap) Len() int { return len(h) }
func (h jobHeap) Less(i, j int) bool {
	if h[i].Priority != h[j].Priority {
		return h[i].Priority > h[j].Priority // descending: higher priority wins
	}
	return h[i].RunAt.Before(h[j].RunAt) // ascending: earlier RunAt wins
}
func (h jobHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *jobHeap) Push(x any)   { *h = append(*h, x.(*Job)) }
func (h *jobHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	old[n-1] = nil // clear the slot to avoid a memory leak
	*h = old[:n-1]
	return x
}

// ── Queue ─────────────────────────────────────────────────────────────────────

var (
	// ErrQueueClosed is returned by Enqueue and dequeue after Close is called.
	ErrQueueClosed = errors.New("jobqueue: queue is closed")
	// ErrDuplicateJob is returned when a job with the same ID is enqueued twice.
	ErrDuplicateJob = errors.New("jobqueue: job ID already exists")
)

// Queue is the core broker.
//
// Internally it maintains two collections:
//   - pending: a max-heap of jobs ready to run right now.
//   - scheduled: a slice of future jobs sorted ascending by RunAt.
//
// A background scheduler goroutine (started via [Queue.Start]) promotes
// scheduled jobs into pending exactly when their RunAt arrives.
// Worker goroutines block on [Queue.dequeue] until a job is ready.
type Queue struct {
	mu        sync.Mutex
	pending   jobHeap // ready-to-run jobs, heap-ordered by priority then RunAt
	scheduled []*Job  // future jobs, sorted ascending by RunAt
	all       map[string]*Job
	dlq       []*Job // dead-letter queue — permanently failed jobs

	// Channels carry one-item signals; senders are non-blocking (drop if full).
	notify     chan struct{} // pinged when a job enters the pending heap
	reschedule chan struct{} // pinged when the scheduled list changes
	closeCh    chan struct{} // closed once by Close()
	closed     bool
}

// NewQueue allocates a Queue ready for use. Call [Queue.Start] before
// submitting jobs so that the internal scheduler goroutine is running.
func NewQueue() *Queue {
	q := &Queue{
		all:        make(map[string]*Job),
		notify:     make(chan struct{}, 1),
		reschedule: make(chan struct{}, 1),
		closeCh:    make(chan struct{}),
	}
	heap.Init(&q.pending)
	return q
}

// Start launches the background scheduler goroutine that promotes
// future-RunAt jobs into the pending heap on time.
// The goroutine exits when ctx is cancelled or [Queue.Close] is called.
func (q *Queue) Start(ctx context.Context) {
	go q.runScheduler(ctx)
}

// Enqueue adds job to the queue.
//   - If job.RunAt is zero or in the past it goes straight to the pending heap
//     and a waiting worker will pick it up immediately.
//   - If job.RunAt is in the future the job is held in the scheduled list
//     until that moment arrives.
//
// Returns [ErrQueueClosed] if Close has been called, or [ErrDuplicateJob] if
// a job with the same ID was already submitted.
func (q *Queue) Enqueue(job *Job) error {
	if job.ID == "" {
		return fmt.Errorf("jobqueue: job ID must not be empty")
	}
	if job.Type == "" {
		return fmt.Errorf("jobqueue: job Type must not be empty")
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 1
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return ErrQueueClosed
	}
	if _, exists := q.all[job.ID]; exists {
		return ErrDuplicateJob
	}

	job.EnqueuedAt = time.Now()
	if job.RunAt.IsZero() {
		job.RunAt = job.EnqueuedAt
	}
	job.Status = StatusPending
	q.all[job.ID] = job

	if job.ready() {
		heap.Push(&q.pending, job)
		q.ping(q.notify)
	} else {
		q.insertScheduled(job)
		q.ping(q.reschedule) // tell scheduler to recalculate its sleep timer
	}
	return nil
}

// dequeue blocks until a ready job is available, ctx is cancelled, or the
// queue is closed. The returned job has Status == StatusProcessing.
// Called exclusively by Dispatcher worker goroutines.
func (q *Queue) dequeue(ctx context.Context) (*Job, error) {
	for {
		q.mu.Lock()
		if job := q.popPending(); job != nil {
			job.Status = StatusProcessing
			q.mu.Unlock()
			return job, nil
		}
		if q.closed {
			q.mu.Unlock()
			return nil, ErrQueueClosed
		}
		q.mu.Unlock()

		// Park until the queue wakes us.
		select {
		case <-q.notify:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-q.closeCh:
			return nil, ErrQueueClosed
		}
	}
}

// requeue places a failed job back in the queue after delay.
// Called by Dispatcher when a handler fails but retries remain.
func (q *Queue) requeue(job *Job, delay time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()

	job.Status = StatusFailed
	job.RunAt = time.Now().Add(delay)

	if job.ready() {
		heap.Push(&q.pending, job)
		q.ping(q.notify)
	} else {
		q.insertScheduled(job)
		q.ping(q.reschedule)
	}
}

// dead moves a permanently-failed job to the dead-letter queue.
func (q *Queue) dead(job *Job) {
	q.mu.Lock()
	defer q.mu.Unlock()
	job.Status = StatusDead
	q.dlq = append(q.dlq, job)
}

// complete marks a job as successfully finished.
func (q *Queue) complete(job *Job) {
	q.mu.Lock()
	defer q.mu.Unlock()
	job.Status = StatusCompleted
}

// DLQ returns a snapshot of all permanently-failed jobs.
func (q *Queue) DLQ() []*Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]*Job, len(q.dlq))
	copy(out, q.dlq)
	return out
}

// Snapshot returns all jobs grouped by Status for observability / dashboards.
func (q *Queue) Snapshot() map[Status][]*Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	m := make(map[Status][]*Job)
	for _, j := range q.all {
		m[j.Status] = append(m[j.Status], j)
	}
	return m
}

// Close signals that no new jobs will be accepted and wakes all blocked
// dequeue callers. In-flight jobs finish normally; call Dispatcher.Wait
// afterward to ensure all workers have exited.
func (q *Queue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.closed = true
		close(q.closeCh)
	}
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// popPending returns the highest-priority ready job from the heap, or nil.
// Must be called with q.mu held.
func (q *Queue) popPending() *Job {
	if q.pending.Len() == 0 {
		return nil
	}
	return heap.Pop(&q.pending).(*Job)
}

// insertScheduled inserts job into q.scheduled while keeping the slice sorted
// ascending by RunAt (binary-search insertion for O(log n) seek, O(n) shift).
// Must be called with q.mu held.
func (q *Queue) insertScheduled(job *Job) {
	idx := sort.Search(len(q.scheduled), func(i int) bool {
		return !q.scheduled[i].RunAt.Before(job.RunAt)
	})
	q.scheduled = append(q.scheduled, nil)
	copy(q.scheduled[idx+1:], q.scheduled[idx:])
	q.scheduled[idx] = job
}

// promoteReady moves all scheduled jobs whose RunAt ≤ now into the pending
// heap. Must NOT be called with q.mu held.
func (q *Queue) promoteReady() {
	now := time.Now()
	q.mu.Lock()
	promoted := 0
	for len(q.scheduled) > 0 && !q.scheduled[0].RunAt.After(now) {
		job := q.scheduled[0]
		q.scheduled = q.scheduled[1:]
		heap.Push(&q.pending, job)
		promoted++
	}
	q.mu.Unlock()

	if promoted > 0 {
		q.ping(q.notify)
	}
}

// runScheduler is the internal goroutine that drives the scheduled-to-pending
// promotion. It sleeps until the next job's RunAt, then calls promoteReady.
// On each reschedule signal it recalculates its sleep duration to account for
// newly added future jobs with earlier RunAt values.
func (q *Queue) runScheduler(ctx context.Context) {
	const maxPark = 24 * time.Hour

	var timer *time.Timer
	stopTimer := func() {
		if timer != nil && !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
	defer stopTimer()

	for {
		q.mu.Lock()
		var d time.Duration
		if len(q.scheduled) > 0 {
			d = time.Until(q.scheduled[0].RunAt)
			if d < 0 {
				d = 0
			}
		} else {
			d = maxPark // nothing scheduled; park and wait for a reschedule ping
		}
		q.mu.Unlock()

		stopTimer()
		timer = time.NewTimer(d)

		select {
		case <-timer.C:
			q.promoteReady()
		case <-q.reschedule:
			// A new future job was added; loop to recalculate the timer.
		case <-ctx.Done():
			return
		case <-q.closeCh:
			return
		}
	}
}

// ping sends a non-blocking signal on ch (silently drops if the buffer is full;
// the receiver will still wake up from the previous signal).
func (q *Queue) ping(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
