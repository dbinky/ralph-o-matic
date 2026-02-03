package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/ryan/ralph-o-matic/internal/auth"
	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
)

// CreateJobRequest is the request body for creating a job
type CreateJobRequest struct {
	RepoURL       string            `json:"repo_url"`
	Branch        string            `json:"branch"`
	Prompt        string            `json:"prompt"`
	MaxIterations int               `json:"max_iterations"`
	Priority      string            `json:"priority,omitempty"`
	WorkingDir    string            `json:"working_dir,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	Backend       string            `json:"backend,omitempty"`
}

// ListJobsResponse is the response for listing jobs
type ListJobsResponse struct {
	Jobs   []*models.Job `json:"jobs"`
	Total  int           `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

// ReorderRequest is the request body for reordering jobs
type ReorderRequest struct {
	JobIDs []int64 `json:"job_ids"`
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 1 MB to prevent denial of service
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Sanitize working_dir to prevent path traversal
	workingDir := req.WorkingDir
	if workingDir != "" {
		workingDir = filepath.Clean(workingDir)
		if strings.Contains(workingDir, "..") {
			writeError(w, http.StatusBadRequest, "working_dir cannot contain path traversal sequences")
			return
		}
	}

	// Validate env vars don't contain dangerous prefixes
	if err := validateEnvVars(req.Env); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	job := models.NewJob(req.RepoURL, req.Branch, req.Prompt, req.MaxIterations)
	job.WorkingDir = workingDir
	job.Env = req.Env

	// Set ownership from authenticated user
	if user := auth.UserFromContext(r.Context()); user != nil {
		job.OwnerID = user.ID
		job.OwnerName = user.Name
	}

	if req.Priority != "" {
		priority, err := models.ParsePriority(req.Priority)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		job.Priority = priority
	}

	if req.Backend != "" {
		backend := models.Backend(req.Backend)
		if !backend.Valid() {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid backend: %q", req.Backend))
			return
		}
		job.Backend = backend
	}

	if err := s.queue.Enqueue(job); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, job)
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	opts := db.ListOptions{}

	// Filter by owner for non-admin users
	if user := auth.UserFromContext(r.Context()); user != nil && !user.IsAdmin() {
		opts.OwnerID = user.ID
	}

	// Parse status filter
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		statuses := strings.Split(statusStr, ",")
		for _, s := range statuses {
			opts.Statuses = append(opts.Statuses, models.JobStatus(s))
		}
	}

	// Parse pagination
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, _ := strconv.Atoi(limitStr)
		opts.Limit = limit
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		offset, _ := strconv.Atoi(offsetStr)
		opts.Offset = offset
	}

	jobs, total, err := db.NewJobRepo(s.db).List(opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, ListJobsResponse{
		Jobs:   jobs,
		Total:  total,
		Limit:  opts.Limit,
		Offset: opts.Offset,
	})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job ID")
		return
	}

	job, err := s.queue.Get(jobID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !canAccessJob(r, job) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job ID")
		return
	}

	job, err := s.queue.Get(jobID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !canAccessJob(r, job) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	if err := s.queue.Cancel(job); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleUpdateJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job ID")
		return
	}

	job, err := s.queue.Get(jobID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !canAccessJob(r, job) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Apply updates
	if priority, ok := updates["priority"].(string); ok {
		p, err := models.ParsePriority(priority)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		job.Priority = p
	}
	if maxIter, ok := updates["max_iterations"].(float64); ok {
		job.MaxIterations = int(maxIter)
	}

	if err := s.queue.Update(job); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handlePauseJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job ID")
		return
	}

	job, err := s.queue.Get(jobID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !canAccessJob(r, job) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	if err := s.queue.Pause(job); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleResumeJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job ID")
		return
	}

	job, err := s.queue.Get(jobID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !canAccessJob(r, job) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	if err := s.queue.Resume(job); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleReorderJobs(w http.ResponseWriter, r *http.Request) {
	var req ReorderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if err := s.queue.Reorder(req.JobIDs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string][]int64{"reordered": req.JobIDs})
}

func (s *Server) handleGetJobLogs(w http.ResponseWriter, r *http.Request) {
	jobID, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job ID")
		return
	}

	logRepo := db.NewLogRepo(s.db)
	logs, err := logRepo.GetForJob(jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"logs": logs})
}

// canAccessJob checks whether the request's user is allowed to access the given job.
// Returns true when: auth is disabled (no user in context), user is admin,
// job has no owner (pre-auth job), or user owns the job.
func canAccessJob(r *http.Request, job *models.Job) bool {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		return true // auth mode none
	}
	if user.IsAdmin() {
		return true
	}
	if job.OwnerID == "" {
		return true // pre-auth job
	}
	return job.OwnerID == user.ID
}

// envVarDenylist contains environment variable names and prefixes that should
// not be overridden by job env settings for security reasons.
var envVarDenylist = []string{
	"LD_",       // Linux dynamic linker
	"DYLD_",     // macOS dynamic linker
	"PATH",      // executable search path
	"HOME",      // home directory
	"SHELL",     // shell executable
	"ANTHROPIC_", // Anthropic API config
	"CLAUDE_",   // Claude CLI config
}

// validateEnvVars checks that no env vars match the denylist.
func validateEnvVars(env map[string]string) error {
	for key := range env {
		upperKey := strings.ToUpper(key)
		for _, denied := range envVarDenylist {
			if strings.HasPrefix(upperKey, denied) || upperKey == denied {
				return fmt.Errorf("environment variable %q is not allowed", key)
			}
		}
	}
	return nil
}
