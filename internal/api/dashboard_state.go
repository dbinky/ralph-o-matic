package api

import (
	"net/http"

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
	CreatedAt string           `json:"createdAt"`
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	allJobs := append(jobs, terminal...)

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
			CreatedAt: j.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"jobs": result})
}
