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

// helpers for local git tests (no network, not gated by -short)

func initLocalRepo(t *testing.T) (string, *Git) {
	t.Helper()
	g := New()
	if !g.IsInstalled() {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	ctx := context.Background()
	require.NoError(t, g.run(ctx, dir, "init", "-b", "main"))
	require.NoError(t, g.run(ctx, dir, "config", "user.email", "test@test.com"))
	require.NoError(t, g.run(ctx, dir, "config", "user.name", "Test"))
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0644)
	require.NoError(t, g.run(ctx, dir, "add", "."))
	require.NoError(t, g.run(ctx, dir, "commit", "-m", "Initial commit"))
	return dir, g
}

func TestGit_AddAll(t *testing.T) {
	dir, g := initLocalRepo(t)
	ctx := context.Background()

	os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("content"), 0644)

	require.NoError(t, g.AddAll(ctx, dir))

	hasChanges, err := g.HasUncommittedChanges(ctx, dir)
	require.NoError(t, err)
	assert.True(t, hasChanges)
}

func TestGit_GetLog(t *testing.T) {
	dir, g := initLocalRepo(t)
	ctx := context.Background()

	log, err := g.GetLog(ctx, dir, 5)
	require.NoError(t, err)
	assert.Contains(t, log, "Initial commit")
}

func TestGit_CheckoutBranch(t *testing.T) {
	dir, g := initLocalRepo(t)
	ctx := context.Background()

	require.NoError(t, g.CreateBranch(ctx, dir, "feature/test"))
	require.NoError(t, g.CheckoutBranch(ctx, dir, "main"))

	branch, err := g.GetCurrentBranch(ctx, dir)
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
}

func TestGit_HasCommitsAhead(t *testing.T) {
	dir, g := initLocalRepo(t)
	ctx := context.Background()

	// Create a result branch with a commit
	require.NoError(t, g.CreateBranch(ctx, dir, "ralph/main-result"))
	os.WriteFile(filepath.Join(dir, "result.txt"), []byte("result"), 0644)
	require.NoError(t, g.AddAll(ctx, dir))
	_, err := g.Commit(ctx, dir, "Add result")
	require.NoError(t, err)

	// Result branch should be ahead of main
	ahead, err := g.HasCommitsAhead(ctx, dir, "main", "ralph/main-result")
	require.NoError(t, err)
	assert.True(t, ahead)

	// main is not ahead of itself
	notAhead, err := g.HasCommitsAhead(ctx, dir, "main", "main")
	require.NoError(t, err)
	assert.False(t, notAhead)
}
