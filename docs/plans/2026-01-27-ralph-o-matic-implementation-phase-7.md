# Phase 7: Web Dashboard

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the web dashboard with queue overview, job details, config panel, and live updates via SSE.

**Architecture:** Server-rendered HTML using Go templates. Minimal JavaScript for drag-drop reordering (Sortable.js) and live updates (EventSource API). Embedded assets for single-binary deployment.

**Tech Stack:** Go 1.22+, html/template, embed, SSE

**Dependencies:** Phase 6 must be complete (executor)

---

## Task 1: Create Base Template

**Files:**
- Create: `web/templates/layout.html`

**Step 1: Create the base layout**

Create `web/templates/layout.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{block "title" .}}Ralph-o-matic{{end}}</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #1a1a2e;
            color: #eee;
            min-height: 100vh;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
        }
        header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 20px 0;
            border-bottom: 1px solid #333;
            margin-bottom: 20px;
        }
        h1 {
            font-size: 1.5rem;
            color: #fff;
        }
        .nav-links {
            display: flex;
            gap: 20px;
        }
        .nav-links a {
            color: #888;
            text-decoration: none;
        }
        .nav-links a:hover {
            color: #fff;
        }
        .badge {
            background: #333;
            padding: 4px 12px;
            border-radius: 12px;
            font-size: 0.875rem;
        }
        .section {
            margin-bottom: 30px;
        }
        .section-header {
            display: flex;
            align-items: center;
            gap: 10px;
            margin-bottom: 15px;
        }
        .section-title {
            font-size: 0.875rem;
            text-transform: uppercase;
            color: #888;
        }
        .job-card {
            background: #16213e;
            border-radius: 8px;
            padding: 15px;
            margin-bottom: 10px;
            border-left: 4px solid #333;
        }
        .job-card.running { border-left-color: #4ade80; }
        .job-card.paused { border-left-color: #fbbf24; }
        .job-card.queued { border-left-color: #60a5fa; }
        .job-card.completed { border-left-color: #22c55e; }
        .job-card.failed { border-left-color: #ef4444; }
        .job-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 10px;
        }
        .job-id {
            color: #888;
            font-size: 0.875rem;
        }
        .job-branch {
            font-weight: 600;
        }
        .job-progress {
            height: 6px;
            background: #333;
            border-radius: 3px;
            overflow: hidden;
            margin: 10px 0;
        }
        .job-progress-bar {
            height: 100%;
            background: linear-gradient(90deg, #4ade80, #22c55e);
            transition: width 0.3s ease;
        }
        .job-meta {
            display: flex;
            gap: 20px;
            font-size: 0.875rem;
            color: #888;
        }
        .job-actions {
            display: flex;
            gap: 10px;
            margin-top: 10px;
        }
        .btn {
            padding: 6px 12px;
            border-radius: 6px;
            border: none;
            cursor: pointer;
            font-size: 0.875rem;
            transition: background 0.2s;
        }
        .btn-primary {
            background: #3b82f6;
            color: white;
        }
        .btn-primary:hover {
            background: #2563eb;
        }
        .btn-danger {
            background: #ef4444;
            color: white;
        }
        .btn-danger:hover {
            background: #dc2626;
        }
        .btn-secondary {
            background: #333;
            color: #eee;
        }
        .btn-secondary:hover {
            background: #444;
        }
        .priority-high { color: #ef4444; }
        .priority-normal { color: #60a5fa; }
        .priority-low { color: #888; }
        .drag-handle {
            cursor: grab;
            color: #555;
            margin-right: 10px;
        }
        .sortable-ghost {
            opacity: 0.4;
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>Ralph-o-matic</h1>
            <div class="nav-links">
                <a href="/">Dashboard</a>
                <a href="/config">Config</a>
                <span class="badge">Queue: {{.QueueSize}}</span>
            </div>
        </header>

        {{block "content" .}}{{end}}
    </div>

    {{block "scripts" .}}{{end}}
</body>
</html>
```

**Step 2: Commit**

```bash
git add web/templates/layout.html
git commit -m "feat(dashboard): add base HTML layout template"
```

---

## Task 2: Create Dashboard Template

**Files:**
- Create: `web/templates/dashboard.html`

**Step 1: Create dashboard template**

Create `web/templates/dashboard.html`:

```html
{{define "title"}}Dashboard - Ralph-o-matic{{end}}

{{define "content"}}
<!-- Running Jobs -->
{{if .Running}}
<div class="section">
    <div class="section-header">
        <span class="section-title">Running</span>
    </div>
    {{range .Running}}
    <div class="job-card running" data-job-id="{{.ID}}">
        <div class="job-header">
            <div>
                <span class="job-id">#{{.ID}}</span>
                <span class="job-branch">{{.Branch}}</span>
            </div>
            <span>iter {{.Iteration}}/{{.MaxIterations}}</span>
        </div>
        <div class="job-progress">
            <div class="job-progress-bar" style="width: {{printf "%.0f" (multiply .Progress 100)}}%"></div>
        </div>
        <div class="job-meta">
            <span>{{.Prompt | truncate 50}}</span>
            <span>Running {{.Duration | duration}}</span>
        </div>
        <div class="job-actions">
            <button class="btn btn-secondary" onclick="pauseJob({{.ID}})">Pause</button>
            <button class="btn btn-danger" onclick="cancelJob({{.ID}})">Cancel</button>
        </div>
    </div>
    {{end}}
</div>
{{end}}

<!-- Paused Jobs -->
{{if .Paused}}
<div class="section">
    <div class="section-header">
        <span class="section-title">Paused</span>
    </div>
    {{range .Paused}}
    <div class="job-card paused" data-job-id="{{.ID}}">
        <div class="job-header">
            <div>
                <span class="drag-handle">≡</span>
                <span class="job-id">#{{.ID}}</span>
                <span class="job-branch">{{.Branch}}</span>
            </div>
            <span>iter {{.Iteration}}/{{.MaxIterations}}</span>
        </div>
        <div class="job-meta">
            <span>Paused {{.PausedAt | timeago}}</span>
        </div>
        <div class="job-actions">
            <button class="btn btn-primary" onclick="resumeJob({{.ID}})">Resume</button>
            <button class="btn btn-danger" onclick="cancelJob({{.ID}})">Cancel</button>
        </div>
    </div>
    {{end}}
</div>
{{end}}

<!-- Queued Jobs -->
<div class="section">
    <div class="section-header">
        <span class="section-title">Queued</span>
        <span class="badge">{{len .Queued}}</span>
    </div>
    <div id="queued-jobs">
        {{range .Queued}}
        <div class="job-card queued" data-job-id="{{.ID}}">
            <div class="job-header">
                <div>
                    <span class="drag-handle">≡</span>
                    <span class="job-id">#{{.ID}}</span>
                    <span class="job-branch">{{.Branch}}</span>
                </div>
                <span class="priority-{{.Priority}}">{{.Priority}}</span>
            </div>
            <div class="job-meta">
                <span>0/{{.MaxIterations}}</span>
            </div>
            <div class="job-actions">
                <button class="btn btn-danger" onclick="cancelJob({{.ID}})">Cancel</button>
            </div>
        </div>
        {{else}}
        <p style="color: #666; text-align: center; padding: 20px;">No jobs in queue</p>
        {{end}}
    </div>
</div>

<!-- Completed Today -->
{{if .Completed}}
<div class="section">
    <div class="section-header">
        <span class="section-title">Completed Today</span>
    </div>
    {{range .Completed}}
    <div class="job-card {{if eq .Status "completed"}}completed{{else}}failed{{end}}" data-job-id="{{.ID}}">
        <div class="job-header">
            <div>
                <span class="job-id">#{{.ID}}</span>
                <span class="job-branch">{{.Branch}}</span>
            </div>
            <div>
                {{if eq .Status "completed"}}✓ PASSED{{else}}✗ FAILED{{end}}
                <span>{{.Iteration}} iters</span>
            </div>
        </div>
        {{if .PRURL}}
        <div class="job-meta">
            <a href="{{.PRURL}}" target="_blank" style="color: #60a5fa;">View PR</a>
        </div>
        {{end}}
    </div>
    {{end}}
</div>
{{end}}
{{end}}

{{define "scripts"}}
<script src="https://cdn.jsdelivr.net/npm/sortablejs@1.15.0/Sortable.min.js"></script>
<script>
    // Initialize sortable for queued jobs
    new Sortable(document.getElementById('queued-jobs'), {
        animation: 150,
        handle: '.drag-handle',
        ghostClass: 'sortable-ghost',
        onEnd: function(evt) {
            const jobCards = document.querySelectorAll('#queued-jobs .job-card');
            const jobIds = Array.from(jobCards).map(card => parseInt(card.dataset.jobId));
            reorderJobs(jobIds);
        }
    });

    async function pauseJob(id) {
        await fetch(`/api/jobs/${id}/pause`, { method: 'POST' });
        location.reload();
    }

    async function resumeJob(id) {
        await fetch(`/api/jobs/${id}/resume`, { method: 'POST' });
        location.reload();
    }

    async function cancelJob(id) {
        if (confirm('Cancel this job?')) {
            await fetch(`/api/jobs/${id}`, { method: 'DELETE' });
            location.reload();
        }
    }

    async function reorderJobs(jobIds) {
        await fetch('/api/jobs/order', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ job_ids: jobIds })
        });
    }

    // SSE for live updates
    const evtSource = new EventSource('/api/events');
    evtSource.onmessage = function(event) {
        // Simple refresh on any update
        location.reload();
    };
</script>
{{end}}
```

**Step 2: Commit**

```bash
git add web/templates/dashboard.html
git commit -m "feat(dashboard): add dashboard template with job cards"
```

---

## Task 3: Create Job Detail Template

**Files:**
- Create: `web/templates/job.html`

**Step 1: Create job detail template**

Create `web/templates/job.html`:

```html
{{define "title"}}Job #{{.Job.ID}} - Ralph-o-matic{{end}}

{{define "content"}}
<div style="margin-bottom: 20px;">
    <a href="/" style="color: #888; text-decoration: none;">← Back to Dashboard</a>
</div>

<div class="job-card {{.Job.Status}}" style="margin-bottom: 30px;">
    <div class="job-header">
        <div>
            <span class="job-id">#{{.Job.ID}}</span>
            <span class="job-branch">{{.Job.Branch}}</span>
        </div>
        <span class="badge">{{.Job.Status | upper}}</span>
    </div>

    <div style="display: grid; grid-template-columns: repeat(4, 1fr); gap: 20px; margin: 20px 0;">
        <div>
            <div style="color: #888; font-size: 0.75rem; text-transform: uppercase;">Iteration</div>
            <div style="font-size: 1.5rem;">{{.Job.Iteration}}/{{.Job.MaxIterations}}</div>
        </div>
        <div>
            <div style="color: #888; font-size: 0.75rem; text-transform: uppercase;">Priority</div>
            <div class="priority-{{.Job.Priority}}">{{.Job.Priority}}</div>
        </div>
        <div>
            <div style="color: #888; font-size: 0.75rem; text-transform: uppercase;">Started</div>
            <div>{{if .Job.StartedAt}}{{.Job.StartedAt | timeago}}{{else}}Not started{{end}}</div>
        </div>
        <div>
            <div style="color: #888; font-size: 0.75rem; text-transform: uppercase;">Duration</div>
            <div>{{.Job.Duration | duration}}</div>
        </div>
    </div>

    {{if .Job.PRURL}}
    <div style="margin: 15px 0;">
        <a href="{{.Job.PRURL}}" target="_blank" class="btn btn-primary">View Pull Request</a>
    </div>
    {{end}}

    <div class="job-actions">
        {{if eq .Job.Status "running"}}
        <button class="btn btn-secondary" onclick="pauseJob({{.Job.ID}})">Pause</button>
        {{end}}
        {{if eq .Job.Status "paused"}}
        <button class="btn btn-primary" onclick="resumeJob({{.Job.ID}})">Resume</button>
        {{end}}
        {{if not .Job.Status.IsTerminal}}
        <button class="btn btn-danger" onclick="cancelJob({{.Job.ID}})">Cancel</button>
        {{end}}
    </div>
</div>

<!-- Prompt -->
<div class="section">
    <div class="section-header">
        <span class="section-title">Prompt</span>
    </div>
    <div style="background: #0d1117; padding: 15px; border-radius: 8px; font-family: monospace; white-space: pre-wrap; font-size: 0.875rem; max-height: 200px; overflow-y: auto;">{{.Job.Prompt}}</div>
</div>

<!-- Logs -->
<div class="section">
    <div class="section-header">
        <span class="section-title">Logs</span>
    </div>
    <div id="logs" style="background: #0d1117; padding: 15px; border-radius: 8px; font-family: monospace; font-size: 0.75rem; max-height: 400px; overflow-y: auto;">
        {{range .Logs}}
        <div style="margin-bottom: 4px;">
            <span style="color: #888;">[iter {{.Iteration}}]</span>
            <span>{{.Message}}</span>
        </div>
        {{else}}
        <div style="color: #666;">No logs yet</div>
        {{end}}
    </div>
</div>
{{end}}

{{define "scripts"}}
<script>
    async function pauseJob(id) {
        await fetch(`/api/jobs/${id}/pause`, { method: 'POST' });
        location.reload();
    }

    async function resumeJob(id) {
        await fetch(`/api/jobs/${id}/resume`, { method: 'POST' });
        location.reload();
    }

    async function cancelJob(id) {
        if (confirm('Cancel this job?')) {
            await fetch(`/api/jobs/${id}`, { method: 'DELETE' });
            location.href = '/';
        }
    }

    // Auto-scroll logs
    const logsDiv = document.getElementById('logs');
    logsDiv.scrollTop = logsDiv.scrollHeight;

    // SSE for live log updates
    const evtSource = new EventSource('/api/jobs/{{.Job.ID}}/events');
    evtSource.onmessage = function(event) {
        const data = JSON.parse(event.data);
        if (data.type === 'log') {
            const logLine = document.createElement('div');
            logLine.style.marginBottom = '4px';
            logLine.innerHTML = `<span style="color: #888;">[iter ${data.iteration}]</span> <span>${data.message}</span>`;
            logsDiv.appendChild(logLine);
            logsDiv.scrollTop = logsDiv.scrollHeight;
        }
    };
</script>
{{end}}
```

**Step 2: Commit**

```bash
git add web/templates/job.html
git commit -m "feat(dashboard): add job detail template with logs"
```

---

## Task 4: Implement Dashboard Handler

**Files:**
- Create: `internal/dashboard/dashboard.go`
- Create: `internal/dashboard/dashboard_test.go`

**Step 1: Write the tests**

Create `internal/dashboard/dashboard_test.go`:

```go
package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/ryan/ralph-o-matic/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDashboard(t *testing.T) (*Dashboard, *queue.Queue) {
	t.Helper()
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { database.Close() })

	q := queue.New(database)
	d := New(database, q)
	return d, q
}

func TestDashboard_Index(t *testing.T) {
	d, q := newTestDashboard(t)

	// Add some jobs
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	d.HandleIndex(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Ralph-o-matic")
	assert.Contains(t, w.Body.String(), "main") // Branch name
}

func TestDashboard_Job(t *testing.T) {
	d, q := newTestDashboard(t)

	job := models.NewJob("git@github.com:user/repo.git", "feature/test", "test prompt", 10)
	require.NoError(t, q.Enqueue(job))

	req := httptest.NewRequest("GET", "/jobs/1", nil)
	w := httptest.NewRecorder()

	// Note: In real implementation, we'd use chi.URLParam
	d.HandleJob(w, req, job.ID)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "feature/test")
}
```

**Step 2: Write implementation**

Create `internal/dashboard/dashboard.go`:

```go
package dashboard

import (
	"embed"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/ryan/ralph-o-matic/internal/queue"
)

//go:embed templates/*.html
var templatesFS embed.FS

// Dashboard handles web UI requests
type Dashboard struct {
	db        *db.DB
	queue     *queue.Queue
	templates *template.Template
}

// New creates a new dashboard handler
func New(database *db.DB, q *queue.Queue) *Dashboard {
	funcMap := template.FuncMap{
		"truncate": func(s string, n int) string {
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
				return template.HTMLEscapeString(time.Duration(hours)*time.Hour + time.Duration(mins)*time.Minute).String()
			}
			return template.HTMLEscapeString(time.Duration(mins) * time.Minute).String()
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
				return template.HTMLEscapeString(time.Duration(int(d.Minutes()))*time.Minute).String() + " ago"
			}
			return template.HTMLEscapeString(time.Duration(int(d.Hours()))*time.Hour).String() + " ago"
		},
		"multiply": func(a, b float64) float64 {
			return a * b
		},
	}

	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.html"))

	return &Dashboard{
		db:        database,
		queue:     q,
		templates: tmpl,
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

	d.render(w, "layout.html", data)
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

	d.render(w, "job.html", data)
}

func (d *Dashboard) render(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := d.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

**Step 3: Commit**

```bash
git add internal/dashboard/dashboard.go internal/dashboard/dashboard_test.go
git commit -m "feat(dashboard): add dashboard handler with template rendering"
```

---

## Phase 7 Completion Checklist

- [ ] Base HTML layout template
- [ ] Dashboard template with job cards
- [ ] Job detail template with logs
- [ ] Dashboard handler with data loading
- [ ] Template functions (truncate, duration, timeago)
- [ ] Sortable.js integration for drag-drop
- [ ] SSE placeholder for live updates
- [ ] All tests passing
- [ ] All code committed

**Next Phase:** Phase 8 - CLI
