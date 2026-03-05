# OpenRouter Backend Support — Design

## Goal

Add OpenRouter as a third backend alongside Ollama and Anthropic, allowing users to run ralph jobs against any model available on OpenRouter (Kimi K2.5, Grok 4.1 Fast, Devstral 2 2512, etc.). Update the installer to guide users through OpenRouter setup with API key entry, model selection, and validation.

## Architecture

Same pattern as the existing Anthropic backend: new `BackendOpenRouter` constant, dedicated `OpenRouterConfig` struct, own case in `BuildEnv()`. OpenRouter is OpenAI-compatible, so it works by setting `ANTHROPIC_BASE_URL` to `https://openrouter.ai/api/v1` and `ANTHROPIC_AUTH_TOKEN` to the user's API key.

Backend resolution is unchanged: `job.Backend > server.DefaultBackend > "ollama"`.

## Decision Record

- **API key storage:** Server config (DB), not environment variable. Consistent with how other config is stored. Redacted from API responses.
- **Model defaults:** Kimi K2.5, Grok 4.1 Fast, Devstral 2 2512 as installer choices. Users can enter custom model IDs.
- **Model validation:** Live API call to OpenRouter `/api/v1/models` endpoint. Confirms model exists before accepting it.
- **Installer placement:** Third option in menu alongside Ollama and Anthropic. No replacements.
- **Cost controls:** None. OpenRouter has its own usage dashboard. Ralph treats it as a simple API backend.

## Non-Goals

- Spending limits or cost tracking
- OpenRouter-specific error handling beyond Claude Code's built-in handling
- Model catalog/browsing UI in the dashboard
- Automatic model updates or recommendations

---

## 1. Data Model

### New types in `internal/models/config.go`

```go
BackendOpenRouter Backend = "openrouter"

type OpenRouterConfig struct {
    APIKey     string `json:"api_key"`
    BaseURL    string `json:"base_url"`
    LargeModel string `json:"large_model"`
    SmallModel string `json:"small_model"`
}
```

### Changes to existing types

- `Backend.Valid()` — accept `"openrouter"`
- `ServerConfig` — add `OpenRouter OpenRouterConfig` field
- `DefaultServerConfig()` — populate defaults:
  - `BaseURL`: `"https://openrouter.ai/api/v1"`
  - `LargeModel`: `"moonshotai/kimi-k2.5"`
  - `SmallModel`: `"mistralai/devstral-2-2512"`
  - `APIKey`: `""` (must be configured by user)
- `ServerConfig.Validate()` — when `DefaultBackend == BackendOpenRouter`, validate API key and both models are non-empty
- `ServerConfigResponse` / `OpenRouterConfigResponse` — expose model names and base URL, redact API key

### Database

No new migrations. Backend column is already free-text. Server config is JSON key-value, handles new fields automatically.

---

## 2. Executor Integration

### `BuildEnv()` in `internal/executor/claude.go`

New case in the backend switch:

```go
case models.BackendOpenRouter:
    backendEnv = map[string]string{
        "ANTHROPIC_BASE_URL":            e.config.OpenRouter.BaseURL,
        "ANTHROPIC_AUTH_TOKEN":          e.config.OpenRouter.APIKey,
        "ANTHROPIC_MODEL":               e.config.OpenRouter.LargeModel,
        "ANTHROPIC_DEFAULT_HAIKU_MODEL": e.config.OpenRouter.SmallModel,
    }
```

No changes to executor, worker, ralph handler, or job model. The backend resolution chain and CLI `--backend` flag already handle any valid backend string.

---

## 3. Installer

### Backend selection menu (`scripts/install.sh`)

```
How would you like to run ralph-o-matic?
  [1] Local models via Ollama (GPU/CPU — free, private, requires hardware)
  [2] Anthropic API via Claude Code (uses your Claude subscription/API credits)
  [3] OpenRouter API (cloud, multi-provider — pay-per-token via openrouter.ai)
```

### `configure_openrouter()` function

1. **Prompt for API key** — masked input (`read -s`). Validate by calling `GET https://openrouter.ai/api/v1/models` with `Authorization: Bearer $key`. Success = key is valid. Cache the models response for step 2-3.

2. **Select large model** — present menu:
   ```
   Select the LARGE model (main work):
     [1] Kimi K2.5 (moonshotai/kimi-k2.5)
     [2] Grok 4.1 Fast (x-ai/grok-4-1-fast)
     [3] Devstral 2 2512 (mistralai/devstral-2-2512)
     [4] Enter custom model ID
   ```
   If custom: prompt for model ID, validate it exists in the cached models response.

3. **Select small model** — same menu, same defaults, same validation. Can be the same model as large.

4. **Write config** — call `ralph server-config set` for each field: `default_backend`, `openrouter.api_key`, `openrouter.base_url`, `openrouter.large_model`, `openrouter.small_model`.

### Validation helper

```bash
validate_openrouter_model() {
    local model_id="$1"
    local models_json="$2"
    # Check if model_id appears in the models response
    echo "$models_json" | grep -q "\"id\":\"$model_id\""
}
```

---

## 4. Config Management

### Server config CLI

Already supports dotted keys. Once `OpenRouterConfig` is added to `ServerConfig`, these work automatically:
```bash
ralph server-config set openrouter.api_key sk-or-...
ralph server-config set openrouter.large_model moonshotai/kimi-k2.5
ralph server-config set openrouter.small_model mistralai/devstral-2-2512
ralph server-config set openrouter.base_url https://openrouter.ai/api/v1
ralph server-config set default_backend openrouter
```

### Dashboard

No changes needed. Backend is already displayed per-job. Config API will expose OpenRouter settings via the existing endpoint.

---

## 5. Testing

- `models/config_test.go` — `OpenRouterConfig.Validate()`: missing API key, missing models, valid config. `Backend.Valid()` accepts `"openrouter"`.
- `executor/claude_test.go` — `BuildEnv` for OpenRouter backend: verify `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_MODEL`, `ANTHROPIC_DEFAULT_HAIKU_MODEL` are set correctly.
- `scripts/tests/` — BATS tests for `configure_openrouter()`: model selection, custom model entry, validation logic (sourced function testing, same pattern as existing Ollama tests).
- `db/config_test.go` — round-trip serialization of `OpenRouterConfig` through the config repo.
