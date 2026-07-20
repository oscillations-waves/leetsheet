package jobqueue

import (
	"context"
	"time"
)

// Scheduler is a thin helper for submitting delayed or time-triggered jobs.
//
// Rather than sleeping in the caller's goroutine, Scheduler delegates the
// waiting to the Queue's internal scheduler goroutine — the job is stored
// in the scheduled list and promoted to the pending heap exactly on time,
// even if the caller goroutine has long since exited.
//
// Usage:
//
//	s := NewScheduler(q)
//
//	// Run 30 seconds from now.
//	s.Schedule(ctx, &Job{ID: "j1", Type: "send-email", Payload: data}, 30*time.Second)
//
//	// Run at a specific moment.
//	s.ScheduleAt(ctx, &Job{ID: "j2", Type: "report"}, midnight)
type Scheduler struct {
	queue *Queue
}

// NewScheduler wraps q. The Queue must already be started via [Queue.Start].
func NewScheduler(q *Queue) *Scheduler { return &Scheduler{queue: q} }

// Schedule enqueues job to execute after delay has elapsed.
// It sets job.RunAt = now + delay before forwarding to [Queue.Enqueue].
// Returns any error from Enqueue (e.g., ErrQueueClosed, ErrDuplicateJob).
func (s *Scheduler) Schedule(_ context.Context, job *Job, delay time.Duration) error {
	job.RunAt = time.Now().Add(delay)
	return s.queue.Enqueue(job)
}

// ScheduleAt enqueues job to execute at the given absolute wall-clock time.
// If at is in the past the job becomes immediately eligible (treated as RunAt = now).
func (s *Scheduler) ScheduleAt(_ context.Context, job *Job, at time.Time) error {
	job.RunAt = at
	return s.queue.Enqueue(job)
}
