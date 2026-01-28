package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
