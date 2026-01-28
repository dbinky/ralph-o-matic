package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGit_IsInstalled(t *testing.T) {
	g := New()
	assert.True(t, g.IsInstalled())
}

func TestGit_Clone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	g := New()
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "repo")

	// Clone a small public repo
	err := g.Clone(context.Background(), "https://github.com/octocat/Hello-World.git", "master", dest)
	require.NoError(t, err)

	// Verify .git exists
	assert.DirExists(t, filepath.Join(dest, ".git"))
}

func TestGit_CreateBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	g := New()
	tmpDir := t.TempDir()

	// Initialize a git repo
	_ = g.run(context.Background(), tmpDir, "init")
	_ = g.run(context.Background(), tmpDir, "config", "user.email", "test@test.com")
	_ = g.run(context.Background(), tmpDir, "config", "user.name", "Test")

	// Create initial commit
	os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Test"), 0644)
	_ = g.run(context.Background(), tmpDir, "add", ".")
	_ = g.run(context.Background(), tmpDir, "commit", "-m", "Initial commit")

	// Create branch
	err := g.CreateBranch(context.Background(), tmpDir, "ralph/test-result")
	require.NoError(t, err)

	// Verify branch exists
	output, err := g.runOutput(context.Background(), tmpDir, "branch", "--list", "ralph/test-result")
	require.NoError(t, err)
	assert.Contains(t, output, "ralph/test-result")
}

func TestGit_Commit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	g := New()
	tmpDir := t.TempDir()

	// Initialize
	_ = g.run(context.Background(), tmpDir, "init")
	_ = g.run(context.Background(), tmpDir, "config", "user.email", "test@test.com")
	_ = g.run(context.Background(), tmpDir, "config", "user.name", "Test")

	// Create and stage file
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("hello"), 0644)
	_ = g.run(context.Background(), tmpDir, "add", ".")

	// Commit
	hash, err := g.Commit(context.Background(), tmpDir, "Test commit")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.Len(t, hash, 7) // Short hash
}

func TestGit_GetCurrentBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	g := New()
	tmpDir := t.TempDir()

	// Initialize with main branch
	_ = g.run(context.Background(), tmpDir, "init", "-b", "main")
	_ = g.run(context.Background(), tmpDir, "config", "user.email", "test@test.com")
	_ = g.run(context.Background(), tmpDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Test"), 0644)
	_ = g.run(context.Background(), tmpDir, "add", ".")
	_ = g.run(context.Background(), tmpDir, "commit", "-m", "Initial")

	branch, err := g.GetCurrentBranch(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
}

func TestGit_HasUncommittedChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	g := New()
	tmpDir := t.TempDir()

	// Initialize
	_ = g.run(context.Background(), tmpDir, "init")
	_ = g.run(context.Background(), tmpDir, "config", "user.email", "test@test.com")
	_ = g.run(context.Background(), tmpDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Test"), 0644)
	_ = g.run(context.Background(), tmpDir, "add", ".")
	_ = g.run(context.Background(), tmpDir, "commit", "-m", "Initial")

	// Clean state
	hasChanges, err := g.HasUncommittedChanges(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.False(t, hasChanges)

	// Make a change
	os.WriteFile(filepath.Join(tmpDir, "new.txt"), []byte("new"), 0644)

	hasChanges, err = g.HasUncommittedChanges(context.Background(), tmpDir)
	require.NoError(t, err)
	assert.True(t, hasChanges)
}
