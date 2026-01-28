# Phase 4: REST API

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the HTTP REST API with all job and config endpoints, proper error handling, and JSON serialization.

**Architecture:** Standard Go HTTP server with chi router for clean routing. Middleware for logging, recovery, and CORS. Handler functions delegate to queue and database layers.

**Tech Stack:** Go 1.22+, net/http, chi router, JSON encoding

**Dependencies:** Phase 3 must be complete (queue layer)

---

## Task 1: Add Router Dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add chi router**

Run:
```bash
go get github.com/go-chi/chi/v5
```

Expected: chi added to go.mod

**Step 2: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add chi router for HTTP API"
```

---

## Task 2: Implement API Server Foundation

**Files:**
- Create: `internal/api/server.go`
- Create: `internal/api/server_test.go`

**Step 1: Write the failing tests**

Create `internal/api/server_test.go`:

```go
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
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/api/... -v
```

Expected: FAIL - Server not defined

**Step 3: Write minimal implementation**

Create `internal/api/server.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/queue"
)

// Server is the HTTP API server
type Server struct {
	db     *db.DB
	queue  *queue.Queue
	addr   string
	router chi.Router
	server *http.Server
}

// NewServer creates a new API server
func NewServer(database *db.DB, q *queue.Queue, addr string) *Server {
	s := &Server{
		db:    database,
		queue: q,
		addr:  addr,
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(corsMiddleware)

	// Health check
	r.Get("/health", s.handleHealth)

	// API routes
	r.Route("/api", func(r chi.Router) {
		r.Route("/jobs", func(r chi.Router) {
			r.Post("/", s.handleCreateJob)
			r.Get("/", s.handleListJobs)
			r.Put("/order", s.handleReorderJobs)

			r.Route("/{jobID}", func(r chi.Router) {
				r.Get("/", s.handleGetJob)
				r.Delete("/", s.handleCancelJob)
				r.Patch("/", s.handleUpdateJob)
				r.Get("/logs", s.handleGetJobLogs)
				r.Post("/pause", s.handlePauseJob)
				r.Post("/resume", s.handleResumeJob)
			})
		})

		r.Route("/config", func(r chi.Router) {
			r.Get("/", s.handleGetConfig)
			r.Patch("/", s.handleUpdateConfig)
		})
	})

	s.router = r
}

// Router returns the chi router for testing
func (s *Server) Router() chi.Router {
	return s.router
}

// Start begins listening for HTTP requests
func (s *Server) Start() error {
	s.server = &http.Server{
		Addr:    s.addr,
		Handler: s.router,
	}

	log.Printf("API server starting on %s", s.addr)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Response helpers
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// CORS middleware
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Placeholder handlers (implemented in next tasks)
func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request)   {}
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request)    {}
func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request)      {}
func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request)   {}
func (s *Server) handleUpdateJob(w http.ResponseWriter, r *http.Request)   {}
func (s *Server) handleGetJobLogs(w http.ResponseWriter, r *http.Request)  {}
func (s *Server) handlePauseJob(w http.ResponseWriter, r *http.Request)    {}
func (s *Server) handleResumeJob(w http.ResponseWriter, r *http.Request)   {}
func (s *Server) handleReorderJobs(w http.ResponseWriter, r *http.Request) {}
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request)   {}
func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/api/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/server.go internal/api/server_test.go
git commit -m "feat(api): add HTTP server foundation with routing"
```

---

## Task 3: Implement Job Handlers

**Files:**
- Create: `internal/api/jobs.go`
- Create: `internal/api/jobs_test.go`

**Step 1: Write the failing tests**

Create `internal/api/jobs_test.go`:

```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

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

	// Start and pause
	_, _ = srv.queue.Dequeue()
	_ = srv.queue.Pause(job)

	req := httptest.NewRequest("POST", "/api/jobs/"+strconv.FormatInt(job.ID, 10)+"/resume", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.Job
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, models.StatusRunning, resp.Status)
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
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/api/... -v
```

Expected: FAIL - handlers return empty responses

**Step 3: Write minimal implementation**

Create `internal/api/jobs.go`:

```go
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
)

// CreateJobRequest is the request body for creating a job
type CreateJobRequest struct {
	RepoURL       string            `json:"repo_url"`
	Branch        string            `json:"branch"`
	Prompt        string            `json:"prompt"`
	MaxIterations int               `json:"max_iterations"`
	Priority      string            `json:"priority,omitempty"`
	WorkingDir    string            `json:"working_dir,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
}

// ListJobsResponse is the response for listing jobs
type ListJobsResponse struct {
	Jobs   []*models.Job `json:"jobs"`
	Total  int           `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

// ReorderRequest is the request body for reordering jobs
type ReorderRequest struct {
	JobIDs []int64 `json:"job_ids"`
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	job := models.NewJob(req.RepoURL, req.Branch, req.Prompt, req.MaxIterations)
	job.WorkingDir = req.WorkingDir
	job.Env = req.Env

	if req.Priority != "" {
		priority, err := models.ParsePriority(req.Priority)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		job.Priority = priority
	}

	if err := s.queue.Enqueue(job); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, job)
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	opts := db.ListOptions{}

	// Parse status filter
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		statuses := strings.Split(statusStr, ",")
		for _, s := range statuses {
			opts.Statuses = append(opts.Statuses, models.JobStatus(s))
		}
	}

	// Parse pagination
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, _ := strconv.Atoi(limitStr)
		opts.Limit = limit
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		offset, _ := strconv.Atoi(offsetStr)
		opts.Offset = offset
	}

	jobs, total, err := db.NewJobRepo(s.db).List(opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, ListJobsResponse{
		Jobs:   jobs,
		Total:  total,
		Limit:  opts.Limit,
		Offset: opts.Offset,
	})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job ID")
		return
	}

	job, err := s.queue.Get(jobID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job ID")
		return
	}

	job, err := s.queue.Get(jobID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.queue.Cancel(job); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleUpdateJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job ID")
		return
	}

	job, err := s.queue.Get(jobID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Apply updates
	if priority, ok := updates["priority"].(string); ok {
		p, err := models.ParsePriority(priority)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		job.Priority = p
	}
	if maxIter, ok := updates["max_iterations"].(float64); ok {
		job.MaxIterations = int(maxIter)
	}

	if err := s.queue.Update(job); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handlePauseJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job ID")
		return
	}

	job, err := s.queue.Get(jobID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.queue.Pause(job); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleResumeJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job ID")
		return
	}

	job, err := s.queue.Get(jobID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.queue.Resume(job); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleReorderJobs(w http.ResponseWriter, r *http.Request) {
	var req ReorderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if err := s.queue.Reorder(req.JobIDs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string][]int64{"reordered": req.JobIDs})
}

func (s *Server) handleGetJobLogs(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job ID")
		return
	}

	logRepo := db.NewLogRepo(s.db)
	logs, err := logRepo.GetForJob(jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"logs": logs})
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/api/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/jobs.go internal/api/jobs_test.go
git commit -m "feat(api): add job CRUD and control endpoints"
```

---

## Task 4: Implement Config Handlers

**Files:**
- Create: `internal/api/config.go`
- Create: `internal/api/config_test.go`

**Step 1: Write the failing tests**

Create `internal/api/config_test.go`:

```go
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
	assert.Equal(t, "qwen3-coder:70b", resp.LargeModel)
}

func TestAPI_UpdateConfig(t *testing.T) {
	srv, _ := newTestServer(t)

	payload := map[string]interface{}{
		"large_model":     "custom-model:latest",
		"concurrent_jobs": 3,
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("PATCH", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.ServerConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, "custom-model:latest", resp.LargeModel)
	assert.Equal(t, 3, resp.ConcurrentJobs)
}
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/api/... -v
```

Expected: FAIL - config handlers return empty responses

**Step 3: Write minimal implementation**

Create `internal/api/config.go`:

```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/ryan/ralph-o-matic/internal/db"
)

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	configRepo := db.NewConfigRepo(s.db)

	cfg, err := configRepo.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	configRepo := db.NewConfigRepo(s.db)

	// Get current config
	current, err := configRepo.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Parse updates
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Apply updates
	if v, ok := updates["large_model"].(string); ok {
		current.LargeModel = v
	}
	if v, ok := updates["small_model"].(string); ok {
		current.SmallModel = v
	}
	if v, ok := updates["default_max_iterations"].(float64); ok {
		current.DefaultMaxIterations = int(v)
	}
	if v, ok := updates["concurrent_jobs"].(float64); ok {
		current.ConcurrentJobs = int(v)
	}
	if v, ok := updates["workspace_dir"].(string); ok {
		current.WorkspaceDir = v
	}
	if v, ok := updates["job_retention_days"].(float64); ok {
		current.JobRetentionDays = int(v)
	}

	// Validate
	if err := current.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Save
	if err := configRepo.Save(current); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, current)
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/api/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/config.go internal/api/config_test.go
git commit -m "feat(api): add config get and update endpoints"
```

---

## Phase 4 Completion Checklist

- [ ] Chi router integrated
- [ ] Server foundation with middleware
- [ ] Health endpoint
- [ ] CORS support
- [ ] Job CRUD endpoints
- [ ] Job control endpoints (pause/resume/cancel)
- [ ] Queue reorder endpoint
- [ ] Job logs endpoint
- [ ] Config endpoints
- [ ] All tests passing
- [ ] All code committed

**Next Phase:** Phase 5 - Git Integration
