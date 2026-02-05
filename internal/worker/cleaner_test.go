package worker

import (
	"context"
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
