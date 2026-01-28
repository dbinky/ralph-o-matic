#!/usr/bin/env bats

# Load the install script functions
setup() {
    source scripts/install.sh 2>/dev/null || true
}

@test "detect_platform sets OS correctly on Linux" {
    if [[ "$(uname -s)" != "Linux" ]]; then
        skip "Not running on Linux"
    fi

    detect_platform
    [ "$OS" = "linux" ]
}

@test "detect_platform sets OS correctly on macOS" {
    if [[ "$(uname -s)" != "Darwin" ]]; then
        skip "Not running on macOS"
    fi

    detect_platform
    [ "$OS" = "darwin" ]
}

@test "detect_platform detects RAM" {
    detect_platform
    [ "$RAM_GB" -gt 0 ]
}

@test "check_ram_requirement fails with insufficient RAM" {
    MODE="server"
    RAM_GB=16

    run check_ram_requirement
    [ "$status" -eq 1 ]
}

@test "check_ram_requirement passes with sufficient RAM" {
    MODE="server"
    RAM_GB=64

    run check_ram_requirement
    [ "$status" -eq 0 ]
}

@test "check_ram_requirement skips check for client mode" {
    MODE="client"
    RAM_GB=8

    run check_ram_requirement
    [ "$status" -eq 0 ]
}

@test "detect_gpu identifies no GPU when nvidia-smi missing" {
    # This test assumes nvidia-smi is not installed
    if command -v nvidia-smi &>/dev/null; then
        skip "nvidia-smi is installed"
    fi

    detect_gpu
    [ "$GPU_TYPE" = "none" ] || [ "$GPU_TYPE" = "apple" ]
}
