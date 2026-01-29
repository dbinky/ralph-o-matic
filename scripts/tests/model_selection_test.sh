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

    [[ "$LARGE_MODEL" == *"70b"* ]] || [[ "$LARGE_MODEL" == *"32b"* ]]
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

    # Should not pick 70b for 8GB machine
    [[ "$LARGE_MODEL" != *"70b"* ]]
    [ -n "$SMALL_MODEL" ]
    [ "$INFERENCE_MODE" = "cpu_only" ]
}

@test "configure_ralph writes structured config" {
    OS="linux"
    ARCH="amd64"
    MODE="server"
    LARGE_MODEL="qwen2.5-coder:14b"
    SMALL_MODEL="qwen2.5-coder:7b"
    OLLAMA_URL="http://localhost:11434"
    INFERENCE_MODE="cpu_only"

    # Use temp dir for config
    export HOME="$(mktemp -d)"
    mkdir -p "$HOME/.config/ralph-o-matic"

    configure_ralph

    local config_file="$HOME/.config/ralph-o-matic/config.yaml"
    [ -f "$config_file" ]

    # Check config contains model info
    grep -q "large_model" "$config_file"
    grep -q "qwen2.5-coder:14b" "$config_file"

    # Cleanup
    rm -rf "$HOME"
}
