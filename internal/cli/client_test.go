package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/jobs", r.URL.Path)

		resp := map[string]interface{}{
			"jobs":  []*models.Job{},
			"total": 0,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	jobs, total, err := client.GetJobs(nil)

	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Len(t, jobs, 0)
}

func TestClient_CreateJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/jobs", r.URL.Path)

		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		job.ID = 1
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(job)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	job, err := client.CreateJob(&CreateJobRequest{
		RepoURL:       "git@github.com:user/repo.git",
		Branch:        "main",
		Prompt:        "test",
		MaxIterations: 10,
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1), job.ID)
}

func TestClient_PauseJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/jobs/1/pause", r.URL.Path)

		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		job.ID = 1
		job.Status = models.StatusPaused
		json.NewEncoder(w).Encode(job)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	job, err := client.PauseJob(1)

	require.NoError(t, err)
	assert.Equal(t, models.StatusPaused, job.Status)
}
