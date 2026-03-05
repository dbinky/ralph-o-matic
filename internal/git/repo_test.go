package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoManager_WorkspacePath(t *testing.T) {
	rm := NewRepoManager("/workspace")

	path := rm.WorkspacePath(42)
	assert.Equal(t, "/workspace/job-42", path)
}

func TestRepoManager_ResultBranch(t *testing.T) {
	rm := NewRepoManager("/workspace")

	result := rm.ResultBranch("feature/auth")
	assert.Equal(t, "ralph/feature/auth-result", result)
}

func TestRepoManager_Commit_NoChanges(t *testing.T) {
	dir, _ := initLocalRepo(t)
	rm := NewRepoManager(t.TempDir())

	// No new changes after the initial commit
	hash, err := rm.Commit(context.Background(), dir, "empty")
	require.NoError(t, err)
	assert.Equal(t, "", hash, "empty commit should return empty hash")
}

func TestRepoManager_Commit_WithChanges(t *testing.T) {
	dir, _ := initLocalRepo(t)
	rm := NewRepoManager(t.TempDir())
	ctx := context.Background()

	os.WriteFile(filepath.Join(dir, "change.txt"), []byte("change"), 0644)

	hash, err := rm.Commit(ctx, dir, "My commit")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestRepoManager_Cleanup(t *testing.T) {
	tmpDir := t.TempDir()
	rm := NewRepoManager(tmpDir)

	// Create a fake workspace for job 99
	workspacePath := rm.WorkspacePath(99)
	require.NoError(t, os.MkdirAll(workspacePath, 0755))
	os.WriteFile(filepath.Join(workspacePath, "file.txt"), []byte("data"), 0644)

	assert.DirExists(t, workspacePath)

	require.NoError(t, rm.Cleanup(99))
	assert.NoDirExists(t, workspacePath)
}

func TestRepoManager_Cleanup_NonExistent(t *testing.T) {
	rm := NewRepoManager(t.TempDir())
	// Cleanup of nonexistent workspace should not error
	assert.NoError(t, rm.Cleanup(12345))
}
