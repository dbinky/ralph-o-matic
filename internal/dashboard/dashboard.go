package dashboard

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/ryan/ralph-o-matic/internal/queue"
)

// TemplateFuncs returns the template function map used by the dashboard
func TemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"truncate": func(n int, s string) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "..."
		},
		"upper": strings.ToUpper,
		"duration": func(d time.Duration) string {
			if d < time.Minute {
				return "< 1m"
			}
			hours := int(d.Hours())
			mins := int(d.Minutes()) % 60
			if hours > 0 {
				return fmt.Sprintf("%dh%dm", hours, mins)
			}
			return fmt.Sprintf("%dm", mins)
		},
		"timeago": func(t *time.Time) string {
			if t == nil {
				return "never"
			}
			d := time.Since(*t)
			if d < time.Minute {
				return "just now"
			}
			if d < time.Hour {
				return fmt.Sprintf("%dm ago", int(d.Minutes()))
			}
			return fmt.Sprintf("%dh ago", int(d.Hours()))
		},
		"multiply": func(a interface{}, b interface{}) float64 {
			return toFloat64(a) * toFloat64(b)
		},
	}
}

// Dashboard handles web UI requests
type Dashboard struct {
	db            *db.DB
	queue         *queue.Queue
	dashboardTmpl *template.Template
	jobTmpl       *template.Template
}

// New creates a new dashboard handler from a template filesystem
func New(database *db.DB, q *queue.Queue, templatesFS fs.FS) *Dashboard {
	funcs := TemplateFuncs()

	dashboardTmpl := template.Must(
		template.New("layout.html").Funcs(funcs).ParseFS(templatesFS, "layout.html", "dashboard.html"),
	)

	jobTmpl := template.Must(
		template.New("layout.html").Funcs(funcs).ParseFS(templatesFS, "layout.html", "job.html"),
	)

	return &Dashboard{
		db:            database,
		queue:         q,
		dashboardTmpl: dashboardTmpl,
		jobTmpl:       jobTmpl,
	}
}

// IndexData is the data for the dashboard index
type IndexData struct {
	QueueSize int
	Running   []*models.Job
	Paused    []*models.Job
	Queued    []*models.Job
	Completed []*models.Job
}

// HandleIndex renders the dashboard
func (d *Dashboard) HandleIndex(w http.ResponseWriter, r *http.Request) {
	jobRepo := db.NewJobRepo(d.db)

	running, _, _ := jobRepo.List(db.ListOptions{Statuses: []models.JobStatus{models.StatusRunning}})
	paused, _, _ := jobRepo.List(db.ListOptions{Statuses: []models.JobStatus{models.StatusPaused}})
	queued, _, _ := jobRepo.List(db.ListOptions{Statuses: []models.JobStatus{models.StatusQueued}})
	completed, _, _ := jobRepo.List(db.ListOptions{
		Statuses: []models.JobStatus{models.StatusCompleted, models.StatusFailed},
		Limit:    10,
	})

	data := IndexData{
		QueueSize: len(queued),
		Running:   running,
		Paused:    paused,
		Queued:    queued,
		Completed: completed,
	}

	d.render(w, d.dashboardTmpl, data)
}

// JobData is the data for the job detail page
type JobData struct {
	QueueSize int
	Job       *models.Job
	Logs      []*db.JobLog
}

// HandleJob renders the job detail page
func (d *Dashboard) HandleJob(w http.ResponseWriter, r *http.Request, jobID int64) {
	job, err := d.queue.Get(jobID)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	logRepo := db.NewLogRepo(d.db)
	logs, _ := logRepo.GetForJob(jobID)

	data := JobData{
		QueueSize: d.queue.Size(),
		Job:       job,
		Logs:      logs,
	}

	d.render(w, d.jobTmpl, data)
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

func (d *Dashboard) render(w http.ResponseWriter, tmpl *template.Template, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
