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
