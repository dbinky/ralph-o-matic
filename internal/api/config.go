package api

import (
	"encoding/json"
	"net/http"

	"github.com/ryan/ralph-o-matic/internal/db"
)

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	configRepo := db.NewConfigRepo(s.db)

	cfg, err := configRepo.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	configRepo := db.NewConfigRepo(s.db)

	// Get current config
	current, err := configRepo.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Parse updates
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Apply updates
	if v, ok := updates["large_model"].(string); ok {
		current.LargeModel = v
	}
	if v, ok := updates["small_model"].(string); ok {
		current.SmallModel = v
	}
	if v, ok := updates["default_max_iterations"].(float64); ok {
		current.DefaultMaxIterations = int(v)
	}
	if v, ok := updates["concurrent_jobs"].(float64); ok {
		current.ConcurrentJobs = int(v)
	}
	if v, ok := updates["workspace_dir"].(string); ok {
		current.WorkspaceDir = v
	}
	if v, ok := updates["job_retention_days"].(float64); ok {
		current.JobRetentionDays = int(v)
	}

	// Validate
	if err := current.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Save
	if err := configRepo.Save(current); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, current)
}
