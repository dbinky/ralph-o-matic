# Installer: Anthropic API Backend Support

**Date:** 2026-02-17
**Status:** Approved
**Branch:** dev-installer-update

## Problem

The installer only supports local Ollama models. Users with Claude Max plans or Anthropic API access can't configure ralph-o-matic to use Claude models without manual post-install configuration. This makes it hard to test on machines without GPUs or to use frontier models for higher-quality iterations.

## Decision

Add a top-level backend choice to both installer scripts (bash and PowerShell) that lets users pick between local Ollama models or Anthropic API via their existing Claude Code authentication.

## Design

### Installer Flow

New top-level prompt immediately after the welcome banner:

```
How would you like to run ralph-o-matic?

  1) Local models via Ollama (GPU/CPU — free, private, requires hardware)
  2) Anthropic API via Claude Code (uses your Claude subscription/API credits)

Select [1-2]:
```

**Ollama path (1):** Existing flow unchanged — hardware detection, inference mode, model selection, Ollama install, model pull.

**Anthropic path (2):**

1. Validate `claude` CLI is installed (`claude --version`)
2. Validate auth works (quick haiku test call)
3. Interactive large model selection (curated list + custom)
4. Interactive small model selection (curated list + custom)
5. Skip all Ollama steps

Both paths rejoin at: binary install → notification setup → service install → start server → apply config.

### Auth Validation

```bash
# Step 1: CLI exists
command -v claude &>/dev/null || { echo "Error: claude CLI not found"; exit 1; }

# Step 2: Auth works (haiku = fast + cheap)
claude --print "respond with OK" --model claude-haiku-4-5-20251001 2>/dev/null | grep -qi "ok"
```

Fail early with actionable error messages if either check fails.

### Curated Model Lists

**Large model picker:**

| # | Model ID | Description |
|---|----------|-------------|
| 1 | `claude-opus-4-8` | Most capable, slower, higher cost |
| 2 | `claude-sonnet-4-5-20250929` | Strong balance of quality and speed |
| 3 | `claude-sonnet-4-5-20250929` (1M) | Extended context window |
| 4 | Custom model ID | User types model ID |

**Small model picker:**

| # | Model ID | Description |
|---|----------|-------------|
| 1 | `claude-haiku-4-5-20251001` | Fast, efficient, low cost |
| 2 | `claude-sonnet-4-5-20250929` | Higher quality for small tasks |
| 3 | Custom model ID | User types model ID |

### Config Application

For Anthropic backend, the installer sends:

```json
{
  "default_backend": "anthropic",
  "anthropic": {
    "large_model": "<selected-large>",
    "small_model": "<selected-small>"
  }
}
```

No Ollama config keys are sent. No API key is stored — the `claude` CLI handles its own authentication via the user's existing login.

### Files Changed

- `scripts/install.sh` — Add backend choice, Anthropic validation, model picker, config path
- `scripts/install.ps1` — Same changes for PowerShell
- `scripts/tests/*.bats` — New test cases for Anthropic functions

### No Go Changes Required

The server already supports `BackendAnthropic`, `AnthropicConfig`, and the executor (`internal/executor/claude.go`) builds the correct env vars for the Anthropic backend. This is purely an installer-side change.

## Non-Goals

- API key management (users rely on Claude Code auth)
- Ollama installation when Anthropic is selected
- Model validation against Anthropic's API (trust the user's selection)
- Changes to the Go server or executor code
