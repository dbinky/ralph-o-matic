package dashboard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/ryan/ralph-o-matic/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDashboard(t *testing.T) (*Dashboard, *queue.Queue) {
	t.Helper()
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { database.Close() })

	q := queue.New(database)

	// Use the actual web/templates directory
	templatesDir := os.DirFS("../../web/templates")
	d := New(database, q, templatesDir)
	return d, q
}

func TestDashboard_Index(t *testing.T) {
	d, q := newTestDashboard(t)

	// Add some jobs
	job := models.NewJob("git@github.com:user/repo.git", "main", "test prompt", 10)
	require.NoError(t, q.Enqueue(job))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	d.HandleIndex(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Ralph-o-matic")
	assert.Contains(t, w.Body.String(), "main") // Branch name
}

func TestDashboard_Job(t *testing.T) {
	d, q := newTestDashboard(t)

	job := models.NewJob("git@github.com:user/repo.git", "feature/test", "test prompt", 10)
	require.NoError(t, q.Enqueue(job))

	req := httptest.NewRequest("GET", "/jobs/1", nil)
	w := httptest.NewRecorder()

	d.HandleJob(w, req, job.ID)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "feature/test")
}

func TestDashboard_JobNotFound(t *testing.T) {
	d, _ := newTestDashboard(t)

	req := httptest.NewRequest("GET", "/jobs/999", nil)
	w := httptest.NewRecorder()

	d.HandleJob(w, req, 999)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
