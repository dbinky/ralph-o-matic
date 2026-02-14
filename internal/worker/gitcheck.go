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
