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

// Handle executes the ralph loop for a job
func (h *RalphHandler) Handle(ctx context.Context, job *models.Job) error {
	log.Printf("Starting ralph loop for job %d: %s", job.ID, job.Branch)

	// Determine working directory
	var workDir string
	if job.WorkingDir != "" && filepath.IsAbs(job.WorkingDir) {
		// Direct mode: use the absolute working directory as-is (no clone)
		workDir = job.WorkingDir
	} else {
		// Standard mode: clone into workspace
		var err error
		workDir, err = h.repoManager.Setup(ctx, job.ID, job.RepoURL, job.Branch)
		if err != nil {
			return fmt.Errorf("failed to setup workspace: %w", err)
		}
		if job.WorkingDir != "" {
			workDir = workDir + "/" + job.WorkingDir
		}
	}

	// Resolve backend: job > server default > ollama
	backend := effectiveBackend(job.Backend, h.config.DefaultBackend)

	// Execute claude with the prompt
	result, err := h.executor.Execute(ctx, workDir, job.Prompt, backend, job.Env, func(line string) {
		_ = h.logRepo.Append(job.ID, job.Iteration, line)
	})

	if err != nil {
		return fmt.Errorf("claude execution failed: %w", err)
	}

	// Update iteration from output
	if result.Iterations > job.Iteration {
		h.updateIteration(job, result.Iterations)
	}

	// Check completion
	if result.Completed {
		log.Printf("Job %d completed successfully after %d iterations", job.ID, job.Iteration)
		return h.finalize(ctx, job, true)
	}

	// Check max iterations
	if job.HasReachedMaxIterations() {
		log.Printf("Job %d reached max iterations (%d)", job.ID, job.MaxIterations)
		return h.finalize(ctx, job, false)
	}

	// Continue running (scheduler will handle re-execution)
	return nil
}

func (h *RalphHandler) updateIteration(job *models.Job, iteration int) {
	job.Iteration = iteration
	if err := h.jobRepo.Update(job); err != nil {
		log.Printf("Failed to update job iteration: %v", err)
	}
}

func (h *RalphHandler) finalize(ctx context.Context, job *models.Job, success bool) error {
	var workDir string
	if job.WorkingDir != "" && filepath.IsAbs(job.WorkingDir) {
		workDir = job.WorkingDir
	} else {
		workDir = h.repoManager.WorkspacePath(job.ID)
		if job.WorkingDir != "" {
			workDir = workDir + "/" + job.WorkingDir
		}
	}

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

func shouldContinue(job *models.Job) bool {
	return job.Iteration < job.MaxIterations
}
