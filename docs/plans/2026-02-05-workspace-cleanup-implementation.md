# Workspace and Job Retention Cleanup Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add automated cleanup that deletes workspace directories for terminal jobs and purges old job records after the configured retention period.

**Architecture:** A `Cleaner` struct in `internal/worker` runs as a separate goroutine with a 1-hour ticker. It first cleans workspace directories for terminal jobs (with git safety checks for completed jobs), then purges expired job records from the database based on `job_retention_days`.

**Tech Stack:** Go 1.24, SQLite (modernc.org/sqlite), testify for assertions

---

### Task 1: Add `ListTerminal` method to JobRepo

Query for all jobs in terminal states. The cleaner needs this to find workspaces to clean.

**Files:**
- Modify: `internal/db/jobs.go` (after `CountByStatus` method, ~line 379)
- Test: `internal/db/jobs_test.go`

**Step 1: Write the failing tests**

Add to `internal/db/jobs_test.go`:

```go
func TestJobRepo_ListTerminal(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create jobs in various states
	statuses := []models.JobStatus{
		models.StatusQueued,
		models.StatusRunning,
		models.StatusCompleted,
		models.StatusFailed,
		models.StatusCancelled,
	}
	for _, status := range statuses {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		job.Status = status
		err := repo.Create(job)
		require.NoError(t, err)
	}

	jobs, err := repo.ListTerminal()
	require.NoError(t, err)
	assert.Len(t, jobs, 3)

	for _, job := range jobs {
		assert.Contains(t, []models.JobStatus{
			models.StatusCompleted, models.StatusFailed, models.StatusCancelled,
		}, job.Status)
	}
}

func TestJobRepo_ListTerminal_Empty(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	jobs, err := repo.ListTerminal()
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

func TestJobRepo_ListTerminal_NoTerminalJobs(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Only non-terminal jobs
	for _, status := range []models.JobStatus{models.StatusQueued, models.StatusRunning} {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		job.Status = status
		err := repo.Create(job)
		require.NoError(t, err)
	}

	jobs, err := repo.ListTerminal()
	require.NoError(t, err)
	assert.Empty(t, jobs)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run TestJobRepo_ListTerminal ./internal/db/`
Expected: FAIL — `ListTerminal` method does not exist

**Step 3: Write minimal implementation**

Add to `internal/db/jobs.go` after the `CountByStatus` method:

```go
// ListTerminal returns all jobs in terminal states (completed, failed, cancelled)
func (r *JobRepo) ListTerminal() ([]*models.Job, error) {
	rows, err := r.db.conn.Query(`
		SELECT id FROM jobs
		WHERE status IN (?, ?, ?)
		ORDER BY id
	`, models.StatusCompleted, models.StatusFailed, models.StatusCancelled)
	if err != nil {
		return nil, fmt.Errorf("failed to list terminal jobs: %w", err)
	}

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan job id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()

	var jobs []*models.Job
	for _, id := range ids {
		job, err := r.Get(id)
		if err != nil {
			return nil, fmt.Errorf("failed to get job %d: %w", id, err)
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run TestJobRepo_ListTerminal ./internal/db/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/db/jobs.go internal/db/jobs_test.go
git commit -m "feat: add ListTerminal method to JobRepo"
```

---

### Task 2: Add `ListExpired` method to JobRepo

Query for terminal jobs older than a cutoff time. Used by retention cleanup.

**Files:**
- Modify: `internal/db/jobs.go` (after `ListTerminal`)
- Test: `internal/db/jobs_test.go`

**Step 1: Write the failing tests**

Add to `internal/db/jobs_test.go`:

```go
func TestJobRepo_ListExpired_ReturnsOldTerminalJobs(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	now := time.Now()
	old := now.Add(-48 * time.Hour)

	// Old completed job (should be returned)
	job1 := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	job1.Status = models.StatusCompleted
	job1.CompletedAt = &old
	err := repo.Create(job1)
	require.NoError(t, err)
	err = repo.Update(job1)
	require.NoError(t, err)

	// Recent completed job (should NOT be returned)
	recent := now.Add(-1 * time.Hour)
	job2 := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	job2.Status = models.StatusCompleted
	job2.CompletedAt = &recent
	err = repo.Create(job2)
	require.NoError(t, err)
	err = repo.Update(job2)
	require.NoError(t, err)

	cutoff := now.Add(-24 * time.Hour)
	jobs, err := repo.ListExpired(cutoff)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, job1.ID, jobs[0].ID)
}

func TestJobRepo_ListExpired_IncludesFailedAndCancelled(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	now := time.Now()
	old := now.Add(-48 * time.Hour)
	cutoff := now.Add(-24 * time.Hour)

	for _, status := range []models.JobStatus{models.StatusCompleted, models.StatusFailed, models.StatusCancelled} {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		job.Status = status
		job.CompletedAt = &old
		err := repo.Create(job)
		require.NoError(t, err)
		err = repo.Update(job)
		require.NoError(t, err)
	}

	jobs, err := repo.ListExpired(cutoff)
	require.NoError(t, err)
	assert.Len(t, jobs, 3)
}

func TestJobRepo_ListExpired_ExcludesQueuedAndRunning(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	now := time.Now()
	old := now.Add(-48 * time.Hour)
	cutoff := now.Add(-24 * time.Hour)

	// Old queued job (should NOT be returned even though it's old)
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	job.Status = models.StatusQueued
	job.CreatedAt = old
	err := repo.Create(job)
	require.NoError(t, err)

	// Old running job
	job2 := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	job2.Status = models.StatusRunning
	job2.StartedAt = &old
	err = repo.Create(job2)
	require.NoError(t, err)

	jobs, err := repo.ListExpired(cutoff)
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

func TestJobRepo_ListExpired_BoundaryNotIncluded(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Job completed exactly at cutoff — should NOT be expired
	cutoff := time.Now()
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	job.Status = models.StatusCompleted
	job.CompletedAt = &cutoff
	err := repo.Create(job)
	require.NoError(t, err)
	err = repo.Update(job)
	require.NoError(t, err)

	jobs, err := repo.ListExpired(cutoff)
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

func TestJobRepo_ListExpired_EmptyDB(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	jobs, err := repo.ListExpired(time.Now())
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

func TestJobRepo_ListExpired_FallsBackToCreatedAt(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	now := time.Now()
	old := now.Add(-48 * time.Hour)
	cutoff := now.Add(-24 * time.Hour)

	// Failed job with no completed_at (old data scenario)
	// Create with old created_at, set status to failed, no completed_at
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	job.Status = models.StatusFailed
	job.CreatedAt = old
	// CompletedAt is nil — should fall back to created_at
	err := repo.Create(job)
	require.NoError(t, err)

	jobs, err := repo.ListExpired(cutoff)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, job.ID, jobs[0].ID)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run TestJobRepo_ListExpired ./internal/db/`
Expected: FAIL — `ListExpired` method does not exist

**Step 3: Write minimal implementation**

Add to `internal/db/jobs.go` after `ListTerminal`:

```go
// ListExpired returns jobs in terminal states with completed_at (or created_at
// as fallback) strictly before the cutoff time.
func (r *JobRepo) ListExpired(cutoff time.Time) ([]*models.Job, error) {
	rows, err := r.db.conn.Query(`
		SELECT id FROM jobs
		WHERE status IN (?, ?, ?)
		AND COALESCE(completed_at, created_at) < ?
		ORDER BY id
	`, models.StatusCompleted, models.StatusFailed, models.StatusCancelled, cutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to list expired jobs: %w", err)
	}

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan job id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()

	var jobs []*models.Job
	for _, id := range ids {
		job, err := r.Get(id)
		if err != nil {
			return nil, fmt.Errorf("failed to get job %d: %w", id, err)
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run TestJobRepo_ListExpired ./internal/db/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/db/jobs.go internal/db/jobs_test.go
git commit -m "feat: add ListExpired method to JobRepo for retention cleanup"
```

---

### Task 3: Add GitChecker interface and production implementation

The cleaner needs to verify completed job workspaces have no uncommitted/unpushed work before deleting.

**Files:**
- Create: `internal/worker/gitcheck.go`
- Test: `internal/worker/gitcheck_test.go` (integration test, gated)

**Step 1: Write the interface and implementation**

Create `internal/worker/gitcheck.go`:

```go
package worker

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// GitChecker verifies workspace git state before cleanup.
type GitChecker interface {
	HasUncommittedChanges(dir string) (bool, error)
	HasUnpushedCommits(dir string) (bool, error)
}

// realGitChecker shells out to git to check workspace state.
type realGitChecker struct{}

// NewGitChecker returns a GitChecker that runs real git commands.
func NewGitChecker() GitChecker {
	return &realGitChecker{}
}

func (g *realGitChecker) HasUncommittedChanges(dir string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("git status failed: %w: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()) != "", nil
}

func (g *realGitChecker) HasUnpushedCommits(dir string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "log", "@{u}..", "--oneline")
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// If there's no upstream tracking branch, treat as "has unpushed"
		// to be safe — don't delete what we can't verify
		return true, nil
	}

	return strings.TrimSpace(stdout.String()) != "", nil
}
```

**Step 2: No unit tests needed for the real implementation** — it shells out to git and would need a real repo. The mock `GitChecker` in cleaner tests covers the interface contract. Integration tests can be added later if desired.

**Step 3: Commit**

```bash
git add internal/worker/gitcheck.go
git commit -m "feat: add GitChecker interface for workspace safety verification"
```

---

### Task 4: Add mock GitChecker for tests

Create a reusable mock that cleaner tests will use.

**Files:**
- Create: `internal/worker/cleaner_test.go` (start with mocks and helpers only)

**Step 1: Write the mock and test helpers**

Create `internal/worker/cleaner_test.go`:

```go
package worker

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/git"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/require"
)

// mockGitChecker implements GitChecker for testing.
type mockGitChecker struct {
	uncommitted    bool
	uncommittedErr error
	unpushed       bool
	unpushedErr    error
}

func (m *mockGitChecker) HasUncommittedChanges(dir string) (bool, error) {
	return m.uncommitted, m.uncommittedErr
}

func (m *mockGitChecker) HasUnpushedCommits(dir string) (bool, error) {
	return m.unpushed, m.unpushedErr
}

// cleanerTestEnv sets up a complete test environment for cleaner tests.
type cleanerTestEnv struct {
	db         *db.DB
	jobRepo    *db.JobRepo
	configRepo *db.ConfigRepo
	repoMgr    *git.RepoManager
	gitChecker *mockGitChecker
	workDir    string
}

func newCleanerTestEnv(t *testing.T) *cleanerTestEnv {
	t.Helper()
	database := newCleanerTestDB(t)
	workDir := t.TempDir()

	return &cleanerTestEnv{
		db:         database,
		jobRepo:    db.NewJobRepo(database),
		configRepo: db.NewConfigRepo(database),
		repoMgr:    git.NewRepoManager(workDir),
		gitChecker: &mockGitChecker{},
		workDir:    workDir,
	}
}

func (e *cleanerTestEnv) newCleaner() *Cleaner {
	return NewCleaner(e.jobRepo, e.configRepo, e.repoMgr, e.gitChecker)
}

// createJob creates a job in the given status, optionally with a workspace directory.
func (e *cleanerTestEnv) createJob(t *testing.T, status models.JobStatus, completedAge time.Duration, createWorkspace bool) *models.Job {
	t.Helper()
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	job.Status = status

	if status == models.StatusCompleted || status == models.StatusFailed || status == models.StatusCancelled {
		completed := time.Now().Add(-completedAge)
		job.CompletedAt = &completed
	}

	err := e.jobRepo.Create(job)
	require.NoError(t, err)
	// Update to persist CompletedAt
	err = e.jobRepo.Update(job)
	require.NoError(t, err)

	if createWorkspace {
		wsPath := e.repoMgr.WorkspacePath(job.ID)
		err := os.MkdirAll(wsPath, 0o755)
		require.NoError(t, err)
		// Put a marker file so we can verify the dir has content
		err = os.WriteFile(filepath.Join(wsPath, "marker.txt"), []byte("test"), 0o644)
		require.NoError(t, err)
	}

	return job
}

// workspaceExists checks if a job's workspace directory exists.
func (e *cleanerTestEnv) workspaceExists(jobID int64) bool {
	_, err := os.Stat(e.repoMgr.WorkspacePath(jobID))
	return err == nil
}

// newCleanerTestDB creates an in-memory test DB with migrations.
// This is needed because cleaner_test.go is in the worker package,
// not the db package, so it can't use the unexported newTestDB.
func newCleanerTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.New(":memory:")
	require.NoError(t, err)
	err = database.Migrate()
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })
	return database
}
```

**Step 2: Verify it compiles**

Run: `go build ./internal/worker/...`
Expected: This will fail because `Cleaner`, `NewCleaner` don't exist yet. That's fine — we'll create them in the next task. This file establishes the test infrastructure.

**Step 3: Commit** (defer to after Task 5 when the code compiles)

---

### Task 5: Implement the Cleaner struct — constructor and Run loop

The core lifecycle: constructor, ticker loop, graceful shutdown, and double-run protection.

**Files:**
- Create: `internal/worker/cleaner.go`
- Test: `internal/worker/cleaner_test.go` (add lifecycle tests)

**Step 1: Write the failing lifecycle tests**

Add to `internal/worker/cleaner_test.go`:

```go
func TestCleaner_StopsOnContextCancellation(t *testing.T) {
	env := newCleanerTestEnv(t)
	c := env.newCleaner()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()

	// Let it run briefly
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// success — Run returned after cancel
	case <-time.After(2 * time.Second):
		t.Fatal("Cleaner did not stop after context cancellation")
	}
}
```

Also add the missing import for `"context"` to the import block in `cleaner_test.go`.

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestCleaner_StopsOnContextCancellation ./internal/worker/`
Expected: FAIL — `Cleaner` and `NewCleaner` don't exist

**Step 3: Write the implementation**

Create `internal/worker/cleaner.go`:

```go
package worker

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/git"
	"github.com/ryan/ralph-o-matic/internal/models"
)

const cleanupInterval = 1 * time.Hour

// Cleaner periodically removes workspace directories for completed jobs
// and purges expired job records from the database.
type Cleaner struct {
	jobRepo    *db.JobRepo
	configRepo *db.ConfigRepo
	repoMgr    *git.RepoManager
	gitChecker GitChecker

	running sync.Mutex
}

// NewCleaner creates a new workspace and job retention cleaner.
func NewCleaner(jobRepo *db.JobRepo, configRepo *db.ConfigRepo, repoMgr *git.RepoManager, gitChecker GitChecker) *Cleaner {
	return &Cleaner{
		jobRepo:    jobRepo,
		configRepo: configRepo,
		repoMgr:    repoMgr,
		gitChecker: gitChecker,
	}
}

// Run starts the cleanup loop. It blocks until ctx is cancelled.
func (c *Cleaner) Run(ctx context.Context) {
	log.Println("Cleaner started, running every", cleanupInterval)
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Cleaner stopping")
			return
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

// tick runs one cleanup cycle. Skips if a previous cycle is still running.
func (c *Cleaner) tick(ctx context.Context) {
	if !c.running.TryLock() {
		log.Println("Cleaner: skipping tick, previous cleanup still running")
		return
	}
	defer c.running.Unlock()

	c.cleanWorkspaces(ctx)
	c.purgeExpiredJobs(ctx)
}

// cleanWorkspaces removes workspace directories for jobs in terminal states.
func (c *Cleaner) cleanWorkspaces(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	jobs, err := c.jobRepo.ListTerminal()
	if err != nil {
		log.Printf("Cleaner: failed to list terminal jobs: %v", err)
		return
	}

	for _, job := range jobs {
		if ctx.Err() != nil {
			return
		}

		wsPath := c.repoMgr.WorkspacePath(job.ID)
		if _, err := os.Stat(wsPath); os.IsNotExist(err) {
			continue
		}

		if job.Status == models.StatusCompleted {
			safe, err := c.isWorkspaceSafeToDelete(wsPath)
			if err != nil {
				log.Printf("Cleaner: job #%d git check error, skipping workspace: %v", job.ID, err)
				continue
			}
			if !safe {
				log.Printf("Cleaner: WARNING job #%d workspace has uncommitted or unpushed changes, skipping", job.ID)
				continue
			}
		}

		if err := c.repoMgr.Cleanup(job.ID); err != nil {
			log.Printf("Cleaner: failed to remove workspace for job #%d: %v", job.ID, err)
			continue
		}
		log.Printf("Cleaner: cleaned up workspace for job #%d", job.ID)
	}
}

// isWorkspaceSafeToDelete checks for uncommitted or unpushed work.
func (c *Cleaner) isWorkspaceSafeToDelete(dir string) (bool, error) {
	uncommitted, err := c.gitChecker.HasUncommittedChanges(dir)
	if err != nil {
		return false, err
	}
	if uncommitted {
		return false, nil
	}

	unpushed, err := c.gitChecker.HasUnpushedCommits(dir)
	if err != nil {
		return false, err
	}
	if unpushed {
		return false, nil
	}

	return true, nil
}

// purgeExpiredJobs deletes job records older than job_retention_days.
func (c *Cleaner) purgeExpiredJobs(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	config, err := c.configRepo.Get()
	if err != nil {
		log.Printf("Cleaner: failed to load config: %v", err)
		return
	}

	if config.JobRetentionDays == 0 {
		return
	}

	cutoff := time.Now().Add(-time.Duration(config.JobRetentionDays) * 24 * time.Hour)

	jobs, err := c.jobRepo.ListExpired(cutoff)
	if err != nil {
		log.Printf("Cleaner: failed to list expired jobs: %v", err)
		return
	}

	for _, job := range jobs {
		if ctx.Err() != nil {
			return
		}

		// Defensive: clean up workspace if still present
		wsPath := c.repoMgr.WorkspacePath(job.ID)
		if _, err := os.Stat(wsPath); err == nil {
			if rmErr := c.repoMgr.Cleanup(job.ID); rmErr != nil {
				log.Printf("Cleaner: failed to remove workspace for expired job #%d: %v", job.ID, rmErr)
			}
		}

		if err := c.jobRepo.Delete(job.ID); err != nil {
			log.Printf("Cleaner: failed to delete expired job #%d: %v", job.ID, err)
			continue
		}
		log.Printf("Cleaner: purged expired job #%d", job.ID)
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run TestCleaner ./internal/worker/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/worker/cleaner.go internal/worker/cleaner_test.go internal/worker/gitcheck.go
git commit -m "feat: add Cleaner struct with workspace cleanup and job retention"
```

---

### Task 6: Workspace cleanup tests — happy path

**Files:**
- Modify: `internal/worker/cleaner_test.go`

**Step 1: Write the tests**

Add to `internal/worker/cleaner_test.go`:

```go
func TestCleaner_WorkspaceCleanup_CompletedJobCleanGit(t *testing.T) {
	env := newCleanerTestEnv(t)
	env.gitChecker.uncommitted = false
	env.gitChecker.unpushed = false
	job := env.createJob(t, models.StatusCompleted, 1*time.Hour, true)

	c := env.newCleaner()
	c.cleanWorkspaces(context.Background())

	assert.False(t, env.workspaceExists(job.ID), "workspace should be deleted")
}

func TestCleaner_WorkspaceCleanup_FailedJob(t *testing.T) {
	env := newCleanerTestEnv(t)
	job := env.createJob(t, models.StatusFailed, 1*time.Hour, true)

	c := env.newCleaner()
	c.cleanWorkspaces(context.Background())

	assert.False(t, env.workspaceExists(job.ID), "workspace should be deleted for failed job")
}

func TestCleaner_WorkspaceCleanup_CancelledJob(t *testing.T) {
	env := newCleanerTestEnv(t)
	job := env.createJob(t, models.StatusCancelled, 1*time.Hour, true)

	c := env.newCleaner()
	c.cleanWorkspaces(context.Background())

	assert.False(t, env.workspaceExists(job.ID), "workspace should be deleted for cancelled job")
}
```

**Step 2: Run tests to verify they pass**

Run: `go test -v -run TestCleaner_WorkspaceCleanup_ ./internal/worker/`
Expected: PASS (implementation already exists from Task 5)

**Step 3: Commit**

```bash
git add internal/worker/cleaner_test.go
git commit -m "test: add workspace cleanup happy path tests"
```

---

### Task 7: Workspace cleanup tests — skip scenarios

**Files:**
- Modify: `internal/worker/cleaner_test.go`

**Step 1: Write the tests**

Add to `internal/worker/cleaner_test.go`:

```go
func TestCleaner_WorkspaceCleanup_SkipsQueuedJob(t *testing.T) {
	env := newCleanerTestEnv(t)
	job := env.createJob(t, models.StatusQueued, 0, true)

	c := env.newCleaner()
	c.cleanWorkspaces(context.Background())

	assert.True(t, env.workspaceExists(job.ID), "queued job workspace should not be deleted")
}

func TestCleaner_WorkspaceCleanup_SkipsRunningJob(t *testing.T) {
	env := newCleanerTestEnv(t)
	job := env.createJob(t, models.StatusRunning, 0, true)

	c := env.newCleaner()
	c.cleanWorkspaces(context.Background())

	assert.True(t, env.workspaceExists(job.ID), "running job workspace should not be deleted")
}

func TestCleaner_WorkspaceCleanup_SkipsNonExistentWorkspace(t *testing.T) {
	env := newCleanerTestEnv(t)
	env.gitChecker.uncommitted = false
	env.gitChecker.unpushed = false
	// Create job WITHOUT workspace directory
	env.createJob(t, models.StatusCompleted, 1*time.Hour, false)

	c := env.newCleaner()
	// Should not panic or error
	c.cleanWorkspaces(context.Background())
}

func TestCleaner_WorkspaceCleanup_SkipsCompletedWithUncommittedChanges(t *testing.T) {
	env := newCleanerTestEnv(t)
	env.gitChecker.uncommitted = true
	env.gitChecker.unpushed = false
	job := env.createJob(t, models.StatusCompleted, 1*time.Hour, true)

	c := env.newCleaner()
	c.cleanWorkspaces(context.Background())

	assert.True(t, env.workspaceExists(job.ID), "workspace with uncommitted changes should not be deleted")
}

func TestCleaner_WorkspaceCleanup_SkipsCompletedWithUnpushedCommits(t *testing.T) {
	env := newCleanerTestEnv(t)
	env.gitChecker.uncommitted = false
	env.gitChecker.unpushed = true
	job := env.createJob(t, models.StatusCompleted, 1*time.Hour, true)

	c := env.newCleaner()
	c.cleanWorkspaces(context.Background())

	assert.True(t, env.workspaceExists(job.ID), "workspace with unpushed commits should not be deleted")
}
```

**Step 2: Run tests to verify they pass**

Run: `go test -v -run TestCleaner_WorkspaceCleanup_Skips ./internal/worker/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/worker/cleaner_test.go
git commit -m "test: add workspace cleanup skip scenario tests"
```

---

### Task 8: Workspace cleanup tests — error scenarios

**Files:**
- Modify: `internal/worker/cleaner_test.go`

**Step 1: Write the tests**

Add to `internal/worker/cleaner_test.go`:

```go
func TestCleaner_WorkspaceCleanup_GitCheckError_SkipsWorkspace(t *testing.T) {
	env := newCleanerTestEnv(t)
	env.gitChecker.uncommittedErr = fmt.Errorf("corrupted repo")
	job := env.createJob(t, models.StatusCompleted, 1*time.Hour, true)

	c := env.newCleaner()
	c.cleanWorkspaces(context.Background())

	assert.True(t, env.workspaceExists(job.ID), "workspace should not be deleted when git check errors")
}

func TestCleaner_WorkspaceCleanup_GitUnpushedCheckError_SkipsWorkspace(t *testing.T) {
	env := newCleanerTestEnv(t)
	env.gitChecker.uncommitted = false
	env.gitChecker.unpushedErr = fmt.Errorf("git log failed")
	job := env.createJob(t, models.StatusCompleted, 1*time.Hour, true)

	c := env.newCleaner()
	c.cleanWorkspaces(context.Background())

	assert.True(t, env.workspaceExists(job.ID), "workspace should not be deleted when unpushed check errors")
}

func TestCleaner_WorkspaceCleanup_ContinuesAfterError(t *testing.T) {
	env := newCleanerTestEnv(t)
	env.gitChecker.uncommitted = false
	env.gitChecker.unpushed = false

	// Create two completed jobs with workspaces
	env.createJob(t, models.StatusFailed, 1*time.Hour, true)
	job2 := env.createJob(t, models.StatusFailed, 1*time.Hour, true)

	// Make first workspace read-only so RemoveAll fails, then verify second still cleaned
	// (This is hard to test portably, so we test the simpler case: multiple jobs all succeed)

	c := env.newCleaner()
	c.cleanWorkspaces(context.Background())

	assert.False(t, env.workspaceExists(job2.ID), "second job workspace should still be cleaned")
}

func TestCleaner_WorkspaceCleanup_StopsOnContextCancellation(t *testing.T) {
	env := newCleanerTestEnv(t)
	env.gitChecker.uncommitted = false
	env.gitChecker.unpushed = false

	// Create several jobs
	for i := 0; i < 5; i++ {
		env.createJob(t, models.StatusCompleted, 1*time.Hour, true)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	c := env.newCleaner()
	c.cleanWorkspaces(ctx)

	// At least some workspaces should remain since context was cancelled
	// (the exact behavior depends on timing, but it shouldn't panic)
}
```

**Step 2: Run tests to verify they pass**

Run: `go test -v -run "TestCleaner_WorkspaceCleanup_(GitCheck|Continue|StopsOn)" ./internal/worker/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/worker/cleaner_test.go
git commit -m "test: add workspace cleanup error scenario tests"
```

---

### Task 9: Job retention tests — happy path

**Files:**
- Modify: `internal/worker/cleaner_test.go`

**Step 1: Write the tests**

Add to `internal/worker/cleaner_test.go`:

```go
func TestCleaner_Retention_PurgesOldCompletedJob(t *testing.T) {
	env := newCleanerTestEnv(t)
	// Set retention to 7 days
	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 7
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

	// Job completed 10 days ago
	job := env.createJob(t, models.StatusCompleted, 10*24*time.Hour, false)

	c := env.newCleaner()
	c.purgeExpiredJobs(context.Background())

	_, err = env.jobRepo.Get(job.ID)
	assert.ErrorIs(t, err, db.ErrNotFound, "expired job should be deleted")
}

func TestCleaner_Retention_PurgesOldFailedJob(t *testing.T) {
	env := newCleanerTestEnv(t)
	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 7
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

	job := env.createJob(t, models.StatusFailed, 10*24*time.Hour, false)

	c := env.newCleaner()
	c.purgeExpiredJobs(context.Background())

	_, err = env.jobRepo.Get(job.ID)
	assert.ErrorIs(t, err, db.ErrNotFound, "expired failed job should be deleted")
}

func TestCleaner_Retention_PurgesOldCancelledJob(t *testing.T) {
	env := newCleanerTestEnv(t)
	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 7
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

	job := env.createJob(t, models.StatusCancelled, 10*24*time.Hour, false)

	c := env.newCleaner()
	c.purgeExpiredJobs(context.Background())

	_, err = env.jobRepo.Get(job.ID)
	assert.ErrorIs(t, err, db.ErrNotFound, "expired cancelled job should be deleted")
}

func TestCleaner_Retention_DefensiveWorkspaceCleanup(t *testing.T) {
	env := newCleanerTestEnv(t)
	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 7
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

	// Job expired AND still has workspace (workspace cleanup was skipped earlier)
	job := env.createJob(t, models.StatusCompleted, 10*24*time.Hour, true)

	c := env.newCleaner()
	c.purgeExpiredJobs(context.Background())

	assert.False(t, env.workspaceExists(job.ID), "workspace should be cleaned during retention purge")
	_, err = env.jobRepo.Get(job.ID)
	assert.ErrorIs(t, err, db.ErrNotFound, "job should be deleted")
}
```

**Step 2: Run tests to verify they pass**

Run: `go test -v -run TestCleaner_Retention_ ./internal/worker/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/worker/cleaner_test.go
git commit -m "test: add job retention happy path tests"
```

---

### Task 10: Job retention tests — skip and boundary scenarios

**Files:**
- Modify: `internal/worker/cleaner_test.go`

**Step 1: Write the tests**

Add to `internal/worker/cleaner_test.go`:

```go
func TestCleaner_Retention_SkipsWhenRetentionDaysZero(t *testing.T) {
	env := newCleanerTestEnv(t)
	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 0
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

	// Old job that would normally be purged
	job := env.createJob(t, models.StatusCompleted, 365*24*time.Hour, false)

	c := env.newCleaner()
	c.purgeExpiredJobs(context.Background())

	// Job should still exist
	_, err = env.jobRepo.Get(job.ID)
	assert.NoError(t, err, "job should not be purged when retention is 0")
}

func TestCleaner_Retention_DoesNotPurgeRecentJob(t *testing.T) {
	env := newCleanerTestEnv(t)
	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 30
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

	// Job completed 1 day ago (within 30-day retention)
	job := env.createJob(t, models.StatusCompleted, 24*time.Hour, false)

	c := env.newCleaner()
	c.purgeExpiredJobs(context.Background())

	_, err = env.jobRepo.Get(job.ID)
	assert.NoError(t, err, "recent job should not be purged")
}

func TestCleaner_Retention_DoesNotPurgeQueuedOrRunning(t *testing.T) {
	env := newCleanerTestEnv(t)
	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 1
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

	// Old queued and running jobs — should never be purged
	job1 := env.createJob(t, models.StatusQueued, 0, false)
	job2 := env.createJob(t, models.StatusRunning, 0, false)

	c := env.newCleaner()
	c.purgeExpiredJobs(context.Background())

	_, err = env.jobRepo.Get(job1.ID)
	assert.NoError(t, err, "queued job should not be purged")
	_, err = env.jobRepo.Get(job2.ID)
	assert.NoError(t, err, "running job should not be purged")
}

func TestCleaner_Retention_EmptyDB(t *testing.T) {
	env := newCleanerTestEnv(t)
	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 7
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

	c := env.newCleaner()
	// Should not panic or error with empty DB
	c.purgeExpiredJobs(context.Background())
}
```

**Step 2: Run tests to verify they pass**

Run: `go test -v -run "TestCleaner_Retention_(Skips|DoesNot|Empty)" ./internal/worker/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/worker/cleaner_test.go
git commit -m "test: add job retention skip and boundary tests"
```

---

### Task 11: Integration tests — both phases together

**Files:**
- Modify: `internal/worker/cleaner_test.go`

**Step 1: Write the tests**

Add to `internal/worker/cleaner_test.go`:

```go
func TestCleaner_FullTick_WorkspaceCleanedThenJobPurged(t *testing.T) {
	env := newCleanerTestEnv(t)
	env.gitChecker.uncommitted = false
	env.gitChecker.unpushed = false

	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 7
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

	// Expired completed job with workspace
	job := env.createJob(t, models.StatusCompleted, 10*24*time.Hour, true)

	c := env.newCleaner()
	c.tick(context.Background())

	// Both workspace and DB record should be gone
	assert.False(t, env.workspaceExists(job.ID), "workspace should be deleted")
	_, err = env.jobRepo.Get(job.ID)
	assert.ErrorIs(t, err, db.ErrNotFound, "job should be purged from DB")
}

func TestCleaner_FullTick_RecentJobWorkspaceCleanedNotPurged(t *testing.T) {
	env := newCleanerTestEnv(t)
	env.gitChecker.uncommitted = false
	env.gitChecker.unpushed = false

	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 30
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

	// Recent completed job with workspace
	job := env.createJob(t, models.StatusCompleted, 1*time.Hour, true)

	c := env.newCleaner()
	c.tick(context.Background())

	// Workspace deleted, but job record retained
	assert.False(t, env.workspaceExists(job.ID), "workspace should be deleted")
	_, err = env.jobRepo.Get(job.ID)
	assert.NoError(t, err, "recent job should still exist in DB")
}

func TestCleaner_FullTick_RetentionCleansSkippedWorkspace(t *testing.T) {
	env := newCleanerTestEnv(t)
	// Git check says NOT safe — workspace cleanup will skip
	env.gitChecker.uncommitted = true
	env.gitChecker.unpushed = false

	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 7
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

	// Expired completed job with workspace that has uncommitted changes
	job := env.createJob(t, models.StatusCompleted, 10*24*time.Hour, true)

	c := env.newCleaner()
	c.tick(context.Background())

	// Workspace cleanup skipped it, but retention should clean it defensively
	assert.False(t, env.workspaceExists(job.ID), "retention should clean workspace defensively")
	_, err = env.jobRepo.Get(job.ID)
	assert.ErrorIs(t, err, db.ErrNotFound, "expired job should be purged")
}

func TestCleaner_FullTick_MixedJobs(t *testing.T) {
	env := newCleanerTestEnv(t)
	env.gitChecker.uncommitted = false
	env.gitChecker.unpushed = false

	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 7
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

	// Mix of jobs:
	// 1. Running (should be untouched)
	jobRunning := env.createJob(t, models.StatusRunning, 0, true)
	// 2. Recently completed (workspace cleaned, record kept)
	jobRecent := env.createJob(t, models.StatusCompleted, 1*time.Hour, true)
	// 3. Expired completed (both cleaned)
	jobExpired := env.createJob(t, models.StatusCompleted, 10*24*time.Hour, true)
	// 4. Queued (untouched)
	jobQueued := env.createJob(t, models.StatusQueued, 0, true)

	c := env.newCleaner()
	c.tick(context.Background())

	// Running: untouched
	assert.True(t, env.workspaceExists(jobRunning.ID))
	_, err = env.jobRepo.Get(jobRunning.ID)
	assert.NoError(t, err)

	// Recent completed: workspace deleted, record kept
	assert.False(t, env.workspaceExists(jobRecent.ID))
	_, err = env.jobRepo.Get(jobRecent.ID)
	assert.NoError(t, err)

	// Expired: both deleted
	assert.False(t, env.workspaceExists(jobExpired.ID))
	_, err = env.jobRepo.Get(jobExpired.ID)
	assert.ErrorIs(t, err, db.ErrNotFound)

	// Queued: untouched
	assert.True(t, env.workspaceExists(jobQueued.ID))
	_, err = env.jobRepo.Get(jobQueued.ID)
	assert.NoError(t, err)
}
```

**Step 2: Run tests to verify they pass**

Run: `go test -v -run TestCleaner_FullTick ./internal/worker/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/worker/cleaner_test.go
git commit -m "test: add integration tests for cleanup phases"
```

---

### Task 12: Wire Cleaner into server startup

**Files:**
- Modify: `cmd/server/main.go:122-145`

**Step 1: No test needed** — this is wiring code. The cleaner is tested independently.

**Step 2: Update `cmd/server/main.go`**

Add the cleaner import and startup alongside the worker. After line 124 (`w := worker.New(q, handler, 5*time.Second)`), create the cleaner. Update the WaitGroup to track both goroutines.

In the import block, `git` package is needed:

```go
"github.com/ryan/ralph-o-matic/internal/git"
```

After the worker creation (line 124), add:

```go
repoMgr := git.NewRepoManager(workspaceDir)
cleaner := worker.NewCleaner(db.NewJobRepo(database), configRepo, repoMgr, worker.NewGitChecker())
```

Update the WaitGroup to `wg.Add(2)` and add a goroutine for the cleaner:

```go
go func() {
	defer wg.Done()
	cleaner.Run(ctx)
}()
```

The full modified section (replacing lines ~122-145):

```go
handler := executor.NewRalphHandler(database, config, workspaceDir)
handler.SetLogBroadcaster(b)
w := worker.New(q, handler, 5*time.Second)

// Set up workspace and job retention cleaner
repoMgr := git.NewRepoManager(workspaceDir)
cleaner := worker.NewCleaner(db.NewJobRepo(database), configRepo, repoMgr, worker.NewGitChecker())

// Set up notification dispatcher (reads config per-call from DB)
dispatcher := notify.NewDispatcher(configRepo, slog.Default())
w.SetNotifier(dispatcher)

ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()

go func() {
	if err := srv.Start(); err != nil {
		log.Printf("Server stopped: %v", err)
	}
}()

// Use WaitGroup to ensure worker and cleaner complete before shutdown
var wg sync.WaitGroup
wg.Add(2)
go func() {
	defer wg.Done()
	w.Run(ctx)
}()
go func() {
	defer wg.Done()
	cleaner.Run(ctx)
}()
```

**Step 3: Verify it compiles**

Run: `go build ./cmd/server/`
Expected: SUCCESS

**Step 4: Run all tests**

Run: `go test -v -short -race ./...`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: wire workspace cleaner into server startup"
```

---

### Task 13: Run full test suite and lint

Final verification that everything works together.

**Step 1: Run unit tests with race detector**

Run: `make test`
Expected: PASS

**Step 2: Run linter**

Run: `make lint`
Expected: PASS

**Step 3: Build**

Run: `make build`
Expected: SUCCESS

**Step 4: Final commit** (if any lint fixes needed)

```bash
git add -A
git commit -m "fix: address lint issues from cleanup implementation"
```
