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

// PushAndCreatePR pushes the branch and creates a PR.
// Returns empty string with no error if there are no changes to push.
func (rm *RepoManager) PushAndCreatePR(ctx context.Context, workDir, baseBranch string, iterations int, success bool, specPath string) (string, error) {
	resultBranch := rm.ResultBranch(baseBranch)

	// Check if there are any commits on the result branch beyond the base
	hasChanges, err := rm.git.HasCommitsAhead(ctx, workDir, baseBranch, resultBranch)
	if err != nil {
		// If we can't determine, try pushing anyway
		hasChanges = true
	}
	if !hasChanges {
		return "", nil
	}

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
