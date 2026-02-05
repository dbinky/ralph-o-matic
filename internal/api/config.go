package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/ryan/ralph-o-matic/internal/notify"
)

// anthropicConfigResponse redacts the API key, exposing only whether one is set.
type anthropicConfigResponse struct {
	APIKeySet  bool   `json:"api_key_set"`
	LargeModel string `json:"large_model"`
	SmallModel string `json:"small_model"`
}

// configResponse mirrors ServerConfig but redacts sensitive fields.
type configResponse struct {
	Ollama               models.OllamaConfig     `json:"ollama"`
	LargeModel           models.ModelPlacement   `json:"large_model"`
	SmallModel           models.ModelPlacement   `json:"small_model"`
	DefaultMaxIterations int                     `json:"default_max_iterations"`
	WorkspaceDir         string                  `json:"workspace_dir,omitempty"`
	JobRetentionDays     int                     `json:"job_retention_days"`
	DefaultBackend       models.Backend          `json:"default_backend"`
	Anthropic            anthropicConfigResponse `json:"anthropic"`
	MaxClaudeRetries     int                     `json:"max_claude_retries"`
	MaxGitRetries        int                     `json:"max_git_retries"`
	GitRetryBackoffMs    int                     `json:"git_retry_backoff_ms"`
}

func newConfigResponse(cfg *models.ServerConfig) *configResponse {
	return &configResponse{
		Ollama:               cfg.Ollama,
		LargeModel:           cfg.LargeModel,
		SmallModel:           cfg.SmallModel,
		DefaultMaxIterations: cfg.DefaultMaxIterations,
		WorkspaceDir:         cfg.WorkspaceDir,
		JobRetentionDays:     cfg.JobRetentionDays,
		DefaultBackend:       cfg.DefaultBackend,
		Anthropic: anthropicConfigResponse{
			APIKeySet:  cfg.Anthropic.APIKey != "",
			LargeModel: cfg.Anthropic.LargeModel,
			SmallModel: cfg.Anthropic.SmallModel,
		},
		MaxClaudeRetries:  cfg.MaxClaudeRetries,
		MaxGitRetries:     cfg.MaxGitRetries,
		GitRetryBackoffMs: cfg.GitRetryBackoffMs,
	}
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	configRepo := db.NewConfigRepo(s.db)

	cfg, err := configRepo.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, newConfigResponse(cfg))
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	configRepo := db.NewConfigRepo(s.db)

	// Limit request body to 1 MB to prevent denial of service
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

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

	// Warn if an API key is being persisted to the database
	if merged.Anthropic.APIKey != "" && merged.Anthropic.APIKey != current.Anthropic.APIKey {
		log.Printf("Warning: Anthropic API key stored in database. Consider using ANTHROPIC_API_KEY environment variable instead.")
	}

	// Save
	if err := configRepo.Save(merged); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := newConfigResponse(merged)
	writeJSON(w, http.StatusOK, struct {
		*configResponse
		Note string `json:"_note,omitempty"`
	}{
		configResponse: resp,
		Note:           "Configuration saved. Some changes may require a server restart to take effect.",
	})
}

func (s *Server) handleTestNotify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"` // "smtp" or "teams"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Channel != "smtp" && req.Channel != "teams" {
		writeError(w, http.StatusBadRequest, "channel must be 'smtp' or 'teams'")
		return
	}

	configRepo := db.NewConfigRepo(s.db)
	cfg, err := configRepo.Get()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load config: "+err.Error())
		return
	}

	// Build a test job for the notification
	testJob := &models.Job{
		ID:            0,
		RepoURL:       "https://github.com/example/test-repo.git",
		Branch:        "test-branch",
		OwnerName:     "Test User",
		Iteration:     3,
		MaxIterations: 10,
		PRURL:         "https://github.com/example/test-repo/pull/1",
	}
	now := time.Now()
	started := now.Add(-5 * time.Minute)
	testJob.StartedAt = &started
	testJob.CompletedAt = &now

	var notifier notify.Notifier
	switch req.Channel {
	case "smtp":
		if !cfg.Notify.SMTP.Enabled {
			writeError(w, http.StatusBadRequest, "SMTP notifications are not enabled. Set notify.smtp.enabled to true first.")
			return
		}
		notifier = notify.NewSMTPNotifier(cfg.Notify.SMTP)
	case "teams":
		if !cfg.Notify.Teams.Enabled {
			writeError(w, http.StatusBadRequest, "Teams notifications are not enabled. Set notify.teams.enabled to true first.")
			return
		}
		notifier = notify.NewTeamsNotifier(cfg.Notify.Teams)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if err := notifier.Notify(ctx, testJob, notify.EventCompleted); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("%s notification failed: %v", req.Channel, err),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Test %s notification sent successfully", req.Channel),
	})
}
