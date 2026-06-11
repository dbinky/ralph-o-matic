# Installer Anthropic API Support — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Anthropic API backend option to both installer scripts so users can run ralph-o-matic with Claude models via their existing Claude Code auth.

**Architecture:** Top-level backend choice forks the installer into Ollama (existing) or Anthropic (new) paths. Both rejoin for shared steps. New bash functions are testable via BATS. No Go changes needed.

**Tech Stack:** Bash, PowerShell, BATS (test framework)

---

### Task 1: Add backend choice and state variable to bash installer

**Files:**
- Modify: `scripts/install.sh:26-32` (add BACKEND variable)
- Modify: `scripts/install.sh:1153-1173` (wire into main flow)

**Step 1: Add the BACKEND variable alongside existing state variables**

At `scripts/install.sh:32`, after the `INFERENCE_MODE=""` line, add:

```bash
BACKEND="ollama"  # ollama or anthropic
```

**Step 2: Add `select_backend()` function**

Insert this function before `select_models()` (before line 302):

```bash
select_backend() {
    # Skip if --yes flag (default to ollama)
    if [[ "$YES_FLAG" == true ]]; then
        return
    fi

    echo ""
    echo "How would you like to run ralph-o-matic?"
    echo ""
    echo "  [1] Local models via Ollama (GPU/CPU — free, private, requires hardware)"
    echo "  [2] Anthropic API via Claude Code (uses your Claude subscription/API credits)"
    echo ""
    read -p "Select [1-2]: " -n 1 -r
    echo ""

    case $REPLY in
        2) BACKEND="anthropic" ;;
        *) BACKEND="ollama" ;;
    esac
}
```

**Step 3: Run tests to verify nothing broke**

Run: `make test-bats`
Expected: All existing tests PASS (new function exists but isn't called by existing tests)

**Step 4: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(installer): add backend choice variable and select_backend function"
```

---

### Task 2: Add Claude auth validation to bash installer

**Files:**
- Modify: `scripts/install.sh` (add `validate_claude_auth()` function after `select_backend()`)

**Step 1: Write the BATS test first**

Add to `scripts/tests/model_selection_test.bats`:

```bash
@test "validate_claude_auth fails when claude not installed" {
    # Override command to simulate claude not found
    command() {
        if [[ "$2" == "claude" ]]; then return 1; fi
        builtin command "$@"
    }
    export -f command

    run validate_claude_auth
    [ "$status" -eq 1 ]
    [[ "$output" == *"not found"* ]]
}

@test "validate_claude_auth fails when auth check fails" {
    # claude exists but auth fails
    claude() { return 1; }
    export -f claude

    run validate_claude_auth
    [ "$status" -eq 1 ]
    [[ "$output" == *"auth"* ]] || [[ "$output" == *"failed"* ]]
}
```

**Step 2: Run test to verify it fails**

Run: `make test-bats`
Expected: FAIL — `validate_claude_auth` not defined

**Step 3: Add `validate_claude_auth()` function**

Insert after `select_backend()` in `scripts/install.sh`:

```bash
validate_claude_auth() {
    info "Validating Claude Code installation..."

    if ! command -v claude &>/dev/null; then
        error "Claude Code CLI not found. Install it first:
  npm install -g @anthropic-ai/claude-code
  Then run 'claude' to log in."
    fi
    success "Claude Code CLI found"

    info "Checking authentication (this makes a quick API call)..."
    if ! claude --print "respond with only the word OK" --model claude-haiku-4-5-20251001 2>/dev/null | grep -qi "ok"; then
        error "Claude Code authentication failed. Run 'claude' to log in first."
    fi
    success "Claude Code authenticated"
}
```

**Step 4: Run tests to verify they pass**

Run: `make test-bats`
Expected: Both new tests PASS

**Step 5: Commit**

```bash
git add scripts/install.sh scripts/tests/model_selection_test.bats
git commit -m "feat(installer): add Claude auth validation with tests"
```

---

### Task 3: Add Anthropic model selection to bash installer

**Files:**
- Modify: `scripts/install.sh` (add `select_anthropic_models()` function)
- Modify: `scripts/tests/model_selection_test.bats` (add tests)

**Step 1: Write the BATS tests first**

Add to `scripts/tests/model_selection_test.bats`:

```bash
@test "select_anthropic_models picks opus with choice 1" {
    LARGE_MODEL=""
    SMALL_MODEL=""

    # Simulate user picking 1 for large, 1 for small
    select_anthropic_models <<< $'1\n1\n'

    [ "$LARGE_MODEL" = "claude-opus-4-8" ]
    [ "$SMALL_MODEL" = "claude-haiku-4-5-20251001" ]
}

@test "select_anthropic_models picks sonnet for both with choices 2,2" {
    LARGE_MODEL=""
    SMALL_MODEL=""

    select_anthropic_models <<< $'2\n2\n'

    [ "$LARGE_MODEL" = "claude-sonnet-4-5-20250929" ]
    [ "$SMALL_MODEL" = "claude-sonnet-4-5-20250929" ]
}
```

**Step 2: Run test to verify it fails**

Run: `make test-bats`
Expected: FAIL — `select_anthropic_models` not defined

**Step 3: Add `select_anthropic_models()` function**

Insert after `validate_claude_auth()` in `scripts/install.sh`:

```bash
select_anthropic_models() {
    echo ""
    echo "Select the LARGE model (used for main coding iterations):"
    echo ""
    echo "  [1] claude-opus-4-8               (most capable, slower, higher cost)"
    echo "  [2] claude-sonnet-4-5-20250929     (strong balance of quality and speed)"
    echo "  [3] claude-sonnet-4-5-20250929     (same as 2, with extended 1M context)"
    echo "  [4] Custom model ID"
    echo ""
    read -p "Select [1-4]: " -r
    echo ""
    case $REPLY in
        1) LARGE_MODEL="claude-opus-4-8" ;;
        2) LARGE_MODEL="claude-sonnet-4-5-20250929" ;;
        3) LARGE_MODEL="claude-sonnet-4-5-20250929" ;;
        4)
            read -p "Enter model ID: " -r LARGE_MODEL
            if [[ -z "$LARGE_MODEL" ]]; then
                warn "Empty model ID, using claude-sonnet-4-5-20250929"
                LARGE_MODEL="claude-sonnet-4-5-20250929"
            fi
            ;;
        *) warn "Invalid choice, using claude-sonnet-4-5-20250929"; LARGE_MODEL="claude-sonnet-4-5-20250929" ;;
    esac

    echo "Select the SMALL model (used for quick checks and lightweight tasks):"
    echo ""
    echo "  [1] claude-haiku-4-5-20251001     (fast, efficient, low cost)"
    echo "  [2] claude-sonnet-4-5-20250929     (higher quality for small tasks)"
    echo "  [3] Custom model ID"
    echo ""
    read -p "Select [1-3]: " -r
    echo ""
    case $REPLY in
        1) SMALL_MODEL="claude-haiku-4-5-20251001" ;;
        2) SMALL_MODEL="claude-sonnet-4-5-20250929" ;;
        3)
            read -p "Enter model ID: " -r SMALL_MODEL
            if [[ -z "$SMALL_MODEL" ]]; then
                warn "Empty model ID, using claude-haiku-4-5-20251001"
                SMALL_MODEL="claude-haiku-4-5-20251001"
            fi
            ;;
        *) warn "Invalid choice, using claude-haiku-4-5-20251001"; SMALL_MODEL="claude-haiku-4-5-20251001" ;;
    esac

    success "Selected: large=$LARGE_MODEL, small=$SMALL_MODEL"
}
```

**Step 4: Run tests to verify they pass**

Run: `make test-bats`
Expected: Both new tests PASS

**Step 5: Commit**

```bash
git add scripts/install.sh scripts/tests/model_selection_test.bats
git commit -m "feat(installer): add Anthropic model selection with tests"
```

---

### Task 4: Add Anthropic config application to bash installer

**Files:**
- Modify: `scripts/install.sh:791-846` (extend `apply_model_config()`)
- Modify: `scripts/tests/model_selection_test.bats` (add test)

**Step 1: Write the BATS test first**

Add to `scripts/tests/model_selection_test.bats`:

```bash
@test "apply_model_config sends anthropic payload when backend is anthropic" {
    BACKEND="anthropic"
    LARGE_MODEL="claude-opus-4-8"
    SMALL_MODEL="claude-haiku-4-5-20251001"

    # Capture the curl call
    local captured_payload=""
    curl() {
        for arg in "$@"; do
            if [[ "$arg" == "{"* ]]; then
                captured_payload="$arg"
            fi
        done
        # Simulate successful server check and config apply
        if [[ "$1" == "-sf" ]] && [[ "$2" != "-X" ]]; then
            return 0  # server health check
        fi
        echo "$captured_payload"
        return 0
    }
    export -f curl

    # Mock jq to just echo the args for verification
    run apply_model_config
    [ "$status" -eq 0 ]
    [[ "$output" == *"anthropic"* ]] || [[ "$output" == *"claude-opus"* ]]
}
```

**Step 2: Run test to verify it fails**

Run: `make test-bats`
Expected: FAIL — current `apply_model_config` doesn't handle `BACKEND="anthropic"`

**Step 3: Modify `apply_model_config()` to handle Anthropic backend**

In `scripts/install.sh`, replace the `apply_model_config()` function. After the server readiness check (the while loop), add a backend fork:

```bash
    # Fork based on backend
    if [[ "$BACKEND" == "anthropic" ]]; then
        local json_payload
        json_payload=$(jq -n \
            --arg large "$LARGE_MODEL" \
            --arg small "$SMALL_MODEL" \
            '{default_backend:"anthropic",anthropic:{large_model:$large,small_model:$small}}')

        local http_code
        http_code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH http://localhost:9090/api/config \
            -H "Content-Type: application/json" \
            -d "$json_payload")
        if [[ "$http_code" -lt 200 || "$http_code" -ge 300 ]]; then
            warn "Config update failed (HTTP $http_code) — check server logs"
            return
        fi

        success "Anthropic config applied (large=$LARGE_MODEL, small=$SMALL_MODEL)"
        return
    fi
```

Insert this block right after the server readiness while-loop, before the existing Ollama config logic (the `local is_remote=false` line).

**Step 4: Run tests to verify they pass**

Run: `make test-bats`
Expected: All tests PASS

**Step 5: Commit**

```bash
git add scripts/install.sh scripts/tests/model_selection_test.bats
git commit -m "feat(installer): extend apply_model_config for Anthropic backend"
```

---

### Task 5: Wire Anthropic path into bash main flow

**Files:**
- Modify: `scripts/install.sh:1153-1186` (main function)

**Step 1: Modify `main()` to add the backend fork**

Replace the server-mode block in `main()` (lines 1168-1173) with:

```bash
    if [[ "$MODE" != "client" ]]; then
        select_backend
        if [[ "$BACKEND" == "anthropic" ]]; then
            validate_claude_auth
            select_anthropic_models
        else
            detect_gpu
            select_models
            configure_ollama
            pull_models
        fi
    fi
```

Also update `check_dependencies` and `install_missing_dependencies`: when `BACKEND` is `"anthropic"`, skip the Ollama dependency check/install. Modify the Ollama sections in both functions to be conditional:

In `check_dependencies` (around line 501-511), wrap the Ollama check:
```bash
    # Ollama (only for server mode with ollama backend)
    if [[ "$MODE" != "client" ]] && [[ "$BACKEND" != "anthropic" ]]; then
```

In `install_missing_dependencies` (around line 578-583), wrap the Ollama install:
```bash
    # Install Ollama (server mode + ollama backend only)
    if [[ "$MODE" != "client" ]] && [[ "$BACKEND" != "anthropic" ]] && [[ "${DEPS_INSTALLED[ollama]}" == "false" ]]; then
```

Also update the `check_ram_requirement` function: skip the 16GB minimum check when backend is anthropic (Anthropic doesn't need local model RAM):

```bash
    if [[ "$MODE" == "client" ]] || [[ "$BACKEND" == "anthropic" ]]; then
        return 0
    fi
```

Note: `select_backend()` must be called before `check_ram_requirement()` in main. Reorder main to:

```bash
    print_banner
    detect_platform
    prompt_mode
    if [[ "$MODE" != "client" ]]; then
        select_backend
    fi
    check_ram_requirement
    check_dependencies
    install_missing_dependencies
```

**Step 2: Run all tests**

Run: `make test-bats`
Expected: All tests PASS

**Step 3: Manual smoke test**

Run: `bash scripts/install.sh` — verify the backend prompt appears, selecting option 2 shows the Claude validation and model picker, selecting option 1 shows the existing Ollama flow.

(Don't complete the install — just verify the prompts work, then Ctrl-C.)

**Step 4: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(installer): wire Anthropic path into main install flow"
```

---

### Task 6: Add the same features to PowerShell installer

**Files:**
- Modify: `scripts/install.ps1`

**Step 1: Add `$script:Backend` variable**

After line 12 (`[string]$SmallModel = ""`), within the param block or after it:

```powershell
$script:Backend = "ollama"  # ollama or anthropic
```

**Step 2: Add `Select-Backend` function**

```powershell
function Select-Backend {
    if ($Yes) { return }

    Write-Host ""
    Write-Host "How would you like to run ralph-o-matic?"
    Write-Host ""
    Write-Host "  [1] Local models via Ollama (GPU/CPU - free, private, requires hardware)"
    Write-Host "  [2] Anthropic API via Claude Code (uses your Claude subscription/API credits)"
    Write-Host ""
    $choice = Read-Host "Select [1-2]"

    switch ($choice) {
        "2" { $script:Backend = "anthropic" }
        default { $script:Backend = "ollama" }
    }
}
```

**Step 3: Add `Test-ClaudeAuth` function**

```powershell
function Test-ClaudeAuth {
    Write-Info "Validating Claude Code installation..."

    try {
        $null = & claude --version 2>$null
    } catch {
        Write-Err "Claude Code CLI not found. Install it first: npm install -g @anthropic-ai/claude-code"
    }
    Write-Success "Claude Code CLI found"

    Write-Info "Checking authentication (this makes a quick API call)..."
    try {
        $result = & claude --print "respond with only the word OK" --model claude-haiku-4-5-20251001 2>$null
        if ($result -notmatch "(?i)ok") {
            Write-Err "Claude Code authentication failed. Run 'claude' to log in first."
        }
    } catch {
        Write-Err "Claude Code authentication failed. Run 'claude' to log in first."
    }
    Write-Success "Claude Code authenticated"
}
```

**Step 4: Add `Select-AnthropicModels` function**

```powershell
function Select-AnthropicModels {
    Write-Host ""
    Write-Host "Select the LARGE model (used for main coding iterations):"
    Write-Host ""
    Write-Host "  [1] claude-opus-4-8               (most capable, slower, higher cost)"
    Write-Host "  [2] claude-sonnet-4-5-20250929     (strong balance of quality and speed)"
    Write-Host "  [3] claude-sonnet-4-5-20250929     (same as 2, with extended 1M context)"
    Write-Host "  [4] Custom model ID"
    Write-Host ""
    $choice = Read-Host "Select [1-4]"

    switch ($choice) {
        "1" { $LargeModel = "claude-opus-4-8" }
        "2" { $LargeModel = "claude-sonnet-4-5-20250929" }
        "3" { $LargeModel = "claude-sonnet-4-5-20250929" }
        "4" {
            $LargeModel = Read-Host "Enter model ID"
            if (-not $LargeModel) {
                Write-Warn "Empty model ID, using claude-sonnet-4-5-20250929"
                $LargeModel = "claude-sonnet-4-5-20250929"
            }
        }
        default {
            Write-Warn "Invalid choice, using claude-sonnet-4-5-20250929"
            $LargeModel = "claude-sonnet-4-5-20250929"
        }
    }

    Write-Host ""
    Write-Host "Select the SMALL model (used for quick checks and lightweight tasks):"
    Write-Host ""
    Write-Host "  [1] claude-haiku-4-5-20251001     (fast, efficient, low cost)"
    Write-Host "  [2] claude-sonnet-4-5-20250929     (higher quality for small tasks)"
    Write-Host "  [3] Custom model ID"
    Write-Host ""
    $choice = Read-Host "Select [1-3]"

    switch ($choice) {
        "1" { $SmallModel = "claude-haiku-4-5-20251001" }
        "2" { $SmallModel = "claude-sonnet-4-5-20250929" }
        "3" {
            $SmallModel = Read-Host "Enter model ID"
            if (-not $SmallModel) {
                Write-Warn "Empty model ID, using claude-haiku-4-5-20251001"
                $SmallModel = "claude-haiku-4-5-20251001"
            }
        }
        default {
            Write-Warn "Invalid choice, using claude-haiku-4-5-20251001"
            $SmallModel = "claude-haiku-4-5-20251001"
        }
    }

    Write-Success "Selected: large=$LargeModel, small=$SmallModel"
}
```

**Step 5: Update `Set-Configuration` for Anthropic backend**

When `$script:Backend -eq "anthropic"`, write a simpler YAML that reflects the anthropic backend:

```powershell
    } else {
        # Server/full mode
        if ($script:Backend -eq "anthropic") {
            @"
server: http://localhost:9090
default_priority: normal
default_max_iterations: 50
"@ | Out-File -FilePath "$configDir\config.yaml" -Encoding utf8
        } else {
            # existing Ollama YAML block...
        }
```

**Step 6: Add Anthropic config push function**

Add a block to the Main function (or a new function) that calls the API with Anthropic config after server start, similar to the bash `apply_model_config` anthropic branch:

```powershell
function Push-AnthropicConfig {
    Write-Info "Applying Anthropic configuration to server..."

    # Wait for server
    $retries = 0
    while ($retries -lt 15) {
        try {
            $null = Invoke-RestMethod -Uri "http://localhost:9090/api/config" -TimeoutSec 2
            break
        } catch {
            $retries++
            Start-Sleep -Seconds 1
        }
    }
    if ($retries -ge 15) {
        Write-Warn "Server not responding - skipping Anthropic config"
        return
    }

    $body = @{
        default_backend = "anthropic"
        anthropic = @{
            large_model = $LargeModel
            small_model = $SmallModel
        }
    } | ConvertTo-Json -Depth 3

    try {
        Invoke-RestMethod -Uri "http://localhost:9090/api/config" -Method Patch -ContentType "application/json" -Body $body | Out-Null
        Write-Success "Anthropic config applied (large=$LargeModel, small=$SmallModel)"
    } catch {
        Write-Warn "Failed to apply Anthropic config: $_"
    }
}
```

**Step 7: Wire into PowerShell `Main` function**

Update the Main function to match the bash flow:

```powershell
function Main {
    Show-Banner
    Get-Platform
    Get-InstallMode

    if ($Mode -ne "client") {
        Select-Backend
    }

    Test-RamRequirement  # Update to skip for anthropic
    Test-Dependencies    # Update to skip Ollama for anthropic
    Install-MissingDependencies  # Update to skip Ollama for anthropic

    if ($Mode -ne "client") {
        if ($script:Backend -eq "anthropic") {
            Test-ClaudeAuth
            Select-AnthropicModels
        } else {
            Get-Gpu
            Select-Models
            Install-Models
        }
    }

    Install-Binaries
    Install-Skill
    Set-Configuration

    if ($Mode -ne "client") {
        Set-NotificationConfig
        Request-StartServer
        if ($script:Backend -eq "anthropic") {
            Push-AnthropicConfig
        } else {
            # existing Ollama model config push (if any)
        }
        Push-NotificationConfig
        Test-NotificationConfig
    }

    Show-Success
}
```

Also update `Test-RamRequirement` to skip for anthropic:
```powershell
    if ($Mode -eq "client" -or $script:Backend -eq "anthropic") {
        return
    }
```

And update `Test-Dependencies` / `Install-MissingDependencies` to skip Ollama when anthropic.

**Step 8: Commit**

```bash
git add scripts/install.ps1
git commit -m "feat(installer): add Anthropic API support to PowerShell installer"
```

---

### Task 7: Add comprehensive BATS tests for all new paths

**Files:**
- Modify: `scripts/tests/model_selection_test.bats`

**Step 1: Add tests for the full Anthropic flow**

```bash
@test "select_backend defaults to ollama with --yes" {
    YES_FLAG=true
    BACKEND="ollama"

    select_backend

    [ "$BACKEND" = "ollama" ]
}

@test "select_backend sets anthropic with choice 2" {
    YES_FLAG=false
    BACKEND="ollama"

    select_backend <<< "2"

    [ "$BACKEND" = "anthropic" ]
}

@test "apply_model_config sends ollama payload when backend is ollama" {
    BACKEND="ollama"
    OLLAMA_URL="http://localhost:11434"
    LARGE_MODEL="devstral"
    SMALL_MODEL="qwen3:8b"
    INFERENCE_MODE="gpu_only"

    # Mock curl and jq
    curl() {
        if [[ "$1" == "-sf" ]] && [[ "$*" != *PATCH* ]]; then return 0; fi
        echo "ollama-payload"
        return 0
    }
    jq() { echo '{"ollama":{"host":"test"}}'; }
    export -f curl jq

    run apply_model_config
    [ "$status" -eq 0 ]
    [[ "$output" == *"Model config applied"* ]]
}

@test "check_ram_requirement skips for anthropic backend" {
    MODE="server"
    BACKEND="anthropic"
    RAM_GB=4  # Would normally fail

    run check_ram_requirement
    [ "$status" -eq 0 ]
}
```

**Step 2: Run all tests**

Run: `make test-bats`
Expected: All tests PASS

**Step 3: Commit**

```bash
git add scripts/tests/model_selection_test.bats
git commit -m "test(installer): add comprehensive BATS tests for Anthropic path"
```

---

### Task 8: Final verification and squash commit

**Step 1: Run full test suite**

Run: `make test-all`
Expected: All Go tests + BATS tests PASS

**Step 2: Run lint**

Run: `make lint`
Expected: 0 issues

**Step 3: Manual smoke test of bash installer**

Run: `bash scripts/install.sh`

Verify:
- Backend choice prompt appears after mode selection
- Selecting "2" (Anthropic) shows auth validation
- Model picker shows curated list with custom option
- No Ollama-related prompts appear in Anthropic path
- Ctrl-C to exit (don't complete install)

**Step 4: Push**

```bash
git push -u origin dev-installer-update
```
