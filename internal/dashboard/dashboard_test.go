package dashboard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/auth"
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
	d := New(database, q, templatesDir, "test")
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

func createDashboardJob(t *testing.T, database *db.DB, ownerID, ownerName string) *models.Job {
	t.Helper()
	repo := db.NewJobRepo(database)
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	job.OwnerID = ownerID
	job.OwnerName = ownerName
	err := repo.Create(job)
	require.NoError(t, err)
	return job
}

func TestDashboard_Index_UserSeesOnlyOwnJobs(t *testing.T) {
	d, _ := newTestDashboard(t)
	database := d.db

	createDashboardJob(t, database, "user-a", "Alice")
	createDashboardJob(t, database, "user-b", "Bob")

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "user-a", Name: "Alice", Roles: []string{"User"},
	}))
	w := httptest.NewRecorder()

	d.HandleIndex(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Alice")
	assert.NotContains(t, body, "Bob")
}

func TestDashboard_Index_AdminSeesAllJobs(t *testing.T) {
	d, _ := newTestDashboard(t)
	database := d.db

	createDashboardJob(t, database, "user-a", "Alice")
	createDashboardJob(t, database, "user-b", "Bob")

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "admin-1", Name: "Admin", Roles: []string{"Admin"},
	}))
	w := httptest.NewRecorder()

	d.HandleIndex(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Alice")
	assert.Contains(t, body, "Bob")
}

func TestDashboard_Job_UserCanAccessOwnJob(t *testing.T) {
	d, _ := newTestDashboard(t)
	job := createDashboardJob(t, d.db, "user-a", "Alice")

	req := httptest.NewRequest("GET", "/jobs/1", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "user-a", Name: "Alice", Roles: []string{"User"},
	}))
	w := httptest.NewRecorder()

	d.HandleJob(w, req, job.ID)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDashboard_Job_UserCannotAccessOtherJob(t *testing.T) {
	d, _ := newTestDashboard(t)
	job := createDashboardJob(t, d.db, "user-b", "Bob")

	req := httptest.NewRequest("GET", "/jobs/1", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "user-a", Name: "Alice", Roles: []string{"User"},
	}))
	w := httptest.NewRecorder()

	d.HandleJob(w, req, job.ID)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDashboard_Job_AdminCanAccessAnyJob(t *testing.T) {
	d, _ := newTestDashboard(t)
	job := createDashboardJob(t, d.db, "user-b", "Bob")

	req := httptest.NewRequest("GET", "/jobs/1", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "admin-1", Name: "Admin", Roles: []string{"Admin"},
	}))
	w := httptest.NewRecorder()

	d.HandleJob(w, req, job.ID)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDashboard_Job_PreAuthJob_AccessibleByAll(t *testing.T) {
	d, _ := newTestDashboard(t)
	job := createDashboardJob(t, d.db, "", "")

	req := httptest.NewRequest("GET", "/jobs/1", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "user-a", Name: "Alice", Roles: []string{"User"},
	}))
	w := httptest.NewRecorder()

	d.HandleJob(w, req, job.ID)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDashboard_Config_NonAdminGetsForbidden(t *testing.T) {
	d, _ := newTestDashboard(t)

	req := httptest.NewRequest("GET", "/config", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "user-a", Name: "Alice", Roles: []string{"User"},
	}))
	w := httptest.NewRecorder()

	d.HandleConfig(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDashboard_Config_AdminCanAccess(t *testing.T) {
	d, _ := newTestDashboard(t)

	req := httptest.NewRequest("GET", "/config", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "admin-1", Name: "Admin", Roles: []string{"Admin"},
	}))
	w := httptest.NewRecorder()

	d.HandleConfig(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDashboard_Config_NoAuth_CanAccess(t *testing.T) {
	d, _ := newTestDashboard(t)

	req := httptest.NewRequest("GET", "/config", nil)
	// No auth context — auth mode none
	w := httptest.NewRecorder()

	d.HandleConfig(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDashboard_Index_NoAuth_SeesAllJobs(t *testing.T) {
	d, _ := newTestDashboard(t)
	database := d.db

	createDashboardJob(t, database, "user-a", "Alice")
	createDashboardJob(t, database, "user-b", "Bob")

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	d.HandleIndex(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Alice")
	assert.Contains(t, body, "Bob")
}

// --- TemplateFuncs tests ---

func TestTemplateFuncs_Truncate(t *testing.T) {
	funcs := TemplateFuncs()
	truncate := funcs["truncate"].(func(int, string) string)

	assert.Equal(t, "hello", truncate(10, "hello"))
	assert.Equal(t, "hel...", truncate(3, "hello world"))
	assert.Equal(t, "abc", truncate(3, "abc"))
}

func TestTemplateFuncs_Upper(t *testing.T) {
	funcs := TemplateFuncs()
	upper := funcs["upper"].(func(string) string)

	assert.Equal(t, "HELLO", upper("hello"))
}

func TestTemplateFuncs_Duration(t *testing.T) {
	funcs := TemplateFuncs()
	duration := funcs["duration"].(func(time.Duration) string)

	assert.Equal(t, "< 1m", duration(30*time.Second))
	assert.Equal(t, "5m", duration(5*time.Minute+30*time.Second))
	assert.Equal(t, "2h30m", duration(2*time.Hour+30*time.Minute))
}

func TestTemplateFuncs_TimeAgo(t *testing.T) {
	funcs := TemplateFuncs()
	timeago := funcs["timeago"].(func(*time.Time) string)

	assert.Equal(t, "never", timeago(nil))

	recent := time.Now().Add(-5 * time.Second)
	assert.Equal(t, "just now", timeago(&recent))

	minsAgo := time.Now().Add(-10 * time.Minute)
	result := timeago(&minsAgo)
	assert.Contains(t, result, "ago")
}

func TestTemplateFuncs_Multiply(t *testing.T) {
	funcs := TemplateFuncs()
	multiply := funcs["multiply"].(func(interface{}, interface{}) float64)

	assert.Equal(t, 6.0, multiply(2, 3))
	assert.Equal(t, 6.0, multiply(int64(2), int64(3)))
	assert.Equal(t, 6.0, multiply(2.0, 3.0))
	assert.Equal(t, 0.0, multiply("bad", 3)) // unsupported type = 0
}

func TestToFloat64(t *testing.T) {
	assert.Equal(t, float64(3), toFloat64(3))
	assert.Equal(t, float64(3), toFloat64(int64(3)))
	assert.Equal(t, float64(3.14), toFloat64(float64(3.14)))
	assert.InDelta(t, float64(3.14), toFloat64(float32(3.14)), 1e-6)
	assert.Equal(t, float64(0), toFloat64("bad"))
	assert.Equal(t, float64(0), toFloat64(nil))
}
