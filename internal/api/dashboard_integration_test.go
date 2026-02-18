package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/ryan/ralph-o-matic/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_SSE_ReceivesJobStatusEvent(t *testing.T) {
	srv, database, b := newTestServerWithBroadcaster(t)
	q := queue.New(database)
	q.SetBroadcaster(b)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Connect SSE client
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(ts.URL + "/api/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	// Enqueue a job (triggers job_status event)
	job := models.NewJob("https://github.com/user/repo.git", "main", "test prompt", 10)
	require.NoError(t, q.Enqueue(job))

	// Read the SSE event
	scanner := bufio.NewScanner(resp.Body)
	require.True(t, scanner.Scan(), "should read an SSE line")
	line := strings.TrimPrefix(scanner.Text(), "data: ")

	var evt map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(line), &evt))
	assert.Equal(t, "job_status", evt["type"])
	assert.Equal(t, "queued", evt["status"])
	assert.Equal(t, "main", evt["branch"])
	assert.Equal(t, "https://github.com/user/repo.git", evt["repo"])
	assert.Equal(t, float64(job.ID), evt["jobID"])
}

func TestIntegration_SSE_ReceivesJobLogEvent(t *testing.T) {
	srv, database, b := newTestServerWithBroadcaster(t)

	logRepo := db.NewLogRepo(database)
	logRepo.SetBroadcaster(b)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Connect SSE client
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(ts.URL + "/api/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Create a job first so log append doesn't fail on FK constraint
	jobRepo := db.NewJobRepo(database)
	job := models.NewJob("https://github.com/user/repo.git", "feat-x", "test", 5)
	require.NoError(t, jobRepo.Create(job))

	// Append a log (triggers job_log on global topic)
	require.NoError(t, logRepo.Append(job.ID, 1, "test output line"))

	// Read the SSE event
	scanner := bufio.NewScanner(resp.Body)
	require.True(t, scanner.Scan(), "should read an SSE line")
	line := strings.TrimPrefix(scanner.Text(), "data: ")

	var evt map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(line), &evt))
	assert.Equal(t, "job_log", evt["type"])
	assert.Equal(t, float64(job.ID), evt["jobID"])
	assert.Equal(t, float64(1), evt["iteration"])
	assert.Equal(t, "test output line", evt["message"])
}

func TestIntegration_DashboardState_ReturnsJobsByStatus(t *testing.T) {
	srv, database, _ := newTestServerWithBroadcaster(t)

	jobRepo := db.NewJobRepo(database)

	// Create a running job
	running := models.NewJob("https://github.com/user/repo.git", "feat-a", "fix tests", 10)
	running.Status = models.StatusRunning
	now := time.Now()
	running.StartedAt = &now
	running.Iteration = 3
	require.NoError(t, jobRepo.Create(running))

	// Create a queued job
	queued := models.NewJob("https://github.com/user/repo.git", "feat-b", "add feature", 5)
	require.NoError(t, jobRepo.Create(queued))

	// Create a completed job (terminal — should appear in recent terminal list)
	completed := models.NewJob("https://github.com/user/repo.git", "feat-c", "old work", 8)
	completed.Status = models.StatusCompleted
	completedAt := time.Now()
	completed.CompletedAt = &completedAt
	require.NoError(t, jobRepo.Create(completed))

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/dashboard-state")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Jobs []DashboardJob `json:"jobs"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	// Should contain all 3 jobs: running + queued (active) and completed (recent terminal)
	assert.Len(t, result.Jobs, 3)

	// Verify we can find each status in the results
	statuses := make(map[models.JobStatus]bool)
	for _, j := range result.Jobs {
		statuses[j.Status] = true
	}
	assert.True(t, statuses[models.StatusRunning], "should include running job")
	assert.True(t, statuses[models.StatusQueued], "should include queued job")
	assert.True(t, statuses[models.StatusCompleted], "should include completed job")
}

func TestIntegration_DashboardState_EmptyReturnsEmptyArray(t *testing.T) {
	srv, _, _ := newTestServerWithBroadcaster(t)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/dashboard-state")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Jobs []DashboardJob `json:"jobs"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Len(t, result.Jobs, 0)
}

func TestIntegration_SSE_StatusTransitionFlow(t *testing.T) {
	srv, database, b := newTestServerWithBroadcaster(t)
	q := queue.New(database)
	q.SetBroadcaster(b)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Connect SSE client
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(ts.URL + "/api/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Enqueue -> Dequeue (queued -> running) -> Complete
	job := models.NewJob("https://github.com/user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	scanner := bufio.NewScanner(resp.Body)

	// Read queued event
	require.True(t, scanner.Scan(), "should read queued event")
	line := strings.TrimPrefix(scanner.Text(), "data: ")
	var evt1 map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(line), &evt1))
	assert.Equal(t, "job_status", evt1["type"])
	assert.Equal(t, "queued", evt1["status"])

	// Dequeue triggers running event
	dequeuedJob, err := q.Dequeue()
	require.NoError(t, err)
	require.NotNil(t, dequeuedJob)

	// Skip blank line between SSE frames, then read running event
	require.True(t, scanner.Scan(), "should read running event")
	line = scanner.Text()
	// SSE frames have "data: ...\n\n" — scanner splits on \n so we may get
	// an empty line between frames. Skip it if so.
	if line == "" {
		require.True(t, scanner.Scan(), "should read running event after blank")
		line = scanner.Text()
	}
	line = strings.TrimPrefix(line, "data: ")

	var evt2 map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(line), &evt2))
	assert.Equal(t, "job_status", evt2["type"])
	assert.Equal(t, "running", evt2["status"])

	// Complete triggers completed event
	require.NoError(t, q.Complete(dequeuedJob))

	require.True(t, scanner.Scan(), "should read completed event")
	line = scanner.Text()
	if line == "" {
		require.True(t, scanner.Scan(), "should read completed event after blank")
		line = scanner.Text()
	}
	line = strings.TrimPrefix(line, "data: ")

	var evt3 map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(line), &evt3))
	assert.Equal(t, "job_status", evt3["type"])
	assert.Equal(t, "completed", evt3["status"])
}
