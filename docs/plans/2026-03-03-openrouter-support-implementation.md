# OpenRouter Backend Support — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add OpenRouter as a third backend alongside Ollama and Anthropic, with installer support for API key entry, model selection, and validation.

**Architecture:** New `BackendOpenRouter` constant, dedicated `OpenRouterConfig` struct, own case in `BuildEnv()`. OpenRouter is OpenAI-compatible — Claude Code talks to it via `ANTHROPIC_BASE_URL` + `ANTHROPIC_AUTH_TOKEN`. Installer gets a third menu option with API key prompt and model selection.

**Tech Stack:** Go 1.24, SQLite (key-value config), Bash installer, BATS tests

**Design doc:** `docs/plans/2026-03-03-openrouter-support-design.md`

---

### Task 1: Data Model — OpenRouterConfig struct and Backend constant

**Files:**
- Modify: `internal/models/config.go:11-14` (add `BackendOpenRouter`)
- Modify: `internal/models/config.go:17-24` (update `Backend.Valid()`)
- Modify: `internal/models/config.go:61-77` (add `OpenRouterConfig` after `AnthropicConfig`)
- Test: `internal/models/config_test.go`

**Step 1: Write failing tests**

Add to `internal/models/config_test.go`:

```go
func TestBackend_Valid_OpenRouter(t *testing.T) {
	assert.True(t, models.BackendOpenRouter.Valid())
}

func TestOpenRouterConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  models.OpenRouterConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: models.OpenRouterConfig{
				APIKey:     "sk-or-v1-test",
				BaseURL:    "https://openrouter.ai/api/v1",
				LargeModel: "moonshotai/kimi-k2.5",
				SmallModel: "mistralai/devstral-2-2512",
			},
			wantErr: false,
		},
		{
			name: "missing API key",
			config: models.OpenRouterConfig{
				BaseURL:    "https://openrouter.ai/api/v1",
				LargeModel: "moonshotai/kimi-k2.5",
				SmallModel: "mistralai/devstral-2-2512",
			},
			wantErr: true,
		},
		{
			name: "missing large model",
			config: models.OpenRouterConfig{
				APIKey:     "sk-or-v1-test",
				BaseURL:    "https://openrouter.ai/api/v1",
				SmallModel: "mistralai/devstral-2-2512",
			},
			wantErr: true,
		},
		{
			name: "missing small model",
			config: models.OpenRouterConfig{
				APIKey:     "sk-or-v1-test",
				BaseURL:    "https://openrouter.ai/api/v1",
				LargeModel: "moonshotai/kimi-k2.5",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run 'TestBackend_Valid_OpenRouter|TestOpenRouterConfig_Validate' ./internal/models/`
Expected: FAIL — `BackendOpenRouter` and `OpenRouterConfig` don't exist

**Step 3: Implement**

In `internal/models/config.go`:

Add the constant at line 13:
```go
const (
	BackendOllama      Backend = "ollama"
	BackendAnthropic   Backend = "anthropic"
	BackendOpenRouter  Backend = "openrouter"
)
```

Update `Valid()` to accept `BackendOpenRouter`:
```go
func (b Backend) Valid() bool {
	switch b {
	case "", BackendOllama, BackendAnthropic, BackendOpenRouter:
		return true
	default:
		return false
	}
}
```

Add `OpenRouterConfig` struct after `AnthropicConfig` (after line 77):
```go
// OpenRouterConfig holds settings for the OpenRouter API backend.
type OpenRouterConfig struct {
	APIKey     string `json:"api_key"`
	BaseURL    string `json:"base_url"`
	LargeModel string `json:"large_model"`
	SmallModel string `json:"small_model"`
}

// Validate checks that API key and model names are set
func (orc *OpenRouterConfig) Validate() error {
	if orc.APIKey == "" {
		return fmt.Errorf("api_key is required")
	}
	if orc.LargeModel == "" {
		return fmt.Errorf("large_model is required")
	}
	if orc.SmallModel == "" {
		return fmt.Errorf("small_model is required")
	}
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run 'TestBackend_Valid_OpenRouter|TestOpenRouterConfig_Validate' ./internal/models/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/config.go internal/models/config_test.go
git commit -m "feat: add BackendOpenRouter constant and OpenRouterConfig struct"
```

---

### Task 2: ServerConfig — add OpenRouter field, defaults, validation, merge

**Files:**
- Modify: `internal/models/config.go:102-129` (add `OpenRouter` field to `ServerConfig`)
- Modify: `internal/models/config.go:131-148` (`DefaultServerConfig`)
- Modify: `internal/models/config.go:150-176` (`Validate`)
- Modify: `internal/models/config.go:178-274` (`Merge`)
- Modify: `internal/models/config.go:279-362` (`MergeJSON`)
- Test: `internal/models/config_test.go`

**Step 1: Write failing tests**

Add to `internal/models/config_test.go`:

```go
func TestServerConfig_Validate_OpenRouter(t *testing.T) {
	cfg := models.DefaultServerConfig()
	cfg.DefaultBackend = models.BackendOpenRouter
	cfg.OpenRouter.APIKey = "sk-or-v1-test"
	cfg.OpenRouter.LargeModel = "moonshotai/kimi-k2.5"
	cfg.OpenRouter.SmallModel = "mistralai/devstral-2-2512"

	assert.NoError(t, cfg.Validate())
}

func TestServerConfig_Validate_OpenRouter_MissingKey(t *testing.T) {
	cfg := models.DefaultServerConfig()
	cfg.DefaultBackend = models.BackendOpenRouter
	// API key left empty

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "openrouter")
}

func TestServerConfig_Merge_OpenRouter(t *testing.T) {
	base := models.DefaultServerConfig()
	updates := &models.ServerConfig{
		OpenRouter: models.OpenRouterConfig{
			APIKey:     "sk-or-v1-new",
			LargeModel: "x-ai/grok-4-1-fast",
		},
	}

	merged := base.Merge(updates)
	assert.Equal(t, "sk-or-v1-new", merged.OpenRouter.APIKey)
	assert.Equal(t, "x-ai/grok-4-1-fast", merged.OpenRouter.LargeModel)
	// SmallModel should keep default
	assert.Equal(t, "mistralai/devstral-2-2512", merged.OpenRouter.SmallModel)
}

func TestServerConfig_MergeJSON_OpenRouter(t *testing.T) {
	base := models.DefaultServerConfig()
	raw := json.RawMessage(`{"openrouter":{"api_key":"sk-or-v1-json","large_model":"test/model"}}`)

	merged, err := base.MergeJSON(raw)
	require.NoError(t, err)
	assert.Equal(t, "sk-or-v1-json", merged.OpenRouter.APIKey)
	assert.Equal(t, "test/model", merged.OpenRouter.LargeModel)
	assert.Equal(t, "mistralai/devstral-2-2512", merged.OpenRouter.SmallModel)
}

func TestDefaultServerConfig_OpenRouter_Defaults(t *testing.T) {
	cfg := models.DefaultServerConfig()
	assert.Equal(t, "https://openrouter.ai/api/v1", cfg.OpenRouter.BaseURL)
	assert.Equal(t, "moonshotai/kimi-k2.5", cfg.OpenRouter.LargeModel)
	assert.Equal(t, "mistralai/devstral-2-2512", cfg.OpenRouter.SmallModel)
	assert.Equal(t, "", cfg.OpenRouter.APIKey)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run 'TestServerConfig_Validate_OpenRouter|TestServerConfig_Merge_OpenRouter|TestServerConfig_MergeJSON_OpenRouter|TestDefaultServerConfig_OpenRouter' ./internal/models/`
Expected: FAIL — `ServerConfig` has no `OpenRouter` field

**Step 3: Implement**

Add `OpenRouter` field to `ServerConfig` (after `Anthropic` at line 120):
```go
	// Backend
	DefaultBackend Backend           `json:"default_backend"`
	Anthropic      AnthropicConfig   `json:"anthropic"`
	OpenRouter     OpenRouterConfig  `json:"openrouter"`
```

Add defaults in `DefaultServerConfig()`:
```go
	OpenRouter: OpenRouterConfig{
		BaseURL:    "https://openrouter.ai/api/v1",
		LargeModel: "moonshotai/kimi-k2.5",
		SmallModel: "mistralai/devstral-2-2512",
	},
```

Add validation case in `Validate()` (after the Anthropic block at line 174):
```go
	if c.DefaultBackend == BackendOpenRouter {
		if err := c.OpenRouter.Validate(); err != nil {
			return fmt.Errorf("openrouter: %w", err)
		}
	}
```

Add merge logic in `Merge()` (after the Anthropic merge block at line 242):
```go
	// OpenRouter: merge individual fields
	if updates.OpenRouter.APIKey != "" {
		result.OpenRouter.APIKey = updates.OpenRouter.APIKey
	}
	if updates.OpenRouter.BaseURL != "" {
		result.OpenRouter.BaseURL = updates.OpenRouter.BaseURL
	}
	if updates.OpenRouter.LargeModel != "" {
		result.OpenRouter.LargeModel = updates.OpenRouter.LargeModel
	}
	if updates.OpenRouter.SmallModel != "" {
		result.OpenRouter.SmallModel = updates.OpenRouter.SmallModel
	}
```

Add zero-value handling in `MergeJSON()` (after the Anthropic block at line 334):
```go
	if openrouterRaw, ok := rawMap["openrouter"]; ok {
		var openrouterMap map[string]json.RawMessage
		if err := json.Unmarshal(openrouterRaw, &openrouterMap); err == nil {
			if _, ok := openrouterMap["api_key"]; ok {
				result.OpenRouter.APIKey = updates.OpenRouter.APIKey
			}
			if _, ok := openrouterMap["base_url"]; ok {
				result.OpenRouter.BaseURL = updates.OpenRouter.BaseURL
			}
			if _, ok := openrouterMap["large_model"]; ok {
				result.OpenRouter.LargeModel = updates.OpenRouter.LargeModel
			}
			if _, ok := openrouterMap["small_model"]; ok {
				result.OpenRouter.SmallModel = updates.OpenRouter.SmallModel
			}
		}
	}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run 'TestServerConfig_Validate_OpenRouter|TestServerConfig_Merge_OpenRouter|TestServerConfig_MergeJSON_OpenRouter|TestDefaultServerConfig_OpenRouter' ./internal/models/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/config.go internal/models/config_test.go
git commit -m "feat: add OpenRouter to ServerConfig with defaults, validation, and merge"
```

---

### Task 3: Config Response — redacted OpenRouter for API

**Files:**
- Modify: `internal/models/config_response.go:1-7` (add `OpenRouterConfigResponse`)
- Modify: `internal/models/config_response.go:34-50` (add field to `ServerConfigResponse`)
- Modify: `internal/models/config_response.go:52-85` (`NewServerConfigResponse`)
- Test: `internal/models/config_test.go`

**Step 1: Write failing test**

Add to `internal/models/config_test.go`:

```go
func TestNewServerConfigResponse_OpenRouter_RedactsKey(t *testing.T) {
	cfg := models.DefaultServerConfig()
	cfg.OpenRouter.APIKey = "sk-or-v1-secret-key"
	cfg.OpenRouter.LargeModel = "moonshotai/kimi-k2.5"
	cfg.OpenRouter.SmallModel = "mistralai/devstral-2-2512"
	cfg.OpenRouter.BaseURL = "https://openrouter.ai/api/v1"

	resp := models.NewServerConfigResponse(cfg)

	assert.Equal(t, "moonshotai/kimi-k2.5", resp.OpenRouter.LargeModel)
	assert.Equal(t, "mistralai/devstral-2-2512", resp.OpenRouter.SmallModel)
	assert.Equal(t, "https://openrouter.ai/api/v1", resp.OpenRouter.BaseURL)
	assert.True(t, resp.OpenRouter.APIKeySet)
}

func TestNewServerConfigResponse_OpenRouter_NoKey(t *testing.T) {
	cfg := models.DefaultServerConfig()

	resp := models.NewServerConfigResponse(cfg)

	assert.False(t, resp.OpenRouter.APIKeySet)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run 'TestNewServerConfigResponse_OpenRouter' ./internal/models/`
Expected: FAIL — `OpenRouterConfigResponse` and `resp.OpenRouter` don't exist

**Step 3: Implement**

Add response type in `internal/models/config_response.go` (after `AnthropicConfigResponse`):
```go
// OpenRouterConfigResponse is the OpenRouter config returned by the API.
// The API key is replaced with a boolean indicating whether one is set.
type OpenRouterConfigResponse struct {
	BaseURL    string `json:"base_url"`
	LargeModel string `json:"large_model"`
	SmallModel string `json:"small_model"`
	APIKeySet  bool   `json:"api_key_set"`
}
```

Add field to `ServerConfigResponse`:
```go
	Anthropic  AnthropicConfigResponse   `json:"anthropic"`
	OpenRouter OpenRouterConfigResponse  `json:"openrouter"`
```

Add to `NewServerConfigResponse()`:
```go
		OpenRouter: OpenRouterConfigResponse{
			BaseURL:    cfg.OpenRouter.BaseURL,
			LargeModel: cfg.OpenRouter.LargeModel,
			SmallModel: cfg.OpenRouter.SmallModel,
			APIKeySet:  cfg.OpenRouter.APIKey != "",
		},
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run 'TestNewServerConfigResponse_OpenRouter' ./internal/models/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/models/config_response.go internal/models/config_test.go
git commit -m "feat: add OpenRouterConfigResponse with API key redaction"
```

---

### Task 4: DB Config — serialize/deserialize OpenRouter

**Files:**
- Modify: `internal/db/config.go:50-110` (`Save` — add openrouter marshal + key)
- Modify: `internal/db/config.go:138-229` (`applyConfigValue` — add case)
- Test: `internal/db/config_test.go`

**Step 1: Write failing test**

Add to `internal/db/config_test.go`:

```go
func TestConfigRepo_SaveOpenRouter(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.DefaultBackend = models.BackendOpenRouter
	cfg.OpenRouter.APIKey = "sk-or-v1-test-key"
	cfg.OpenRouter.BaseURL = "https://openrouter.ai/api/v1"
	cfg.OpenRouter.LargeModel = "moonshotai/kimi-k2.5"
	cfg.OpenRouter.SmallModel = "mistralai/devstral-2-2512"

	err := repo.Save(cfg)
	require.NoError(t, err)

	fetched, err := repo.Get()
	require.NoError(t, err)

	assert.Equal(t, models.BackendOpenRouter, fetched.DefaultBackend)
	assert.Equal(t, "sk-or-v1-test-key", fetched.OpenRouter.APIKey)
	assert.Equal(t, "https://openrouter.ai/api/v1", fetched.OpenRouter.BaseURL)
	assert.Equal(t, "moonshotai/kimi-k2.5", fetched.OpenRouter.LargeModel)
	assert.Equal(t, "mistralai/devstral-2-2512", fetched.OpenRouter.SmallModel)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestConfigRepo_SaveOpenRouter ./internal/db/`
Expected: FAIL — OpenRouter fields not saved/loaded

**Step 3: Implement**

In `internal/db/config.go` `Save()`, add marshaling alongside the existing blocks:
```go
	openrouterJSON, err := json.Marshal(cfg.OpenRouter)
	if err != nil {
		return fmt.Errorf("marshal openrouter: %w", err)
	}
```

Add `"openrouter"` key to the values map:
```go
	values := map[string]string{
		// ... existing keys ...
		"openrouter": string(openrouterJSON),
	}
```

In `applyConfigValue()`, add a case:
```go
	case "openrouter":
		var orc models.OpenRouterConfig
		if err := json.Unmarshal([]byte(value), &orc); err != nil {
			return err
		}
		cfg.OpenRouter = orc
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestConfigRepo_SaveOpenRouter ./internal/db/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/db/config.go internal/db/config_test.go
git commit -m "feat: serialize/deserialize OpenRouterConfig in config repo"
```

---

### Task 5: Executor — BuildEnv OpenRouter case

**Files:**
- Modify: `internal/executor/claude.go:75-88` (add `BackendOpenRouter` case in `BuildEnv`)
- Test: `internal/executor/claude_test.go`

**Step 1: Write failing test**

Add to `internal/executor/claude_test.go`:

```go
func TestClaudeExecutor_BuildEnv_OpenRouter(t *testing.T) {
	cfg := models.DefaultServerConfig()
	cfg.OpenRouter.APIKey = "sk-or-v1-test-key"
	cfg.OpenRouter.BaseURL = "https://openrouter.ai/api/v1"
	cfg.OpenRouter.LargeModel = "moonshotai/kimi-k2.5"
	cfg.OpenRouter.SmallModel = "mistralai/devstral-2-2512"
	exec := NewClaudeExecutor(cfg)

	env := exec.BuildEnv(models.BackendOpenRouter, nil)

	envMap := envToMap(env)
	assert.Equal(t, "https://openrouter.ai/api/v1", envMap["ANTHROPIC_BASE_URL"])
	assert.Equal(t, "sk-or-v1-test-key", envMap["ANTHROPIC_AUTH_TOKEN"])
	assert.Equal(t, "moonshotai/kimi-k2.5", envMap["ANTHROPIC_MODEL"])
	assert.Equal(t, "mistralai/devstral-2-2512", envMap["ANTHROPIC_DEFAULT_HAIKU_MODEL"])
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestClaudeExecutor_BuildEnv_OpenRouter ./internal/executor/`
Expected: FAIL — falls through to default (Ollama) case, env vars are wrong

**Step 3: Implement**

In `internal/executor/claude.go` `BuildEnv()`, add `BackendOpenRouter` case before the default:

```go
	case models.BackendOpenRouter:
		backendEnv = map[string]string{
			"ANTHROPIC_BASE_URL":            e.config.OpenRouter.BaseURL,
			"ANTHROPIC_AUTH_TOKEN":          e.config.OpenRouter.APIKey,
			"ANTHROPIC_MODEL":               e.config.OpenRouter.LargeModel,
			"ANTHROPIC_DEFAULT_HAIKU_MODEL": e.config.OpenRouter.SmallModel,
		}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestClaudeExecutor_BuildEnv_OpenRouter ./internal/executor/`
Expected: PASS

**Step 5: Run all executor tests to verify nothing broke**

Run: `go test -v ./internal/executor/`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/executor/claude.go internal/executor/claude_test.go
git commit -m "feat: add OpenRouter case to BuildEnv"
```

---

### Task 6: Installer — backend selection and model config

**Files:**
- Modify: `scripts/install.sh:30-34` (add `OPENROUTER_API_KEY` var)
- Modify: `scripts/install.sh:306-325` (`select_backend` — add option 3)
- Modify: `scripts/install.sh:344-401` (add `select_openrouter_models` after `select_anthropic_models`)
- Modify: `scripts/install.sh:898-968` (`apply_model_config` — add openrouter branch)
- Modify: `scripts/install.sh:1319-1350` (main flow — add openrouter gate)
- Test: `scripts/tests/model_selection_test.bats`

**Step 1: Write failing BATS tests**

Add to `scripts/tests/model_selection_test.bats`:

```bash
@test "select_backend sets openrouter with choice 3" {
    YES_FLAG=false
    BACKEND="ollama"
    select_backend <<< "3"
    [ "$BACKEND" = "openrouter" ]
}

@test "select_backend defaults to ollama with --yes (not openrouter)" {
    YES_FLAG=true
    BACKEND="ollama"
    select_backend
    [ "$BACKEND" = "ollama" ]
}

@test "select_openrouter_models picks defaults with --yes" {
    YES_FLAG=true
    LARGE_MODEL=""
    SMALL_MODEL=""
    select_openrouter_models
    [ "$LARGE_MODEL" = "moonshotai/kimi-k2.5" ]
    [ "$SMALL_MODEL" = "mistralai/devstral-2-2512" ]
}

@test "select_openrouter_models picks kimi with choice 1 and devstral with choice 3" {
    YES_FLAG=false
    LARGE_MODEL=""
    SMALL_MODEL=""
    select_openrouter_models <<< $'13'
    [ "$LARGE_MODEL" = "moonshotai/kimi-k2.5" ]
    [ "$SMALL_MODEL" = "mistralai/devstral-2-2512" ]
}

@test "select_openrouter_models picks grok with choice 2" {
    YES_FLAG=false
    LARGE_MODEL=""
    SMALL_MODEL=""
    select_openrouter_models <<< $'22'
    [ "$LARGE_MODEL" = "x-ai/grok-4-1-fast" ]
    [ "$SMALL_MODEL" = "x-ai/grok-4-1-fast" ]
}

@test "check_ram_requirement skips for openrouter backend" {
    MODE="server"
    BACKEND="openrouter"
    RAM_GB=4
    run check_ram_requirement
    [ "$status" -eq 0 ]
}

@test "apply_model_config sends openrouter payload" {
    BACKEND="openrouter"
    LARGE_MODEL="moonshotai/kimi-k2.5"
    SMALL_MODEL="mistralai/devstral-2-2512"
    OPENROUTER_API_KEY="sk-or-v1-test"

    curl() {
        if [[ "$1" == "-sf" ]] && [[ "$2" != "-X" ]]; then
            return 0
        fi
        printf "200"
        return 0
    }
    export -f curl

    run apply_model_config
    [ "$status" -eq 0 ]
    [[ "$output" == *"OpenRouter config applied"* ]]
}
```

**Step 2: Run BATS tests to verify they fail**

Run: `make test-bats`
Expected: FAIL — `select_openrouter_models` function doesn't exist, option 3 not handled

**Step 3: Implement installer changes**

Add global variable at line 34:
```bash
OPENROUTER_API_KEY=""
```

Update `select_backend()`:
```bash
select_backend() {
    if [[ "$YES_FLAG" == true ]]; then
        return
    fi

    echo ""
    echo "How would you like to run ralph-o-matic?"
    echo ""
    echo "  [1] Local models via Ollama (GPU/CPU — free, private, requires hardware)"
    echo "  [2] Anthropic API via Claude Code (uses your Claude subscription/API credits)"
    echo "  [3] OpenRouter API (cloud, multi-provider — pay-per-token via openrouter.ai)"
    echo ""
    read -p "Select [1-3]: " -n 1 -r
    echo ""

    case $REPLY in
        2) BACKEND="anthropic" ;;
        3) BACKEND="openrouter" ;;
        *) BACKEND="ollama" ;;
    esac
}
```

Add `validate_openrouter_key()` function (after `select_anthropic_models`):
```bash
validate_openrouter_key() {
    if [[ "$YES_FLAG" == true ]]; then
        if [[ -z "$OPENROUTER_API_KEY" ]]; then
            die "OpenRouter API key required. Pass via OPENROUTER_API_KEY env var with --yes."
        fi
        return
    fi

    echo ""
    echo "Enter your OpenRouter API key (from https://openrouter.ai/keys):"
    read -s -p "API key: " -r OPENROUTER_API_KEY
    echo ""

    if [[ -z "$OPENROUTER_API_KEY" ]]; then
        die "API key cannot be empty"
    fi

    # Validate by calling the models endpoint
    local http_code
    http_code=$(curl -s -o /dev/null -w '%{http_code}' \
        -H "Authorization: Bearer $OPENROUTER_API_KEY" \
        "https://openrouter.ai/api/v1/models")

    if [[ "$http_code" != "200" ]]; then
        die "API key validation failed (HTTP $http_code). Check your key at https://openrouter.ai/keys"
    fi

    success "API key validated"
}
```

Add `select_openrouter_models()` function:
```bash
select_openrouter_models() {
    if [[ "$YES_FLAG" == true ]]; then
        LARGE_MODEL="moonshotai/kimi-k2.5"
        SMALL_MODEL="mistralai/devstral-2-2512"
        return
    fi

    echo ""
    echo "Select the LARGE model (used for main coding iterations):"
    echo ""
    echo "  [1] Kimi K2.5        (moonshotai/kimi-k2.5)"
    echo "  [2] Grok 4.1 Fast    (x-ai/grok-4-1-fast)"
    echo "  [3] Devstral 2 2512  (mistralai/devstral-2-2512)"
    echo "  [4] Custom model ID"
    echo ""
    read -p "Select [1-4]: " -n 1 -r
    echo ""
    case $REPLY in
        1) LARGE_MODEL="moonshotai/kimi-k2.5" ;;
        2) LARGE_MODEL="x-ai/grok-4-1-fast" ;;
        3) LARGE_MODEL="mistralai/devstral-2-2512" ;;
        4)
            read -p "Enter model ID: " -r LARGE_MODEL
            if [[ -z "$LARGE_MODEL" ]]; then
                warn "Empty model ID, using moonshotai/kimi-k2.5"
                LARGE_MODEL="moonshotai/kimi-k2.5"
            fi
            ;;
        *) warn "Invalid choice, using moonshotai/kimi-k2.5"; LARGE_MODEL="moonshotai/kimi-k2.5" ;;
    esac

    echo ""
    echo "Select the SMALL model (used for fast tasks and tool calls):"
    echo ""
    echo "  [1] Kimi K2.5        (moonshotai/kimi-k2.5)"
    echo "  [2] Grok 4.1 Fast    (x-ai/grok-4-1-fast)"
    echo "  [3] Devstral 2 2512  (mistralai/devstral-2-2512)"
    echo "  [4] Custom model ID"
    echo ""
    read -p "Select [1-4]: " -n 1 -r
    echo ""
    case $REPLY in
        1) SMALL_MODEL="moonshotai/kimi-k2.5" ;;
        2) SMALL_MODEL="x-ai/grok-4-1-fast" ;;
        3) SMALL_MODEL="mistralai/devstral-2-2512" ;;
        4)
            read -p "Enter model ID: " -r SMALL_MODEL
            if [[ -z "$SMALL_MODEL" ]]; then
                warn "Empty model ID, using mistralai/devstral-2-2512"
                SMALL_MODEL="mistralai/devstral-2-2512"
            fi
            ;;
        *) warn "Invalid choice, using mistralai/devstral-2-2512"; SMALL_MODEL="mistralai/devstral-2-2512" ;;
    esac

    success "Selected: large=$LARGE_MODEL, small=$SMALL_MODEL"
}
```

Update `check_ram_requirement()` to skip for openrouter (same pattern as anthropic):
```bash
    # Cloud backends don't need local RAM
    if [[ "$BACKEND" == "anthropic" || "$BACKEND" == "openrouter" ]]; then
        return
    fi
```

Add openrouter branch to `apply_model_config()`:
```bash
    elif [[ "$BACKEND" == "openrouter" ]]; then
        local json_payload
        json_payload=$(jq -n \
            --arg key "$OPENROUTER_API_KEY" \
            --arg large "$LARGE_MODEL" \
            --arg small "$SMALL_MODEL" \
            '{default_backend:"openrouter",openrouter:{api_key:$key,base_url:"https://openrouter.ai/api/v1",large_model:$large,small_model:$small}}')

        local http_code
        http_code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH http://localhost:9090/api/config \
            -H "Content-Type: application/json" \
            -d "$json_payload")
        if [[ "$http_code" -lt 200 || "$http_code" -ge 300 ]]; then
            warn "Config update failed (HTTP $http_code) — check server logs"
            return
        fi

        success "OpenRouter config applied (large=$LARGE_MODEL, small=$SMALL_MODEL)"
        return
```

Add openrouter gate in `main()`:
```bash
    if [[ "$BACKEND" == "anthropic" ]]; then
        validate_claude_auth
        select_anthropic_models
    elif [[ "$BACKEND" == "openrouter" ]]; then
        validate_openrouter_key
        select_openrouter_models
    else
        detect_gpu
        select_models
        configure_ollama
        pull_models
    fi
```

**Step 4: Run BATS tests to verify they pass**

Run: `make test-bats`
Expected: PASS

**Step 5: Commit**

```bash
git add scripts/install.sh scripts/tests/model_selection_test.bats
git commit -m "feat: add OpenRouter option to installer with API key validation and model selection"
```

---

### Task 7: Full integration test — build and run all tests

**Step 1: Run full test suite**

Run: `make test`
Expected: All PASS

**Step 2: Run BATS tests**

Run: `make test-bats`
Expected: All PASS

**Step 3: Run lint**

Run: `make lint`
Expected: No new issues

**Step 4: Build**

Run: `make build`
Expected: Clean build

**Step 5: Commit any fixups (if needed)**

```bash
git add -A
git commit -m "fix: address lint/test issues from OpenRouter integration"
```

---

### Task 8: Final verification — manual smoke test

**Step 1: Install the new build**

```bash
sudo cp build/ralph-o-matic /usr/local/bin/
```

**Step 2: Restart the server**

```bash
launchctl kickstart -k gui/$(id -u)/com.ralph-o-matic.server
```

**Step 3: Verify config CLI accepts OpenRouter settings**

```bash
ralph-o-matic server-config set default_backend openrouter
ralph-o-matic server-config set openrouter.api_key "test-key"
ralph-o-matic server-config set openrouter.large_model "moonshotai/kimi-k2.5"
ralph-o-matic server-config set openrouter.small_model "mistralai/devstral-2-2512"
ralph-o-matic server-config get
```

Expected: Config shows openrouter backend with model names and `api_key_set: true`

**Step 4: Reset config back to previous state**

```bash
ralph-o-matic server-config set default_backend ollama
```

**Step 5: Commit if any final adjustments needed**

---

## Summary

| Task | What | Files |
|------|------|-------|
| 1 | `BackendOpenRouter` constant + `OpenRouterConfig` struct | `models/config.go`, `models/config_test.go` |
| 2 | `ServerConfig` field, defaults, validation, merge | `models/config.go`, `models/config_test.go` |
| 3 | Redacted API response type | `models/config_response.go`, `models/config_test.go` |
| 4 | DB serialization/deserialization | `db/config.go`, `db/config_test.go` |
| 5 | `BuildEnv` executor switch case | `executor/claude.go`, `executor/claude_test.go` |
| 6 | Installer: menu + key prompt + model selection | `scripts/install.sh`, `scripts/tests/model_selection_test.bats` |
| 7 | Full test/lint/build pass | — |
| 8 | Manual smoke test | — |
