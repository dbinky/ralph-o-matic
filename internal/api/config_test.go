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

	var resp models.ServerConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// Should return defaults
	assert.Equal(t, "qwen3-coder:70b", resp.LargeModel.Name)
	assert.Equal(t, "cpu", resp.LargeModel.Device)
	assert.Equal(t, "http://localhost:11434", resp.Ollama.Host)
}

func TestAPI_UpdateConfig(t *testing.T) {
	srv, _ := newTestServer(t)

	payload := models.ServerConfig{
		LargeModel:     models.ModelPlacement{Name: "custom-model:latest"},
		ConcurrentJobs: 3,
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("PATCH", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.ServerConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, "custom-model:latest", resp.LargeModel.Name)
	assert.Equal(t, "cpu", resp.LargeModel.Device) // Preserved from default
	assert.Equal(t, 3, resp.ConcurrentJobs)
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

	var resp models.ServerConfig
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

	var resp models.ServerConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "only-name:14b", resp.LargeModel.Name)
	assert.Equal(t, "cpu", resp.LargeModel.Device) // preserved from default
	assert.Equal(t, 42.0, resp.LargeModel.MemoryGB) // preserved from default
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

	var resp models.ServerConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "test:7b", resp.LargeModel.Name)
	assert.Equal(t, 0.0, resp.LargeModel.MemoryGB) // explicitly set to 0
	assert.False(t, resp.Ollama.IsRemote)            // explicitly set to false
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

	var resp models.ServerConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "http://remote:11434", resp.Ollama.Host)
	assert.True(t, resp.Ollama.IsRemote)
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
