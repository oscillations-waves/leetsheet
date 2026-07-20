package jobqueue

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"
)

// HandlerFunc is the signature every job handler must satisfy.
// Return nil to mark the job completed; return an error to trigger a retry
// (or dead-lettering when MaxAttempts is exhausted).
type HandlerFunc func(ctx context.Context, job *Job) error

// Dispatcher binds a Queue to a pool of worker goroutines and a registry of
// HandlerFuncs keyed by job type.
//
// Execution flow for each job:
//
//	Queue.dequeue
//	    └─► handler lookup by job.Type
//	            └─► HandlerFunc(ctx, job)
//	                    │ nil  ──► Queue.complete
//	                    │ err, retries remain ──► Queue.requeue (with backoff)
//	                    └─► err, retries exhausted ──► Queue.dead (DLQ)
type Dispatcher struct {
	queue       *Queue
	workerCount int

	mu       sync.RWMutex
	handlers map[string]HandlerFunc

	wg sync.WaitGroup
}

// NewDispatcher creates a Dispatcher backed by q with workerCount concurrent
// workers. Provide workerCount ≥ 1; zero or negative values are clamped to 1.
// Call [Dispatcher.Register] for each job type before calling [Dispatcher.Start].
func NewDispatcher(q *Queue, workerCount int) *Dispatcher {
	if workerCount <= 0 {
		workerCount = 1
	}
	return &Dispatcher{
		queue:       q,
		workerCount: workerCount,
		handlers:    make(map[string]HandlerFunc),
	}
}

// Register associates h with jobType. Panics if jobType is already registered
// to prevent accidental silent overwrites.
func (d *Dispatcher) Register(jobType string, h HandlerFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.handlers[jobType]; ok {
		panic(fmt.Sprintf("jobqueue: handler already registered for type %q", jobType))
	}
	d.handlers[jobType] = h
}

// Start launches workerCount worker goroutines and returns immediately.
// Workers run until ctx is cancelled or [Queue.Close] is called.
// Call [Dispatcher.Wait] to block until all workers have exited.
func (d *Dispatcher) Start(ctx context.Context) {
	for i := range d.workerCount {
		d.wg.Add(1)
		go d.runWorker(ctx, i+1)
	}
}

// Wait blocks until all worker goroutines have exited.
// Typically called after ctx is cancelled and [Queue.Close] to ensure
// all in-flight jobs have been handled before the process exits.
func (d *Dispatcher) Wait() { d.wg.Wait() }

// runWorker is the goroutine body: pull → execute → ack in a tight loop.
func (d *Dispatcher) runWorker(ctx context.Context, id int) {
	defer d.wg.Done()
	for {
		job, err := d.queue.dequeue(ctx)
		if err != nil {
			// Queue closed or context cancelled — exit cleanly.
			return
		}
		d.process(ctx, job, id)
	}
}

// process executes the handler for job and decides what happens next.
func (d *Dispatcher) process(ctx context.Context, job *Job, workerID int) {
	d.mu.RLock()
	h, ok := d.handlers[job.Type]
	d.mu.RUnlock()

	if !ok {
		// No handler registered → dead-letter immediately.
		// Re-queuing would loop forever, so we fail fast.
		job.LastError = fmt.Sprintf("no handler registered for type %q", job.Type)
		slog.Warn("jobqueue: no handler; dead-lettering",
			"job_id", job.ID, "type", job.Type, "worker", workerID)
		d.queue.dead(job)
		return
	}

	job.Attempt++
	slog.Info("jobqueue: processing",
		"job_id", job.ID, "type", job.Type,
		"attempt", job.Attempt, "of", job.MaxAttempts, "worker", workerID)

	if err := h(ctx, job); err != nil {
		job.LastError = err.Error()
		slog.Warn("jobqueue: handler error",
			"job_id", job.ID, "type", job.Type,
			"attempt", job.Attempt, "error", err)

		if job.Attempt < job.MaxAttempts {
			delay := backoff(job.Attempt)
			slog.Info("jobqueue: retry scheduled",
				"job_id", job.ID, "next_attempt", job.Attempt+1, "delay", delay)
			d.queue.requeue(job, delay)
		} else {
			slog.Error("jobqueue: max attempts reached; moving to DLQ",
				"job_id", job.ID, "type", job.Type, "attempts", job.Attempt)
			d.queue.dead(job)
		}
		return
	}

	slog.Info("jobqueue: completed",
		"job_id", job.ID, "type", job.Type,
		"attempt", job.Attempt, "worker", workerID)
	d.queue.complete(job)
}

// backoff returns the exponential delay for retry attempt n (1-based):
//
//	attempt 1 → 1 s
//	attempt 2 → 2 s
//	attempt 3 → 4 s
//	attempt 4 → 8 s   …capped at 5 min
//
// In production you would add jitter (±10 %) to prevent retry storms when
// many jobs fail simultaneously.
func backoff(attempt int) time.Duration {
	const (
		base = time.Second
		cap  = 5 * time.Minute
	)
	d := base * time.Duration(math.Pow(2, float64(attempt-1)))
	if d > cap {
		return cap
	}
	return d
}
