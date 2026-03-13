# ralph-o-matic

A job queue server that runs iterative AI coding refinement loops — locally via [Ollama](https://ollama.com), against the Anthropic API, or through [OpenRouter](https://openrouter.ai) — so you can queue work, walk away, and review results as PRs.

## The Problem

Iterating on code until tests pass and acceptance criteria are met produces excellent results, but doing it manually is tedious and doing it against cloud APIs burns credits fast. Most iterations are mechanical refinement, not creative problem-solving.

## The Solution

Draft your implementation with Claude Code + Opus. Then hand off to ralph-o-matic, which runs the refinement loop with built-in circuit breakers, retry logic, session continuity, and per-iteration commits. Use local models via Ollama to save API credits, use the Anthropic API when you need Claude-grade quality, or use OpenRouter for access to models from multiple providers on a pay-per-token basis.

```
Your Dev Env (Opus 4.6)            Ralph-o-Matic Server
┌─────────────────┐               ┌─────────────────────────┐
│ Brainstorm      │  submit job   │ ralph-o-matic-server    │
│ Plan            │──────────────>│ Queue → Execute loop    │
│ Draft           │               │ Commit → Push → PR      │
└─────────────────┘               └─────────────────────────┘
                                          │
                                  Review PR when done
```

## Features

- **Three backends** — run loops against local Ollama models, the Anthropic API (via Claude Code), or OpenRouter (Kimi, Grok, Devstral, and more)
- **Job queue** with priority scheduling (high/normal/low), pause/resume, drag-and-drop reordering
- **Circuit breaker** — detects no-progress loops and repeated errors, stops wasting compute
- **Session continuity** — resumes Claude sessions across iterations for better context
- **Per-iteration commits** — every successful iteration is committed, so crashes don't lose work
- **Retry with backoff** — transient errors are retried with exponential backoff
- **Rate limiting** — configurable per-hour call limits (essential for Anthropic API, disabled for Ollama)
- **Smart model selection** — detects your hardware (RAM, GPU VRAM, Apple Silicon) and recommends optimal model placement
- **Split-device inference** — run the large model on CPU/RAM and the small model on GPU, or both on GPU if you have the VRAM
- **Remote Ollama support** — point at a remote Ollama instance instead of running locally
- **Notifications** — email (SMTP) and Microsoft Teams webhook notifications on job completion, failure, or cancellation
- **Authentication** — optional Microsoft Entra ID (Azure AD) SSO with role-based access control
- **Web dashboard** with live updates via SSE
- **Git integration** — auto-clones repos, creates result branches, opens PRs on completion
- **Claude Code skills** — `brainstorm-to-ralph` for end-to-end idea-to-job workflows, `direct-to-ralph` for submitting ready work without brainstorming, `plan-to-ralph` for generating loop configuration files through guided Q&A
- **Cross-platform** — macOS, Linux, and Windows; amd64 and arm64

## Quick Start

### Install (macOS / Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/dbinky/ralph-o-matic/main/scripts/install.sh | bash
```

### Install (Windows PowerShell)

```powershell
irm https://raw.githubusercontent.com/dbinky/ralph-o-matic/main/scripts/install.ps1 | iex
```

Both installers detect your hardware, let you choose a backend, recommend models, install dependencies, and start the server. Use `--yes` (bash) or `-Yes` (PowerShell) for non-interactive mode.

### Installation Modes

| Mode | What it installs |
|------|-----------------|
| `full` (default) | Server + CLI + backend dependencies + models |
| `server` | Server + backend dependencies + models only |
| `client` | CLI only (connects to a remote server) |

**Bash examples:**

```bash
# Server-only on a remote machine
curl -fsSL .../install.sh | bash -s -- --mode=server --yes

# Client-only, pointing at your server
curl -fsSL .../install.sh | bash -s -- --mode=client --server=http://192.168.1.50:9090

# Non-interactive with Anthropic backend
curl -fsSL .../install.sh | bash -s -- --yes --backend=anthropic
```

**PowerShell examples:**

```powershell
# Download and run with flags
$script = irm https://raw.githubusercontent.com/dbinky/ralph-o-matic/main/scripts/install.ps1
& ([scriptblock]::Create($script)) -Mode client -Server http://192.168.1.50:9090

# Or save and run
irm .../install.ps1 -OutFile install.ps1
.\install.ps1 -Yes -Backend anthropic
```

### Choosing a Backend

The installer prompts you to choose one of three backends:

| Backend | When to use |
|---------|------------|
| **Ollama** (default) | Free, private, runs on your hardware. Requires 16+ GB RAM. |
| **Anthropic** | Uses your Claude Code subscription or API credits. No local hardware needed. |
| **OpenRouter** | Pay-per-token access to models from multiple providers (Kimi, Grok, Devstral, etc.). Requires an API key from [openrouter.ai](https://openrouter.ai). |

### Submit a Job

```bash
# From a git repo with a RALPH.md prompt file
ralph-o-matic submit

# Or with an inline prompt
ralph-o-matic submit --prompt "Fix the failing tests in auth.go. Exit criteria: all tests pass."

# With options
ralph-o-matic submit --priority high --max-iterations 100 --open-ended

# Use a specific backend
ralph-o-matic submit --backend anthropic --prompt "Refactor the auth module"
ralph-o-matic submit --backend openrouter --prompt "Add input validation"
```

### Monitor

```bash
ralph-o-matic status              # Queue overview
ralph-o-matic status <job-id>     # Job details
ralph-o-matic logs <job-id>       # View logs
```

Or open the dashboard at `http://<server-ip>:9090`.

### Control Jobs

```bash
ralph-o-matic pause <job-id>      # Pause (preserves iteration state)
ralph-o-matic resume <job-id>     # Resume from where it left off
ralph-o-matic cancel <job-id>     # Cancel
ralph-o-matic move <job-id> --first  # Move to front of queue
```

## Model Catalog

### Ollama (Local Models)

ralph-o-matic ships with a curated catalog of coding models:

| Model | Size | Role | Quality | Notes |
|-------|------|------|---------|-------|
| devstral | 15 GB | large | 9 | SWE-bench #1 open source (24B dense) |
| qwen3-coder:30b | 19 GB | large | 8 | Best open coding with tool use (30B MoE) |
| qwen3:14b | 9.3 GB | large | 6 | Tool use capable (14B dense) |
| qwen3:8b | 5.2 GB | large + small | 4 | Minimum viable (8B dense) |
| qwen3:4b | 2.5 GB | small | 2 | Lightweight helper only |

The installer recommends a (large, small) pairing based on your hardware:

- **48+ GB Apple Silicon** — devstral + qwen3:8b on GPU (unified memory)
- **32 GB Apple Silicon** — qwen3-coder:30b + qwen3:8b on GPU
- **64 GB RAM + GPU** — devstral on CPU + qwen3:8b on GPU (split)
- **16 GB RAM, no GPU** — qwen3:14b + qwen3:8b on CPU
- **8 GB RAM** — qwen3:8b + qwen3:4b on CPU

### Anthropic Models

| Model | Role | Notes |
|-------|------|-------|
| claude-opus-4-6 | large | Most capable, slower, higher cost |
| claude-sonnet-4-6-20260218 | large + small (default) | Fast and capable |
| claude-haiku-4-5-20251001 | small | Fastest, lowest cost |

### OpenRouter Models

| Model | ID | Role |
|-------|-----|------|
| Kimi K2.5 | `moonshotai/kimi-k2.5` | large (default) |
| Grok 4.1 Fast | `x-ai/grok-4-1-fast` | large or small |
| Devstral 2 2512 | `mistralai/devstral-2-2512` | small (default) |

You can also enter any model ID available on [OpenRouter](https://openrouter.ai/models) during installation.

## Configuration

Server config is managed via the API and CLI:

```bash
ralph-o-matic server-config                        # View current config
ralph-o-matic server-config set large_model.name qwen3-coder:30b
ralph-o-matic server-config set ollama.host http://remote:11434
```

### Ollama Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `ollama.host` | `http://localhost:11434` | Ollama server URL |
| `ollama.is_remote` | `false` | Skip local model management |
| `large_model.name` | `devstral` | Primary coding model |
| `large_model.device` | `cpu` | Where to run it (`cpu`, `gpu`, `auto`) |
| `small_model.name` | `qwen3:8b` | Fast model for simple tasks |
| `small_model.device` | `gpu` | Where to run it |

### Anthropic Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `default_backend` | `ollama` | Active backend (`ollama`, `anthropic`, `openrouter`) |
| `anthropic.large_model` | `claude-sonnet-4-6-20260218` | Primary coding model |
| `anthropic.small_model` | `claude-sonnet-4-6-20260218` | Fast model for simple tasks |

Authentication is handled by Claude Code's built-in auth — no API key needed.

### OpenRouter Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `openrouter.api_key` | | Your OpenRouter API key (from [openrouter.ai/keys](https://openrouter.ai/keys)) |
| `openrouter.base_url` | `https://openrouter.ai/api/v1` | API endpoint (must be HTTPS for non-localhost) |
| `openrouter.large_model` | `moonshotai/kimi-k2.5` | Primary coding model |
| `openrouter.small_model` | `mistralai/devstral-2-2512` | Fast model for simple tasks |

### General Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `default_max_iterations` | `50` | Default iteration cap |
| `job_retention_days` | `30` | Days to keep completed jobs |

### Switching Backends

You can switch the active backend at any time via the API:

```bash
# Switch to OpenRouter
ralph-o-matic server-config set default_backend openrouter

# Switch to Anthropic
ralph-o-matic server-config set default_backend anthropic

# Switch back to Ollama
ralph-o-matic server-config set default_backend ollama
```

## Notifications

ralph-o-matic can notify you when jobs complete, fail, or are cancelled via email (SMTP) or Microsoft Teams webhooks.

**Configure during install** — the installer prompts for notification settings and sends a test message to verify the setup works.

**Configure manually:**

```bash
# SMTP email
ralph-o-matic server-config set notify.smtp.host smtp.example.com
ralph-o-matic server-config set notify.smtp.port 587
ralph-o-matic server-config set notify.smtp.username user@example.com
ralph-o-matic server-config set notify.smtp.password secret
ralph-o-matic server-config set notify.smtp.from ralph@example.com
ralph-o-matic server-config set notify.smtp.recipients dev@example.com,team@example.com

# Teams webhook
ralph-o-matic server-config set notify.teams.webhook_url https://outlook.office.com/webhook/...

# Verify configuration
ralph-o-matic test-notify smtp
ralph-o-matic test-notify teams
```

## Authentication

Authentication is optional. By default, the server runs with no auth (suitable for trusted networks).

For production deployments, ralph-o-matic supports Microsoft Entra ID (Azure AD) SSO:

```bash
export RALPH_AUTH_MODE=entra
export RALPH_ENTRA_TENANT_ID=your-tenant-id
export RALPH_ENTRA_CLIENT_ID=your-client-id
export RALPH_ENTRA_CLIENT_SECRET=your-client-secret
```

Admin-only endpoints (like `test-notify`) require the admin role.

## Claude Code Skills

The installer sets up two Claude Code skills that integrate directly with ralph-o-matic:

### `/brainstorm-to-ralph`

End-to-end workflow from idea to queued refinement job. Walks through brainstorming, planning, and drafting locally with Opus, then submits the work to ralph-o-matic for iterative refinement.

```
/brainstorm-to-ralph "Add user authentication with OAuth"
/brainstorm-to-ralph "Refactor the payment module" --max-iterations 100
```

### `/direct-to-ralph`

Skip brainstorming and planning — submit work directly when you already have code or a clear task for ralph to refine.

```
/direct-to-ralph "Fix all failing tests in the auth package"
/direct-to-ralph "Improve test coverage to 80%" --spec docs/coverage-plan.md
```

### `/plan-to-ralph`

Generate ralph loop files (RALPH.md, focus-areas.md, gaps-identified.md) through interactive Q&A. Scans your codebase to suggest focus areas, generates a review persona, and produces ready-to-use loop configuration.

```
/plan-to-ralph
/plan-to-ralph "on the new authentication system"
/plan-to-ralph --reset
```

## API

The server exposes a REST API at `http://<host>:9090/api/`:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/jobs` | List jobs (filter with `?status=queued`) |
| `POST` | `/api/jobs` | Submit a new job |
| `GET` | `/api/jobs/:id` | Get job details |
| `DELETE` | `/api/jobs/:id` | Cancel a job |
| `POST` | `/api/jobs/:id/pause` | Pause a running job |
| `POST` | `/api/jobs/:id/resume` | Resume a paused job |
| `PUT` | `/api/jobs/order` | Reorder queue |
| `GET` | `/api/config` | Get server config |
| `PATCH` | `/api/config` | Update server config (partial) |
| `POST` | `/api/config/test-notify` | Send test notification (admin only) |
| `GET` | `/health` | Health check |

## Deploying for a Team

See the [Operations & Deployment Guide](docs/ops-guide.md) for reverse proxy setup, TLS termination, backup, monitoring, and production hardening.

## Development

Requires Go 1.24+.

```bash
make deps          # Download dependencies
make build         # Build server + CLI
make test          # Run unit tests with race detector
make test-all      # Unit tests + BATS installer tests
make lint          # Run golangci-lint
make build-all     # Cross-compile for all platforms
make release       # Build all + package skill
```

## Architecture

```
cmd/
  server/           HTTP server entry point
  cli/              CLI entry point (cobra)
internal/
  api/              REST API server (chi)
  auth/             Authentication (none, Entra ID SSO), RBAC, sessions
  cli/              CLI client logic
  dashboard/        Web UI (Go templates, SSE)
  db/               SQLite persistence
  executor/         Claude subprocess, response parsing, circuit breaker, session management
  git/              Git/GitHub operations
  models/           Core data types, per-backend loop config
  notify/           Notification dispatcher (SMTP, Teams webhooks)
  worker/           Job execution loop, retry, rate limiting
  platform/         Hardware detection, model catalog, Ollama client, selection algorithm
  queue/            Priority job queue with state machine
scripts/
  install.sh        Interactive installer (macOS/Linux)
  install.ps1       Interactive installer (Windows)
  tests/            BATS tests for install script
skills/
  brainstorm-to-ralph/   Idea → brainstorm → plan → draft → ralph
  direct-to-ralph/       Submit ready work directly to ralph
  plan-to-ralph/         Generate loop files via guided Q&A
web/
  templates/        Dashboard HTML
  static/           CSS/JS
```

## License

MIT - https://www.youtube.com/watch?v=PsQzRZyWidk
