package api

import (
	"encoding/json"
	"io"
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

	// Read raw body for field-presence-aware merge
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	// Validate it's valid JSON
	if !json.Valid(raw) {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Apply updates via merge with field-presence detection
	merged, err := current.MergeJSON(json.RawMessage(raw))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

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
