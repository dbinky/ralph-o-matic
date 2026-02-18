package executor

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ryan/ralph-o-matic/internal/broadcast"
	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/git"
	"github.com/ryan/ralph-o-matic/internal/models"
)

// DefaultSessionExpiry is the default duration before a session expires.
const DefaultSessionExpiry = 24 * time.Hour

// RalphHandler implements the ralph loop execution
type RalphHandler struct {
	db          *db.DB
	config      *models.ServerConfig
	repoManager *git.RepoManager
	executor    *ClaudeExecutor
	jobRepo     *db.JobRepo
	logRepo     *db.LogRepo

	sessionMu sync.Mutex
	sessions  map[int64]*Session // keyed by job ID
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
		sessions:    make(map[int64]*Session),
	}
}

// SetLogBroadcaster sets the broadcaster on the handler's LogRepo for live log streaming.
func (h *RalphHandler) SetLogBroadcaster(b *broadcast.Broadcaster) {
	h.logRepo.SetBroadcaster(b)
}

// Handle executes a single iteration of the ralph loop for a job.
// The caller (worker) is responsible for iteration counting, looping,
// and calling Finalize when the job is done.
func (h *RalphHandler) Handle(ctx context.Context, job *models.Job) (*ExecutionResult, error) {
	log.Printf("Starting ralph iteration %d for job %d: %s", job.Iteration, job.ID, job.Branch)

	workDir := h.resolveWorkDir(job)
	if workDir == "" {
		var err error
		workDir, err = h.setupWorkDir(ctx, job)
		if err != nil {
			return nil, err
		}
	}

	// Bootstrap progress file on first iteration
	if job.Iteration <= 1 {
		progressPath := ProgressFilePath(job.Branch)
		bounded := strings.Contains(job.Prompt, "<promise>")
		if created, err := BootstrapProgressFile(workDir, progressPath, bounded); err != nil {
			log.Printf("Warning: failed to bootstrap progress file for job %d: %v", job.ID, err)
		} else if created {
			log.Printf("Job %d: created progress file at %s", job.ID, progressPath)
		}
	}

	// Resolve backend: job > server default > ollama
	backend := effectiveBackend(job.Backend, h.config.DefaultBackend)

	// Get session for continuity across iterations
	session := h.getSession(job.ID)

	// Execute claude with the prompt
	result, err := h.executor.Execute(ctx, workDir, job.Prompt, backend, job.Env, session, func(line string) {
		_ = h.logRepo.Append(job.ID, job.Iteration, line)
	})

	if err != nil {
		return nil, fmt.Errorf("claude execution failed: %w", err)
	}

	// Store session ID for next iteration
	if result.SessionID != "" {
		h.setSession(job.ID, NewSession(result.SessionID, DefaultSessionExpiry))
	}

	// Update iteration from output if claude reports higher
	if result.Iterations > job.Iteration {
		h.updateIteration(job, result.Iterations)
	}

	// Per-iteration commit: save progress to prevent loss on crash
	hash, commitErr := h.repoManager.Commit(ctx, workDir, fmt.Sprintf("Ralph iteration %d", job.Iteration))
	if commitErr != nil {
		log.Printf("Warning: per-iteration commit failed for job %d: %v", job.ID, commitErr)
	} else if hash != "" {
		log.Printf("Job %d iteration %d committed: %s", job.ID, job.Iteration, hash)
		// Git diff fallback: if metadata reports no files modified but we committed,
		// there were actual changes that weren't detected via RALPH_STATUS.
		// Update FilesModified to indicate progress.
		if result.Metadata != nil && result.Metadata.FilesModified == 0 {
			result.Metadata.FilesModified = 1
			log.Printf("Job %d: git commit detected changes not reported in metadata", job.ID)
		}
	}

	return result, nil
}

// Finalize commits remaining changes and creates a PR for the job.
func (h *RalphHandler) Finalize(ctx context.Context, job *models.Job, success bool) error {
	h.clearSession(job.ID)
	return h.finalize(ctx, job, success)
}

func (h *RalphHandler) getSession(jobID int64) *Session {
	h.sessionMu.Lock()
	defer h.sessionMu.Unlock()
	s := h.sessions[jobID]
	if s != nil && !s.IsValid() {
		delete(h.sessions, jobID)
		return nil
	}
	return s
}

func (h *RalphHandler) setSession(jobID int64, s *Session) {
	h.sessionMu.Lock()
	defer h.sessionMu.Unlock()
	h.sessions[jobID] = s
}

func (h *RalphHandler) clearSession(jobID int64) {
	h.sessionMu.Lock()
	defer h.sessionMu.Unlock()
	delete(h.sessions, jobID)
}

func (h *RalphHandler) updateIteration(job *models.Job, iteration int) {
	job.Iteration = iteration
	if err := h.jobRepo.Update(job); err != nil {
		log.Printf("Failed to update job iteration: %v", err)
	}
}

// resolveWorkDir returns the working directory for a job. For direct mode
// (absolute WorkingDir), it returns the path as-is. For standard mode, it
// returns the workspace path only if a clone exists (has .git directory).
// Returns "" if the workspace needs initial setup via setupWorkDir.
func (h *RalphHandler) resolveWorkDir(job *models.Job) string {
	if job.WorkingDir != "" && filepath.IsAbs(job.WorkingDir) {
		return job.WorkingDir
	}
	base := h.repoManager.WorkspacePath(job.ID)
	if base == "" {
		return ""
	}
	// Verify the workspace has a cloned repo, not just a computed path
	if _, err := os.Stat(filepath.Join(base, ".git")); err != nil {
		return ""
	}
	if job.WorkingDir != "" {
		// Sanitize: reject path traversal attempts
		cleanDir := filepath.Clean(job.WorkingDir)
		if strings.Contains(cleanDir, "..") {
			log.Printf("Warning: rejecting path traversal attempt in job %d WorkingDir: %s", job.ID, job.WorkingDir)
			return base
		}
		return filepath.Join(base, cleanDir)
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
		// Sanitize: reject path traversal attempts
		cleanDir := filepath.Clean(job.WorkingDir)
		if strings.Contains(cleanDir, "..") {
			return "", fmt.Errorf("working_dir contains path traversal sequence: %s", job.WorkingDir)
		}
		workDir = filepath.Join(workDir, cleanDir)
	}
	return workDir, nil
}

func (h *RalphHandler) finalize(ctx context.Context, job *models.Job, success bool) error {
	workDir := h.resolveWorkDir(job)

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
