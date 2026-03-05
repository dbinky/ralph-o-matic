package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/auth"
	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_CreateJob(t *testing.T) {
	srv, _ := newTestServer(t)

	payload := map[string]interface{}{
		"repo_url":       "git@github.com:user/repo.git",
		"branch":         "feature/test",
		"prompt":         "Run all tests",
		"max_iterations": 50,
		"priority":       "high",
		"working_dir":    "packages/auth",
		"env":            map[string]string{"NODE_ENV": "test"},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Greater(t, resp.ID, int64(0))
	assert.Equal(t, models.StatusQueued, resp.Status)
	assert.Equal(t, "ralph/feature/test-result", resp.ResultBranch)
}

func TestAPI_CreateJob_Invalid(t *testing.T) {
	srv, _ := newTestServer(t)

	payload := map[string]interface{}{
		"repo_url": "", // Missing required field
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_GetJob(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create a job first
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	req := httptest.NewRequest("GET", "/api/jobs/"+strconv.FormatInt(job.ID, 10), nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, job.ID, resp.ID)
}

func TestAPI_GetJob_InvalidID(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/jobs/notanint", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_GetJob_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/jobs/99999", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPI_ListJobs(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create multiple jobs
	for i := 0; i < 5; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		require.NoError(t, srv.queue.Enqueue(job))
	}

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Jobs  []*models.Job `json:"jobs"`
		Total int           `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 5, resp.Total)
	assert.Len(t, resp.Jobs, 5)
}

func TestAPI_ListJobs_WithStatus(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create jobs
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	req := httptest.NewRequest("GET", "/api/jobs?status=queued", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Jobs  []*models.Job `json:"jobs"`
		Total int           `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Total)
}

func TestAPI_ListJobs_InvalidStatus(t *testing.T) {
	srv, _ := newTestServer(t)

	tests := []struct {
		name   string
		status string
	}{
		{"unknown status", "pending"},
		{"garbage string", "foobar"},
		{"valid with invalid", "queued,bogus"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/jobs?status="+tc.status, nil)
			w := httptest.NewRecorder()
			srv.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "invalid status filter")
		})
	}
}

func TestAPI_ListJobs_InvalidPagination(t *testing.T) {
	srv, _ := newTestServer(t)

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"non-numeric limit", "limit=abc", "invalid limit"},
		{"negative limit", "limit=-1", "invalid limit"},
		{"non-numeric offset", "offset=xyz", "invalid offset"},
		{"negative offset", "offset=-5", "invalid offset"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/jobs?"+tc.query, nil)
			w := httptest.NewRecorder()
			srv.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), tc.want)
		})
	}
}

func TestAPI_ListJobs_DefaultLimit(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create a job so we can verify the response structure
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListJobsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, defaultListLimit, resp.Limit)
}

func TestAPI_ListJobs_ExplicitLimitZero_AppliesDefault(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create 3 jobs
	for i := 0; i < 3; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		require.NoError(t, srv.queue.Enqueue(job))
	}

	// Explicit limit=0 should be treated as omitted and apply the default limit
	req := httptest.NewRequest("GET", "/api/jobs?limit=0", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListJobsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 3, resp.Total)
	assert.Len(t, resp.Jobs, 3)
	assert.Equal(t, defaultListLimit, resp.Limit, "limit=0 should apply default limit")
}

func TestAPI_CancelJob(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	req := httptest.NewRequest("DELETE", "/api/jobs/"+strconv.FormatInt(job.ID, 10), nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.StatusCancelled, resp.Status)
}

func TestAPI_PauseJob(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	// Start the job first
	_, err := srv.queue.Dequeue()
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/jobs/"+strconv.FormatInt(job.ID, 10)+"/pause", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.StatusPaused, resp.Status)
}

func TestAPI_ResumeJob(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	// Start and pause - must use returned job from Dequeue since it updates the pointer
	runningJob, err := srv.queue.Dequeue()
	require.NoError(t, err)
	require.NoError(t, srv.queue.Pause(runningJob))

	req := httptest.NewRequest("POST", "/api/jobs/"+strconv.FormatInt(job.ID, 10)+"/resume", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.StatusQueued, resp.Status)
}

func TestAPI_ReorderJobs(t *testing.T) {
	srv, _ := newTestServer(t)

	var jobs []*models.Job
	for i := 0; i < 3; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		require.NoError(t, srv.queue.Enqueue(job))
		jobs = append(jobs, job)
	}

	// Reorder: [3, 1, 2]
	payload := map[string]interface{}{
		"job_ids": []int64{jobs[2].ID, jobs[0].ID, jobs[1].ID},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("PUT", "/api/jobs/order", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAPI_CreateJob_WithBackend(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"repo_url":"https://github.com/foo/bar","branch":"main","prompt":"fix bugs","max_iterations":10,"backend":"anthropic"}`
	req := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var job models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &job))
	assert.Equal(t, models.BackendAnthropic, job.Backend)
}

func TestAPI_CreateJob_InvalidBackend(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"repo_url":"https://github.com/foo/bar","branch":"main","prompt":"fix bugs","max_iterations":10,"backend":"gpt"}`
	req := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_CreateJob_EmptyBackend_UsesDefault(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"repo_url":"https://github.com/foo/bar","branch":"main","prompt":"fix bugs","max_iterations":10}`
	req := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var job models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &job))
	assert.Equal(t, models.Backend(""), job.Backend)
}

func TestAPI_CreateJob_PathTraversal_Rejected(t *testing.T) {
	srv, _ := newTestServer(t)

	tests := []struct {
		name       string
		workingDir string
	}{
		{"simple traversal", "../etc"},
		{"nested traversal", "foo/../../bar"},
		{"deep traversal", "a/b/c/../../../.."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]interface{}{
				"repo_url":       "https://github.com/foo/bar",
				"branch":         "main",
				"prompt":         "fix bugs",
				"max_iterations": 10,
				"working_dir":    tc.workingDir,
			}
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "path traversal")
		})
	}
}

func TestAPI_CreateJob_ValidWorkingDir_Accepted(t *testing.T) {
	srv, _ := newTestServer(t)

	tests := []struct {
		name       string
		workingDir string
	}{
		{"simple subdir", "packages/auth"},
		{"nested subdir", "src/components/ui"},
		{"with dots in name", "my.package/src"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]interface{}{
				"repo_url":       "https://github.com/foo/bar",
				"branch":         "main",
				"prompt":         "fix bugs",
				"max_iterations": 10,
				"working_dir":    tc.workingDir,
			}
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)
		})
	}
}

func TestAPI_CreateJob_DeniedEnvVars_Rejected(t *testing.T) {
	srv, _ := newTestServer(t)

	tests := []struct {
		name   string
		envKey string
	}{
		{"LD_PRELOAD", "LD_PRELOAD"},
		{"LD_LIBRARY_PATH", "LD_LIBRARY_PATH"},
		{"DYLD_INSERT_LIBRARIES", "DYLD_INSERT_LIBRARIES"},
		{"PATH", "PATH"},
		{"HOME", "HOME"},
		{"SHELL", "SHELL"},
		{"ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY"},
		{"CLAUDE_CONFIG", "CLAUDE_CONFIG"},
		{"lowercase path", "path"},
		{"lowercase ld_preload", "ld_preload"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]interface{}{
				"repo_url":       "https://github.com/foo/bar",
				"branch":         "main",
				"prompt":         "fix bugs",
				"max_iterations": 10,
				"env":            map[string]string{tc.envKey: "malicious"},
			}
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "not allowed")
		})
	}
}

func TestAPI_CreateJob_AllowedEnvVars_Accepted(t *testing.T) {
	srv, _ := newTestServer(t)

	tests := []struct {
		name   string
		envKey string
	}{
		{"NODE_ENV", "NODE_ENV"},
		{"CUSTOM_VAR", "CUSTOM_VAR"},
		{"MY_PATH", "MY_PATH"}, // not PATH itself
		{"DEBUG", "DEBUG"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]interface{}{
				"repo_url":       "https://github.com/foo/bar",
				"branch":         "main",
				"prompt":         "fix bugs",
				"max_iterations": 10,
				"env":            map[string]string{tc.envKey: "value"},
			}
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)
		})
	}
}

// Helper to create a job directly in the DB with a specific owner
func createJobWithOwner(t *testing.T, database *db.DB, ownerID, ownerName string) *models.Job {
	t.Helper()
	repo := db.NewJobRepo(database)
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	job.OwnerID = ownerID
	job.OwnerName = ownerName
	err := repo.Create(job)
	require.NoError(t, err)
	return job
}

func TestCreateJob_SetsOwnerFromContext(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"repo_url":"https://github.com/foo/bar","branch":"main","prompt":"fix bugs","max_iterations":10}`
	req := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	user := &auth.User{
		ID:    "user-abc-123",
		Name:  "Alice Smith",
		Email: "alice@example.com",
		Roles: []string{"User"},
	}
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))

	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "user-abc-123", resp.OwnerID)
	assert.Equal(t, "Alice Smith", resp.OwnerName)
}

func TestCreateJob_NoAuth_EmptyOwner(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"repo_url":"https://github.com/foo/bar","branch":"main","prompt":"fix bugs","max_iterations":10}`
	req := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.OwnerID)
	assert.Empty(t, resp.OwnerName)
}

func TestListJobs_UserSeesOnlyOwnJobs(t *testing.T) {
	srv, database := newTestServer(t)

	// Create jobs for two different owners
	createJobWithOwner(t, database, "user-a", "Alice")
	createJobWithOwner(t, database, "user-a", "Alice")
	createJobWithOwner(t, database, "user-b", "Bob")

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	user := &auth.User{
		ID:    "user-a",
		Name:  "Alice",
		Email: "alice@example.com",
		Roles: []string{"User"},
	}
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))

	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Jobs  []*models.Job `json:"jobs"`
		Total int           `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Total)
	assert.Len(t, resp.Jobs, 2)
	for _, job := range resp.Jobs {
		assert.Equal(t, "user-a", job.OwnerID)
	}
}

func TestListJobs_AdminSeesAllJobs(t *testing.T) {
	srv, database := newTestServer(t)

	createJobWithOwner(t, database, "user-a", "Alice")
	createJobWithOwner(t, database, "user-b", "Bob")
	createJobWithOwner(t, database, "user-c", "Charlie")

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	admin := &auth.User{
		ID:    "admin-1",
		Name:  "Admin",
		Email: "admin@example.com",
		Roles: []string{"Admin"},
	}
	req = req.WithContext(auth.ContextWithUser(req.Context(), admin))

	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Jobs  []*models.Job `json:"jobs"`
		Total int           `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 3, resp.Total)
	assert.Len(t, resp.Jobs, 3)
}

func TestGetJob_UserCanAccessOwnJob(t *testing.T) {
	srv, database := newTestServer(t)

	job := createJobWithOwner(t, database, "user-a", "Alice")

	req := httptest.NewRequest("GET", "/api/jobs/"+strconv.FormatInt(job.ID, 10), nil)
	user := &auth.User{
		ID:    "user-a",
		Name:  "Alice",
		Email: "alice@example.com",
		Roles: []string{"User"},
	}
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))

	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetJob_UserCannotAccessOtherJob(t *testing.T) {
	srv, database := newTestServer(t)

	job := createJobWithOwner(t, database, "user-b", "Bob")

	req := httptest.NewRequest("GET", "/api/jobs/"+strconv.FormatInt(job.ID, 10), nil)
	user := &auth.User{
		ID:    "user-a",
		Name:  "Alice",
		Email: "alice@example.com",
		Roles: []string{"User"},
	}
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))

	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestGetJob_PreAuthJob_AccessibleByAll(t *testing.T) {
	srv, database := newTestServer(t)

	// Job with empty owner_id (pre-auth job)
	job := createJobWithOwner(t, database, "", "")

	req := httptest.NewRequest("GET", "/api/jobs/"+strconv.FormatInt(job.ID, 10), nil)
	user := &auth.User{
		ID:    "user-a",
		Name:  "Alice",
		Email: "alice@example.com",
		Roles: []string{"User"},
	}
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))

	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAPI_UpdateJob_Priority(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	body := `{"priority":"high"}`
	req := httptest.NewRequest("PATCH", "/api/jobs/"+strconv.FormatInt(job.ID, 10), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.PriorityHigh, resp.Priority)
}

func TestAPI_UpdateJob_MaxIterations(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	body := `{"max_iterations":25}`
	req := httptest.NewRequest("PATCH", "/api/jobs/"+strconv.FormatInt(job.ID, 10), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 25, resp.MaxIterations)
}

func TestAPI_UpdateJob_RunningJob(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	// Dequeue to move to running state
	_, err := srv.queue.Dequeue()
	require.NoError(t, err)

	body := `{"max_iterations":50}`
	req := httptest.NewRequest("PATCH", "/api/jobs/"+strconv.FormatInt(job.ID, 10), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 50, resp.MaxIterations)
}

func TestAPI_UpdateJob_TerminalState_Rejected(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	// Cancel the job to put it in a terminal state
	require.NoError(t, srv.queue.Cancel(job))

	body := `{"priority":"high"}`
	req := httptest.NewRequest("PATCH", "/api/jobs/"+strconv.FormatInt(job.ID, 10), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "cannot update job")
}

func TestAPI_UpdateJob_InvalidMaxIterations(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	tests := []struct {
		name string
		body string
	}{
		{"zero", `{"max_iterations":0}`},
		{"negative", `{"max_iterations":-5}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("PATCH", "/api/jobs/"+strconv.FormatInt(job.ID, 10), strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			srv.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "max_iterations must be positive")
		})
	}
}

func TestAPI_UpdateJob_InvalidPriority(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	body := `{"priority":"urgent"}`
	req := httptest.NewRequest("PATCH", "/api/jobs/"+strconv.FormatInt(job.ID, 10), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_UpdateJob_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	req := httptest.NewRequest("PATCH", "/api/jobs/"+strconv.FormatInt(job.ID, 10), strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid JSON")
}

func TestAPI_UpdateJob_UnknownField_Rejected(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	tests := []struct {
		name string
		body string
	}{
		{"typo max_iteration", `{"max_iteration":25}`},
		{"unknown field", `{"foo":"bar"}`},
		{"mixed known and unknown", `{"priority":"high","unknown":true}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("PATCH", "/api/jobs/"+strconv.FormatInt(job.ID, 10), strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			srv.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "unknown field")
		})
	}
}

func TestAPI_UpdateJob_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"priority":"high"}`
	req := httptest.NewRequest("PATCH", "/api/jobs/99999", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPI_CancelJob_InvalidID(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("DELETE", "/api/jobs/notanint", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_CancelJob_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("DELETE", "/api/jobs/99999", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPI_CancelJob_AlreadyCancelled(t *testing.T) {
	srv, _ := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))
	require.NoError(t, srv.queue.Cancel(job))

	req := httptest.NewRequest("DELETE", "/api/jobs/"+strconv.FormatInt(job.ID, 10), nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_PauseJob_InvalidID(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("POST", "/api/jobs/notanint/pause", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_PauseJob_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("POST", "/api/jobs/99999/pause", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPI_PauseJob_WrongState(t *testing.T) {
	srv, _ := newTestServer(t)

	// Queued job cannot be paused (must be running first)
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	req := httptest.NewRequest("POST", "/api/jobs/"+strconv.FormatInt(job.ID, 10)+"/pause", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_ResumeJob_InvalidID(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("POST", "/api/jobs/notanint/resume", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_ResumeJob_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("POST", "/api/jobs/99999/resume", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPI_ResumeJob_WrongState(t *testing.T) {
	srv, _ := newTestServer(t)

	// Queued job cannot be resumed (must be paused first)
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	req := httptest.NewRequest("POST", "/api/jobs/"+strconv.FormatInt(job.ID, 10)+"/resume", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_ReorderJobs_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("PUT", "/api/jobs/order", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_GetJobLogs(t *testing.T) {
	srv, database := newTestServer(t)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, srv.queue.Enqueue(job))

	// Write a log entry
	logRepo := db.NewLogRepo(database)
	require.NoError(t, logRepo.Append(job.ID, 1, "line one\n"))

	req := httptest.NewRequest("GET", "/api/jobs/"+strconv.FormatInt(job.ID, 10)+"/logs", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp, "logs")
}

func TestAPI_GetJobLogs_InvalidID(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/jobs/notanint/logs", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_GetJobLogs_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/jobs/99999/logs", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
