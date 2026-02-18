package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
)

// DashboardJob is a lightweight job summary for SSE reconnect reconciliation.
type DashboardJob struct {
	ID        int64            `json:"id"`
	Status    models.JobStatus `json:"status"`
	Repo      string           `json:"repo"`
	Branch    string           `json:"branch"`
	User      string           `json:"user"`
	Priority  models.Priority  `json:"priority"`
	Iteration int              `json:"iteration"`
	CreatedAt time.Time         `json:"createdAt"`
}

func (s *Server) handleDashboardState(w http.ResponseWriter, r *http.Request) {
	jobRepo := db.NewJobRepo(s.db)

	// Fetch all non-terminal jobs (running, paused, queued)
	activeStatuses := []models.JobStatus{
		models.StatusRunning,
		models.StatusPaused,
		models.StatusQueued,
	}

	jobs, _, err := jobRepo.List(db.ListOptions{
		Statuses: activeStatuses,
		Limit:    100,
	})
	if err != nil {
		slog.Error("dashboard-state: failed to list active jobs", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load dashboard state")
		return
	}

	// Also include recent terminal jobs (last 10)
	terminal, _, err := jobRepo.List(db.ListOptions{
		Statuses: []models.JobStatus{
			models.StatusCompleted,
			models.StatusFailed,
			models.StatusCancelled,
		},
		Limit: 10,
	})
	if err != nil {
		slog.Error("dashboard-state: failed to list terminal jobs", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load dashboard state")
		return
	}

	allJobs := make([]*models.Job, 0, len(jobs)+len(terminal))
	allJobs = append(allJobs, jobs...)
	allJobs = append(allJobs, terminal...)

	result := make([]DashboardJob, 0, len(allJobs))
	for _, j := range allJobs {
		result = append(result, DashboardJob{
			ID:        j.ID,
			Status:    j.Status,
			Repo:      j.RepoURL,
			Branch:    j.Branch,
			User:      j.OwnerName,
			Priority:  j.Priority,
			Iteration: j.Iteration,
			CreatedAt: j.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"jobs": result})
}
