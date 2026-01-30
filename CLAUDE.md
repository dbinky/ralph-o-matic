# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
make build              # Build server + CLI to build/
make test               # Unit tests with race detector (go test -v -short -race ./...)
make lint               # golangci-lint (v2 config in .golangci.yml)
make test-bats          # BATS shell tests for install script (scripts/tests/)
make test-all           # Unit + BATS tests
make build-all          # Cross-compile: darwin/linux/windows × amd64/arm64
make release            # Cross-compile + package Claude Code skill
```

Run a single test:
```bash
go test -v -run TestJobRepo_Create ./internal/db/
```

## Project Overview

Go module: `github.com/ryan/ralph-o-matic` (Go 1.24.12)

ralph-o-matic is a job queue server that runs iterative AI coding refinement loops using local LLMs via Ollama, freeing cloud API credits. You brainstorm/plan with Claude Code + Opus, then hand off mechanical refinement to ralph-o-matic running on a machine with local models.

## Architecture

**Entry points:**
- `cmd/server/main.go` — HTTP server on `:9090` (env: `RALPH_ADDR`, `RALPH_DB`)
- `cmd/cli/main.go` — Cobra CLI (`submit`, `status`, `logs`, `cancel`, `pause`, `resume`, `move`, `config`, `server-config`)

**Job flow:** CLI submits → `POST /api/jobs` → `queue.Enqueue()` → `executor.RalphHandler.Handle()` → clones repo via git/gh → creates result branch → runs `claude --print` subprocess with Ollama env vars → streams logs to DB → commits, pushes, creates PR via `gh pr create`

**Key packages:**
- `internal/api` — Chi router, REST endpoints, CORS, dashboard routes. Middleware: Logger, Recoverer, Timeout(60s).
- `internal/queue` — Priority-based job scheduling (high/normal/low), position-based reordering, status state machine (queued → running → completed/failed, with pause/resume/cancel)
- `internal/executor` — `RalphHandler` orchestrates the loop; `ClaudeExecutor` manages the subprocess. Ollama integration via env vars (`ANTHROPIC_BASE_URL`, `ANTHROPIC_MODEL`, `ANTHROPIC_DEFAULT_HAIKU_MODEL`)
- `internal/db` — Pure Go SQLite (`modernc.org/sqlite`). Embedded SQL migrations in `internal/db/migrations/`. Repos: `JobRepo`, `ConfigRepo`, `LogRepo`
- `internal/git` — Git CLI wrapper + GitHub CLI wrapper. Tries `gh repo clone` first, falls back to `git clone`. Auto-creates `ralph/{branch}-result` branches.
- `internal/models` — Domain types: `Job`, `JobStatus`, `Priority`, `ServerConfig`, `ModelPlacement`
- `internal/platform` — Hardware detection (RAM, GPU/VRAM, Apple Silicon unified memory), model catalog, selection algorithm for optimal (large, small) model pairing
- `internal/dashboard` — Template-based web UI with SSE live updates. Templates embedded via `web/embed.go`
- `internal/cli` — HTTP client wrapper for API, config file management (`~/.config/ralph-o-matic/config.yaml`)

**Database:** SQLite with custom migration runner. Tables: `jobs`, `config` (key-value), `job_logs`. Test helper `newTestDB(t)` creates in-memory DBs.

**Install script** (`scripts/install.sh`): Detects hardware, recommends models, installs deps via package managers, pulls Ollama models, installs binaries to `/usr/local/bin`, sets up launchd (macOS) or systemd (Linux) service. Uses `set -euo pipefail`. BATS tests validate model selection logic.

## Testing Patterns

- `testify/assert` and `testify/require` for assertions
- Table-driven tests in models package
- In-memory SQLite via `newTestDB(t)` for DB tests
- Integration tests gated behind `-tags=integration`
- BATS tests in `scripts/tests/` for install script logic (sourced functions, not full script execution)

## CI/CD

GitHub Actions workflows in `.github/workflows/`:
- `ci.yml` — Branch guard (main←dev, dev←dev-*), lint, test, BATS, build matrix, govulncheck
- `release.yml` — Triggered on `v*` tags, builds all platforms, creates GitHub release with SHA256SUMS
- `codeql.yml` — Weekly security scanning

Branch model: `dev-*` → `dev` → `main`. Version injected via `-ldflags "-X main.version=$(VERSION)"`.

## Issue Tracking

This project uses `bd` (beads) for issue tracking. See AGENTS.md for the workflow. Run `bd ready` to find available work, `bd sync` before ending sessions.
