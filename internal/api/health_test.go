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
