#!/usr/bin/env bats

# Tests for --update flag, stop_server, install_skill, and update flow

setup() {
    # Override error() to not exit during tests
    error() { echo "ERROR: $1"; return 1; }
    export -f error

    source scripts/install.sh
}

# Helper: exercise the main() logic without the exec 0</dev/tty redirect.
# We replicate main()'s body with the tty redirect removed so tests can
# run in non-interactive BATS environments. All side-effect functions are
# expected to be mocked by the caller.
_run_main_logic() {
    parse_args "$@"
    print_banner
    detect_platform

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

# =============================================================================
# 1. Flag parsing
# =============================================================================

@test "parse_args: --update sets UPDATE_FLAG=true" {
    UPDATE_FLAG=false

    parse_args --update

    [ "$UPDATE_FLAG" = "true" ]
}

@test "parse_args: --update --yes sets both flags" {
    UPDATE_FLAG=false
    YES_FLAG=false

    parse_args --update --yes

    [ "$UPDATE_FLAG" = "true" ]
    [ "$YES_FLAG" = "true" ]
}

@test "parse_args: --update --backend=anthropic both parsed" {
    UPDATE_FLAG=false
    BACKEND="ollama"

    parse_args --update --backend=anthropic

    [ "$UPDATE_FLAG" = "true" ]
    [ "$BACKEND" = "anthropic" ]
}

# =============================================================================
# 2. stop_server function
# =============================================================================

@test "stop_server on macOS calls launchctl" {
    OS="darwin"

    # Track what launchctl receives
    launchctl() { echo "launchctl $*"; return 0; }
    export -f launchctl
    # Stub id for the gui/UID path
    id() { echo "501"; }
    export -f id
    # Stub sleep to avoid waiting
    sleep() { :; }
    export -f sleep

    run stop_server
    [ "$status" -eq 0 ]
    [[ "$output" == *"launchctl bootout"* ]]
}

@test "stop_server on Linux calls systemctl" {
    OS="linux"

    systemctl() { echo "systemctl $*"; return 0; }
    export -f systemctl
    sleep() { :; }
    export -f sleep

    run stop_server
    [ "$status" -eq 0 ]
    [[ "$output" == *"systemctl --user stop"* ]]
}

@test "stop_server succeeds when server not running (launchctl returns 1)" {
    OS="darwin"

    # launchctl fails (server not loaded) — should still succeed due to || true
    launchctl() { return 1; }
    export -f launchctl
    id() { echo "501"; }
    export -f id
    sleep() { :; }
    export -f sleep

    run stop_server
    [ "$status" -eq 0 ]
}

# =============================================================================
# 3. install_skill multi-skill support
# =============================================================================

@test "install_skill skips when claude command not found" {
    command() {
        if [[ "$2" == "claude" ]]; then return 1; fi
        builtin command "$@"
    }
    export -f command

    run install_skill
    [ "$status" -eq 0 ]
    [[ "$output" == *"skipping"* ]]
}

@test "install_skill falls back to manual install when plugin commands fail" {
    export HOME="$(mktemp -d)"

    # claude exists but plugin commands fail
    command() {
        if [[ "$1" == "-v" ]] && [[ "$2" == "claude" ]]; then return 0; fi
        builtin command "$@"
    }
    export -f command

    # Stub claude to fail (plugin commands not available or marketplace not configured)
    claude() { return 1; }
    export -f claude

    # Stub curl to fail (no remote skills available) — skills won't be found locally either
    curl() { return 1; }
    export -f curl

    # Skills dir should not exist yet
    [ ! -d "$HOME/.claude/skills" ]

    run install_skill

    # Fallback path should have created the directory
    [ -d "$HOME/.claude/skills" ]

    # Cleanup
    rm -rf "$HOME"
}

# =============================================================================
# 4. Update flow (mock all functions, verify call order)
# =============================================================================

@test "update flow calls stop_server, install_binaries, install_skill, start_server, verify_installation" {
    call_log=""

    # Override parse_args to set UPDATE_FLAG directly
    parse_args() { UPDATE_FLAG=true; }
    print_banner() { :; }
    detect_platform() { :; }
    stop_server() { call_log="${call_log}stop_server "; }
    install_binaries() { call_log="${call_log}install_binaries "; }
    install_skill() { call_log="${call_log}install_skill "; }
    start_server() { call_log="${call_log}start_server "; }
    verify_installation() { call_log="${call_log}verify_installation "; }
    print_success() { :; }

    _run_main_logic

    [[ "$call_log" == *"stop_server"* ]]
    [[ "$call_log" == *"install_binaries"* ]]
    [[ "$call_log" == *"install_skill"* ]]
    [[ "$call_log" == *"start_server"* ]]
    [[ "$call_log" == *"verify_installation"* ]]

    # Verify order: stop_server before install_binaries before start_server
    local pos_stop pos_install pos_start
    pos_stop=$(echo "$call_log" | grep -bo "stop_server" | head -1 | cut -d: -f1)
    pos_install=$(echo "$call_log" | grep -bo "install_binaries" | head -1 | cut -d: -f1)
    pos_start=$(echo "$call_log" | grep -bo "start_server" | head -1 | cut -d: -f1)

    [ "$pos_stop" -lt "$pos_install" ]
    [ "$pos_install" -lt "$pos_start" ]
}

@test "update flow does NOT call prompt_mode, select_backend, configure_ralph, configure_notifications" {
    call_log=""

    parse_args() { UPDATE_FLAG=true; }
    print_banner() { :; }
    detect_platform() { :; }
    stop_server() { :; }
    install_binaries() { :; }
    install_skill() { :; }
    start_server() { :; }
    verify_installation() { :; }
    print_success() { :; }
    prompt_mode() { call_log="${call_log}prompt_mode "; }
    select_backend() { call_log="${call_log}select_backend "; }
    configure_ralph() { call_log="${call_log}configure_ralph "; }
    configure_notifications() { call_log="${call_log}configure_notifications "; }

    _run_main_logic

    [[ "$call_log" != *"prompt_mode"* ]]
    [[ "$call_log" != *"select_backend"* ]]
    [[ "$call_log" != *"configure_ralph"* ]]
    [[ "$call_log" != *"configure_notifications"* ]]
}

@test "update flow with --yes skips prompt_start_server" {
    call_log=""

    parse_args() { UPDATE_FLAG=true; YES_FLAG=true; }
    print_banner() { :; }
    detect_platform() { :; }
    stop_server() { call_log="${call_log}stop_server "; }
    install_binaries() { call_log="${call_log}install_binaries "; }
    install_skill() { call_log="${call_log}install_skill "; }
    start_server() { call_log="${call_log}start_server "; }
    verify_installation() { call_log="${call_log}verify_installation "; }
    print_success() { :; }
    prompt_start_server() { call_log="${call_log}prompt_start_server "; }

    _run_main_logic

    # Update flow calls start_server directly, not prompt_start_server
    [[ "$call_log" == *"start_server"* ]]
    [[ "$call_log" != *"prompt_start_server"* ]]
}

# =============================================================================
# 5. Normal install stop-before-install
# =============================================================================

@test "normal install calls stop_server before install_binaries" {
    call_log=""

    # Set up for normal (non-update) install, client mode to minimize branches
    parse_args() { UPDATE_FLAG=false; YES_FLAG=true; MODE="client"; MODE_SET=true; }
    print_banner() { :; }
    detect_platform() { :; }
    prompt_mode() { :; }
    select_backend() { :; }
    check_ram_requirement() { :; }
    check_dependencies() { :; }
    install_missing_dependencies() { :; }
    stop_server() { call_log="${call_log}stop_server "; }
    install_binaries() { call_log="${call_log}install_binaries "; }
    install_skill() { :; }
    configure_ralph() { :; }
    verify_installation() { :; }
    print_success() { :; }

    _run_main_logic

    [[ "$call_log" == *"stop_server"* ]]
    [[ "$call_log" == *"install_binaries"* ]]

    # Verify order
    local pos_stop pos_install
    pos_stop=$(echo "$call_log" | grep -bo "stop_server" | head -1 | cut -d: -f1)
    pos_install=$(echo "$call_log" | grep -bo "install_binaries" | head -1 | cut -d: -f1)
    [ "$pos_stop" -lt "$pos_install" ]
}

@test "start_server calls stop_server before starting" {
    call_log=""

    OS="darwin"

    # Track stop_server being called from within start_server
    stop_server() { call_log="${call_log}stop_server "; }
    export -f stop_server

    # Mock launchctl and pgrep so start_server doesn't actually run anything
    launchctl() { return 0; }
    export -f launchctl
    pgrep() { return 0; }
    export -f pgrep
    sleep() { :; }
    export -f sleep

    start_server

    [[ "$call_log" == *"stop_server"* ]]
}

# =============================================================================
# 6. Edge cases
# =============================================================================

@test "stop_server is no-op on first install (launchctl fails, function still succeeds)" {
    OS="darwin"

    # Simulate first install: launchctl fails because service was never loaded
    launchctl() { echo "Could not find service" >&2; return 113; }
    export -f launchctl
    id() { echo "501"; }
    export -f id
    sleep() { :; }
    export -f sleep

    run stop_server
    [ "$status" -eq 0 ]
}
