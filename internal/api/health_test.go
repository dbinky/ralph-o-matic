package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer_Readiness_Healthy(t *testing.T) {
	srv, database := newTestServer(t)

	// Set backend to anthropic so the Ollama check is skipped.
	configRepo := db.NewConfigRepo(database)
	cfg := models.DefaultServerConfig()
	cfg.DefaultBackend = models.BackendAnthropic
	require.NoError(t, configRepo.Save(cfg))

	req := httptest.NewRequest("GET", "/readiness", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp["status"])

	checks := resp["checks"].(map[string]interface{})
	assert.Equal(t, "ok", checks["database"])
	assert.Equal(t, "ok", checks["disk"])
}

func TestServer_Readiness_DBClosed(t *testing.T) {
	srv, database := newTestServer(t)
	database.Close()

	req := httptest.NewRequest("GET", "/readiness", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "unhealthy", resp["status"])

	checks := resp["checks"].(map[string]interface{})
	assert.NotEqual(t, "ok", checks["database"])
}

func TestServer_Readiness_OllamaDown(t *testing.T) {
	srv, database := newTestServer(t)

	// Configure Ollama backend pointing to a closed server
	configRepo := db.NewConfigRepo(database)
	cfg, err := configRepo.Get()
	require.NoError(t, err)
	cfg.DefaultBackend = models.BackendOllama
	cfg.Ollama.Host = "http://127.0.0.1:1" // nothing listening
	require.NoError(t, configRepo.Save(cfg))

	req := httptest.NewRequest("GET", "/readiness", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "unhealthy", resp["status"])

	checks := resp["checks"].(map[string]interface{})
	assert.NotEqual(t, "ok", checks["ollama"])
}

func TestServer_Readiness_OllamaHealthy(t *testing.T) {
	// Fake Ollama server
	fakeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"models":[]}`))
	}))
	defer fakeSrv.Close()

	srv, database := newTestServer(t)

	configRepo := db.NewConfigRepo(database)
	cfg, err := configRepo.Get()
	require.NoError(t, err)
	cfg.DefaultBackend = models.BackendOllama
	cfg.Ollama.Host = fakeSrv.URL
	require.NoError(t, configRepo.Save(cfg))

	req := httptest.NewRequest("GET", "/readiness", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp["status"])
	checks := resp["checks"].(map[string]interface{})
	assert.Equal(t, "ok", checks["ollama"])
}

func TestServer_Readiness_AnthropicBackend_SkipsOllama(t *testing.T) {
	srv, database := newTestServer(t)

	configRepo := db.NewConfigRepo(database)
	cfg, err := configRepo.Get()
	require.NoError(t, err)
	cfg.DefaultBackend = models.BackendAnthropic
	cfg.Anthropic.LargeModel = "claude-opus-4-5-20251101"
	cfg.Anthropic.SmallModel = "claude-haiku-4-5-20251001"
	require.NoError(t, configRepo.Save(cfg))

	req := httptest.NewRequest("GET", "/readiness", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp["status"])

	checks := resp["checks"].(map[string]interface{})
	_, hasOllama := checks["ollama"]
	assert.False(t, hasOllama, "ollama check should be skipped for anthropic backend")
}
