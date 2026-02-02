package executor

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/git"
	"github.com/ryan/ralph-o-matic/internal/models"
)

// RalphHandler implements the ralph loop execution
type RalphHandler struct {
	db          *db.DB
	config      *models.ServerConfig
	repoManager *git.RepoManager
	executor    *ClaudeExecutor
	jobRepo     *db.JobRepo
	logRepo     *db.LogRepo
}

// NewRalphHandler creates a new ralph handler
func NewRalphHandler(database *db.DB, config *models.ServerConfig, workspaceDir string) *RalphHandler {
	return &RalphHandler{
		db:          database,
		config:      config,
		repoManager: git.NewRepoManager(workspaceDir),
		executor:    NewClaudeExecutor(config),
		jobRepo:     db.NewJobRepo(database),
		logRepo:     db.NewLogRepo(database),
	}
}

// Handle executes a single iteration of the ralph loop for a job.
// The caller (worker) is responsible for iteration counting, looping,
// and calling Finalize when the job is done.
func (h *RalphHandler) Handle(ctx context.Context, job *models.Job) error {
	log.Printf("Starting ralph iteration %d for job %d: %s", job.Iteration, job.ID, job.Branch)

	workDir := h.resolveWorkDir(ctx, job)
	if workDir == "" {
		var err error
		workDir, err = h.setupWorkDir(ctx, job)
		if err != nil {
			return err
		}
	}

	// Resolve backend: job > server default > ollama
	backend := effectiveBackend(job.Backend, h.config.DefaultBackend)

	// Pre-flight: validate Anthropic API key before running
	if backend == models.BackendAnthropic {
		if key := h.executor.resolveAnthropicKey(); key == "" {
			return fmt.Errorf("anthropic backend requires an API key; set ANTHROPIC_API_KEY env var or configure via API")
		}
	}

	// Execute claude with the prompt
	result, err := h.executor.Execute(ctx, workDir, job.Prompt, backend, job.Env, func(line string) {
		_ = h.logRepo.Append(job.ID, job.Iteration, line)
	})

	if err != nil {
		return fmt.Errorf("claude execution failed: %w", err)
	}

	// Update iteration from output if claude reports higher
	if result.Iterations > job.Iteration {
		h.updateIteration(job, result.Iterations)
	}

	return nil
}

// Finalize commits remaining changes and creates a PR for the job.
func (h *RalphHandler) Finalize(ctx context.Context, job *models.Job, success bool) error {
	return h.finalize(ctx, job, success)
}

func (h *RalphHandler) updateIteration(job *models.Job, iteration int) {
	job.Iteration = iteration
	if err := h.jobRepo.Update(job); err != nil {
		log.Printf("Failed to update job iteration: %v", err)
	}
}

// resolveWorkDir returns the working directory for a job. For direct mode
// (absolute WorkingDir), it returns the path as-is. For standard mode, it
// returns the workspace path with any relative WorkingDir appended.
// This does NOT clone; use setupWorkDir for initial setup.
func (h *RalphHandler) resolveWorkDir(ctx context.Context, job *models.Job) string {
	if job.WorkingDir != "" && filepath.IsAbs(job.WorkingDir) {
		return job.WorkingDir
	}
	base := h.repoManager.WorkspacePath(job.ID)
	if base == "" {
		return ""
	}
	if job.WorkingDir != "" {
		return base + "/" + job.WorkingDir
	}
	return base
}

// setupWorkDir clones the repo and returns the working directory.
func (h *RalphHandler) setupWorkDir(ctx context.Context, job *models.Job) (string, error) {
	if job.WorkingDir != "" && filepath.IsAbs(job.WorkingDir) {
		return job.WorkingDir, nil
	}
	workDir, err := h.repoManager.Setup(ctx, job.ID, job.RepoURL, job.Branch)
	if err != nil {
		return "", fmt.Errorf("failed to setup workspace: %w", err)
	}
	if job.WorkingDir != "" {
		workDir = workDir + "/" + job.WorkingDir
	}
	return workDir, nil
}

func (h *RalphHandler) finalize(ctx context.Context, job *models.Job, success bool) error {
	workDir := h.resolveWorkDir(ctx, job)

	// Commit any remaining changes
	hash, err := h.repoManager.Commit(ctx, workDir, fmt.Sprintf("Ralph iteration %d", job.Iteration))
	if err != nil {
		log.Printf("Warning: failed to commit final changes: %v", err)
	}
	if hash != "" {
		log.Printf("Final commit: %s", hash)
	}

	// Push and create PR
	prURL, err := h.repoManager.PushAndCreatePR(ctx, workDir, job.Branch, job.Iteration, success, "")
	if err != nil {
		return fmt.Errorf("failed to create PR: %w", err)
	}

	job.PRURL = prURL
	if err := h.jobRepo.Update(job); err != nil {
		log.Printf("Failed to update job with PR URL: %v", err)
	}

	log.Printf("Job %d PR created: %s", job.ID, prURL)
	return nil
}

// effectiveBackend resolves which backend to use: job > server > ollama
func effectiveBackend(jobBackend, serverDefault models.Backend) models.Backend {
	if jobBackend != "" {
		return jobBackend
	}
	if serverDefault != "" {
		return serverDefault
	}
	return models.BackendOllama
}

