package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { database.Close() })

	q := queue.New(database)
	srv := NewServer(database, q, ":9090")
	return srv, database
}

func TestServer_Health(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}

func TestServer_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServer_CORS(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("OPTIONS", "/api/jobs", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Origin"))
}
