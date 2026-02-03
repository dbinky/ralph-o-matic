# Phase 5: Job Ownership & Database Migration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `owner_id` and `owner_name` fields to the Job model, create a database migration, update the API layer to set ownership from authenticated user claims, and filter job lists by ownership for non-admin users.

**Architecture:** New migration `003_add_owner_fields.sql` adds two columns with empty-string defaults (backward compatible). `Job` model gets `OwnerID` and `OwnerName` fields. API handlers extract the user from request context (set by Phase 4 middleware) and apply ownership at creation time. List/Get/Cancel/Pause/Resume handlers enforce ownership filtering: Users see only their own jobs, Admins see all.

**Tech Stack:** Go stdlib, SQLite, existing `internal/auth` context helpers from Phase 2

---

### Task 1: Create database migration

**Files:**
- Create: `internal/db/migrations/003_add_owner_fields.sql`

**Step 1: Write the migration**

Create `internal/db/migrations/003_add_owner_fields.sql`:

```sql
-- Add owner fields for job ownership when auth is enabled
ALTER TABLE jobs ADD COLUMN owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN owner_name TEXT NOT NULL DEFAULT '';

-- Index for filtering jobs by owner
CREATE INDEX IF NOT EXISTS idx_jobs_owner_id ON jobs(owner_id);
```

**Step 2: Verify migration applies**

Run: `go test -v -run TestDB_Migrate ./internal/db/`
Expected: PASS — migration system picks up and applies the new file

**Step 3: Commit**

```bash
git add internal/db/migrations/003_add_owner_fields.sql
git commit -m "db: add migration 003 for job owner_id and owner_name"
```

---

### Task 2: Add owner fields to Job model

**Files:**
- Modify: `internal/models/job.go`
- Test: `internal/models/job_test.go`

**Step 1: Write the failing test**

Add to `internal/models/job_test.go`:

```go
func TestNewJob_OwnerFieldsEmpty(t *testing.T) {
	job := NewJob("https://github.com/user/repo.git", "main", "Fix tests", 50)
	assert.Equal(t, "", job.OwnerID)
	assert.Equal(t, "", job.OwnerName)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestNewJob_OwnerFieldsEmpty ./internal/models/`
Expected: FAIL — `OwnerID` field does not exist

**Step 3: Write minimal implementation**

Add fields to `Job` struct in `internal/models/job.go`:

```go
type Job struct {
	ID       int64     `json:"id"`
	Status   JobStatus `json:"status"`
	Priority Priority  `json:"priority"`
	Position int       `json:"position"`

	// Repository info
	RepoURL      string `json:"repo_url"`
	Branch       string `json:"branch"`
	ResultBranch string `json:"result_branch"`
	WorkingDir   string `json:"working_dir,omitempty"`

	// Execution config
	Prompt        string            `json:"prompt"`
	MaxIterations int               `json:"max_iterations"`
	Backend       Backend           `json:"backend,omitempty"`
	Env           map[string]string `json:"env,omitempty"`

	// Progress tracking
	Iteration  int `json:"iteration"`
	RetryCount int `json:"retry_count"`

	// Ownership (set when auth is enabled, empty when auth is none)
	OwnerID   string `json:"owner_id,omitempty"`
	OwnerName string `json:"owner_name,omitempty"`

	// Timestamps
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	PausedAt    *time.Time `json:"paused_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// Results
	PRURL string `json:"pr_url,omitempty"`
	Error string `json:"error,omitempty"`
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestNewJob_OwnerFieldsEmpty ./internal/models/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/job.go internal/models/job_test.go
git commit -m "feat(models): add OwnerID and OwnerName to Job"
```

---

### Task 3: Update JobRepo to persist owner fields

**Files:**
- Modify: `internal/db/jobs.go`
- Test: `internal/db/jobs_test.go`

**Step 1: Write the failing test**

Add to `internal/db/jobs_test.go`:

```go
func TestJobRepo_Create_WithOwner(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	job.OwnerID = "oid-abc-123"
	job.OwnerName = "Ryan"

	err := repo.Create(job)
	require.NoError(t, err)

	fetched, err := repo.Get(job.ID)
	require.NoError(t, err)
	assert.Equal(t, "oid-abc-123", fetched.OwnerID)
	assert.Equal(t, "Ryan", fetched.OwnerName)
}

func TestJobRepo_Create_WithoutOwner(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := repo.Create(job)
	require.NoError(t, err)

	fetched, err := repo.Get(job.ID)
	require.NoError(t, err)
	assert.Equal(t, "", fetched.OwnerID)
	assert.Equal(t, "", fetched.OwnerName)
}

func TestJobRepo_List_FilterByOwner(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create jobs with different owners
	for _, owner := range []string{"user-a", "user-a", "user-b"} {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		job.OwnerID = owner
		err := repo.Create(job)
		require.NoError(t, err)
	}

	// Filter by owner
	jobs, total, err := repo.List(ListOptions{OwnerID: "user-a"})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, jobs, 2)
	for _, job := range jobs {
		assert.Equal(t, "user-a", job.OwnerID)
	}
}

func TestJobRepo_List_NoOwnerFilter_ReturnsAll(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	for _, owner := range []string{"user-a", "user-b"} {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		job.OwnerID = owner
		err := repo.Create(job)
		require.NoError(t, err)
	}

	jobs, total, err := repo.List(ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, jobs, 2)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run "TestJobRepo_Create_With|TestJobRepo_List_FilterByOwner|TestJobRepo_List_NoOwnerFilter" ./internal/db/`
Expected: FAIL — owner columns not in SQL queries, `OwnerID` field on `ListOptions` doesn't exist

**Step 3: Write minimal implementation**

Update `internal/db/jobs.go`:

1. Add `owner_id` and `owner_name` to the `Create` INSERT statement
2. Add `owner_id` and `owner_name` to the `Get` SELECT statement and scan
3. Add `owner_id` and `owner_name` to the `Update` UPDATE statement
4. Add `OwnerID` to `ListOptions`
5. Add owner filtering to `List`

For `Create`, add to the INSERT columns and values:

```go
result, err := r.db.conn.Exec(`
	INSERT INTO jobs (
		status, priority, position,
		repo_url, branch, result_branch, working_dir,
		prompt, max_iterations, env,
		iteration, retry_count,
		owner_id, owner_name,
		created_at, started_at, paused_at, completed_at,
		pr_url, error
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
	job.Status, job.Priority, job.Position,
	job.RepoURL, job.Branch, job.ResultBranch, job.WorkingDir,
	job.Prompt, job.MaxIterations, envJSON,
	job.Iteration, job.RetryCount,
	job.OwnerID, job.OwnerName,
	job.CreatedAt, job.StartedAt, job.PausedAt, job.CompletedAt,
	job.PRURL, job.Error,
)
```

For `Get`, add to SELECT and Scan:

```go
err := r.db.conn.QueryRow(`
	SELECT
		id, status, priority, position,
		repo_url, branch, result_branch, working_dir,
		prompt, max_iterations, env,
		iteration, retry_count,
		owner_id, owner_name,
		created_at, started_at, paused_at, completed_at,
		pr_url, error
	FROM jobs WHERE id = ?
`, id).Scan(
	&job.ID, &job.Status, &job.Priority, &job.Position,
	&job.RepoURL, &job.Branch, &job.ResultBranch, &workingDir,
	&job.Prompt, &job.MaxIterations, &envJSON,
	&job.Iteration, &job.RetryCount,
	&job.OwnerID, &job.OwnerName,
	&job.CreatedAt, &startedAt, &pausedAt, &completedAt,
	&prURL, &errStr,
)
```

For `Update`, add `owner_id` and `owner_name`:

```go
_, err = r.db.conn.Exec(`
	UPDATE jobs SET
		status = ?, priority = ?, position = ?,
		repo_url = ?, branch = ?, result_branch = ?, working_dir = ?,
		prompt = ?, max_iterations = ?, env = ?,
		iteration = ?, retry_count = ?,
		owner_id = ?, owner_name = ?,
		started_at = ?, paused_at = ?, completed_at = ?,
		pr_url = ?, error = ?
	WHERE id = ?
`,
	job.Status, job.Priority, job.Position,
	job.RepoURL, job.Branch, job.ResultBranch, job.WorkingDir,
	job.Prompt, job.MaxIterations, envJSON,
	job.Iteration, job.RetryCount,
	job.OwnerID, job.OwnerName,
	job.StartedAt, job.PausedAt, job.CompletedAt,
	job.PRURL, job.Error,
	job.ID,
)
```

For `ListOptions` and `List`:

```go
type ListOptions struct {
	Statuses []models.JobStatus
	OwnerID  string
	Limit    int
	Offset   int
}

// In List(), add owner filtering:
if opts.OwnerID != "" {
	where = append(where, "owner_id = ?")
	args = append(args, opts.OwnerID)
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run "TestJobRepo_Create_With|TestJobRepo_List_FilterByOwner|TestJobRepo_List_NoOwnerFilter" ./internal/db/`
Expected: PASS

**Step 5: Run all existing DB tests to verify no regressions**

Run: `go test -v -race ./internal/db/`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/db/jobs.go internal/db/jobs_test.go
git commit -m "feat(db): persist owner_id and owner_name, add owner filtering"
```

---

### Task 4: Update API handlers for job ownership

**Files:**
- Modify: `internal/api/jobs.go`
- Test: `internal/api/jobs_test.go`

**Step 1: Write the failing test**

Add to `internal/api/jobs_test.go`:

```go
func TestCreateJob_SetsOwnerFromContext(t *testing.T) {
	srv, database := newTestServer(t)

	// Create a job request with an authenticated user in context
	body := `{"repo_url":"https://github.com/test/repo","branch":"main","prompt":"test","max_iterations":10}`
	req := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Simulate authenticated user via context
	user := &auth.User{ID: "oid-abc", Name: "Test User", Email: "test@example.com", Roles: []string{"User"}}
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))

	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var job models.Job
	require.NoError(t, json.NewDecoder(w.Body).Decode(&job))
	assert.Equal(t, "oid-abc", job.OwnerID)
	assert.Equal(t, "Test User", job.OwnerName)
}

func TestCreateJob_NoAuth_EmptyOwner(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"repo_url":"https://github.com/test/repo","branch":"main","prompt":"test","max_iterations":10}`
	req := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No user in context (auth mode none)

	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var job models.Job
	require.NoError(t, json.NewDecoder(w.Body).Decode(&job))
	assert.Equal(t, "", job.OwnerID)
	assert.Equal(t, "", job.OwnerName)
}
```

Add the necessary imports (`strings`, `auth`, `models`, `json`).

**Step 2: Run test to verify it fails**

Run: `go test -v -run "TestCreateJob_SetsOwner|TestCreateJob_NoAuth" ./internal/api/`
Expected: FAIL (or PASS if owner fields are already zero-valued — verify the actual owner setting)

**Step 3: Modify handleCreateJob to set ownership**

In `internal/api/jobs.go`, update `handleCreateJob`:

```go
import "github.com/ryan/ralph-o-matic/internal/auth"

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	// ... existing validation code ...

	job := models.NewJob(req.RepoURL, req.Branch, req.Prompt, req.MaxIterations)
	job.WorkingDir = workingDir
	job.Env = req.Env

	// Set ownership from authenticated user
	if user := auth.UserFromContext(r.Context()); user != nil {
		job.OwnerID = user.ID
		job.OwnerName = user.Name
	}

	// ... rest of existing handler ...
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run "TestCreateJob_SetsOwner|TestCreateJob_NoAuth" ./internal/api/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/jobs.go internal/api/jobs_test.go
git commit -m "feat(api): set job ownership from authenticated user context"
```

---

### Task 5: Add ownership-based access control to job endpoints

**Files:**
- Modify: `internal/api/jobs.go`
- Test: `internal/api/jobs_test.go`

**Step 1: Write the failing test**

Add to `internal/api/jobs_test.go`:

```go
func TestListJobs_UserSeesOnlyOwnJobs(t *testing.T) {
	srv, database := newTestServer(t)
	repo := db.NewJobRepo(database)

	// Create jobs with different owners
	job1 := models.NewJob("https://github.com/test/repo", "main", "test", 10)
	job1.OwnerID = "user-a"
	require.NoError(t, repo.Create(job1))

	job2 := models.NewJob("https://github.com/test/repo", "main", "test", 10)
	job2.OwnerID = "user-b"
	require.NoError(t, repo.Create(job2))

	// Request as user-a (User role)
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	user := &auth.User{ID: "user-a", Roles: []string{"User"}}
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Jobs  []*models.Job `json:"jobs"`
		Total int           `json:"total"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, "user-a", resp.Jobs[0].OwnerID)
}

func TestListJobs_AdminSeesAllJobs(t *testing.T) {
	srv, database := newTestServer(t)
	repo := db.NewJobRepo(database)

	job1 := models.NewJob("https://github.com/test/repo", "main", "test", 10)
	job1.OwnerID = "user-a"
	require.NoError(t, repo.Create(job1))

	job2 := models.NewJob("https://github.com/test/repo", "main", "test", 10)
	job2.OwnerID = "user-b"
	require.NoError(t, repo.Create(job2))

	// Request as admin
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	user := &auth.User{ID: "admin-user", Roles: []string{"Admin"}}
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Jobs  []*models.Job `json:"jobs"`
		Total int           `json:"total"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 2, resp.Total)
}

func TestGetJob_UserCanAccessOwnJob(t *testing.T) {
	srv, database := newTestServer(t)
	repo := db.NewJobRepo(database)

	job := models.NewJob("https://github.com/test/repo", "main", "test", 10)
	job.OwnerID = "user-a"
	require.NoError(t, repo.Create(job))

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/jobs/%d", job.ID), nil)
	user := &auth.User{ID: "user-a", Roles: []string{"User"}}
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetJob_UserCannotAccessOtherJob(t *testing.T) {
	srv, database := newTestServer(t)
	repo := db.NewJobRepo(database)

	job := models.NewJob("https://github.com/test/repo", "main", "test", 10)
	job.OwnerID = "user-b"
	require.NoError(t, repo.Create(job))

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/jobs/%d", job.ID), nil)
	user := &auth.User{ID: "user-a", Roles: []string{"User"}}
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestGetJob_PreAuthJob_AccessibleByAll(t *testing.T) {
	srv, database := newTestServer(t)
	repo := db.NewJobRepo(database)

	job := models.NewJob("https://github.com/test/repo", "main", "test", 10)
	// OwnerID is empty (pre-auth job)
	require.NoError(t, repo.Create(job))

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/jobs/%d", job.ID), nil)
	user := &auth.User{ID: "any-user", Roles: []string{"User"}}
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run "TestListJobs_User|TestListJobs_Admin|TestGetJob_User|TestGetJob_PreAuth" ./internal/api/`
Expected: FAIL — ownership filtering not implemented in handlers

**Step 3: Add ownership helper and update handlers**

Add a helper to `internal/api/jobs.go`:

```go
// canAccessJob checks if the authenticated user can access a job.
// Returns true when: auth is off (no user), user is admin, user owns the job,
// or the job has no owner (pre-auth).
func canAccessJob(r *http.Request, job *models.Job) bool {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		return true // auth mode none
	}
	if user.IsAdmin() {
		return true
	}
	if job.OwnerID == "" {
		return true // pre-auth job
	}
	return job.OwnerID == user.ID
}
```

Update `handleListJobs`:

```go
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	opts := db.ListOptions{}

	// Apply ownership filter for non-admin authenticated users
	if user := auth.UserFromContext(r.Context()); user != nil && !user.IsAdmin() {
		opts.OwnerID = user.ID
	}

	// ... rest of existing handler (status filter, pagination) ...
}
```

Update `handleGetJob`, `handleCancelJob`, `handlePauseJob`, `handleResumeJob`, `handleUpdateJob` to add ownership check:

```go
func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	// ... existing parse + fetch ...

	if !canAccessJob(r, job) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	writeJSON(w, http.StatusOK, job)
}
```

Apply the same pattern to `handleCancelJob`, `handlePauseJob`, `handleResumeJob`, and `handleUpdateJob`.

**Step 4: Run test to verify it passes**

Run: `go test -v -run "TestListJobs_|TestGetJob_" ./internal/api/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/jobs.go internal/api/jobs_test.go
git commit -m "feat(api): add job ownership filtering and access control"
```

---

### Task 6: Run full test suite

**Step 1: Run all tests**

Run: `go test -v -short -race ./...`
Expected: All PASS

**Step 2: Run linter**

Run: `make lint`
Expected: No lint errors

---

## Dependencies

- **Depends on:** Phase 1 (Config), Phase 2 (User context helpers), Phase 4 (Middleware sets user in context)
- **Blocks:** Phase 7 (Dashboard shows owner name)

## Reference Files

- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 287-310, "Job Ownership")
- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 138-151, "Role Enforcement" table)
- Existing migration: `internal/db/migrations/002_add_backend.sql` (pattern for ALTER TABLE)
- Existing job repo: `internal/db/jobs.go` (SQL patterns)
- Existing job test: `internal/db/jobs_test.go` (test patterns)
