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
