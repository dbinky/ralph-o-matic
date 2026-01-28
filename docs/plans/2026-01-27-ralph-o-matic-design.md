# Ralph-o-matic: Distributed AI Development Pipeline

## Overview

Ralph-o-matic is a distributed job queue system that orchestrates AI-powered iterative development workflows. It enables developers to draft implementations using Claude Code with Opus 4.5 on their local machine, then hand off to a dedicated server running local LLMs (via Ollama) for cost-free iterative refinement using the ralph-wiggum loop technique.

### Problem Statement

The ralph-wiggum loop is a powerful technique for achieving iterative convergence on solutions—feeding the same prompt repeatedly while the AI sees its previous work in files and git history. However, this brute-force approach is expensive when using cloud APIs. Developers need a way to:

1. Use premium models (Opus 4.5) for creative, architectural work
2. Offload iterative grinding to free local models
3. Queue multiple refinement jobs without babysitting
4. Review results via standard PR workflows

### Solution

Ralph-o-matic provides:

1. **A job queue server** that runs on dedicated hardware (e.g., a gaming PC with GPU)
2. **A CLI** for submitting jobs and managing the queue
3. **A Claude Code skill** (`brainstorm-to-ralph`) that orchestrates the entire workflow from ideation to PR

### Core Workflow

```
Developer's Mac (Opus 4.5)          Gaming PC (Ollama)
┌─────────────────────────┐         ┌─────────────────────────┐
│                         │         │                         │
│  1. Brainstorm idea     │         │  Ralph-o-matic Server   │
│  2. Design doc          │         │  ├── Job Queue          │
│  3. Phase plans         │         │  ├── Ollama             │
│  4. Beads task DAG      │         │  │   ├── qwen2.5-coder  │
│  5. Parallel execution  │         │  │   └── qwen3-coder    │
│  6. Commit & push       │         │  └── Claude Code CLI    │
│         │               │         │         │               │
│         └───────────────┼────────►│         ▼               │
│           Submit job    │   API   │  Ralph loop executes    │
│                         │         │  until tests pass       │
│                         │         │         │               │
│         ◄───────────────┼─────────┼─────────┘               │
│           PR created    │   Git   │                         │
│                         │         │                         │
└─────────────────────────┘         └─────────────────────────┘
```

---

## System Architecture

### Components

| Component | Technology | Platform | Purpose |
|-----------|------------|----------|---------|
| `ralph-o-matic-server` | Go + SQLite | Cross-platform | Job queue, executor, dashboard, REST API |
| `ralph-o-matic` CLI | Go | Cross-platform | Submit jobs, manage queue, view logs |
| `brainstorm-to-ralph` skill | Claude Code skill | Claude Code | End-to-end orchestration from idea to submission |
| Install scripts | Bash + PowerShell | macOS/Linux/Windows | "It just works" installation |

### Server Architecture

```
ralph-o-matic/
├── cmd/
│   ├── server/
│   │   └── main.go                 # Server entry point
│   └── cli/
│       └── main.go                 # CLI entry point
├── internal/
│   ├── api/
│   │   ├── server.go               # HTTP server setup, middleware, routing
│   │   ├── jobs.go                 # Job CRUD handlers
│   │   ├── config.go               # Config handlers
│   │   └── middleware.go           # Logging, recovery, CORS
│   ├── dashboard/
│   │   ├── dashboard.go            # HTML dashboard handler
│   │   ├── assets.go               # Embedded static assets
│   │   └── sse.go                  # Server-sent events for live updates
│   ├── queue/
│   │   ├── queue.go                # Priority queue implementation
│   │   ├── worker.go               # Job executor goroutine
│   │   └── scheduler.go            # Job scheduling logic
│   ├── executor/
│   │   ├── ralph.go                # Ralph loop execution
│   │   ├── claude.go               # Claude Code subprocess management
│   │   └── process.go              # Process lifecycle management
│   ├── git/
│   │   ├── git.go                  # Git operations wrapper
│   │   ├── gh.go                   # GitHub CLI wrapper
│   │   └── clone.go                # Repository cloning logic
│   ├── db/
│   │   ├── sqlite.go               # SQLite connection and migrations
│   │   ├── jobs.go                 # Job persistence
│   │   ├── config.go               # Config persistence
│   │   └── migrations/             # SQL migration files
│   ├── models/
│   │   ├── job.go                  # Job struct and validation
│   │   └── config.go               # Config struct
│   └── platform/
│       ├── paths.go                # OS-specific path resolution
│       ├── paths_windows.go        # Windows path implementation
│       ├── paths_unix.go           # Unix path implementation
│       ├── process.go              # OS-specific process management
│       ├── process_windows.go      # Windows process implementation
│       └── process_unix.go         # Unix process implementation
├── web/
│   ├── templates/
│   │   ├── layout.html             # Base layout
│   │   ├── dashboard.html          # Main dashboard
│   │   ├── job.html                # Job detail view
│   │   └── config.html             # Config panel
│   └── static/
│       ├── css/
│       └── js/
├── scripts/
│   ├── install.sh                  # Bash installer
│   └── install.ps1                 # PowerShell installer
├── go.mod
├── go.sum
└── Makefile
```

### Runtime Dependencies (Server Machine)

- **Ollama** (running, with models pulled)
- **Claude Code CLI** (with ralph-wiggum plugin installed)
- **gh CLI** (authenticated via `gh auth login`)
- **Git**

### Network Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Server Machine (e.g., Gaming PC)                               │
│                                                                 │
│  ┌─────────────────┐    ┌─────────────────┐                    │
│  │  Ollama         │    │  ralph-o-matic  │                    │
│  │  :11434         │◄───│  server         │                    │
│  │                 │    │  :9090          │◄──── HTTP API      │
│  │  - qwen2.5:7b   │    │                 │◄──── Dashboard     │
│  │  - qwen3:70b    │    └────────┬────────┘                    │
│  └─────────────────┘             │                             │
│                                  │ spawns                      │
│                                  ▼                             │
│                        ┌─────────────────┐                     │
│                        │  Claude Code    │                     │
│                        │  (subprocess)   │                     │
│                        └─────────────────┘                     │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
         ▲
         │ LAN (no auth, port 9090)
         ▼
┌─────────────────────────────────────────────────────────────────┐
│  Developer Machine (e.g., MacBook)                              │
│                                                                 │
│  ┌─────────────────┐    ┌─────────────────┐                    │
│  │  Claude Code    │    │  ralph-o-matic  │                    │
│  │  + Opus 4.5     │    │  CLI            │                    │
│  │  + brainstorm-  │───►│                 │────► Server API    │
│  │    to-ralph     │    │                 │                    │
│  └─────────────────┘    └─────────────────┘                    │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## Data Models

### Job

```go
type Job struct {
    ID              int64           `json:"id"`
    Status          JobStatus       `json:"status"`
    Priority        Priority        `json:"priority"`
    Position        int             `json:"position"`

    // Repository info
    RepoURL         string          `json:"repo_url"`
    Branch          string          `json:"branch"`
    ResultBranch    string          `json:"result_branch"`
    WorkingDir      string          `json:"working_dir,omitempty"`

    // Execution config
    Prompt          string          `json:"prompt"`
    MaxIterations   int             `json:"max_iterations"`
    Env             map[string]string `json:"env,omitempty"`

    // Progress tracking
    Iteration       int             `json:"iteration"`
    RetryCount      int             `json:"retry_count"`

    // Timestamps
    CreatedAt       time.Time       `json:"created_at"`
    StartedAt       *time.Time      `json:"started_at,omitempty"`
    PausedAt        *time.Time      `json:"paused_at,omitempty"`
    CompletedAt     *time.Time      `json:"completed_at,omitempty"`

    // Results
    PRURL           string          `json:"pr_url,omitempty"`
    Error           string          `json:"error,omitempty"`
}

type JobStatus string

const (
    StatusQueued    JobStatus = "queued"
    StatusRunning   JobStatus = "running"
    StatusPaused    JobStatus = "paused"
    StatusCompleted JobStatus = "completed"
    StatusFailed    JobStatus = "failed"
    StatusCancelled JobStatus = "cancelled"
)

type Priority string

const (
    PriorityHigh   Priority = "high"
    PriorityNormal Priority = "normal"
    PriorityLow    Priority = "low"
)
```

### Job State Machine

```
                    ┌─────────┐
                    │ queued  │◄─────────────────────┐
                    └────┬────┘                      │
                         │ start                     │
                         ▼                           │
              ┌─────────────────────┐                │
         ┌───►│      running        │────┐           │
         │    └──────────┬──────────┘    │           │
         │               │               │           │
         │ resume        │ pause         │           │
         │               ▼               │           │
         │    ┌─────────────────────┐    │           │
         └────│      paused         │    │           │
              └─────────────────────┘    │           │
                                         │           │
         ┌───────────────┬───────────────┼───────────┘
         │               │               │ (cancel from any state)
         ▼               ▼               ▼
    ┌─────────┐    ┌─────────┐    ┌───────────┐
    │completed│    │ failed  │    │ cancelled │
    └─────────┘    └─────────┘    └───────────┘
```

### Server Config

```go
type ServerConfig struct {
    // Models
    LargeModel      string `json:"large_model"`       // default: "qwen3-coder:70b"
    SmallModel      string `json:"small_model"`       // default: "qwen2.5-coder:7b"

    // Execution
    DefaultMaxIterations int `json:"default_max_iterations"` // default: 50
    ConcurrentJobs       int `json:"concurrent_jobs"`        // default: 1

    // Storage
    WorkspaceDir    string `json:"workspace_dir"`     // OS-dependent default
    JobRetentionDays int   `json:"job_retention_days"` // default: 30

    // Retry behavior
    MaxClaudeRetries     int `json:"max_claude_retries"`     // default: 3
    MaxGitRetries        int `json:"max_git_retries"`        // default: 3
    GitRetryBackoffMs    int `json:"git_retry_backoff_ms"`   // default: 1000
}
```

### CLI Config

```yaml
# ~/.config/ralph-o-matic/config.yaml
server: http://192.168.1.50:9090
default_priority: normal
default_max_iterations: 50
```

---

## REST API

### Base URL

`http://<server-ip>:9090/api`

### Endpoints

#### Jobs

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/jobs` | Submit a new job |
| `GET` | `/jobs` | List jobs (filterable) |
| `GET` | `/jobs/:id` | Get job details |
| `DELETE` | `/jobs/:id` | Cancel job |
| `GET` | `/jobs/:id/logs` | Get job logs |
| `POST` | `/jobs/:id/pause` | Pause running job |
| `POST` | `/jobs/:id/resume` | Resume paused job |
| `PATCH` | `/jobs/:id` | Update job properties |
| `PUT` | `/jobs/order` | Bulk reorder queue |

#### Config

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/config` | Get server config |
| `PATCH` | `/config` | Update server config |

### Request/Response Examples

#### Submit Job

**Request:**
```http
POST /api/jobs
Content-Type: application/json

{
    "repo_url": "git@github.com:ryan/myproject.git",
    "branch": "feature/auth-refactor",
    "prompt": "You are completing a feature to production-ready quality.\n\nSpecification: docs/plans/2026-01-27-auth-design.md\n\nEach iteration:\n1. Read the spec (every time - don't assume you remember it)\n2. Run tests to see current state\n3. Identify the single highest-impact gap between current state and spec\n4. Fix it\n5. Run tests again to verify\n\nThe code was drafted by another agent and may be incomplete or have bugs.\nDo not trust it. Verify everything against the spec.\n\nWhen tests pass AND the spec is fully satisfied, output:\n<promise>COMPLETE</promise>\n\nIf tests don't exist for a requirement, write them first.",
    "max_iterations": 50,
    "working_dir": "packages/auth",
    "env": {
        "NODE_ENV": "test",
        "DEBUG": "true"
    },
    "priority": "high"
}
```

**Response:**
```http
HTTP/1.1 201 Created
Content-Type: application/json

{
    "id": 47,
    "status": "queued",
    "priority": "high",
    "position": 1,
    "repo_url": "git@github.com:ryan/myproject.git",
    "branch": "feature/auth-refactor",
    "result_branch": "ralph/feature/auth-refactor-result",
    "working_dir": "packages/auth",
    "prompt": "...",
    "max_iterations": 50,
    "env": {"NODE_ENV": "test", "DEBUG": "true"},
    "iteration": 0,
    "retry_count": 0,
    "created_at": "2026-01-27T10:30:00Z",
    "started_at": null,
    "paused_at": null,
    "completed_at": null,
    "pr_url": null,
    "error": null
}
```

#### List Jobs

**Request:**
```http
GET /api/jobs?status=queued,running&limit=20
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "jobs": [...],
    "total": 45,
    "limit": 20,
    "offset": 0
}
```

#### Pause Job

**Request:**
```http
POST /api/jobs/47/pause
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "id": 47,
    "status": "paused",
    "iteration": 27,
    "paused_at": "2026-01-27T11:45:00Z",
    ...
}
```

#### Reorder Queue

**Request:**
```http
PUT /api/jobs/order
Content-Type: application/json

{
    "job_ids": [49, 47, 52, 48]
}
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "reordered": [49, 47, 52, 48]
}
```

---

## Web Dashboard

### URL

`http://<server-ip>:9090/`

### Features

1. **Queue Overview** - All jobs grouped by status with drag-and-drop reordering
2. **Job Detail View** - Logs, iteration history, controls
3. **Config Panel** - Edit server settings
4. **Live Updates** - SSE for real-time progress

### Dashboard Layout

```
╔══════════════════════════════════════════════════════════════════════╗
║  Ralph-o-matic                               [Config ⚙]  [Queue: 3]  ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║  ▶ RUNNING                                                           ║
║  ┌─────────────────────────────────────────────────────────────────┐ ║
║  │ #47 feature/auth-refactor         iter 27/50   ████████░░ 54%   │ ║
║  │     "Make all auth tests pass"                  Running 1h 12m  │ ║
║  │                                          [Pause ⏸] [Cancel ✕]   │ ║
║  └─────────────────────────────────────────────────────────────────┘ ║
║                                                                      ║
║  ⏸ PAUSED                                                           ║
║  ┌─────────────────────────────────────────────────────────────────┐ ║
║  │ ≡ #51 fix/memory-leak              iter 12/30   Paused 2h ago   │ ║
║  │                                          [Resume ▶] [Cancel ✕]  │ ║
║  └─────────────────────────────────────────────────────────────────┘ ║
║                                                                      ║
║  ⏳ QUEUED                                                [+ Add]    ║
║  ┌─────────────────────────────────────────────────────────────────┐ ║
║  │ ≡ #48 feature/cache-layer          0/50     ⚡ high   [Cancel]  │ ║
║  │ ≡ #49 fix/token-refresh            0/50     • normal [Cancel]  │ ║
║  │ ≡ #52 feature/logging              0/25     ○ low    [Cancel]  │ ║
║  └─────────────────────────────────────────────────────────────────┘ ║
║        ↑ drag ≡ handles to reorder                                   ║
║                                                                      ║
║  ✓ COMPLETED TODAY                                       [View all]  ║
║  ┌─────────────────────────────────────────────────────────────────┐ ║
║  │ #46 feature/user-profile    ✓ PASSED   8 iters   PR #123   1h   │ ║
║  │ #45 fix/login-bug           ✗ FAILED   50 iters  PR #122   3h   │ ║
║  └─────────────────────────────────────────────────────────────────┘ ║
║                                                                      ║
╚══════════════════════════════════════════════════════════════════════╝
```

### Job Detail View

```
╔══════════════════════════════════════════════════════════════════════╗
║  ← Back    Job #47: feature/auth-refactor              [Pause] [✕]   ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║  Status: RUNNING        Iteration: 27/50        Started: 1h 12m ago  ║
║  Priority: high         Branch: ralph/feature/auth-refactor-result   ║
║                                                                      ║
║  ┌─ Prompt ────────────────────────────────────────────────────────┐ ║
║  │ You are completing a feature to production-ready quality.       │ ║
║  │ Specification: docs/plans/2026-01-27-auth-design.md             │ ║
║  │ ...                                                             │ ║
║  └─────────────────────────────────────────────────────────────────┘ ║
║                                                                      ║
║  ┌─ Live Log ──────────────────────────────────────────────────────┐ ║
║  │ [iter 27] Reading spec file...                                  │ ║
║  │ [iter 27] Running test suite...                                 │ ║
║  │ [iter 27] 23/25 tests passing                                   │ ║
║  │ [iter 27] Identified gap: missing error handling in logout      │ ║
║  │ [iter 27] Editing src/auth/logout.ts...                         │ ║
║  │ █                                                               │ ║
║  └─────────────────────────────────────────────────────────────────┘ ║
║                                                                      ║
║  ┌─ Iteration History ─────────────────────────────────────────────┐ ║
║  │ #27  in progress   Working on error handling                    │ ║
║  │ #26  ✓ complete    Fixed token validation         abc1234       │ ║
║  │ #25  ✓ complete    Added missing test case        def5678       │ ║
║  │ #24  ✓ complete    Fixed import error             ghi9012       │ ║
║  │ ...                                                             │ ║
║  └─────────────────────────────────────────────────────────────────┘ ║
║                                                                      ║
╚══════════════════════════════════════════════════════════════════════╝
```

### Technology

- Server-rendered HTML with Go templates
- Minimal JavaScript for drag-drop (Sortable.js) and live updates (SSE)
- No heavy frontend framework - keep it simple

---

## CLI

### Binary

`ralph-o-matic` (cross-platform: macOS, Linux, Windows)

### Commands

```
ralph-o-matic submit [OPTIONS]
    Submit a new job to the queue

    Options:
      --prompt TEXT           Prompt text (overrides RALPH.md)
      --priority LEVEL        Priority: high, normal, low (default: normal)
      --max-iterations N      Max iterations (default: 50)
      --working-dir PATH      Subdirectory to work in
      --open-ended            Use polish prompt without exit criteria

    Prompt resolution order:
      1. --prompt argument
      2. {working_dir}/RALPH.md
      3. {repo_root}/RALPH.md

ralph-o-matic status [JOB_ID]
    Show queue overview or specific job details

ralph-o-matic logs JOB_ID [OPTIONS]
    View job logs

    Options:
      --follow, -f            Stream logs in real-time

ralph-o-matic cancel JOB_ID
    Cancel a queued or running job

ralph-o-matic pause JOB_ID
    Pause a running job (preserves iteration state)

ralph-o-matic resume JOB_ID
    Resume a paused job

ralph-o-matic move JOB_ID [OPTIONS]
    Move job in queue

    Options:
      --position N            Move to specific position
      --after JOB_ID          Move after another job
      --first                 Move to front of queue

ralph-o-matic config [set KEY VALUE]
    Show or set local CLI configuration

ralph-o-matic server-config [set KEY VALUE]
    Show or set remote server configuration
```

### Output Examples

**Submit:**
```
$ ralph-o-matic submit --priority high

Submitting job...
  Repository:    git@github.com:ryan/myproject.git
  Branch:        feature/auth-refactor
  Prompt:        RALPH.md (1,247 chars)
  Max iterations: 50
  Priority:      high

✓ Job #52 queued (position: 1)

Dashboard: http://192.168.1.50:9090/jobs/52
```

**Status:**
```
$ ralph-o-matic status

Ralph-o-matic Queue
═══════════════════

▶ RUNNING
  #47 feature/auth-refactor    iter 27/50    ████████░░ 54%    1h 12m

⏸ PAUSED
  #51 fix/memory-leak          iter 12/30                      paused 2h ago

⏳ QUEUED (3)
  #48 feature/cache-layer      ⚡ high
  #49 fix/token-refresh        • normal
  #52 feature/logging          ○ low

Dashboard: http://192.168.1.50:9090
```

---

## brainstorm-to-ralph Skill

### Purpose

End-to-end orchestration from initial idea to ralph loop submission. Automates the entire development workflow after the interactive brainstorming phase.

### Command

```
/brainstorm-to-ralph "Add user authentication"
```

### Options

```
--max-iterations N      Max iterations for ralph loop (default: 50)
--priority LEVEL        Job priority: high, normal, low (default: normal)
--open-ended            Use polish prompt without exit criteria
```

### Workflow Phases

#### Phase 1: Brainstorm (INTERACTIVE)

- Invoke `superpowers:brainstorming`
- Interactive Q&A with user to refine the idea
- Explore approaches, discuss tradeoffs
- Output: `docs/plans/YYYY-MM-DD-{topic}-design.md`
- Commit design document

#### Phase 2: Plan (AUTOMATIC)

- Invoke `superpowers:writing-plans`
- Break design into implementation phases
- Output:
  - `docs/plans/YYYY-MM-DD-{topic}-design-phase-1.md`
  - `docs/plans/YYYY-MM-DD-{topic}-design-phase-2.md`
  - `docs/plans/YYYY-MM-DD-{topic}-design-phase-3.md`
  - (etc.)
- Commit plan documents

#### Phase 3: Beads Setup (AUTOMATIC)

- Initialize Beads if needed (`bd init`)
- Create parent tasks for each phase
- Create sub-tasks from plan documents
- Map dependencies between tasks (blocks/blocked-by)
- Result: Full task DAG ready for parallel execution
- Commit `.beads/` directory

Example Beads structure:
```
Phase 1: Database Schema [PHASE-1]
├── Create users table [PHASE-1-1]
├── Create sessions table [PHASE-1-2] (blocked by PHASE-1-1)
└── Add indexes [PHASE-1-3] (blocked by PHASE-1-1, PHASE-1-2)

Phase 2: API Endpoints [PHASE-2] (blocked by PHASE-1)
├── POST /auth/register [PHASE-2-1]
├── POST /auth/login [PHASE-2-2]
└── POST /auth/logout [PHASE-2-3]

Phase 3: Frontend Integration [PHASE-3] (blocked by PHASE-2)
├── Login form component [PHASE-3-1]
├── Registration form [PHASE-3-2]
└── Session management [PHASE-3-3]
```

#### Phase 4: Execute (AUTOMATIC, PARALLEL)

- Spawn parallel subagents (one per phase document)
- Each agent:
  1. Consults Beads (`bd list --ready`) before starting
  2. Executes `superpowers:executing-plans {phase-doc}`
  3. Updates Beads as tasks complete (`bd done {task-id}`)
  4. If blocked by tasks in other agents, polls Beads until clear
  5. Consults Beads after completing to verify
- Wait for all agents to complete

Agent instructions include:
```
Before starting any work, run `bd list --ready` to see unblocked tasks.
After completing a task, run `bd done {task-id}`.
If you need work from another phase that isn't complete, run `bd list --blocked`
and wait, polling every 30 seconds until the blocking task is done.
```

#### Phase 5: Ship (AUTOMATIC)

- Commit all implementation changes
- Push to origin
- Generate ralph prompt from design document
- Submit to ralph-o-matic server
- Report job ID and dashboard URL

### Prompt Generation

The skill generates context-aware prompts based on the design document.

**Standard prompt (bounded, with exit criteria):**
```
You are completing a feature to production-ready quality.

Specification: docs/plans/2026-01-27-auth-design.md

Each iteration:
1. Read the spec (every time - don't assume you remember it)
2. Run tests to see current state
3. Identify the single highest-impact gap between current state and spec
4. Fix it
5. Run tests again to verify

The code was drafted by another agent and may be incomplete or have bugs.
Do not trust it. Verify everything against the spec.

When tests pass AND the spec is fully satisfied, output:
<promise>COMPLETE</promise>

If tests don't exist for a requirement, write them first.
```

**Open-ended prompt (unbounded, for polishing):**
```
Polish this feature to production quality.

Specification: docs/plans/2026-01-27-auth-design.md

Each iteration: run tests, find the worst problem, fix it.

Do not output a <promise> tag. Continue improving until stopped.
```

### Pre-flight Checks (Submit Phase)

Before submitting to ralph-o-matic, the skill verifies:

1. **Working tree clean** - No uncommitted changes
2. **Branch pushed to origin** - Server can clone it
3. **Tests exist** - Don't submit if no tests to pass
4. **Prompt available** - Either from design doc or RALPH.md
5. **Server reachable** - Ping the server
6. **Branch not in queue** - Prevent duplicate submissions

```
Pre-flight checks:
  ✓ Working tree clean
  ✓ Branch 'feature/auth-refactor' pushed to origin
  ✓ Tests found (23 test files)
  ✓ Prompt generated from design doc (1,247 chars)
  ✓ Server reachable (192.168.1.50:9090)
  ✓ Branch not in queue

Submitting job...
  ✓ Job #52 queued (priority: normal)

Queue position: 3rd
Dashboard: http://192.168.1.50:9090
```

---

## Job Execution

### Ralph Loop Execution

The server executes ralph loops by shelling out to Claude Code:

```bash
cd /workspace/job-47/myproject

ANTHROPIC_BASE_URL=http://localhost:11434 \
ANTHROPIC_AUTH_TOKEN=ollama \
ANTHROPIC_API_KEY="" \
ANTHROPIC_MODEL=qwen3-coder:70b \
ANTHROPIC_DEFAULT_HAIKU_MODEL=qwen2.5-coder:7b \
NODE_ENV=test \
DEBUG=true \
claude --prompt "$(cat prompt.txt)"
```

The ralph-wiggum plugin handles the looping behavior via Claude Code hooks.

### Git Workflow

1. **Clone**: `gh repo clone {repo_url} -- --branch {branch}`
2. **Create result branch**: `git checkout -b ralph/{branch}-result`
3. **Work**: Claude makes commits as it works
4. **Push**: `git push -u origin ralph/{branch}-result`
5. **Create PR**: `gh pr create --base {branch} --head ralph/{branch}-result`

### PR Creation

On completion (success or failure):

**Success:**
```
Title: Ralph-o-matic: feature/auth-refactor ✓
Body:
  ## Summary
  Completed in 8 iterations. All tests passing.

  ## Changes
  - Fixed token validation logic
  - Added error handling for expired sessions
  - Wrote 5 new test cases

  ## Specification
  See: docs/plans/2026-01-27-auth-design.md
```

**Failure (max iterations reached):**
```
Title: Ralph-o-matic: feature/auth-refactor ✗ FAILED
Body:
  ## Summary
  Reached max iterations (50) without completing. Tests still failing.

  ## Current State
  - 23/25 tests passing
  - Remaining issues: logout error handling, session cleanup

  ## Specification
  See: docs/plans/2026-01-27-auth-design.md

  ## Notes
  Manual intervention may be needed. Review iteration history for context.
```

### Failure Handling

| Scenario | Behavior |
|----------|----------|
| Max iterations reached | Mark failed, create PR with failure notes |
| Claude crashes | Auto-retry up to 3 times, then mark failed |
| Git/GitHub errors | Retry with exponential backoff (3 attempts), then mark failed and preserve workspace |

---

## Configuration

### Two-Mode Paths

The server supports two path modes for flexibility:

**User mode** (default when run manually):
- Config: `~/.config/ralph-o-matic/config.yaml`
- Data: `~/.config/ralph-o-matic/data/`
- Workspace: `~/.config/ralph-o-matic/workspace/`

**System mode** (`--system` flag or `RALPH_SYSTEM=1`):

| | Windows | macOS | Linux |
|---|---------|-------|-------|
| Config | `C:\ProgramData\ralph-o-matic\` | `/Library/Application Support/ralph-o-matic/` | `/etc/ralph-o-matic/` |
| Data | `C:\ProgramData\ralph-o-matic\` | `/var/lib/ralph-o-matic/` | `/var/lib/ralph-o-matic/` |
| Workspace | `C:\ProgramData\ralph-o-matic\workspace\` | `/var/lib/ralph-o-matic/workspace/` | `/var/lib/ralph-o-matic/workspace/` |
| Logs | `C:\ProgramData\ralph-o-matic\logs\` | `/var/log/ralph-o-matic/` | `/var/log/ralph-o-matic/` |

### Server Config Defaults

```yaml
large_model: qwen3-coder:70b
small_model: qwen2.5-coder:7b
default_max_iterations: 50
concurrent_jobs: 1
workspace_dir: <os-dependent>
job_retention_days: 30
max_claude_retries: 3
max_git_retries: 3
git_retry_backoff_ms: 1000
```

### CLI Config

```yaml
# ~/.config/ralph-o-matic/config.yaml
server: http://192.168.1.50:9090
default_priority: normal
default_max_iterations: 50
```

---

## Installation

### Philosophy

**"It just works."** Single command installation that detects the platform, checks dependencies, installs what's missing, and configures everything.

### One-Liner Install

**macOS/Linux:**
```bash
curl -fsSL https://ralph-o-matic.dev/install.sh | bash
```

**Windows (PowerShell):**
```powershell
irm https://ralph-o-matic.dev/install.ps1 | iex
```

### Interactive Installer Flow

```
╔══════════════════════════════════════════════════════════════════╗
║                     Ralph-o-matic Installer                      ║
╚══════════════════════════════════════════════════════════════════╝

What would you like to install?

  [1] Server + Client (full setup for running jobs locally)
  [2] Server only (this machine will run ralph loops)
  [3] Client only (submit jobs to a remote server)

> 1

Detected: macOS 14.2 (arm64), 24GB RAM

Checking dependencies...
  ✓ git 2.43.0
  ✓ gh 2.42.0 (authenticated)
  ✗ ollama (not installed)
  ✗ claude-code (not installed)
  ✗ bd (not installed)

Install missing dependencies? [Y/n] y

Installing ollama...                    ✓
Installing claude-code...               ✓
Installing bd (beads)...                ✓

Pulling Ollama models (this may take a while)...
  qwen2.5-coder:7b                      ✓ (4.7 GB)
  qwen3-coder:70b                       ✓ (40 GB)

Installing Claude Code plugins...
  ralph-wiggum                          ✓
  brainstorm-to-ralph                   ✓

Installing ralph-o-matic...
  Server binary                         ✓
  CLI binary                            ✓

Configuration:
  Server will listen on:                http://192.168.1.50:9090
  Data directory:                       ~/.config/ralph-o-matic/

Start server now? [Y/n] y
  Server started                        ✓

╔══════════════════════════════════════════════════════════════════╗
║                    Installation Complete!                        ║
╠══════════════════════════════════════════════════════════════════╣
║                                                                  ║
║  Dashboard:     http://192.168.1.50:9090                        ║
║                                                                  ║
║  Quick start:                                                    ║
║    claude                                                        ║
║    /brainstorm-to-ralph "Add user authentication"               ║
║                                                                  ║
║  Commands:                                                       ║
║    ralph-o-matic status        # Check queue                     ║
║    ralph-o-matic logs <id>     # View job logs                   ║
║                                                                  ║
╚══════════════════════════════════════════════════════════════════╝
```

### Non-Interactive Flags

```bash
# Full auto, no prompts
curl -fsSL https://ralph-o-matic.dev/install.sh | bash -s -- --yes --mode=full

# Server only, specific models
curl -fsSL https://ralph-o-matic.dev/install.sh | bash -s -- --mode=server --large-model=qwen3-coder:30b

# Client only, point to existing server
curl -fsSL https://ralph-o-matic.dev/install.sh | bash -s -- --mode=client --server=http://192.168.1.50:9090
```

### Script Structure

Both `install.sh` and `install.ps1` follow the same structure:

```
install.sh / install.ps1
├── detect_platform()           # OS, arch, RAM
├── prompt_mode()               # Server, client, or both
├── check_dependencies()
│   ├── git
│   ├── gh (+ auth status)
│   ├── ollama
│   ├── claude-code
│   └── bd
├── install_missing()
│   ├── macOS: brew install ...
│   ├── Linux: apt/dnf/pacman ...
│   └── Windows: winget install ...
├── pull_models()               # Only if server mode
├── install_plugins()           # Claude Code plugins
├── install_binaries()          # Download and install ralph-o-matic
├── configure()                 # Set up config files
├── start_server()              # Optional: start server now
├── verify_installation()       # Smoke test
└── print_success()             # Summary and next steps
```

### Build Matrix

The Makefile produces binaries for all platforms:

```makefile
# Server builds
build-server-darwin-arm64
build-server-darwin-amd64
build-server-linux-amd64
build-server-linux-arm64
build-server-windows-amd64

# CLI builds
build-cli-darwin-arm64
build-cli-darwin-amd64
build-cli-linux-amd64
build-cli-linux-arm64
build-cli-windows-amd64

# All
build-all: build-server-all build-cli-all
```

---

## Testing Strategy

### Philosophy

**Strict TDD.** Tests written before implementation. Every component tested at unit and integration level.

### Scope

Server, CLI, and install scripts. Skill excluded (tested through use).

### Server Testing (Go)

#### Unit Tests

| Package | What's Tested |
|---------|---------------|
| `internal/queue` | Priority ordering, job state transitions, pause/resume logic |
| `internal/db` | CRUD operations, query filters, migrations |
| `internal/git` | Command building, output parsing, error handling |
| `internal/executor` | Process spawning, timeout handling, log capture |
| `internal/api` | Request validation, response formatting, error responses |
| `internal/platform` | Path resolution per OS, process management per OS |

#### Test Scenarios per Package

```
queue/
├── happy_path_test.go
│   ├── TestEnqueueJob
│   ├── TestDequeueByPriority
│   ├── TestJobProgression
│   └── TestPauseResumeFlow
├── failure_test.go
│   ├── TestEnqueueInvalidJob
│   ├── TestDequeueEmptyQueue
│   ├── TestTransitionFromInvalidState
│   └── TestPauseNonRunningJob
├── edge_cases_test.go
│   ├── TestReorderSingleJob
│   ├── TestReorderEmptyQueue
│   ├── TestPauseAlreadyPausedJob
│   ├── TestConcurrentEnqueue
│   └── TestMaxQueueSize
└── error_test.go
    ├── TestDatabaseConnectionLost
    ├── TestCorruptedJobData
    └── TestRecoveryAfterCrash
```

#### Integration Tests

```
integration/
├── job_lifecycle_test.go
│   ├── TestSubmitToCompletion
│   ├── TestSubmitPauseResumeComplete
│   ├── TestSubmitCancelCleanup
│   ├── TestSubmitFailureRetry
│   └── TestSubmitMaxIterationsReached
├── api_test.go
│   ├── TestFullCRUDFlow
│   ├── TestReorderingAPI
│   ├── TestConcurrentAPIRequests
│   └── TestAPIErrorResponses
├── git_integration_test.go
│   ├── TestClonePushPRFlow
│   ├── TestPRCreationOnFailure
│   ├── TestGitAuthFailure
│   └── TestBranchConflictHandling
└── executor_integration_test.go
    ├── TestClaudeCodeExecution
    ├── TestIterationCounting
    ├── TestGracefulPause
    └── TestCrashRecovery
```

### CLI Testing (Go)

#### Unit Tests

```
cli/
├── config_test.go
│   ├── TestLoadConfig
│   ├── TestSaveConfig
│   ├── TestMissingConfig
│   ├── TestInvalidConfig
│   └── TestConfigMergeWithFlags
├── commands_test.go
│   ├── TestSubmitArgParsing
│   ├── TestStatusFormatting
│   ├── TestMoveValidation
│   └── TestPromptResolution
└── output_test.go
    ├── TestProgressBar
    ├── TestColorOutput
    ├── TestJSONOutput
    └── TestTableFormatting
```

#### Integration Tests

```
cli_integration/
├── submit_test.go
│   ├── TestSubmitToRealServer
│   ├── TestSubmitServerUnreachable
│   ├── TestSubmitInvalidRepo
│   └── TestSubmitWithAllOptions
└── roundtrip_test.go
    ├── TestSubmitStatusCancel
    ├── TestSubmitPauseMoveResume
    └── TestConfigRoundtrip
```

### Install Script Testing

#### Unit Tests

Using **bats** for bash, **Pester** for PowerShell:

```
install_tests/
├── detect_test.sh / detect_test.ps1
│   ├── TestDetectMacOS
│   ├── TestDetectMacOSIntel
│   ├── TestDetectUbuntu
│   ├── TestDetectFedora
│   ├── TestDetectWindows
│   ├── TestDetectRAM
│   └── TestDetectInsufficientRAM
├── dependencies_test.sh / dependencies_test.ps1
│   ├── TestGitDetection
│   ├── TestGitMissing
│   ├── TestGhAuthCheck
│   ├── TestGhNotAuthenticated
│   ├── TestOllamaDetection
│   └── TestOllamaVersionParse
└── install_test.sh / install_test.ps1
    ├── TestBinaryDownload
    ├── TestBinaryDownloadFailure
    ├── TestConfigCreation
    ├── TestPathSetup
    └── TestUpgradeExisting
```

#### Integration Tests (Containerized/VM)

```
install_integration/
├── macos_arm64_clean.sh          # Fresh macOS ARM VM
├── macos_intel_clean.sh          # Fresh macOS Intel VM
├── ubuntu_22_clean.sh            # Docker: ubuntu:22.04
├── ubuntu_24_clean.sh            # Docker: ubuntu:24.04
├── fedora_39_clean.sh            # Docker: fedora:39
├── debian_12_clean.sh            # Docker: debian:12
├── windows_11_clean.ps1          # Fresh Windows 11 VM
├── windows_server_clean.ps1      # Windows Server 2022 VM
└── upgrade_existing.sh           # Test upgrade from previous version
```

### Test Infrastructure

#### CI Pipeline

```yaml
name: CI

on: [push, pull_request]

jobs:
  unit-tests:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - run: go test ./... -short -race

  install-script-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: sudo apt-get install -y bats
      - run: bats scripts/tests/

  integration-tests:
    runs-on: ubuntu-latest
    services:
      ollama:
        image: ollama/ollama:latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: go test ./... -tags=integration

  e2e-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: ./scripts/e2e_full_workflow.sh
```

#### Coverage Targets

| Component | Line Coverage | Branch Coverage |
|-----------|---------------|-----------------|
| Server | 90%+ | 85%+ |
| CLI | 90%+ | 85%+ |
| Install scripts | 80%+ | 75%+ |

#### Test Commands

```bash
# Run all unit tests
make test-unit

# Run integration tests (requires running Ollama)
make test-integration

# Run install script tests
make test-install

# Run e2e tests
make test-e2e

# Run all tests
make test-all

# Check coverage
make coverage
```

---

## Security Considerations

### Network Security

- Server binds to LAN IP only (not 0.0.0.0)
- No authentication (trusted home network)
- No TLS (local network)

### Git Security

- Uses existing `gh auth` credentials
- No credentials stored by ralph-o-matic
- SSH keys managed by user's ssh-agent

### Process Security

- Claude Code runs as the same user as the server
- Workspace directories have standard user permissions
- Environment variables passed to Claude may contain secrets (user's responsibility)

---

## Future Considerations (Out of Scope)

The following features were discussed but explicitly deferred (YAGNI):

1. **Automatic PR review** - Watch for completed jobs and trigger review on developer's Mac
2. **Push notifications** - ntfy.sh or similar for job completion alerts
3. **Multi-user support** - Authentication and job ownership
4. **Cloud deployment** - Running the server on cloud infrastructure
5. **Model fine-tuning** - Custom models trained on user's codebase

These can be added later if needed.

---

## Glossary

| Term | Definition |
|------|------------|
| **Ralph loop** | Iterative development technique where the same prompt is fed repeatedly, with the AI seeing its previous work in files |
| **ralph-wiggum** | Claude Code plugin that implements the ralph loop via hooks |
| **Beads** | Steve Yegge's task management system for AI agents, using Git as storage |
| **Ollama** | Local LLM runtime that can serve models with OpenAI/Anthropic-compatible APIs |
| **Phase document** | Implementation plan for one phase of a feature (e.g., `*-phase-1.md`) |
| **Design document** | High-level specification produced during brainstorming |
| **Promise tag** | `<promise>COMPLETE</promise>` - marker that signals successful completion to the ralph loop |

---

## References

- [Ollama Anthropic API Compatibility](https://docs.ollama.com/api/anthropic-compatibility)
- [Ollama Claude Code Integration](https://docs.ollama.com/integrations/claude-code)
- [Claude Code Model Configuration](https://code.claude.com/docs/en/model-config)
- [Beads - Steve Yegge](https://github.com/steveyegge/beads)
- [The Ralph Wiggum Technique - Geoffrey Huntley](https://ghuntley.com/ralph/)
- [qwen3-coder on Ollama](https://ollama.com/library/qwen3-coder)
