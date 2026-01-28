package api

import (
	"encoding/json"
	"net/http"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
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

	// Parse updates as a ServerConfig (partial)
	var updates models.ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Apply updates via merge
	merged := current.Merge(&updates)

	// Validate
	if err := merged.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Save
	if err := configRepo.Save(merged); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, merged)
}
