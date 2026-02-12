# Dashboard Ownership Filtering Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the dashboard respect job ownership, so non-admin users only see their own jobs — matching the API's existing behavior.

**Architecture:** Move the `canAccessJob` helper from `internal/api` to `internal/auth` as an exported `CanAccessJob` function. Update the dashboard's `HandleIndex` to filter by `OwnerID` and `HandleJob` to check access. Add `HandleConfig` admin guard. The DB layer already supports `OwnerID` filtering via `ListOptions`.

**Tech Stack:** Go, `net/http`, `testify/assert`+`require`, in-memory SQLite via `newTestDB`

---

### Task 1: Move `canAccessJob` to `internal/auth` package

**Files:**
- Create: `internal/auth/access.go`
- Create: `internal/auth/access_test.go`
- Modify: `internal/api/jobs.go:320-332` (replace with call to new function)

**Step 1: Write the failing test**

Create `internal/auth/access_test.go`:

```go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCanAccessJob_NoUser_ReturnsTrue(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	assert.True(t, CanAccessJob(req, "owner-1"))
}

func TestCanAccessJob_Admin_ReturnsTrue(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(ContextWithUser(req.Context(), &User{
		ID: "admin-1", Roles: []string{"Admin"},
	}))
	assert.True(t, CanAccessJob(req, "other-owner"))
}

func TestCanAccessJob_OwnerMatch_ReturnsTrue(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(ContextWithUser(req.Context(), &User{
		ID: "user-a", Roles: []string{"User"},
	}))
	assert.True(t, CanAccessJob(req, "user-a"))
}

func TestCanAccessJob_OwnerMismatch_ReturnsFalse(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(ContextWithUser(req.Context(), &User{
		ID: "user-a", Roles: []string{"User"},
	}))
	assert.False(t, CanAccessJob(req, "user-b"))
}

func TestCanAccessJob_EmptyOwner_ReturnsTrue(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(ContextWithUser(req.Context(), &User{
		ID: "user-a", Roles: []string{"User"},
	}))
	assert.True(t, CanAccessJob(req, ""))
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestCanAccessJob ./internal/auth/`
Expected: FAIL — `CanAccessJob` not defined

**Step 3: Write implementation**

Create `internal/auth/access.go`:

```go
package auth

import "net/http"

// CanAccessJob reports whether the request's user is allowed to access a job
// with the given ownerID. Returns true when: auth is disabled (no user in
// context), user is admin, job has no owner (pre-auth job), or user owns the job.
func CanAccessJob(r *http.Request, jobOwnerID string) bool {
	user := UserFromContext(r.Context())
	if user == nil {
		return true // auth mode none
	}
	if user.IsAdmin() {
		return true
	}
	if jobOwnerID == "" {
		return true // pre-auth job
	}
	return jobOwnerID == user.ID
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestCanAccessJob ./internal/auth/`
Expected: PASS

**Step 5: Update API to use the new function**

In `internal/api/jobs.go`, replace the unexported `canAccessJob` function (lines 317-332) with a call to `auth.CanAccessJob`:

Replace:
```go
// canAccessJob checks whether the request's user is allowed to access the given job.
// Returns true when: auth is disabled (no user in context), user is admin,
// job has no owner (pre-auth job), or user owns the job.
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

With:
```go
// canAccessJob checks whether the request's user is allowed to access the given job.
func canAccessJob(r *http.Request, job *models.Job) bool {
	return auth.CanAccessJob(r, job.OwnerID)
}
```

**Step 6: Run existing API tests to verify no regression**

Run: `go test -v -run "TestGetJob_|TestListJobs_" ./internal/api/`
Expected: All PASS (existing ownership tests still work)

**Step 7: Commit**

```bash
git add internal/auth/access.go internal/auth/access_test.go internal/api/jobs.go
git commit -m "refactor: extract CanAccessJob to auth package for reuse by dashboard"
```

---

### Task 2: Add ownership filtering to dashboard index

**Files:**
- Modify: `internal/dashboard/dashboard.go:102-123` (HandleIndex)
- Modify: `internal/dashboard/dashboard_test.go` (add auth tests)

**Step 1: Write the failing tests**

Add to `internal/dashboard/dashboard_test.go`:

```go
func TestDashboard_Index_UserSeesOnlyOwnJobs(t *testing.T) {
	d, _ := newTestDashboard(t)
	database := d.db

	// Create jobs for two different owners
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

func TestDashboard_Index_NoAuth_SeesAllJobs(t *testing.T) {
	d, _ := newTestDashboard(t)
	database := d.db

	createDashboardJob(t, database, "user-a", "Alice")
	createDashboardJob(t, database, "user-b", "Bob")

	req := httptest.NewRequest("GET", "/", nil)
	// No auth context
	w := httptest.NewRecorder()

	d.HandleIndex(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Alice")
	assert.Contains(t, body, "Bob")
}
```

Helper (add near top of test file):

```go
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
```

Note: Will need `"github.com/ryan/ralph-o-matic/internal/auth"` import added to the test file.

**Step 2: Run tests to verify they fail**

Run: `go test -v -run "TestDashboard_Index_User|TestDashboard_Index_Admin|TestDashboard_Index_NoAuth" ./internal/dashboard/`
Expected: FAIL — `TestDashboard_Index_UserSeesOnlyOwnJobs` sees Bob's jobs (no filtering yet)

**Step 3: Implement ownership filtering in HandleIndex**

In `internal/dashboard/dashboard.go`, update `HandleIndex` to extract user and set `OwnerID`:

```go
func (d *Dashboard) HandleIndex(w http.ResponseWriter, r *http.Request) {
	jobRepo := db.NewJobRepo(d.db)

	// Filter by owner for non-admin users
	var ownerID string
	if user := auth.UserFromContext(r.Context()); user != nil && !user.IsAdmin() {
		ownerID = user.ID
	}

	running, _, _ := jobRepo.List(db.ListOptions{Statuses: []models.JobStatus{models.StatusRunning}, OwnerID: ownerID})
	paused, _, _ := jobRepo.List(db.ListOptions{Statuses: []models.JobStatus{models.StatusPaused}, OwnerID: ownerID})
	queued, _, _ := jobRepo.List(db.ListOptions{Statuses: []models.JobStatus{models.StatusQueued}, OwnerID: ownerID})
	completed, _, _ := jobRepo.List(db.ListOptions{
		Statuses: []models.JobStatus{models.StatusCompleted, models.StatusFailed},
		OwnerID:  ownerID,
		Limit:    10,
	})

	data := IndexData{
		QueueSize: len(queued),
		AuthUser:  auth.UserFromContext(r.Context()),
		Running:   running,
		Paused:    paused,
		Queued:    queued,
		Completed: completed,
	}

	d.render(w, d.dashboardTmpl, data)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run "TestDashboard_Index" ./internal/dashboard/`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/dashboard/dashboard.go internal/dashboard/dashboard_test.go
git commit -m "fix: filter dashboard index by job owner for non-admin users"
```

---

### Task 3: Add authorization check to dashboard job detail

**Files:**
- Modify: `internal/dashboard/dashboard.go:134-152` (HandleJob)
- Modify: `internal/dashboard/dashboard_test.go` (add auth tests)

**Step 1: Write the failing tests**

Add to `internal/dashboard/dashboard_test.go`:

```go
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
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run "TestDashboard_Job_User" ./internal/dashboard/`
Expected: FAIL — `TestDashboard_Job_UserCannotAccessOtherJob` returns 200 instead of 403

**Step 3: Add authorization check to HandleJob**

In `internal/dashboard/dashboard.go`, update `HandleJob`:

```go
func (d *Dashboard) HandleJob(w http.ResponseWriter, r *http.Request, jobID int64) {
	job, err := d.queue.Get(jobID)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	if !auth.CanAccessJob(r, job.OwnerID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	logRepo := db.NewLogRepo(d.db)
	logs, _ := logRepo.GetForJob(jobID)

	data := JobData{
		QueueSize: d.queue.Size(),
		AuthUser:  auth.UserFromContext(r.Context()),
		Job:       job,
		Logs:      logs,
	}

	d.render(w, d.jobTmpl, data)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run "TestDashboard_Job" ./internal/dashboard/`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/dashboard/dashboard.go internal/dashboard/dashboard_test.go
git commit -m "fix: add authorization check to dashboard job detail page"
```

---

### Task 4: Add admin guard to config page

**Files:**
- Modify: `internal/api/server.go:100` (wrap route)
- Modify: `internal/dashboard/dashboard_test.go` (add test)

The config page shows server settings — this should be admin-only (matching the API's `PATCH /api/config` which uses `RequireRole("Admin", ...)`).

**Step 1: Write the failing test**

Add to `internal/dashboard/dashboard_test.go`:

```go
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
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run "TestDashboard_Config" ./internal/dashboard/`
Expected: FAIL — `TestDashboard_Config_NonAdminGetsForbidden` returns 200

**Step 3: Add admin guard to HandleConfig**

In `internal/dashboard/dashboard.go`, add a guard at the top of `HandleConfig`:

```go
func (d *Dashboard) HandleConfig(w http.ResponseWriter, r *http.Request) {
	// Config is admin-only when auth is enabled
	if user := auth.UserFromContext(r.Context()); user != nil && !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// ... rest unchanged ...
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run "TestDashboard_Config" ./internal/dashboard/`
Expected: All PASS

**Step 5: Run full test suite**

Run: `go test -v -short -race ./...`
Expected: All PASS

**Step 6: Run linter**

Run: `make lint`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/dashboard/dashboard.go internal/dashboard/dashboard_test.go
git commit -m "fix: restrict dashboard config page to admin users"
```
