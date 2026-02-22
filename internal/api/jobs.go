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
	ExitPromise   string            `json:"exit_promise,omitempty"`
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
	if req.ExitPromise != "" {
		job.ExitPromise = req.ExitPromise
	}

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

// defaultListLimit is the maximum number of jobs returned when no limit is specified.
const defaultListLimit = 100

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	opts := db.ListOptions{}

	// Filter by owner for non-admin users
	if user := auth.UserFromContext(r.Context()); user != nil && !user.IsAdmin() {
		opts.OwnerID = user.ID
	}

	// Parse and validate status filter
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		statuses := strings.Split(statusStr, ",")
		for _, s := range statuses {
			status := models.JobStatus(s)
			if !status.Valid() {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid status filter: %q", s))
				return
			}
			opts.Statuses = append(opts.Statuses, status)
		}
	}

	// Parse pagination with validation
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 0 {
			writeError(w, http.StatusBadRequest, "invalid limit: must be a non-negative integer")
			return
		}
		opts.Limit = limit
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		offset, err := strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			writeError(w, http.StatusBadRequest, "invalid offset: must be a non-negative integer")
			return
		}
		opts.Offset = offset
	}

	// Apply default limit when not specified or when explicitly set to 0.
	if opts.Limit == 0 {
		opts.Limit = defaultListLimit
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

	job, ok := s.authorizedJob(w, r, jobID)
	if !ok {
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

	job, ok := s.authorizedJob(w, r, jobID)
	if !ok {
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

	job, ok := s.authorizedJob(w, r, jobID)
	if !ok {
		return
	}

	// Only allow updates on non-terminal jobs
	if job.Status.IsTerminal() {
		writeError(w, http.StatusConflict, fmt.Sprintf("cannot update job in %s state", job.Status))
		return
	}

	// Limit request body to 1 MB to prevent denial of service
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Reject unknown fields to prevent silent typos (e.g. "max_iteration" vs "max_iterations")
	allowedFields := map[string]bool{"priority": true, "max_iterations": true}
	for key := range updates {
		if !allowedFields[key] {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown field: %q (allowed: priority, max_iterations)", key))
			return
		}
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
		v := int(maxIter)
		if v <= 0 {
			writeError(w, http.StatusBadRequest, "max_iterations must be positive")
			return
		}
		job.MaxIterations = v
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

	job, ok := s.authorizedJob(w, r, jobID)
	if !ok {
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

	job, ok := s.authorizedJob(w, r, jobID)
	if !ok {
		return
	}

	if err := s.queue.Resume(job); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleReorderJobs(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 1 MB to prevent denial of service
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

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

	if _, ok := s.authorizedJob(w, r, jobID); !ok {
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

// authorizedJob fetches a job by ID and checks access control.
// It writes the appropriate HTTP error response and returns nil, false if
// the job is not found, an error occurs, or access is denied.
func (s *Server) authorizedJob(w http.ResponseWriter, r *http.Request, jobID int64) (*models.Job, bool) {
	job, err := s.queue.Get(jobID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}

	if !canAccessJob(r, job) {
		writeError(w, http.StatusForbidden, "access denied")
		return nil, false
	}

	return job, true
}

// canAccessJob checks whether the request's user is allowed to access the given job.
func canAccessJob(r *http.Request, job *models.Job) bool {
	return auth.CanAccessJob(r, job.OwnerID)
}

// envVarDenylist contains environment variable names and prefixes that should
// not be overridden by job env settings for security reasons.
var envVarDenylist = []string{
	"LD_",        // Linux dynamic linker
	"DYLD_",      // macOS dynamic linker
	"PATH",       // executable search path
	"HOME",       // home directory
	"SHELL",      // shell executable
	"ANTHROPIC_", // Anthropic API config
	"CLAUDE_",    // Claude CLI config
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
