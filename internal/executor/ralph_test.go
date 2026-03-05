package executor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { database.Close() })
	return database
}

func TestEffectiveBackend(t *testing.T) {
	t.Run("job backend takes precedence", func(t *testing.T) {
		result := effectiveBackend(models.BackendAnthropic, models.BackendOllama)
		assert.Equal(t, models.BackendAnthropic, result)
	})

	t.Run("falls back to server default", func(t *testing.T) {
		result := effectiveBackend("", models.BackendAnthropic)
		assert.Equal(t, models.BackendAnthropic, result)
	})

	t.Run("falls back to ollama when both empty", func(t *testing.T) {
		result := effectiveBackend("", "")
		assert.Equal(t, models.BackendOllama, result)
	})
}

func TestRalphHandler_UpdateIteration(t *testing.T) {
	database := newTestDB(t)
	jobRepo := db.NewJobRepo(database)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, jobRepo.Create(job))

	handler := NewRalphHandler(database, models.DefaultServerConfig(), "/tmp")

	handler.updateIteration(job, 5)
	assert.Equal(t, 5, job.Iteration)

	// Verify persisted
	fetched, _ := jobRepo.Get(job.ID)
	assert.Equal(t, 5, fetched.Iteration)
}

func TestRalphHandler_SessionLifecycle(t *testing.T) {
	database := newTestDB(t)
	handler := NewRalphHandler(database, models.DefaultServerConfig(), "/tmp")

	const jobID int64 = 42

	// Initially no session
	assert.Nil(t, handler.getSession(jobID))

	// Set and retrieve
	s := NewSession("session-abc", DefaultSessionExpiry)
	handler.setSession(jobID, s)
	got := handler.getSession(jobID)
	require.NotNil(t, got)
	assert.Equal(t, "session-abc", got.ID)

	// Clear removes it
	handler.clearSession(jobID)
	assert.Nil(t, handler.getSession(jobID))
}

func TestRalphHandler_Session_ExpiredIsRemoved(t *testing.T) {
	database := newTestDB(t)
	handler := NewRalphHandler(database, models.DefaultServerConfig(), "/tmp")

	const jobID int64 = 99

	// Set an already-expired session
	s := NewSession("expired-session", -1*time.Second) // negative = already expired
	handler.setSession(jobID, s)

	// getSession should detect expiry and return nil
	got := handler.getSession(jobID)
	assert.Nil(t, got)
}

func TestRalphHandler_ResolveWorkDir_DirectMode(t *testing.T) {
	database := newTestDB(t)
	handler := NewRalphHandler(database, models.DefaultServerConfig(), "/tmp")

	job := &models.Job{
		WorkingDir: "/absolute/path/to/repo",
	}

	result := handler.resolveWorkDir(job)
	assert.Equal(t, "/absolute/path/to/repo", result)
}

func TestRalphHandler_ResolveWorkDir_StandardMode_NoClone(t *testing.T) {
	database := newTestDB(t)
	handler := NewRalphHandler(database, models.DefaultServerConfig(), t.TempDir())

	job := &models.Job{
		ID:         123,
		WorkingDir: "", // no WorkingDir = standard mode
	}

	// No clone exists yet — should return ""
	result := handler.resolveWorkDir(job)
	assert.Equal(t, "", result)
}

func TestRalphHandler_ResolveWorkDir_StandardMode_WithClone(t *testing.T) {
	tmpDir := t.TempDir()
	database := newTestDB(t)
	handler := NewRalphHandler(database, models.DefaultServerConfig(), tmpDir)

	job := &models.Job{ID: 456}

	// Create a fake workspace with .git dir to simulate a cloned repo
	workspacePath := handler.repoManager.WorkspacePath(job.ID)
	gitDir := filepath.Join(workspacePath, ".git")
	require.NoError(t, os.MkdirAll(gitDir, 0755))

	result := handler.resolveWorkDir(job)
	assert.Equal(t, workspacePath, result)
}

func TestRalphHandler_ResolveWorkDir_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	database := newTestDB(t)
	handler := NewRalphHandler(database, models.DefaultServerConfig(), tmpDir)

	job := &models.Job{
		ID:         789,
		WorkingDir: "../../etc/passwd",
	}

	// Create a fake workspace with .git to allow reaching the traversal check
	workspacePath := handler.repoManager.WorkspacePath(job.ID)
	gitDir := filepath.Join(workspacePath, ".git")
	require.NoError(t, os.MkdirAll(gitDir, 0755))

	// Path traversal should be rejected — returns base dir
	result := handler.resolveWorkDir(job)
	assert.Equal(t, workspacePath, result)
	assert.NotContains(t, result, "..")
}
