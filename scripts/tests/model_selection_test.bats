#!/usr/bin/env bats

# Model selection tests for install script

setup() {
    # Source install script functions (main is guarded by BASH_SOURCE check)
    # Override error() to not exit during tests
    error() { echo "ERROR: $1"; return 1; }
    export -f error

    source scripts/install.sh
}

@test "show_hardware_summary outputs system info" {
    OS="linux"
    ARCH="amd64"
    RAM_GB=64
    GPU_TYPE="nvidia"
    GPU_VRAM_MB=24576

    run show_hardware_summary
    [ "$status" -eq 0 ]
    [[ "$output" == *"RAM"* ]]
    [[ "$output" == *"64"* ]]
    [[ "$output" == *"NVIDIA"* ]]
}

@test "select_models with --yes auto-accepts recommendation" {
    OS="linux"
    ARCH="amd64"
    RAM_GB=64
    GPU_TYPE="nvidia"
    GPU_VRAM_MB=24576
    GPU_CAN_RUN_LARGE=false
    GPU_CAN_RUN_SMALL=true
    YES_FLAG=true
    LARGE_MODEL=""
    SMALL_MODEL=""

    select_models

    [[ "$LARGE_MODEL" == "devstral" ]] || [[ "$LARGE_MODEL" == "qwen3-coder:30b" ]]
    [ -n "$SMALL_MODEL" ]
    [ -n "$INFERENCE_MODE" ]
}

@test "select_models for small machine picks smaller models" {
    OS="linux"
    ARCH="amd64"
    RAM_GB=8
    GPU_TYPE="none"
    GPU_VRAM_MB=0
    GPU_CAN_RUN_LARGE=false
    GPU_CAN_RUN_SMALL=false
    YES_FLAG=true
    LARGE_MODEL=""
    SMALL_MODEL=""

    select_models

    # Should pick qwen3:8b for 8GB machine
    [[ "$LARGE_MODEL" == "qwen3:8b" ]]
    [ -n "$SMALL_MODEL" ]
    [ "$INFERENCE_MODE" = "cpu_only" ]
}

@test "configure_notifications skips with --yes flag" {
    YES_FLAG=true
    MODE="server"

    configure_notifications

    [ "$NOTIFY_SMTP_ENABLED" = "false" ]
    [ "$NOTIFY_TEAMS_ENABLED" = "false" ]
}

@test "configure_notifications skips in client mode" {
    YES_FLAG=false
    MODE="client"

    configure_notifications

    [ "$NOTIFY_SMTP_ENABLED" = "false" ]
    [ "$NOTIFY_TEAMS_ENABLED" = "false" ]
}

@test "apply_notification_config skips when nothing configured" {
    NOTIFY_SMTP_ENABLED=false
    NOTIFY_TEAMS_ENABLED=false

    # Should return immediately without errors
    run apply_notification_config
    [ "$status" -eq 0 ]
}

@test "test_notifications skips when nothing configured" {
    NOTIFY_SMTP_ENABLED=false
    NOTIFY_TEAMS_ENABLED=false

    # Should return immediately without errors
    run test_notifications
    [ "$status" -eq 0 ]
}

@test "configure_ralph writes CLI config in server mode" {
    OS="linux"
    ARCH="amd64"
    MODE="server"
    LARGE_MODEL="qwen3:14b"
    SMALL_MODEL="qwen3:8b"
    OLLAMA_URL="http://localhost:11434"
    INFERENCE_MODE="cpu_only"

    # Use temp dir for config
    export HOME="$(mktemp -d)"
    mkdir -p "$HOME/.config/ralph-o-matic"

    configure_ralph

    local config_file="$HOME/.config/ralph-o-matic/config.yaml"
    [ -f "$config_file" ]

    # Server mode writes CLI-compatible config (model config goes to API)
    grep -q "server: http://localhost:9090" "$config_file"
    grep -q "default_priority: normal" "$config_file"
    grep -q "default_max_iterations: 50" "$config_file"

    # Should NOT contain server-side model config (that goes via API)
    ! grep -q "ollama:" "$config_file"
    ! grep -q "large_model:" "$config_file"

    # Cleanup
    rm -rf "$HOME"
}

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

@test "select_anthropic_models picks opus with choice 1" {
    LARGE_MODEL=""
    SMALL_MODEL=""
    YES_FLAG=false

    # With -n 1, read consumes one character at a time (no newlines needed)
    select_anthropic_models <<< $'11'

    [ "$LARGE_MODEL" = "claude-opus-4-6" ]
    [ "$SMALL_MODEL" = "claude-haiku-4-5-20251001" ]
}

@test "select_anthropic_models picks sonnet-4-6 for both with choices 2,2" {
    LARGE_MODEL=""
    SMALL_MODEL=""
    YES_FLAG=false

    # With -n 1, read consumes one character at a time (no newlines needed)
    select_anthropic_models <<< $'22'

    [ "$LARGE_MODEL" = "claude-sonnet-4-6" ]
    [ "$SMALL_MODEL" = "claude-sonnet-4-6" ]
}

@test "select_anthropic_models auto-selects defaults with --yes flag" {
    LARGE_MODEL=""
    SMALL_MODEL=""
    YES_FLAG=true

    select_anthropic_models

    [ "$LARGE_MODEL" = "claude-sonnet-4-5-20250929" ]
    [ "$SMALL_MODEL" = "claude-haiku-4-5-20251001" ]
}

@test "apply_model_config sends anthropic payload when backend is anthropic" {
    BACKEND="anthropic"
    LARGE_MODEL="claude-opus-4-6"
    SMALL_MODEL="claude-haiku-4-5-20251001"

    curl() {
        # Server health check (curl -sf URL)
        if [[ "$1" == "-sf" ]] && [[ "$2" != "-X" ]]; then
            return 0
        fi
        # PATCH call - return HTTP 200 status code (mimics -w '%{http_code}')
        printf "200"
        return 0
    }
    export -f curl

    run apply_model_config
    [ "$status" -eq 0 ]
    # Must use Anthropic-specific success message, not the Ollama one
    [[ "$output" == *"Anthropic config applied"* ]]
    # Must NOT contain Ollama-style device config
    [[ "$output" != *"[cpu]"* ]]
    [[ "$output" != *"[gpu]"* ]]
}

@test "apply_model_config warns on anthropic config HTTP failure" {
    BACKEND="anthropic"
    LARGE_MODEL="claude-opus-4-6"
    SMALL_MODEL="claude-haiku-4-5-20251001"

    curl() {
        if [[ "$1" == "-sf" ]] && [[ "$2" != "-X" ]]; then
            return 0  # health check passes
        fi
        printf "500"
        return 0
    }
    export -f curl

    run apply_model_config
    [ "$status" -eq 0 ]
    [[ "$output" == *"Config update failed (HTTP 500)"* ]]
}

@test "apply_model_config fails gracefully when server unreachable" {
    OLLAMA_URL="http://localhost:11434"
    LARGE_MODEL="qwen3:14b"
    SMALL_MODEL="qwen3:8b"
    INFERENCE_MODE="cpu_only"

    # Override curl to simulate unreachable server
    curl() { return 1; }
    export -f curl

    run apply_model_config
    [ "$status" -eq 0 ]
    [[ "$output" == *"not responding"* ]]
}

# --- Tests added for comprehensive Anthropic path coverage ---

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

@test "validate_claude_auth succeeds with valid auth" {
    claude() { echo "OK"; }
    export -f claude

    run validate_claude_auth
    [ "$status" -eq 0 ]
    [[ "$output" == *"authenticated"* ]]
}

@test "check_ram_requirement skips for anthropic backend" {
    MODE="server"
    BACKEND="anthropic"
    RAM_GB=4  # Would normally fail

    run check_ram_requirement
    [ "$status" -eq 0 ]
}

@test "apply_model_config sends ollama payload when backend is ollama" {
    BACKEND="ollama"
    OLLAMA_URL="http://localhost:11434"
    LARGE_MODEL="devstral"
    SMALL_MODEL="qwen3:8b"
    INFERENCE_MODE="gpu_only"

    curl() {
        # Server health check
        if [[ "$1" == "-sf" ]] && [[ "$2" != "-X" ]]; then
            return 0
        fi
        # PATCH call — return success
        return 0
    }
    jq() { echo '{"ollama":{"host":"test"}}'; }
    export -f curl jq

    run apply_model_config
    [ "$status" -eq 0 ]
    [[ "$output" == *"Model config applied"* ]]
}

@test "select_anthropic_models defaults on invalid input" {
    YES_FLAG=false
    LARGE_MODEL=""
    SMALL_MODEL=""

    # Send "9" for large (invalid) and "9" for small (invalid)
    select_anthropic_models <<< $'99'

    [ "$LARGE_MODEL" = "claude-sonnet-4-6" ]
    [ "$SMALL_MODEL" = "claude-haiku-4-5-20251001" ]
}
