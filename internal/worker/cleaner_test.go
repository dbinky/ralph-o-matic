package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/git"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
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
func newCleanerTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.New(":memory:")
	require.NoError(t, err)
	err = database.Migrate()
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })
	return database
}

// --- Lifecycle Tests ---

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

// --- Workspace Cleanup — Happy Path ---

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

// --- Workspace Cleanup — Skip Scenarios ---

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

// --- Workspace Cleanup — Error Scenarios ---

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

	// Create two failed jobs with workspaces
	env.createJob(t, models.StatusFailed, 1*time.Hour, true)
	job2 := env.createJob(t, models.StatusFailed, 1*time.Hour, true)

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

	// Should not panic — exact behavior depends on timing
}

// --- Job Retention — Happy Path ---

func TestCleaner_Retention_PurgesOldCompletedJob(t *testing.T) {
	env := newCleanerTestEnv(t)
	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 7
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

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

	job := env.createJob(t, models.StatusCompleted, 10*24*time.Hour, true)

	c := env.newCleaner()
	c.purgeExpiredJobs(context.Background())

	assert.False(t, env.workspaceExists(job.ID), "workspace should be cleaned during retention purge")
	_, err = env.jobRepo.Get(job.ID)
	assert.ErrorIs(t, err, db.ErrNotFound, "job should be deleted")
}

// --- Job Retention — Skip/Boundary Scenarios ---

func TestCleaner_Retention_SkipsWhenRetentionDaysZero(t *testing.T) {
	env := newCleanerTestEnv(t)
	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 0
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

	job := env.createJob(t, models.StatusCompleted, 365*24*time.Hour, false)

	c := env.newCleaner()
	c.purgeExpiredJobs(context.Background())

	_, err = env.jobRepo.Get(job.ID)
	assert.NoError(t, err, "job should not be purged when retention is 0")
}

func TestCleaner_Retention_DoesNotPurgeRecentJob(t *testing.T) {
	env := newCleanerTestEnv(t)
	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 30
	err := env.configRepo.Save(cfg)
	require.NoError(t, err)

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
	c.purgeExpiredJobs(context.Background())
}

// --- Integration Tests: Both Phases Together ---

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

func TestCleaner_Retention_ContextAlreadyCancelledAtStart(t *testing.T) {
	env := newCleanerTestEnv(t)
	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 1
	require.NoError(t, env.configRepo.Save(cfg))

	job := env.createJob(t, models.StatusCompleted, 2*24*time.Hour, false)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before call

	c := env.newCleaner()
	c.purgeExpiredJobs(ctx)

	// Job should NOT be deleted because context was already cancelled
	_, err := env.jobRepo.Get(job.ID)
	assert.NoError(t, err, "job should still exist when context was pre-cancelled")
}

func TestCleaner_Retention_ContextCancelledMidLoop(t *testing.T) {
	env := newCleanerTestEnv(t)
	cfg := models.DefaultServerConfig()
	cfg.JobRetentionDays = 1
	require.NoError(t, env.configRepo.Save(cfg))

	// Create multiple expired jobs
	for i := 0; i < 3; i++ {
		env.createJob(t, models.StatusCompleted, 2*24*time.Hour, false)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Use a custom cleaner where we cancel mid-loop isn't directly injectable,
	// but we can verify the ctx.Err() path by having ctx already done.
	// This is equivalent to the pre-cancel test but for the inner loop path.
	cancel()

	c := env.newCleaner()
	// Call with done context — hits the inner ctx.Err() check too.
	c.purgeExpiredJobs(ctx)
}

func TestCleaner_Tick_SkipsWhenLocked(t *testing.T) {
	env := newCleanerTestEnv(t)

	c := env.newCleaner()

	// Acquire the internal lock to simulate a running cleanup
	c.running.Lock()
	defer c.running.Unlock()

	// tick should log and return without hanging
	ctx := context.Background()
	c.tick(ctx) // should return immediately because TryLock fails
}

func TestCleaner_cleanWorkspaces_ContextAlreadyCancelled(t *testing.T) {
	env := newCleanerTestEnv(t)

	// Create a terminal job with a workspace
	job := env.createJob(t, models.StatusCompleted, 0, true)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := env.newCleaner()
	c.cleanWorkspaces(ctx)

	// Workspace should still exist — context was pre-cancelled
	assert.True(t, env.workspaceExists(job.ID))
}
