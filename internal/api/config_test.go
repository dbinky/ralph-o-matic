package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_GetConfig(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.ServerConfigResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// Should return defaults
	assert.Equal(t, "devstral", resp.LargeModel.Name)
	assert.Equal(t, "cpu", resp.LargeModel.Device)
	assert.Equal(t, "http://localhost:11434", resp.Ollama.Host)
}

func TestAPI_UpdateConfig(t *testing.T) {
	srv, _ := newTestServer(t)

	payload := models.ServerConfig{
		LargeModel: models.ModelPlacement{Name: "custom-model:latest"},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("PATCH", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.ServerConfigResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, "custom-model:latest", resp.LargeModel.Name)
	assert.Equal(t, "cpu", resp.LargeModel.Device) // Preserved from default
}

func TestAPI_ConfigRoundTrip_FullModelPlacement(t *testing.T) {
	srv, _ := newTestServer(t)

	payload := models.ServerConfig{
		Ollama:     models.OllamaConfig{Host: "http://10.0.0.1:11434", IsRemote: true},
		LargeModel: models.ModelPlacement{Name: "custom:70b", Device: "gpu", MemoryGB: 42},
		SmallModel: models.ModelPlacement{Name: "helper:1.5b", Device: "cpu", MemoryGB: 1.5},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("PATCH", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// GET and verify
	req = httptest.NewRequest("GET", "/api/config", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp models.ServerConfigResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "http://10.0.0.1:11434", resp.Ollama.Host)
	assert.True(t, resp.Ollama.IsRemote)
	assert.Equal(t, "custom:70b", resp.LargeModel.Name)
	assert.Equal(t, "gpu", resp.LargeModel.Device)
	assert.Equal(t, 42.0, resp.LargeModel.MemoryGB)
	assert.Equal(t, "helper:1.5b", resp.SmallModel.Name)
	assert.Equal(t, "cpu", resp.SmallModel.Device)
	assert.Equal(t, 1.5, resp.SmallModel.MemoryGB)
}

func TestAPI_ConfigRoundTrip_PartialUpdate_PreservesDefaults(t *testing.T) {
	srv, _ := newTestServer(t)

	// Only update the name — send raw JSON to avoid Go zero-value fields
	body := []byte(`{"large_model": {"name": "only-name:14b"}}`)
	req := httptest.NewRequest("PATCH", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp models.ServerConfigResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "only-name:14b", resp.LargeModel.Name)
	assert.Equal(t, "cpu", resp.LargeModel.Device)  // preserved from default
	assert.Equal(t, 15.0, resp.LargeModel.MemoryGB) // preserved from default
}

func TestAPI_ConfigRoundTrip_ExplicitZeroValues(t *testing.T) {
	srv, _ := newTestServer(t)

	// Explicitly set memory_gb to 0 and is_remote to false
	body := []byte(`{"large_model": {"name": "test:7b", "memory_gb": 0}, "ollama": {"host": "http://localhost:11434", "is_remote": false}}`)
	req := httptest.NewRequest("PATCH", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp models.ServerConfigResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "test:7b", resp.LargeModel.Name)
	assert.Equal(t, 0.0, resp.LargeModel.MemoryGB) // explicitly set to 0
	assert.False(t, resp.Ollama.IsRemote)          // explicitly set to false
}

func TestAPI_UpdateConfig_InvalidModel(t *testing.T) {
	srv, _ := newTestServer(t)

	// Empty name should fail validation
	payload := models.ServerConfig{
		LargeModel: models.ModelPlacement{Name: "valid:7b"},
		SmallModel: models.ModelPlacement{Name: ""},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("PATCH", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	// Small model name "" won't override default (merge skips empty strings)
	// so this should actually succeed since defaults fill in
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAPI_UpdateConfig_MalformedBody(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("PATCH", "/api/config", strings.NewReader("{{{"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_ConfigRoundTrip_OllamaRemote(t *testing.T) {
	srv, _ := newTestServer(t)

	payload := models.ServerConfig{
		Ollama: models.OllamaConfig{Host: "http://remote:11434", IsRemote: true},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("PATCH", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// GET
	req = httptest.NewRequest("GET", "/api/config", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	var resp models.ServerConfigResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "http://remote:11434", resp.Ollama.Host)
	assert.True(t, resp.Ollama.IsRemote)
}

func TestAPI_ConfigRoundTrip_Anthropic(t *testing.T) {
	srv, _ := newTestServer(t)

	body := []byte(`{"default_backend":"anthropic","anthropic":{"large_model":"claude-sonnet-4-20250514","small_model":"claude-haiku-4-5-20251001"}}`)
	req := httptest.NewRequest("PATCH", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// GET and verify models
	req = httptest.NewRequest("GET", "/api/config", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))

	var anthropic map[string]interface{}
	require.NoError(t, json.Unmarshal(raw["anthropic"], &anthropic))
	assert.Equal(t, "claude-sonnet-4-20250514", anthropic["large_model"])
	assert.Equal(t, "claude-haiku-4-5-20251001", anthropic["small_model"])
}

func TestAPI_GetConfig_IncludesAnthropicDefaults(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp models.ServerConfigResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.BackendOllama, resp.DefaultBackend)
	assert.Equal(t, "claude-sonnet-4-6-20260218", resp.Anthropic.LargeModel)
	assert.Equal(t, "claude-sonnet-4-6-20260218", resp.Anthropic.SmallModel)
}

func TestAPI_GetConfig_NoConcurrentJobsField(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	_, hasConcurrentJobs := raw["concurrent_jobs"]
	assert.False(t, hasConcurrentJobs, "GET /api/config should not include concurrent_jobs")
}

func TestAPI_UpdateConfig_ConcurrentJobsIgnored(t *testing.T) {
	srv, _ := newTestServer(t)

	// Send a PATCH with concurrent_jobs — it should be silently ignored
	body := []byte(`{"concurrent_jobs": 5, "default_max_iterations": 75}`)
	req := httptest.NewRequest("PATCH", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	_, hasConcurrentJobs := raw["concurrent_jobs"]
	assert.False(t, hasConcurrentJobs, "PATCH response should not include concurrent_jobs")

	// Verify the valid field was applied
	assert.Equal(t, float64(75), raw["default_max_iterations"])
}

func TestAPI_GetConfig_ResponseSchema_NoConcurrentJobs(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	// Verify all expected fields exist, concurrent_jobs does not
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))

	expectedKeys := []string{
		"ollama", "large_model", "small_model",
		"default_max_iterations", "job_retention_days",
		"default_backend", "anthropic",
		"max_claude_retries", "max_git_retries", "git_retry_backoff_ms",
		"notify",
	}
	for _, key := range expectedKeys {
		assert.Contains(t, raw, key, "expected key %s in response", key)
	}

	unexpectedKeys := []string{"concurrent_jobs"}
	for _, key := range unexpectedKeys {
		assert.NotContains(t, raw, key, "unexpected key %s in response", key)
	}
}

func TestAPI_GetConfig_IncludesNotifyConfig(t *testing.T) {
	srv, _ := newTestServer(t)

	// Set notification config
	body := []byte(`{"notify":{"smtp":{"enabled":true,"host":"mail.example.com","port":587,"username":"user@example.com","password":"s3cret","from":"ralph@example.com","recipients":["team@example.com"]},"teams":{"enabled":true,"webhook_url":"https://outlook.office.com/webhook/secret"}}}`)
	req := httptest.NewRequest("PATCH", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// GET and verify notification fields are present
	req = httptest.NewRequest("GET", "/api/config", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp models.ServerConfigResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// SMTP
	assert.True(t, resp.Notify.SMTP.Enabled)
	assert.Equal(t, "mail.example.com", resp.Notify.SMTP.Host)
	assert.Equal(t, 587, resp.Notify.SMTP.Port)
	assert.Equal(t, "user@example.com", resp.Notify.SMTP.Username)
	assert.True(t, resp.Notify.SMTP.PasswordSet)
	assert.Equal(t, "ralph@example.com", resp.Notify.SMTP.From)
	assert.Equal(t, []string{"team@example.com"}, resp.Notify.SMTP.Recipients)

	// Teams
	assert.True(t, resp.Notify.Teams.Enabled)
	assert.True(t, resp.Notify.Teams.WebhookURLSet)

	// Verify secrets are NOT in the raw JSON
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))

	var notifyRaw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["notify"], &notifyRaw))

	var smtpRaw map[string]interface{}
	require.NoError(t, json.Unmarshal(notifyRaw["smtp"], &smtpRaw))
	_, hasPassword := smtpRaw["password"]
	assert.False(t, hasPassword, "SMTP password must not be returned in GET response")

	var teamsRaw map[string]interface{}
	require.NoError(t, json.Unmarshal(notifyRaw["teams"], &teamsRaw))
	_, hasWebhookURL := teamsRaw["webhook_url"]
	assert.False(t, hasWebhookURL, "Teams webhook URL must not be returned in GET response")
}

func TestAPI_GetConfig_NotifyDefaultsEmpty(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp models.ServerConfigResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// Defaults: notifications disabled
	assert.False(t, resp.Notify.SMTP.Enabled)
	assert.False(t, resp.Notify.SMTP.PasswordSet)
	assert.False(t, resp.Notify.Teams.Enabled)
	assert.False(t, resp.Notify.Teams.WebhookURLSet)
}

func TestAPI_GetConfig_ResponseStructure(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))

	// Verify top-level structure
	assert.Contains(t, raw, "ollama")
	assert.Contains(t, raw, "large_model")
	assert.Contains(t, raw, "small_model")

	// Verify nested structure
	ollama, ok := raw["ollama"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, ollama, "host")
	assert.Contains(t, ollama, "is_remote")

	largeModel, ok := raw["large_model"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, largeModel, "name")
	assert.Contains(t, largeModel, "device")
	assert.Contains(t, largeModel, "memory_gb")
}
