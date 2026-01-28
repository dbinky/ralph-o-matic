package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_CreateJob(t *testing.T) {
	srv, _ := newTestServer(t)

	payload := map[string]interface{}{
		"repo_url":       "git@github.com:user/repo.git",
		"branch":         "feature/test",
		"prompt":         "Run all tests",
		"max_iterations": 50,
		"priority":       "high",
		"working_dir":    "packages/auth",
		"env":            map[string]string{"NODE_ENV": "test"},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Greater(t, resp.ID, int64(0))
	assert.Equal(t, models.StatusQueued, resp.Status)
	assert.Equal(t, "ralph/feature/test-result", resp.ResultBranch)
}

func TestAPI_CreateJob_Invalid(t *testing.T) {
	srv, _ := newTestServer(t)

	payload := map[string]interface{}{
		"repo_url": "", // Missing required field
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_GetJob(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create a job first
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	req := httptest.NewRequest("GET", "/api/jobs/"+strconv.FormatInt(job.ID, 10), nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, job.ID, resp.ID)
}

func TestAPI_GetJob_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/jobs/99999", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPI_ListJobs(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create multiple jobs
	for i := 0; i < 5; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		require.NoError(t, srv.queue.Enqueue(job))
	}

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Jobs  []*models.Job `json:"jobs"`
		Total int           `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 5, resp.Total)
	assert.Len(t, resp.Jobs, 5)
}

func TestAPI_ListJobs_WithStatus(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create jobs
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	req := httptest.NewRequest("GET", "/api/jobs?status=queued", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Jobs  []*models.Job `json:"jobs"`
		Total int           `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Total)
}

func TestAPI_CancelJob(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	req := httptest.NewRequest("DELETE", "/api/jobs/"+strconv.FormatInt(job.ID, 10), nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.StatusCancelled, resp.Status)
}

func TestAPI_PauseJob(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	// Start the job first
	_, err := srv.queue.Dequeue()
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/jobs/"+strconv.FormatInt(job.ID, 10)+"/pause", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.StatusPaused, resp.Status)
}

func TestAPI_ResumeJob(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	// Start and pause - must use returned job from Dequeue since it updates the pointer
	runningJob, err := srv.queue.Dequeue()
	require.NoError(t, err)
	require.NoError(t, srv.queue.Pause(runningJob))

	req := httptest.NewRequest("POST", "/api/jobs/"+strconv.FormatInt(job.ID, 10)+"/resume", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.StatusRunning, resp.Status)
}

func TestAPI_ReorderJobs(t *testing.T) {
	srv, _ := newTestServer(t)

	var jobs []*models.Job
	for i := 0; i < 3; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		require.NoError(t, srv.queue.Enqueue(job))
		jobs = append(jobs, job)
	}

	// Reorder: [3, 1, 2]
	payload := map[string]interface{}{
		"job_ids": []int64{jobs[2].ID, jobs[0].ID, jobs[1].ID},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("PUT", "/api/jobs/order", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
