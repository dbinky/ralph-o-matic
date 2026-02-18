package worker

import (
	"context"
	"encoding/json"
	"math"
	"sync/atomic"
	"time"

	"github.com/ryan/ralph-o-matic/internal/broadcast"
	"github.com/ryan/ralph-o-matic/internal/models"
)

const defaultProgressInterval = 5 * time.Second

// ProgressReporter emits job_progress events to the global SSE topic
// on a periodic interval while a job is running.
type ProgressReporter struct {
	broadcaster *broadcast.Broadcaster
	interval    time.Duration
	iteration   atomic.Int64
}

// NewProgressReporter creates a new ProgressReporter.
func NewProgressReporter(b *broadcast.Broadcaster) *ProgressReporter {
	return &ProgressReporter{
		broadcaster: b,
		interval:    defaultProgressInterval,
	}
}

// SetIteration updates the iteration counter (safe for concurrent use).
func (p *ProgressReporter) SetIteration(n int) {
	p.iteration.Store(int64(n))
}

// Start emits progress events until ctx is cancelled.
// Blocks until done — call in a goroutine.
//
// Concurrency invariant: job.ID, job.StartedAt, and job.MaxIterations are set
// before Start is called and never modified after. The mutable iteration count
// is read via p.iteration (atomic.Int64), updated by the worker via SetIteration.
func (p *ProgressReporter) Start(ctx context.Context, job *models.Job) {
	if p.broadcaster == nil {
		<-ctx.Done()
		return
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.emit(job)
		}
	}
}

func (p *ProgressReporter) emit(job *models.Job) {
	elapsed := 0.0
	if job.StartedAt != nil {
		elapsed = math.Round(time.Since(*job.StartedAt).Seconds())
	}

	payload, err := json.Marshal(map[string]interface{}{
		"type":          "job_progress",
		"jobID":         job.ID,
		"iteration":     p.iteration.Load(),
		"maxIterations": job.MaxIterations,
		"elapsedSec":    elapsed,
	})
	if err != nil {
		return
	}
	p.broadcaster.Publish("global", payload)
}
