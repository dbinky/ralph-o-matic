package worker

import (
	"context"
	"log"
	"time"

	"github.com/ryan/ralph-o-matic/internal/executor"
	"github.com/ryan/ralph-o-matic/internal/queue"
)

// Worker polls the queue and executes jobs.
type Worker struct {
	queue    *queue.Queue
	handler  *executor.RalphHandler
	interval time.Duration
}

// New creates a worker that polls the queue at the given interval.
func New(q *queue.Queue, handler *executor.RalphHandler, interval time.Duration) *Worker {
	return &Worker{
		queue:    q,
		handler:  handler,
		interval: interval,
	}
}

// Run polls the queue until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	log.Printf("Worker started, polling every %s", w.interval)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Worker stopping")
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

func (w *Worker) poll(ctx context.Context) {
	job, err := w.queue.Dequeue()
	if err != nil {
		log.Printf("Worker: dequeue error: %v", err)
		return
	}
	if job == nil {
		return
	}

	log.Printf("Worker: picked up job #%d (%s), max iterations: %d", job.ID, job.Branch, job.MaxIterations)

	for {
		if ctx.Err() != nil {
			log.Printf("Worker: context cancelled, stopping job #%d", job.ID)
			return
		}

		job.IncrementIteration()
		log.Printf("Worker: job #%d starting iteration %d/%d", job.ID, job.Iteration, job.MaxIterations)

		if err := w.queue.Update(job); err != nil {
			log.Printf("Worker: failed to update job #%d iteration: %v", job.ID, err)
		}

		if err := w.handler.Handle(ctx, job); err != nil {
			log.Printf("Worker: job #%d failed at iteration %d: %v", job.ID, job.Iteration, err)
			if fErr := w.queue.Fail(job, err.Error()); fErr != nil {
				log.Printf("Worker: failed to mark job #%d as failed: %v", job.ID, fErr)
			}
			return
		}

		if job.HasReachedMaxIterations() {
			log.Printf("Worker: job #%d reached max iterations (%d)", job.ID, job.MaxIterations)
			break
		}
	}

	if err := w.queue.Complete(job); err != nil {
		log.Printf("Worker: failed to mark job #%d as complete: %v", job.ID, err)
	} else {
		log.Printf("Worker: job #%d completed after %d iterations", job.ID, job.Iteration)
	}
}
