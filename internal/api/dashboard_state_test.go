package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboardState_ReturnsActiveJobs(t *testing.T) {
	srv, database, _ := newTestServerWithBroadcaster(t)

	// Create jobs in different states
	jobRepo := db.NewJobRepo(database)
	running := models.NewJob("https://github.com/user/repo.git", "feat-a", "test", 10)
	running.Status = models.StatusRunning
	now := time.Now()
	running.StartedAt = &now
	running.Iteration = 3
	require.NoError(t, jobRepo.Create(running))

	queued := models.NewJob("https://github.com/user/repo.git", "feat-b", "test", 5)
	require.NoError(t, jobRepo.Create(queued))

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/dashboard-state")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	jobs := result["jobs"].([]interface{})
	assert.Len(t, jobs, 2)
}

func TestDashboardState_EmptyQueue(t *testing.T) {
	srv, _, _ := newTestServerWithBroadcaster(t)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/dashboard-state")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	jobs := result["jobs"].([]interface{})
	assert.Len(t, jobs, 0)
}
