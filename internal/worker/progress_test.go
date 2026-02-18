package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/broadcast"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProgressReporter_EmitsProgress(t *testing.T) {
	b := broadcast.New()
	_, ch := b.Subscribe("global")

	now := time.Now()
	job := &models.Job{ID: 42, Iteration: 3, StartedAt: &now}

	pr := NewProgressReporter(b)
	pr.interval = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go pr.Start(ctx, job)

	select {
	case msg := <-ch:
		var evt map[string]interface{}
		require.NoError(t, json.Unmarshal(msg, &evt))
		assert.Equal(t, "job_progress", evt["type"])
		assert.Equal(t, float64(42), evt["jobID"])
		assert.Equal(t, float64(3), evt["iteration"])
		assert.True(t, evt["elapsedSec"].(float64) >= 0)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for progress event")
	}
}

func TestProgressReporter_StopsOnCancel(t *testing.T) {
	b := broadcast.New()
	now := time.Now()
	job := &models.Job{ID: 1, Iteration: 1, StartedAt: &now}

	pr := NewProgressReporter(b)
	pr.interval = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		pr.Start(ctx, job)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// Good — returned promptly
	case <-time.After(time.Second):
		t.Fatal("ProgressReporter did not stop after context cancel")
	}
}

func TestProgressReporter_NilBroadcaster(t *testing.T) {
	pr := NewProgressReporter(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should not panic
	pr.Start(ctx, &models.Job{ID: 1})
}
