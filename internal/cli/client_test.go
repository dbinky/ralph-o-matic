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

func TestClient_GetConfig_DeserializesRedactedFields(t *testing.T) {
	// This test verifies that the CLI correctly deserializes the redacted
	// API response. The API sends boolean _set indicators for sensitive
	// fields (api_key_set, password_set, webhook_url_set) rather than
	// the actual secrets.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/config", r.URL.Path)

		// This is exactly what the server's configResponse produces
		resp := map[string]interface{}{
			"ollama":                 map[string]interface{}{"host": "http://localhost:11434", "is_remote": false},
			"large_model":           map[string]interface{}{"name": "qwen2.5-coder:32b", "device": "gpu", "memory_gb": 20.0},
			"small_model":           map[string]interface{}{"name": "qwen2.5-coder:7b", "device": "cpu", "memory_gb": 4.5},
			"default_max_iterations": 50,
			"job_retention_days":     30,
			"default_backend":        "ollama",
			"anthropic": map[string]interface{}{
				"api_key_set":  true,
				"large_model": "claude-sonnet-4-20250514",
				"small_model": "claude-haiku-3-20240307",
			},
			"max_claude_retries":  3,
			"max_git_retries":     3,
			"git_retry_backoff_ms": 1000,
			"notify": map[string]interface{}{
				"smtp": map[string]interface{}{
					"enabled":      true,
					"host":         "smtp.example.com",
					"port":         587,
					"username":     "alerts@example.com",
					"password_set": true,
					"from":         "alerts@example.com",
					"recipients":   []string{"team@example.com"},
				},
				"teams": map[string]interface{}{
					"enabled":         true,
					"webhook_url_set": true,
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	cfg, err := client.GetConfig()

	require.NoError(t, err)

	// Verify redacted boolean fields are properly deserialized
	assert.True(t, cfg.Anthropic.APIKeySet, "api_key_set should be true")
	assert.Equal(t, "claude-sonnet-4-20250514", cfg.Anthropic.LargeModel)
	assert.Equal(t, "claude-haiku-3-20240307", cfg.Anthropic.SmallModel)

	assert.True(t, cfg.Notify.SMTP.Enabled, "smtp enabled should be true")
	assert.Equal(t, "smtp.example.com", cfg.Notify.SMTP.Host)
	assert.Equal(t, 587, cfg.Notify.SMTP.Port)
	assert.True(t, cfg.Notify.SMTP.PasswordSet, "password_set should be true")

	assert.True(t, cfg.Notify.Teams.Enabled, "teams enabled should be true")
	assert.True(t, cfg.Notify.Teams.WebhookURLSet, "webhook_url_set should be true")

	// Verify non-sensitive fields
	assert.Equal(t, "http://localhost:11434", cfg.Ollama.Host)
	assert.Equal(t, "qwen2.5-coder:32b", cfg.LargeModel.Name)
	assert.Equal(t, 50, cfg.DefaultMaxIterations)
	assert.Equal(t, models.Backend("ollama"), cfg.DefaultBackend)
}

func TestClient_UpdateJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PATCH", r.Method)
		assert.Equal(t, "/api/jobs/1", r.URL.Path)

		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "high", body["priority"])

		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		job.ID = 1
		job.Priority = models.PriorityHigh
		json.NewEncoder(w).Encode(job)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	job, err := client.UpdateJob(1, map[string]interface{}{"priority": "high"})

	require.NoError(t, err)
	assert.Equal(t, models.PriorityHigh, job.Priority)
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
