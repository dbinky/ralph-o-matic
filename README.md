# ralph-o-matic

A job queue server that runs iterative AI coding refinement loops — locally via [Ollama](https://ollama.com) or against the Anthropic API — so you can queue work, walk away, and review results as PRs.

## The Problem

Iterating on code until tests pass and acceptance criteria are met produces excellent results, but doing it manually is tedious and doing it against cloud APIs burns credits fast. Most iterations are mechanical refinement, not creative problem-solving.

## The Solution

Draft your implementation with Claude Code + Opus 4.5. Then hand off to ralph-o-matic, which runs the refinement loop with built-in circuit breakers, retry logic, session continuity, and per-iteration commits. Use local models via Ollama to save API credits, or use the Anthropic API with rate limiting when you need cloud-grade quality.

```
Your Dev Env (Opus 4.5)            Ralph-o-Matic Server
┌─────────────────┐               ┌─────────────────────────┐
│ Brainstorm      │  submit job   │ ralph-o-matic-server    │
│ Plan            │──────────────>│ Queue → Execute loop    │
│ Draft           │               │ Commit → Push → PR      │
└─────────────────┘               └─────────────────────────┘
                                           │
                                   Review PR when done
```

## Features

- **Dual backend** — run loops against local Ollama models or the Anthropic API, with per-backend configuration
- **Job queue** with priority scheduling (high/normal/low), pause/resume, drag-and-drop reordering
- **Circuit breaker** — detects no-progress loops and repeated errors, stops wasting compute
- **Session continuity** — resumes Claude sessions across iterations for better context
- **Per-iteration commits** — every successful iteration is committed, so crashes don't lose work
- **Retry with backoff** — transient errors are retried with exponential backoff
- **Rate limiting** — configurable per-hour call limits (essential for Anthropic API, disabled for Ollama)
- **Smart model selection** — detects your hardware (RAM, GPU VRAM, Apple Silicon) and recommends optimal model placement
- **Split-device inference** — run the large model on CPU/RAM and the small model on GPU, or both on GPU if you have the VRAM
- **Remote Ollama support** — point at a remote Ollama instance instead of running locally
- **Web dashboard** with live updates via SSE
- **Git integration** — auto-clones repos, creates result branches, opens PRs on completion
- **Claude Code skill** (`brainstorm-to-ralph`) — end-to-end workflow from idea to queued refinement job
- **Cross-platform** — macOS and Linux, amd64 and arm64

## Quick Start

### Install

```bash
curl -fsSL https://raw.githubusercontent.com/dbinky/ralph-o-matic/main/scripts/install.sh | bash
```

The installer detects your hardware, recommends models, installs dependencies, and starts the server. Use `--yes` for non-interactive mode.

**Installation modes:**

| Mode | What it installs |
|------|-----------------|
| `--mode=full` | Server + CLI + Ollama + models (default) |
| `--mode=server` | Server + Ollama + models only |
| `--mode=client` | CLI only (connects to remote server) |

```bash
# Server-only on a remote machine
curl -fsSL .../install.sh | bash -s -- --mode=server --yes

# Client-only, pointing at your server
curl -fsSL .../install.sh | bash -s -- --mode=client --server=http://192.168.1.50:9090
```

### Submit a Job

```bash
# From a git repo with a RALPH.md prompt file
ralph-o-matic submit

# Or with an inline prompt
ralph-o-matic submit --prompt "Fix the failing tests in auth.go. Exit criteria: all tests pass."

# With options
ralph-o-matic submit --priority high --max-iterations 100 --open-ended

# Use Anthropic API instead of Ollama
ralph-o-matic submit --backend anthropic --prompt "Refactor the auth module"
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

ralph-o-matic ships with a curated catalog of coding models:

| Model | Size | Role | Quality |
|-------|------|------|---------|
| qwen3-coder:70b | 42 GB | large | 10 |
| qwen2.5-coder:32b | 20 GB | large | 8 |
| qwen2.5-coder:14b | 10 GB | large | 6 |
| qwen2.5-coder:7b | 5 GB | large + small | 4 |
| qwen2.5-coder:1.5b | 1.5 GB | small | 2 |

The installer recommends a (large, small) pairing based on your hardware:

- **64 GB RAM + 24 GB GPU** → 70b on CPU + 7b on GPU (split)
- **48 GB Apple Silicon** → 32b + 7b both on GPU (unified memory)
- **16 GB RAM, no GPU** → 14b + 1.5b both on CPU
- **8 GB RAM** → 7b + 1.5b both on CPU

## Configuration

Server config lives at `~/.config/ralph-o-matic/config.yaml` and is editable via the API or CLI:

```bash
ralph-o-matic server-config                        # View current config
ralph-o-matic server-config set large_model.name qwen2.5-coder:32b
ralph-o-matic server-config set ollama.host http://remote:11434
```

Key settings:

| Setting | Default | Description |
|---------|---------|-------------|
| `ollama.host` | `http://localhost:11434` | Ollama server URL |
| `ollama.is_remote` | `false` | Skip local model management |
| `large_model.name` | `qwen3-coder:70b` | Primary coding model |
| `large_model.device` | `cpu` | Where to run it (`cpu`, `gpu`, `auto`) |
| `small_model.name` | `qwen2.5-coder:7b` | Fast model for simple tasks |
| `small_model.device` | `gpu` | Where to run it |
| `concurrent_jobs` | `1` | Parallel job limit |
| `default_max_iterations` | `50` | Default iteration cap |
| `job_retention_days` | `30` | Days to keep completed jobs |

## Claude Code Integration

Install the `brainstorm-to-ralph` skill for end-to-end workflows:

```
/brainstorm-to-ralph "Add user authentication with OAuth"
```

This walks through brainstorming, planning, and drafting locally with Opus 4.5, then submits the refinement work to ralph-o-matic automatically.

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
| `GET` | `/health` | Health check |

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
  cli/              CLI entry point (cobra)
internal/
  api/              REST API server (chi)
  cli/              CLI client logic
  dashboard/        Web UI (Go templates, SSE)
  db/               SQLite persistence
  executor/         Claude subprocess, response parsing, circuit breaker, session management
  git/              Git/GitHub operations
  models/           Core data types, per-backend loop config
  worker/           Job execution loop, retry, rate limiting
  platform/         Hardware detection, model catalog, Ollama client, selection algorithm
  queue/            Priority job queue with state machine
scripts/
  install.sh        Interactive installer (macOS/Linux)
  install.ps1       Interactive installer (Windows)
skills/
  brainstorm-to-ralph/   Claude Code skill
web/
  templates/        Dashboard HTML
  static/           CSS/JS
```

## License

MIT - https://www.youtube.com/watch?v=PsQzRZyWidk
