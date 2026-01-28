# Phase 5: Git Integration

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement git operations wrapper using the `gh` CLI for cloning, branching, pushing, and PR creation.

**Architecture:** Shell out to `gh` and `git` commands. Parse output for status. Implement retry logic with exponential backoff.

**Tech Stack:** Go 1.22+, os/exec for command execution

**Dependencies:** Phase 4 must be complete (API layer)

---

## Task 1: Implement Git Command Wrapper

**Files:**
- Create: `internal/git/git.go`
- Create: `internal/git/git_test.go`

**Step 1: Write the failing tests**

Create `internal/git/git_test.go`:

```go
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
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/git/... -v -short
```

Expected: FAIL - Git not defined

**Step 3: Write minimal implementation**

Create `internal/git/git.go`:

```go
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Git wraps git command execution
type Git struct{}

// New creates a new Git wrapper
func New() *Git {
	return &Git{}
}

// IsInstalled checks if git is available
func (g *Git) IsInstalled() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// Clone clones a repository
func (g *Git) Clone(ctx context.Context, repoURL, branch, dest string) error {
	args := []string{"clone", "--branch", branch, "--single-branch", "--depth", "1", repoURL, dest}
	return g.run(ctx, "", args...)
}

// CreateBranch creates and checks out a new branch
func (g *Git) CreateBranch(ctx context.Context, dir, branchName string) error {
	return g.run(ctx, dir, "checkout", "-b", branchName)
}

// CheckoutBranch switches to an existing branch
func (g *Git) CheckoutBranch(ctx context.Context, dir, branchName string) error {
	return g.run(ctx, dir, "checkout", branchName)
}

// Commit creates a commit with the given message, returns short hash
func (g *Git) Commit(ctx context.Context, dir, message string) (string, error) {
	if err := g.run(ctx, dir, "commit", "-m", message); err != nil {
		return "", err
	}

	// Get the short hash
	output, err := g.runOutput(ctx, dir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(output), nil
}

// Push pushes the current branch to origin
func (g *Git) Push(ctx context.Context, dir, branch string) error {
	return g.run(ctx, dir, "push", "-u", "origin", branch)
}

// GetCurrentBranch returns the current branch name
func (g *Git) GetCurrentBranch(ctx context.Context, dir string) (string, error) {
	output, err := g.runOutput(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// HasUncommittedChanges checks if there are uncommitted changes
func (g *Git) HasUncommittedChanges(ctx context.Context, dir string) (bool, error) {
	output, err := g.runOutput(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

// AddAll stages all changes
func (g *Git) AddAll(ctx context.Context, dir string) error {
	return g.run(ctx, dir, "add", "-A")
}

// GetLog returns the git log
func (g *Git) GetLog(ctx context.Context, dir string, limit int) (string, error) {
	return g.runOutput(ctx, dir, "log", "--oneline", fmt.Sprintf("-n%d", limit))
}

func (g *Git) run(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s failed: %w: %s", args[0], err, stderr.String())
	}

	return nil
}

func (g *Git) runOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w", args[0], err)
	}

	return string(output), nil
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/git/... -v -short
```

Expected: PASS (non-integration tests)

**Step 5: Commit**

```bash
git add internal/git/git.go internal/git/git_test.go
git commit -m "feat(git): add git command wrapper"
```

---

## Task 2: Implement GitHub CLI Wrapper

**Files:**
- Create: `internal/git/gh.go`
- Create: `internal/git/gh_test.go`

**Step 1: Write the failing tests**

Create `internal/git/gh_test.go`:

```go
package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGH_IsInstalled(t *testing.T) {
	gh := NewGH()
	// This may fail if gh is not installed, which is fine for unit tests
	_ = gh.IsInstalled()
}

func TestGH_IsAuthenticated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gh := NewGH()
	if !gh.IsInstalled() {
		t.Skip("gh not installed")
	}

	// Just verify it doesn't panic
	_ = gh.IsAuthenticated()
}

func TestGH_BuildPRBody(t *testing.T) {
	body := BuildPRBody(8, true, "docs/plans/design.md", nil)

	assert.Contains(t, body, "8 iterations")
	assert.Contains(t, body, "docs/plans/design.md")
	assert.Contains(t, body, "Completed")
}

func TestGH_BuildPRBody_Failed(t *testing.T) {
	body := BuildPRBody(50, false, "docs/plans/design.md", map[string]string{
		"remaining_issues": "3 tests failing",
	})

	assert.Contains(t, body, "50")
	assert.Contains(t, body, "FAILED")
	assert.Contains(t, body, "3 tests failing")
}
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/git/... -v -short
```

Expected: FAIL - GH not defined

**Step 3: Write minimal implementation**

Create `internal/git/gh.go`:

```go
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// GH wraps the GitHub CLI
type GH struct{}

// NewGH creates a new GitHub CLI wrapper
func NewGH() *GH {
	return &GH{}
}

// IsInstalled checks if gh is available
func (g *GH) IsInstalled() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// IsAuthenticated checks if gh is authenticated
func (g *GH) IsAuthenticated() bool {
	cmd := exec.Command("gh", "auth", "status")
	return cmd.Run() == nil
}

// Clone clones a repository using gh
func (g *GH) Clone(ctx context.Context, repoURL, branch, dest string) error {
	cmd := exec.CommandContext(ctx, "gh", "repo", "clone", repoURL, dest, "--", "--branch", branch)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh repo clone failed: %w: %s", err, stderr.String())
	}

	return nil
}

// CreatePR creates a pull request
func (g *GH) CreatePR(ctx context.Context, dir, baseBranch, headBranch, title, body string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "create",
		"--base", baseBranch,
		"--head", headBranch,
		"--title", title,
		"--body", body,
	)
	cmd.Dir = dir

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh pr create failed: %w", err)
	}

	// Output is the PR URL
	return strings.TrimSpace(string(output)), nil
}

// GetPRURL gets the URL for an existing PR
func (g *GH) GetPRURL(ctx context.Context, dir, branch string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", branch, "--json", "url", "-q", ".url")
	cmd.Dir = dir

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// BuildPRBody generates the PR description
func BuildPRBody(iterations int, success bool, specPath string, details map[string]string) string {
	var sb strings.Builder

	sb.WriteString("## Summary\n\n")

	if success {
		sb.WriteString(fmt.Sprintf("Completed in %d iterations. All tests passing.\n\n", iterations))
	} else {
		sb.WriteString(fmt.Sprintf("Reached max iterations (%d) without completing. Tests may still be failing.\n\n", iterations))
	}

	if specPath != "" {
		sb.WriteString("## Specification\n\n")
		sb.WriteString(fmt.Sprintf("See: %s\n\n", specPath))
	}

	if !success && len(details) > 0 {
		sb.WriteString("## Current State\n\n")
		for key, value := range details {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", key, value))
		}
		sb.WriteString("\n")
	}

	if !success {
		sb.WriteString("## Notes\n\n")
		sb.WriteString("Manual intervention may be needed. Review iteration history for context.\n\n")
	}

	sb.WriteString("---\n")
	sb.WriteString("Generated by Ralph-o-matic\n")

	return sb.String()
}

// BuildPRTitle generates the PR title
func BuildPRTitle(branch string, success bool) string {
	if success {
		return fmt.Sprintf("Ralph-o-matic: %s ✓", branch)
	}
	return fmt.Sprintf("Ralph-o-matic: %s ✗ FAILED", branch)
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/git/... -v -short
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/gh.go internal/git/gh_test.go
git commit -m "feat(git): add GitHub CLI wrapper for PR creation"
```

---

## Task 3: Implement Repository Manager

**Files:**
- Create: `internal/git/repo.go`
- Create: `internal/git/repo_test.go`

This combines git and gh operations into a higher-level interface for job execution.

**Step 1: Write the failing tests**

Create `internal/git/repo_test.go`:

```go
package git

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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
```

**Step 2: Write implementation**

Create `internal/git/repo.go`:

```go
package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// RepoManager handles repository operations for jobs
type RepoManager struct {
	workspaceDir string
	git          *Git
	gh           *GH
}

// NewRepoManager creates a new repository manager
func NewRepoManager(workspaceDir string) *RepoManager {
	return &RepoManager{
		workspaceDir: workspaceDir,
		git:          New(),
		gh:           NewGH(),
	}
}

// WorkspacePath returns the path for a job's workspace
func (rm *RepoManager) WorkspacePath(jobID int64) string {
	return filepath.Join(rm.workspaceDir, fmt.Sprintf("job-%d", jobID))
}

// ResultBranch returns the result branch name for a source branch
func (rm *RepoManager) ResultBranch(sourceBranch string) string {
	return "ralph/" + sourceBranch + "-result"
}

// Setup clones the repo and creates the result branch
func (rm *RepoManager) Setup(ctx context.Context, jobID int64, repoURL, branch string) (string, error) {
	workDir := rm.WorkspacePath(jobID)

	// Clean up any existing workspace
	os.RemoveAll(workDir)

	// Clone the repository
	if err := rm.gh.Clone(ctx, repoURL, branch, workDir); err != nil {
		// Fallback to git clone
		if err := rm.git.Clone(ctx, repoURL, branch, workDir); err != nil {
			return "", fmt.Errorf("failed to clone repository: %w", err)
		}
	}

	// Create the result branch
	resultBranch := rm.ResultBranch(branch)
	if err := rm.git.CreateBranch(ctx, workDir, resultBranch); err != nil {
		return "", fmt.Errorf("failed to create result branch: %w", err)
	}

	return workDir, nil
}

// Commit commits all changes and returns the short hash
func (rm *RepoManager) Commit(ctx context.Context, workDir, message string) (string, error) {
	// Stage all changes
	if err := rm.git.AddAll(ctx, workDir); err != nil {
		return "", fmt.Errorf("failed to stage changes: %w", err)
	}

	// Check if there are changes to commit
	hasChanges, err := rm.git.HasUncommittedChanges(ctx, workDir)
	if err != nil {
		return "", fmt.Errorf("failed to check for changes: %w", err)
	}

	if !hasChanges {
		return "", nil // Nothing to commit
	}

	// Commit
	hash, err := rm.git.Commit(ctx, workDir, message)
	if err != nil {
		return "", fmt.Errorf("failed to commit: %w", err)
	}

	return hash, nil
}

// PushAndCreatePR pushes the branch and creates a PR
func (rm *RepoManager) PushAndCreatePR(ctx context.Context, workDir, baseBranch string, iterations int, success bool, specPath string) (string, error) {
	resultBranch := rm.ResultBranch(baseBranch)

	// Push the branch
	if err := rm.git.Push(ctx, workDir, resultBranch); err != nil {
		return "", fmt.Errorf("failed to push: %w", err)
	}

	// Create PR
	title := BuildPRTitle(baseBranch, success)
	body := BuildPRBody(iterations, success, specPath, nil)

	prURL, err := rm.gh.CreatePR(ctx, workDir, baseBranch, resultBranch, title, body)
	if err != nil {
		return "", fmt.Errorf("failed to create PR: %w", err)
	}

	return prURL, nil
}

// Cleanup removes the job workspace
func (rm *RepoManager) Cleanup(jobID int64) error {
	return os.RemoveAll(rm.WorkspacePath(jobID))
}
```

**Step 3: Run tests**

Run:
```bash
go test ./internal/git/... -v -short
```

Expected: PASS

**Step 4: Commit**

```bash
git add internal/git/repo.go internal/git/repo_test.go
git commit -m "feat(git): add repository manager for job workspace handling"
```

---

## Phase 5 Completion Checklist

- [ ] Git command wrapper with clone, branch, commit, push
- [ ] GitHub CLI wrapper with PR creation
- [ ] Repository manager for job workflows
- [ ] PR body and title generation
- [ ] Workspace management
- [ ] All tests passing
- [ ] All code committed

**Next Phase:** Phase 6 - Executor
