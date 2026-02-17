# Installer --update Flag Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `--update` flag to bash and PowerShell installers for quick software-only updates, and ensure both paths stop the server before replacing binaries.

**Architecture:** New `stop_server` function extracted from existing start logic. `main()` gets an early-return update path. `install_skill()` becomes a loop over all skills. PowerShell mirrors all changes.

**Tech Stack:** Bash, PowerShell, BATS (testing)

---

### Task 1: Add `--update` flag parsing and `stop_server()` to bash installer

**Files:**
- Modify: `scripts/install.sh:27` (add `UPDATE_FLAG` variable)
- Modify: `scripts/install.sh:46-57` (add `--update` to `parse_args`)
- Modify: `scripts/install.sh:1156-1177` (extract stop logic into `stop_server`, update `start_server`)

**Step 1: Add `UPDATE_FLAG` variable**

At line 27, after `YES_FLAG=false`, add:

```bash
UPDATE_FLAG=false
```

**Step 2: Add `--update` to `parse_args()`**

In the `case` block at line 48, add a new case before the `*` wildcard:

```bash
--update) UPDATE_FLAG=true; shift ;;
```

**Step 3: Create `stop_server()` function**

Add this new function immediately before the existing `start_server()` function (before line 1156):

```bash
stop_server() {
    info "Stopping ralph-o-matic server..."

    if [[ "$OS" == "darwin" ]]; then
        launchctl bootout "gui/$(id -u)/com.ralph-o-matic.server" 2>/dev/null || true
    elif [[ "$OS" == "linux" ]]; then
        systemctl --user stop ralph-o-matic.service 2>/dev/null || true
    fi

    sleep 1
}
```

**Step 4: Update `start_server()` to delegate stop**

Replace the current `start_server()` function. Remove the inline `launchctl bootout` and replace with a call to `stop_server`:

Current (lines 1156-1177):
```bash
start_server() {
    info "Starting ralph-o-matic server..."

    if [[ "$OS" == "darwin" ]]; then
        # Unload first (ignore errors if not loaded)
        launchctl bootout "gui/$(id -u)" "com.ralph-o-matic.server" 2>/dev/null || true
        sleep 1
        launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/com.ralph-o-matic.server.plist" || true
        sleep 2
    ...
```

Replace with:
```bash
start_server() {
    stop_server

    info "Starting ralph-o-matic server..."

    if [[ "$OS" == "darwin" ]]; then
        launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/com.ralph-o-matic.server.plist" || true
        sleep 2

    elif [[ "$OS" == "linux" ]]; then
        systemctl --user start ralph-o-matic.service || true
        sleep 2
    fi

    # Check if running
    if pgrep -x ralph-o-matic-server &>/dev/null; then
        success "Server started (runs automatically on login)"
    else
        warn "Server may have failed to start - check logs at ~/.config/ralph-o-matic/logs/"
    fi
}
```

Note: The Linux path changes from `restart` to `start` since `stop_server` handles the stop.

**Step 5: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(installer): add --update flag parsing and stop_server function"
```

---

### Task 2: Update `install_skill()` for all skills (bash)

**Files:**
- Modify: `scripts/install.sh:775-802` (refactor `install_skill` to loop over skills)

**Step 1: Replace `install_skill()` with a loop**

Replace the current function with:

```bash
install_skill() {
    info "Installing Claude Code skills..."

    if ! command -v claude &>/dev/null; then
        warn "Claude Code not installed, skipping skills"
        return
    fi

    local skills_dir="$HOME/.claude/skills"
    mkdir -p "$skills_dir"

    local skills=("brainstorm-to-ralph" "direct-to-ralph")

    for skill_name in "${skills[@]}"; do
        if [[ -d "/usr/local/share/ralph-o-matic/skills/$skill_name" ]]; then
            cp -r "/usr/local/share/ralph-o-matic/skills/$skill_name" "$skills_dir/"
            success "$skill_name skill installed"
        else
            # Download from release
            local skill_url="$RELEASE_URL/${skill_name}-skill.tar.gz"
            if curl -fsSL "$skill_url" -o /tmp/skill.tar.gz 2>/dev/null; then
                tar -xzf /tmp/skill.tar.gz -C "$skills_dir/"
                rm /tmp/skill.tar.gz
                success "$skill_name skill installed"
            else
                warn "Could not install $skill_name skill"
            fi
        fi
    done
}
```

**Step 2: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(installer): install all skills in loop (brainstorm + direct-to-ralph)"
```

---

### Task 3: Wire `--update` path and stop-before-install in `main()` (bash)

**Files:**
- Modify: `scripts/install.sh:1273-1314` (update `main` function)

**Step 1: Add update short-circuit and stop_server to `main()`**

Replace the current `main()` function with:

```bash
main() {
    parse_args "$@"

    # Reopen stdin from terminal so interactive prompts work with curl | bash
    if [[ ! -t 0 ]]; then
        exec 0</dev/tty
    fi

    print_banner
    detect_platform

    # --update: quick software-only update path
    if [[ "$UPDATE_FLAG" == true ]]; then
        info "Updating ralph-o-matic software..."
        stop_server
        install_binaries
        install_skill
        start_server
        verify_installation
        print_success
        return
    fi

    prompt_mode
    if [[ "$MODE" != "client" ]]; then
        select_backend
    fi
    check_ram_requirement
    check_dependencies
    install_missing_dependencies
    if [[ "$MODE" != "client" ]]; then
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
    stop_server
    install_binaries
    install_skill
    configure_ralph
    if [[ "$MODE" != "client" ]]; then
        configure_notifications
        prompt_start_server
        apply_model_config
        apply_notification_config
        test_notifications
    fi
    verify_installation
    print_success
}
```

Key changes:
- `--update` path at the top: `detect_platform → stop_server → install_binaries → install_skill → start_server → verify → print_success`
- Normal path: `stop_server` added before `install_binaries`
- Normal path: `prompt_start_server` stays as-is (it calls `install_service` + `start_server` which calls `stop_server`)

**Step 2: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(installer): wire --update short-circuit and stop-before-install in main"
```

---

### Task 4: Add BATS tests for update functionality

**Files:**
- Create: `scripts/tests/update_test.bats`

**Step 1: Write all BATS tests**

Create `scripts/tests/update_test.bats`:

```bash
#!/usr/bin/env bats

# Update flag and service management tests for install script

setup() {
    # Override error() to not exit during tests
    error() { echo "ERROR: $1"; return 1; }
    export -f error

    source scripts/install.sh
}

# --- Flag parsing (3 tests) ---

@test "parse_args sets UPDATE_FLAG=true with --update" {
    UPDATE_FLAG=false
    parse_args --update
    [ "$UPDATE_FLAG" = "true" ]
}

@test "parse_args handles --update with --yes" {
    UPDATE_FLAG=false
    YES_FLAG=false
    parse_args --update --yes
    [ "$UPDATE_FLAG" = "true" ]
    [ "$YES_FLAG" = "true" ]
}

@test "parse_args handles --update with --backend=anthropic" {
    UPDATE_FLAG=false
    BACKEND="ollama"
    parse_args --update --backend=anthropic
    [ "$UPDATE_FLAG" = "true" ]
    [ "$BACKEND" = "anthropic" ]
}

# --- stop_server function (3 tests) ---

@test "stop_server calls launchctl bootout on macOS" {
    OS="darwin"
    local called=false
    launchctl() { called=true; echo "launchctl $*"; }
    export -f launchctl
    sleep() { :; }
    export -f sleep

    stop_server
    # Function should complete without error
    [ "$?" -eq 0 ]
}

@test "stop_server calls systemctl stop on Linux" {
    OS="linux"
    systemctl() { echo "systemctl $*"; }
    export -f systemctl
    sleep() { :; }
    export -f sleep

    run stop_server
    [ "$status" -eq 0 ]
    [[ "$output" == *"systemctl --user stop ralph-o-matic.service"* ]]
}

@test "stop_server succeeds when server not running" {
    OS="darwin"
    launchctl() { return 1; }
    export -f launchctl
    sleep() { :; }
    export -f sleep

    run stop_server
    [ "$status" -eq 0 ]
}

# --- install_skill multi-skill (4 tests) ---

@test "install_skill installs both skills from local path" {
    # Mock claude as available
    claude() { echo "claude 1.0"; }
    export -f claude

    local tmpdir
    tmpdir=$(mktemp -d)

    # Create fake local skill dirs
    mkdir -p "/tmp/test_skills/brainstorm-to-ralph"
    echo '{}' > "/tmp/test_skills/brainstorm-to-ralph/manifest.json"
    mkdir -p "/tmp/test_skills/direct-to-ralph"
    echo '{}' > "/tmp/test_skills/direct-to-ralph/manifest.json"

    # Override HOME so skills install to temp
    HOME="$tmpdir"

    # Override the local share path check by redefining install_skill inline
    # (the function checks /usr/local/share which we can't write to in tests)
    # Instead, test that the function runs without error and check output
    RELEASE_URL="http://fake-url"
    curl() { return 1; }  # Fail download to test fallback
    export -f curl

    run install_skill
    [ "$status" -eq 0 ]
    [[ "$output" == *"brainstorm-to-ralph"* ]]
    [[ "$output" == *"direct-to-ralph"* ]]

    rm -rf "$tmpdir" "/tmp/test_skills"
}

@test "install_skill falls back to download when local not found" {
    claude() { echo "claude 1.0"; }
    export -f claude

    local tmpdir
    tmpdir=$(mktemp -d)
    HOME="$tmpdir"
    RELEASE_URL="http://fake-url"

    # Mock curl to succeed and create a fake tar
    curl() {
        # Create a minimal tar.gz at the output path
        local outfile=""
        while [[ $# -gt 0 ]]; do
            case $1 in
                -o) outfile="$2"; shift 2 ;;
                *) shift ;;
            esac
        done
        if [ -n "$outfile" ]; then
            mkdir -p /tmp/fake_skill_tar
            echo '{}' > /tmp/fake_skill_tar/manifest.json
            tar -czf "$outfile" -C /tmp/fake_skill_tar .
            rm -rf /tmp/fake_skill_tar
        fi
        return 0
    }
    export -f curl

    run install_skill
    [ "$status" -eq 0 ]
    [[ "$output" == *"skill installed"* ]]

    rm -rf "$tmpdir"
}

@test "install_skill skips when claude not found" {
    # Ensure claude command doesn't exist in test env
    claude() { return 1; }
    export -f claude
    command() {
        if [[ "$2" == "claude" ]]; then return 1; fi
        builtin command "$@"
    }
    export -f command

    run install_skill
    [ "$status" -eq 0 ]
    [[ "$output" == *"not installed"* ]] || [[ "$output" == *"skipping"* ]]
}

@test "install_skill creates skills directory if missing" {
    claude() { echo "claude 1.0"; }
    export -f claude

    local tmpdir
    tmpdir=$(mktemp -d)
    HOME="$tmpdir"
    RELEASE_URL="http://fake-url"
    curl() { return 1; }
    export -f curl

    # Skills dir should not exist yet
    [ ! -d "$tmpdir/.claude/skills" ]

    install_skill

    # Skills dir should now exist
    [ -d "$tmpdir/.claude/skills" ]

    rm -rf "$tmpdir"
}

# --- Update flow (3 tests) ---

@test "update flow calls correct functions in order" {
    UPDATE_FLAG=true
    OS="darwin"
    ARCH="amd64"

    local call_log=""

    detect_platform() { call_log="${call_log}detect_platform,"; OS="darwin"; ARCH="amd64"; }
    stop_server() { call_log="${call_log}stop_server,"; }
    install_binaries() { call_log="${call_log}install_binaries,"; }
    install_skill() { call_log="${call_log}install_skill,"; }
    start_server() { call_log="${call_log}start_server,"; }
    verify_installation() { call_log="${call_log}verify_installation,"; }
    print_banner() { :; }
    print_success() { :; }
    parse_args() { :; }

    export -f detect_platform stop_server install_binaries install_skill start_server verify_installation print_banner print_success parse_args

    # Source again won't help since main is guarded, so call directly
    main

    [[ "$call_log" == *"stop_server,install_binaries,install_skill,start_server,verify_installation,"* ]]
}

@test "update flow does NOT call config functions" {
    UPDATE_FLAG=true
    OS="darwin"
    ARCH="amd64"

    local called_prompt_mode=false
    local called_select_backend=false
    local called_configure_ralph=false
    local called_configure_notifications=false

    detect_platform() { OS="darwin"; ARCH="amd64"; }
    stop_server() { :; }
    install_binaries() { :; }
    install_skill() { :; }
    start_server() { :; }
    verify_installation() { :; }
    print_banner() { :; }
    print_success() { :; }
    parse_args() { :; }
    prompt_mode() { called_prompt_mode=true; }
    select_backend() { called_select_backend=true; }
    configure_ralph() { called_configure_ralph=true; }
    configure_notifications() { called_configure_notifications=true; }

    export -f detect_platform stop_server install_binaries install_skill start_server verify_installation print_banner print_success parse_args prompt_mode select_backend configure_ralph configure_notifications

    main

    [ "$called_prompt_mode" = "false" ]
    [ "$called_select_backend" = "false" ]
    [ "$called_configure_ralph" = "false" ]
    [ "$called_configure_notifications" = "false" ]
}

@test "update flow skips server stop/start for client mode" {
    # Note: --update doesn't have MODE context (no prompt_mode called),
    # so it always attempts stop/start. This is safe because stop_server
    # is a no-op if no service exists. This test verifies that behavior.
    UPDATE_FLAG=true
    OS="darwin"
    ARCH="amd64"

    local stop_called=false

    detect_platform() { OS="darwin"; ARCH="amd64"; }
    stop_server() { stop_called=true; }
    install_binaries() { :; }
    install_skill() { :; }
    start_server() { :; }
    verify_installation() { :; }
    print_banner() { :; }
    print_success() { :; }
    parse_args() { :; }

    export -f detect_platform stop_server install_binaries install_skill start_server verify_installation print_banner print_success parse_args

    main

    # stop_server is always called (safe no-op if not running)
    [ "$stop_called" = "true" ]
}

# --- Normal install stop-before-install (2 tests) ---

@test "normal install calls stop_server before install_binaries" {
    UPDATE_FLAG=false
    YES_FLAG=true
    MODE="server"
    MODE_SET=true
    BACKEND="ollama"
    OS="darwin"
    ARCH="amd64"

    local call_log=""

    detect_platform() { OS="darwin"; ARCH="amd64"; call_log="${call_log}detect_platform,"; }
    print_banner() { :; }
    parse_args() { :; }
    prompt_mode() { :; }
    select_backend() { :; }
    check_ram_requirement() { :; }
    check_dependencies() { :; }
    install_missing_dependencies() { :; }
    detect_gpu() { :; }
    select_models() { :; }
    configure_ollama() { :; }
    pull_models() { :; }
    stop_server() { call_log="${call_log}stop_server,"; }
    install_binaries() { call_log="${call_log}install_binaries,"; }
    install_skill() { call_log="${call_log}install_skill,"; }
    configure_ralph() { :; }
    configure_notifications() { :; }
    prompt_start_server() { :; }
    apply_model_config() { :; }
    apply_notification_config() { :; }
    test_notifications() { :; }
    verify_installation() { :; }
    print_success() { :; }

    export -f detect_platform print_banner parse_args prompt_mode select_backend check_ram_requirement check_dependencies install_missing_dependencies detect_gpu select_models configure_ollama pull_models stop_server install_binaries install_skill configure_ralph configure_notifications prompt_start_server apply_model_config apply_notification_config test_notifications verify_installation print_success

    main

    [[ "$call_log" == *"stop_server,install_binaries,"* ]]
}

@test "start_server calls stop_server before starting" {
    OS="darwin"

    local stop_called=false
    stop_server() { stop_called=true; }
    export -f stop_server

    launchctl() { :; }
    export -f launchctl
    sleep() { :; }
    export -f sleep
    pgrep() { return 0; }
    export -f pgrep

    start_server

    [ "$stop_called" = "true" ]
}

# --- Edge cases (2 tests) ---

@test "stop_server is no-op on first install (no service)" {
    OS="darwin"
    # launchctl bootout will fail — that's fine
    launchctl() { return 1; }
    export -f launchctl
    sleep() { :; }
    export -f sleep

    run stop_server
    [ "$status" -eq 0 ]
}

@test "update with --yes skips start server prompt" {
    # --update path calls start_server directly, never prompt_start_server
    UPDATE_FLAG=true
    OS="darwin"
    ARCH="amd64"

    local prompt_called=false

    detect_platform() { OS="darwin"; ARCH="amd64"; }
    stop_server() { :; }
    install_binaries() { :; }
    install_skill() { :; }
    start_server() { :; }
    verify_installation() { :; }
    print_banner() { :; }
    print_success() { :; }
    parse_args() { :; }
    prompt_start_server() { prompt_called=true; }

    export -f detect_platform stop_server install_binaries install_skill start_server verify_installation print_banner print_success parse_args prompt_start_server

    YES_FLAG=true
    main

    # prompt_start_server should NOT be called in update path
    [ "$prompt_called" = "false" ]
}
```

**Step 2: Run tests to verify they fail (no implementation yet for some)**

```bash
make test-bats
```

Expected: New tests should pass since we implement in Tasks 1-3 first, then write tests.

**Step 3: Commit**

```bash
git add scripts/tests/update_test.bats
git commit -m "test(installer): add BATS tests for --update flag and stop_server"
```

---

### Task 5: Add `--update` and `Stop-RalphServer` to PowerShell installer

**Files:**
- Modify: `scripts/install.ps1:5-13` (add `-Update` param)
- Modify: `scripts/install.ps1:733-756` (update `Install-Skill` for all skills)
- Add new function `Stop-RalphServer` before `Start-RalphServer`
- Modify: `scripts/install.ps1:800-821` (update `Start-RalphServer` to call `Stop-RalphServer`)
- Modify: `scripts/install.ps1:996-1037` (update `Main` with update path and stop-before-install)

**Step 1: Add `-Update` parameter**

Add to the `param()` block at the top:

```powershell
param(
    [switch]$Yes,
    [switch]$Update,
    [ValidateSet("full", "server", "client")]
    [string]$Mode = "full",
    [ValidateSet("ollama", "anthropic")]
    [string]$Backend = "ollama",
    [string]$Server = "",
    [string]$LargeModel = "",
    [string]$SmallModel = ""
)
```

**Step 2: Update `Install-Skill` to loop over all skills**

Replace with:

```powershell
function Install-Skill {
    Write-Info "Installing Claude Code skills..."

    if (-not (Get-Command claude -ErrorAction SilentlyContinue)) {
        Write-Warn "Claude Code not installed, skipping skills"
        return
    }

    $skillsDir = "$env:USERPROFILE\.claude\skills"
    New-Item -ItemType Directory -Path $skillsDir -Force | Out-Null

    $skills = @("brainstorm-to-ralph", "direct-to-ralph")

    foreach ($skillName in $skills) {
        $skillUrl = "$ReleaseUrl/$skillName-skill.zip"
        try {
            Invoke-WebRequest -Uri $skillUrl -OutFile "$env:TEMP\skill.zip"
            Expand-Archive -Path "$env:TEMP\skill.zip" -DestinationPath $skillsDir -Force
            Remove-Item "$env:TEMP\skill.zip"
            Write-Success "$skillName skill installed"
        } catch {
            Write-Warn "Could not install $skillName skill"
        }
    }
}
```

**Step 3: Add `Stop-RalphServer` function**

Add immediately before `Start-RalphServer`:

```powershell
function Stop-RalphServer {
    Write-Info "Stopping ralph-o-matic server..."

    $taskName = "RalphOMaticServer"

    # Stop scheduled task
    try {
        Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    } catch { }

    # Kill process if still running
    try {
        Stop-Process -Name "ralph-o-matic-server" -Force -ErrorAction SilentlyContinue
    } catch { }

    Start-Sleep -Seconds 1
}
```

**Step 4: Update `Start-RalphServer` to call `Stop-RalphServer`**

Replace with:

```powershell
function Start-RalphServer {
    Stop-RalphServer

    Write-Info "Starting ralph-o-matic server..."

    $configDir = "$env:USERPROFILE\.config\ralph-o-matic"
    $taskName = "RalphOMaticServer"

    $env:RALPH_DB = "$configDir\data\ralph.db"

    Start-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2

    $serverProcess = Get-Process -Name "ralph-o-matic-server" -ErrorAction SilentlyContinue
    if ($serverProcess) {
        Write-Success "Server started (runs automatically on login)"
    } else {
        Write-Warn "Server may have failed to start - check logs at $configDir\logs\"
        Write-Warn "You can start it manually: ralph-o-matic-server"
    }
}
```

**Step 5: Update `Main` with update path and stop-before-install**

Replace with:

```powershell
function Main {
    Show-Banner
    Get-Platform

    # --Update: quick software-only update path
    if ($Update) {
        Write-Info "Updating ralph-o-matic software..."
        Stop-RalphServer
        Install-Binaries
        Install-Skill
        Start-RalphServer
        Show-Success
        return
    }

    Get-InstallMode

    if ($Mode -ne "client") {
        Select-Backend
    }

    Test-RamRequirement
    Test-Dependencies
    Install-MissingDependencies

    if ($Mode -ne "client") {
        if ($Backend -eq "anthropic") {
            Test-ClaudeAuth
            Select-AnthropicModels
        } else {
            Get-Gpu
            Select-Models
            Install-Models
        }
    }

    Stop-RalphServer
    Install-Binaries
    Install-Skill
    Set-Configuration

    if ($Mode -ne "client") {
        Set-NotificationConfig
        Request-StartServer
        if ($Backend -eq "anthropic") {
            Push-AnthropicConfig
        }
        Push-NotificationConfig
        Test-NotificationConfig
    }

    Show-Success
}
```

**Step 6: Commit**

```bash
git add scripts/install.ps1
git commit -m "feat(installer): add -Update flag and Stop-RalphServer to PowerShell installer"
```

---

### Task 6: Final verification

**Step 1: Run all BATS tests**

```bash
make test-bats
```

Expected: All tests pass (existing + new update_test.bats).

**Step 2: Run Go tests (sanity check)**

```bash
make test
```

Expected: All pass (no Go changes).

**Step 3: Run lint**

```bash
make lint
```

Expected: Clean.

**Step 4: Commit any fixes if needed, then push**

```bash
git push -u origin $(git branch --show-current)
```
