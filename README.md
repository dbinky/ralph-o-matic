# ralph-o-matic

A job queue server that runs iterative AI coding refinement loops — locally via [Ollama](https://ollama.com), against the Anthropic API, or through [OpenRouter](https://openrouter.ai) — so you can queue work, walk away, and review results as PRs.

## The Problem

Iterating on code until tests pass and acceptance criteria are met produces excellent results, but doing it manually is tedious and doing it against cloud APIs burns credits fast. Most iterations are mechanical refinement, not creative problem-solving.

## The Solution

Draft your implementation with Claude Code + Opus. Then hand off to ralph-o-matic (ROM), which runs the refinement loop with built-in circuit breakers, retry logic, session continuity, and per-iteration commits. Use local models via Ollama to save API credits, use the Anthropic API when you need Claude-grade quality, or use OpenRouter for access to models from multiple providers on a pay-per-token basis.

## The Feature Pipeline

The `/feature-pipeline` skill (from the [dbinky-skill-set](https://github.com/dbinky/dbinky-skill-set) plugin) automates the full lifecycle of building a feature — from idea to merged PR. You answer brainstorming questions, then walk away. Everything else runs unattended: design alignment, planning, implementation, refinement, and PR review. You wake up to a finished pull request.

```
Your Dev Env (Claude Code + Opus)              ROM Server
┌─────────────────────────────────┐            ┌───────────────────────────┐
│ /feature-pipeline               │            │ ralph-o-matic-server      │
│                                 │            │                           │
│ 1. Brainstorm product spec   ◄─ you         │                           │
│ 2. Brainstorm impl. design   ◄─ you         │                           │
│    ─── walk away ───────────────│────────    │                           │
│ 3. Align designs to spec        │ auto       │                           │
│ 4. Write implementation plans   │ auto       │                           │
│ 5. Align plans to each other    │ auto       │                           │
│ 6. Draft implementation         │ auto       │                           │
│ 7. Generate ralph loop files    │ auto       │                           │
│ 8. Submit to ROM ───────────────│───────────►│ Queue → Execute loop      │
│                                 │            │   Commit → Push → PR      │
└─────────────────────────────────┘            │                           │
                                               │ 9. Post-completion hook   │
 You get a Teams notification ◄────────────────│    → Auto PR review       │
 Review the PR, merge it                       │    → Teams notification   │
                                               └───────────────────────────┘
```

### How it works

| Step | What happens | You involved? |
|------|-------------|---------------|
| 1 | **Brainstorm product spec** — interactive Q&A about what you want to build, outputs `docs/specs/{slug}-spec.md` | Yes |
| 2 | **Brainstorm implementation design** — interactive Q&A about how to build it, outputs `docs/superpowers/specs/{slug}-design-phase-*.md` | Yes |
| — | *You walk away. Everything below is unattended.* | — |
| 3 | **Align designs to spec** — reads spec and all design docs, fixes contradictions, commits | No |
| 4 | **Write implementation plans** — generates task files for each design phase | No |
| 5 | **Align plans** — cross-checks plans against spec/designs and each other, fixes gaps | No |
| 6 | **Draft implementation** — spawns parallel agents to execute plan tasks, runs tests, commits | No |
| 7 | **Generate ralph loop files** — auto-derives RALPH.md, focus-areas.md, gaps-identified.md from spec/design/plan artifacts | No |
| 8 | **Submit to ROM** — pre-flight checks, commit, push, submit job to ralph-o-matic server | No |
| 9 | **Ralph refinement loop** — ROM iterates: review a focus area, fix the most important issue, run tests, commit, repeat | No (server) |
| 10 | **Post-completion hook** — triggers automated PR review, applies fixes, sends Teams notification with PR link | No (server) |

### Invoking the pipeline

```bash
/feature-pipeline Here's what I want to build: a user authentication system
  with OAuth2, session management, and role-based access control.
  It should support Google and GitHub as identity providers.
```

**Flags:**
- `--slug user-auth` — override auto-derived feature slug
- `--max-iterations 200` — override ralph iteration count (default: 200)
- `--priority high` — override ralph priority (default: high)
- `--spec-only` — stop after brainstorming (skip the automated pipeline)

### Teams notifications

The pipeline sends Teams notifications at each milestone so you can monitor progress without watching a terminal:

| When | Notification |
|------|-------------|
| Automation begins (after step 2) | "Pipeline running unattended for `{slug}` on branch `{branch}`" |
| Any automated step fails | "Pipeline failed at `{phase}`. Error: `{msg}`. Resume: `{command}`" |
| Submitted to ROM | "Ralph loop started for `{slug}` — Job #{id}, {N} iterations" |
| Ralph completes | "PR ready for review: `{url}`" |
| Ralph or PR review fails | Error details with resume instructions |

Every failure notification includes the exact command to resume from that point.

### The refinement loop

When ROM picks up a job, it runs Claude Code in a structured iteration loop. Each iteration:

1. **Reads the tracking file** — picks the next incomplete focus area from `docs/reference/focus-areas.md`
2. **Deep-reads the code** — examines every file in that focus area
3. **Analyzes and logs gaps** — cross-references against the design doc, adds issues to `docs/reference/gaps-identified.md`
4. **Fixes the single most important issue** — focused, testable improvement
5. **Runs the test suite** — investigates and fixes any failures
6. **Assesses a checklist** — all tests pass, no open issues, code is ship-ready
7. **Commits and pushes** — every iteration is committed, so crashes don't lose work
8. **Outputs a promise tag** — `FINIT` (all focus areas complete) or `CLOSER` (more work to do)

Single focus areas require 2 review passes (correctness, then robustness). Paired focus areas (integration seams between components) require 1 pass. The loop continues until all areas are complete or the iteration cap is reached.

## Composable Skills

The feature pipeline chains together independently-invocable skills. You can use any of them standalone depending on where you are in your workflow.

### Skill overview

```
feature-pipeline (master orchestrator)
  ├── superpowers:brainstorming    ← interactive (spec)
  ├── superpowers:brainstorming    ← interactive (design)
  ├── spec-to-design               ← automated steps 3–6
  ├── auto-ralph-prep              ← automated step 7
  └── auto-ralph-submit            ← automated step 8
                                      ↓
                                ralph-o-matic server
                                (refinement loop, overnight)
                                      ↓
                                post-completion hook
                                      ↓
                                /pr-review (automated)
```

### Full automation

| Skill | Purpose | When to use |
|-------|---------|-------------|
| `/feature-pipeline` | End-to-end: brainstorm → implement → refine → PR review | Starting a new feature from scratch |
| `/spec-to-design` | Steps 3–6: align designs, write plans, align plans, implement | You already have a spec and design docs |

### Loop file generation

| Skill | Purpose | When to use |
|-------|---------|-------------|
| `/auto-ralph-prep` | Auto-generate RALPH.md + tracking files from spec/design/plan artifacts (no Q&A) | Automated pipeline or when you have complete docs |
| `/plan-to-ralph` | Generate loop files via interactive 7-question Q&A | You want to hand-tune the loop configuration |
| `/plan-to-ralph --auto` | Same as `/auto-ralph-prep` (delegates to it) | Convenience alias |

### Submission

| Skill | Purpose | When to use |
|-------|---------|-------------|
| `/auto-ralph-submit` | Non-interactive submission with pre-flight checks, auto-commit, auto-push, Teams notification | Automated pipeline or known-good state |
| `/direct-to-ralph` | Interactive submission — prompts for missing details, generates RALPH.md if needed | You have code ready and want to submit manually |
| `/brainstorm-to-ralph` | End-to-end: brainstorm → plan → implement → submit | Similar to feature-pipeline but without the alignment/review steps |

### Installing the plugins

```bash
claude plugin install dbinky/ralph-o-matic         # ROM skills (brainstorm-to-ralph, plan-to-ralph, direct-to-ralph)
claude plugin install dbinky/dbinky-skill-set       # Pipeline skills (feature-pipeline, spec-to-design, auto-ralph-prep, auto-ralph-submit)
```

## Features

- **Feature pipeline** — `/feature-pipeline` automates the entire workflow from idea to merged PR. You interact during brainstorming, then walk away.
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
- **Post-completion hooks** — run a shell command when jobs finish (e.g., trigger automated PR review via Claude Code)
- **Notifications** — email (SMTP) and Microsoft Teams webhook notifications on job completion, failure, or cancellation. Skills can send arbitrary messages via `ralph-o-matic notify`.
- **Authentication** — optional Microsoft Entra ID (Azure AD) SSO with role-based access control
- **Web dashboard** with live updates via SSE
- **Git integration** — auto-clones repos, creates result branches, opens PRs on completion
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

Each installer resolves the version to install at runtime by querying the GitHub releases API, so piping `install.sh` or `install.ps1` from `main` always lands on the newest release. If the API is unreachable the installer falls back to a baked-in version. Pin a specific build with `--version=X.Y.Z` (bash) or `-Version X.Y.Z` (PowerShell).

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

# Pin to a specific release
curl -fsSL .../install.sh | bash -s -- --version=0.7.3
```

**PowerShell examples:**

```powershell
# Download and run with flags
$script = irm https://raw.githubusercontent.com/dbinky/ralph-o-matic/main/scripts/install.ps1
& ([scriptblock]::Create($script)) -Mode client -Server http://192.168.1.50:9090

# Or save and run
irm .../install.ps1 -OutFile install.ps1
.\install.ps1 -Yes -Backend anthropic

# Pin to a specific release
.\install.ps1 -Version 0.7.3
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
| `post_completion_command` | | Shell command to run when jobs complete (see [Post-Completion Hooks](#post-completion-hooks)) |

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

**Send arbitrary messages** from scripts or skills:

```bash
ralph-o-matic notify --message "Pipeline started for user-auth on branch dev-user-auth"
ralph-o-matic notify "Deployment complete"  # positional arg also works
```

## Post-Completion Hooks

Run a shell command automatically when ralph jobs finish. The command receives job metadata as environment variables.

**Configure:**

```bash
# Auto-trigger PR review when jobs complete
ralph-o-matic server-config set post_completion_command \
  'claude --print -p "Run /pr-review on the PR at $RALPH_PR_URL. Apply all suggested fixes except those ranked Defer. Commit and push the results."'
```

**Environment variables available to the hook:**

| Variable | Description |
|----------|-------------|
| `RALPH_JOB_ID` | Job ID |
| `RALPH_REPO_URL` | Repository URL |
| `RALPH_BRANCH` | Source branch |
| `RALPH_RESULT_BRANCH` | Result branch name |
| `RALPH_PR_URL` | Pull request URL (empty if failed) |
| `RALPH_WORKING_DIR` | Working directory (empty if clone mode) |
| `RALPH_EXIT_STATUS` | `completed` or `failed` |

The hook runs asynchronously — it doesn't block the worker from picking up the next job. If the hook fails, a Teams notification is sent (if configured). Hook failure does not change the job's status.

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
| `POST` | `/api/config/notify` | Send message to all enabled notification channels |
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
make release       # Build all + validate plugin + package skills
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
  worker/           Job execution loop, retry, rate limiting, post-completion hooks
  platform/         Hardware detection, model catalog, Ollama client, selection algorithm
  queue/            Priority job queue with state machine
.claude-plugin/
  plugin.json       Plugin manifest (name, version, metadata)
scripts/
  install.sh        Interactive installer (macOS/Linux)
  install.ps1       Interactive installer (Windows)
  tests/            BATS tests for install script

# Skills in dbinky/ralph-o-matic plugin:
skills/
  brainstorm-to-ralph/   Idea → brainstorm → plan → draft → ralph
  direct-to-ralph/       Submit ready work directly to ralph
  plan-to-ralph/         Generate loop files via guided Q&A or auto-derive (--auto)

# Skills in dbinky/dbinky-skill-set plugin:
  feature-pipeline/      Full automation: brainstorm → implement → ralph → PR review
  spec-to-design/        Automated steps 3–6: align → plan → align → implement
  auto-ralph-prep/       Auto-generate loop files from spec/designs (no Q&A)
  auto-ralph-submit/     Non-interactive ralph submission with pre-flight checks
  ralph-plan/            Automate ralph loop setup from branch changes or user scope

web/
  templates/        Dashboard HTML
  static/           CSS/JS
```

## License

MIT - https://www.youtube.com/watch?v=PsQzRZyWidk
