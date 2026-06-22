package pkg

import "fmt"

// RunWorkerExample demonstrates the WorkerPool with 3 workers and 8 jobs.
func RunWorkerExample() {
	const numWorkers = 3
	const numJobs = 8

	pool := NewWorkerPool(numWorkers, numJobs)
	pool.Start()

	for j := 1; j <= numJobs; j++ {
		pool.Submit(Job{Id: j, Input: j})
	}
	pool.Done()

	for r := range pool.Results() {
		if r.Err != nil {
			fmt.Printf("Job %d FAILED: %v\n", r.JobId, r.Err)
		} else {
			fmt.Printf("Job %d --> %d\n", r.JobId, r.Output)
		}
	}
}
