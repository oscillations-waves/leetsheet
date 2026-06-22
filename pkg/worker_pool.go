package pkg

import (
	"errors"
	"fmt"
	"sync"
)

type Job struct {
	Id    int
	Input int
}

type Result struct {
	JobId    int
	WorkerId int
	Output   int
	Err      error
}

type WorkerPool struct {
	numWorkers int
	jobs       chan Job
	results    chan Result
	wg         sync.WaitGroup
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(numWorkers, jobBufferSize int) *WorkerPool {
	return &WorkerPool{
		numWorkers: numWorkers,
		jobs:       make(chan Job, jobBufferSize),
		results:    make(chan Result, jobBufferSize),
	}
}

// worker is a method that processes jobs from the pool's jobs channel
func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	for job := range wp.jobs {
		fmt.Printf("Worker %d is processing job %d\n", id, job.Id)
		if job.Input < 0 {
			wp.results <- Result{
				JobId:    job.Id,
				WorkerId: id,
				Err:      errors.New("input cannot be negative"),
			}
			continue
		}
		output := job.Input * job.Input

		wp.results <- Result{
			JobId:    job.Id,
			WorkerId: id,
			Output:   output,
		}
	}
}

// Start initializes and starts all workers
func (wp *WorkerPool) Start() {
	for i := 1; i <= wp.numWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}

	// Close results channel when all workers are done
	go func() {
		wp.wg.Wait()
		close(wp.results)
	}()
}

// Submit sends a job to the pool
func (wp *WorkerPool) Submit(job Job) {
	wp.jobs <- job
}

// Done signals that no more jobs will be submitted
func (wp *WorkerPool) Done() {
	close(wp.jobs)
}

// Results returns the results channel
func (wp *WorkerPool) Results() <-chan Result {
	return wp.results
}

func RunWorkerPool() {
	fmt.Println("\n=== Worker Pool ===")
	const numWorkers = 2
	const numJobs = 10

	pool := NewWorkerPool(numWorkers, numJobs)
	pool.Start()

	for j := 1; j <= numJobs; j++ {
		pool.Submit(Job{Id: j, Input: j})
	}
	pool.Done()

	for result := range pool.Results() {
		if result.Err != nil {
			fmt.Printf("Job %d FAILED: %v\n", result.JobId, result.Err)
		} else {
			fmt.Printf("Job %d --> Output : %d\n", result.JobId, result.Output)
		}
	}
}
