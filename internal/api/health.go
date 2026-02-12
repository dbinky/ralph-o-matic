package api

import (
	"context"
	"net/http"
	"time"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/platform"
)

const (
	readinessTimeout = 5 * time.Second
	minDiskBytes     = 100 * 1024 * 1024 // 100 MB
)

// readinessResponse is the JSON response from /readiness.
type readinessResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()

	checks := map[string]string{}
	healthy := true

	// DB check
	if err := s.db.Ping(); err != nil {
		checks["database"] = err.Error()
		healthy = false
	} else {
		checks["database"] = "ok"
	}

	// Disk check
	if err := checkDisk(".", minDiskBytes); err != nil {
		checks["disk"] = err.Error()
		healthy = false
	} else {
		checks["disk"] = "ok"
	}

	// Ollama check — only when backend is ollama
	configRepo := db.NewConfigRepo(s.db)
	cfg, err := configRepo.Get()
	if err != nil {
		checks["config"] = err.Error()
		healthy = false
	} else if cfg.DefaultBackend == "" || cfg.DefaultBackend == "ollama" {
		client := platform.NewOllamaClient(cfg.Ollama.Host)
		if err := client.Ping(ctx); err != nil {
			checks["ollama"] = err.Error()
			healthy = false
		} else {
			checks["ollama"] = "ok"
		}
	}

	status := http.StatusOK
	resp := readinessResponse{Status: "ok", Checks: checks}
	if !healthy {
		status = http.StatusServiceUnavailable
		resp.Status = "unhealthy"
	}

	writeJSON(w, status, resp)
}
