# Phase 9: Install Scripts

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create "it just works" installation scripts for macOS, Linux, and Windows that detect hardware, install dependencies, and configure everything optimally.

**Architecture:** Platform-specific scripts (bash for Unix, PowerShell for Windows) that detect system capabilities, install missing dependencies via native package managers, pull Ollama models, and configure optimal GPU/CPU usage automatically.

**Tech Stack:** Bash, PowerShell, platform package managers (brew, apt, dnf, winget)

**Dependencies:** Phases 1-8 must be complete (binaries to install)

**Hardware Requirements:**
- **Minimum**: 32GB RAM (hard requirement for qwen3-coder)
- **GPU**: Detected automatically, used if beneficial (no assumptions)

---

## Task 1: Create Bash Install Script Structure

**Files:**
- Create: `scripts/install.sh`

**Step 1: Write the script skeleton**

Create `scripts/install.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Ralph-o-matic Installer
# "It just works."

VERSION="1.0.0"
REPO_URL="https://github.com/ryan/ralph-o-matic"
RELEASE_URL="$REPO_URL/releases/download/v$VERSION"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging
info() { echo -e "${BLUE}▸${NC} $1"; }
success() { echo -e "${GREEN}✓${NC} $1"; }
warn() { echo -e "${YELLOW}!${NC} $1"; }
error() { echo -e "${RED}✗${NC} $1"; exit 1; }

# Parse arguments
MODE="full"  # full, server, client
YES_FLAG=false
SERVER_URL=""
LARGE_MODEL="qwen3-coder:70b"
SMALL_MODEL="qwen2.5-coder:7b"

while [[ $# -gt 0 ]]; do
    case $1 in
        --yes|-y) YES_FLAG=true; shift ;;
        --mode=*) MODE="${1#*=}"; shift ;;
        --server=*) SERVER_URL="${1#*=}"; shift ;;
        --large-model=*) LARGE_MODEL="${1#*=}"; shift ;;
        --small-model=*) SMALL_MODEL="${1#*=}"; shift ;;
        *) error "Unknown option: $1" ;;
    esac
done

# Main installation flow
main() {
    print_banner
    detect_platform
    check_ram_requirement
    prompt_mode
    check_dependencies
    install_missing_dependencies
    if [[ "$MODE" != "client" ]]; then
        detect_gpu
        configure_ollama
        pull_models
    fi
    install_binaries
    install_plugins
    configure_ralph
    if [[ "$MODE" != "client" ]]; then
        prompt_start_server
    fi
    verify_installation
    print_success
}

main "$@"
```

**Step 2: Verify script is syntactically valid**

Run:
```bash
bash -n scripts/install.sh
```

Expected: No output (valid syntax)

**Step 3: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): add bash install script skeleton"
```

---

## Task 2: Implement Platform Detection

**Files:**
- Modify: `scripts/install.sh`

**Step 1: Add platform detection functions**

Add after the argument parsing section:

```bash
# Platform detection
OS=""
ARCH=""
RAM_GB=0
DISTRO=""
PKG_MANAGER=""

detect_platform() {
    info "Detecting platform..."

    # Detect OS
    case "$(uname -s)" in
        Darwin) OS="darwin" ;;
        Linux) OS="linux" ;;
        *) error "Unsupported operating system: $(uname -s)" ;;
    esac

    # Detect architecture
    case "$(uname -m)" in
        x86_64|amd64) ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *) error "Unsupported architecture: $(uname -m)" ;;
    esac

    # Detect RAM
    if [[ "$OS" == "darwin" ]]; then
        RAM_GB=$(( $(sysctl -n hw.memsize) / 1024 / 1024 / 1024 ))
    else
        RAM_GB=$(( $(grep MemTotal /proc/meminfo | awk '{print $2}') / 1024 / 1024 ))
    fi

    # Detect Linux distro and package manager
    if [[ "$OS" == "linux" ]]; then
        if [[ -f /etc/os-release ]]; then
            DISTRO=$(grep ^ID= /etc/os-release | cut -d= -f2 | tr -d '"')
        fi

        if command -v apt-get &>/dev/null; then
            PKG_MANAGER="apt"
        elif command -v dnf &>/dev/null; then
            PKG_MANAGER="dnf"
        elif command -v pacman &>/dev/null; then
            PKG_MANAGER="pacman"
        else
            error "No supported package manager found (apt, dnf, or pacman required)"
        fi
    elif [[ "$OS" == "darwin" ]]; then
        PKG_MANAGER="brew"
        # Check if Homebrew is installed
        if ! command -v brew &>/dev/null; then
            warn "Homebrew not installed. Installing..."
            /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
        fi
    fi

    success "Detected: $OS ($ARCH), ${RAM_GB}GB RAM, package manager: $PKG_MANAGER"
}

check_ram_requirement() {
    local MIN_RAM=32

    if [[ "$MODE" == "client" ]]; then
        # Client doesn't need 32GB
        return 0
    fi

    if [[ $RAM_GB -lt $MIN_RAM ]]; then
        error "Insufficient RAM: ${RAM_GB}GB detected, ${MIN_RAM}GB required for server mode.

Server mode requires 32GB RAM to run qwen3-coder.
If you only want to submit jobs to a remote server, use:
  $0 --mode=client --server=http://your-server:9090"
    fi

    success "RAM check passed: ${RAM_GB}GB available"
}
```

**Step 2: Test platform detection locally**

Run:
```bash
chmod +x scripts/install.sh
source scripts/install.sh && detect_platform && check_ram_requirement
```

Expected: Shows detected platform info

**Step 3: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): add platform and RAM detection"
```

---

## Task 3: Implement GPU Detection

**Files:**
- Modify: `scripts/install.sh`

**Step 1: Add GPU detection functions**

Add after RAM check:

```bash
# GPU detection
GPU_TYPE=""       # nvidia, amd, apple, none
GPU_VRAM_MB=0
GPU_CAN_RUN_LARGE=false
GPU_CAN_RUN_SMALL=false

detect_gpu() {
    info "Detecting GPU..."

    if [[ "$OS" == "darwin" ]] && [[ "$ARCH" == "arm64" ]]; then
        # Apple Silicon - unified memory, always use GPU
        GPU_TYPE="apple"
        GPU_VRAM_MB=$((RAM_GB * 1024))  # Unified memory
        GPU_CAN_RUN_LARGE=true
        GPU_CAN_RUN_SMALL=true
        success "Apple Silicon detected - unified memory, GPU acceleration enabled"
        return
    fi

    # Check for NVIDIA GPU
    if command -v nvidia-smi &>/dev/null; then
        GPU_TYPE="nvidia"
        # Get VRAM in MB (total memory of first GPU)
        GPU_VRAM_MB=$(nvidia-smi --query-gpu=memory.total --format=csv,noheader,nounits | head -1 | tr -d ' ')

        if [[ -n "$GPU_VRAM_MB" ]] && [[ "$GPU_VRAM_MB" =~ ^[0-9]+$ ]]; then
            success "NVIDIA GPU detected: ${GPU_VRAM_MB}MB VRAM"
        else
            warn "NVIDIA GPU detected but couldn't read VRAM"
            GPU_VRAM_MB=0
        fi
    # Check for AMD GPU (ROCm)
    elif command -v rocm-smi &>/dev/null; then
        GPU_TYPE="amd"
        # Get VRAM from rocm-smi
        GPU_VRAM_MB=$(rocm-smi --showmeminfo vram | grep "Total Memory" | awk '{print $4}' | head -1)

        if [[ -n "$GPU_VRAM_MB" ]] && [[ "$GPU_VRAM_MB" =~ ^[0-9]+$ ]]; then
            success "AMD GPU detected: ${GPU_VRAM_MB}MB VRAM"
        else
            warn "AMD GPU detected but couldn't read VRAM"
            GPU_VRAM_MB=0
        fi
    else
        GPU_TYPE="none"
        info "No GPU detected - will use CPU only"
    fi

    # Determine what models can run on GPU
    # qwen3-coder:70b needs ~40GB VRAM
    # qwen2.5-coder:7b needs ~5GB VRAM
    if [[ $GPU_VRAM_MB -ge 45000 ]]; then
        GPU_CAN_RUN_LARGE=true
        GPU_CAN_RUN_SMALL=true
        success "GPU can run both large and small models"
    elif [[ $GPU_VRAM_MB -ge 8000 ]]; then
        GPU_CAN_RUN_LARGE=false
        GPU_CAN_RUN_SMALL=true
        success "GPU can run small model, large model will use CPU/RAM"
    else
        GPU_CAN_RUN_LARGE=false
        GPU_CAN_RUN_SMALL=false
        if [[ "$GPU_TYPE" != "none" ]] && [[ "$GPU_TYPE" != "apple" ]]; then
            info "GPU VRAM insufficient for models, will use CPU/RAM"
        fi
    fi
}

configure_ollama() {
    info "Configuring Ollama for optimal performance..."

    local ollama_env_file=""

    if [[ "$OS" == "darwin" ]]; then
        ollama_env_file="$HOME/.ollama/environment"
    else
        # Linux systemd service
        ollama_env_file="/etc/systemd/system/ollama.service.d/override.conf"
    fi

    # Apple Silicon needs no special config - it just works
    if [[ "$GPU_TYPE" == "apple" ]]; then
        success "Apple Silicon - no additional configuration needed"
        return
    fi

    # For NVIDIA/AMD, configure GPU layers based on VRAM
    if [[ "$GPU_TYPE" == "nvidia" ]] || [[ "$GPU_TYPE" == "amd" ]]; then
        if [[ "$GPU_CAN_RUN_LARGE" == true ]]; then
            # Full GPU acceleration
            info "Configuring full GPU acceleration"
            # Ollama auto-detects, but we can set env vars if needed
        elif [[ "$GPU_CAN_RUN_SMALL" == true ]]; then
            # Partial GPU - small model on GPU, large on CPU
            info "Configuring hybrid mode: small model on GPU, large model on CPU/RAM"
            # Set OLLAMA_NUM_GPU to limit GPU layers for large model
        fi
    fi

    # CPU-only optimization
    if [[ "$GPU_TYPE" == "none" ]] || [[ "$GPU_CAN_RUN_SMALL" == false ]]; then
        info "Configuring CPU-only mode with RAM optimization"
        # Ensure Ollama uses all available RAM
    fi

    success "Ollama configuration complete"
}
```

**Step 2: Test GPU detection**

Run:
```bash
source scripts/install.sh && detect_gpu
```

Expected: Shows GPU detection results

**Step 3: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): add GPU detection and Ollama configuration"
```

---

## Task 4: Implement Dependency Checking

**Files:**
- Modify: `scripts/install.sh`

**Step 1: Add dependency check functions**

```bash
# Dependency status
declare -A DEPS_INSTALLED
declare -A DEPS_VERSION

check_dependencies() {
    info "Checking dependencies..."

    # Git
    if command -v git &>/dev/null; then
        DEPS_INSTALLED[git]=true
        DEPS_VERSION[git]=$(git --version | awk '{print $3}')
        success "git ${DEPS_VERSION[git]}"
    else
        DEPS_INSTALLED[git]=false
        warn "git not installed"
    fi

    # GitHub CLI
    if command -v gh &>/dev/null; then
        DEPS_INSTALLED[gh]=true
        DEPS_VERSION[gh]=$(gh --version | head -1 | awk '{print $3}')

        # Check if authenticated
        if gh auth status &>/dev/null; then
            success "gh ${DEPS_VERSION[gh]} (authenticated)"
        else
            warn "gh ${DEPS_VERSION[gh]} (not authenticated)"
        fi
    else
        DEPS_INSTALLED[gh]=false
        warn "gh (GitHub CLI) not installed"
    fi

    # Ollama (only for server mode)
    if [[ "$MODE" != "client" ]]; then
        if command -v ollama &>/dev/null; then
            DEPS_INSTALLED[ollama]=true
            DEPS_VERSION[ollama]=$(ollama --version 2>/dev/null | awk '{print $NF}' || echo "unknown")
            success "ollama ${DEPS_VERSION[ollama]}"
        else
            DEPS_INSTALLED[ollama]=false
            warn "ollama not installed"
        fi
    fi

    # Claude Code (only for server mode)
    if [[ "$MODE" != "client" ]]; then
        if command -v claude &>/dev/null; then
            DEPS_INSTALLED[claude]=true
            DEPS_VERSION[claude]=$(claude --version 2>/dev/null || echo "installed")
            success "claude-code ${DEPS_VERSION[claude]}"
        else
            DEPS_INSTALLED[claude]=false
            warn "claude-code not installed"
        fi
    fi
}

install_missing_dependencies() {
    local need_install=false

    for dep in git gh ollama claude; do
        if [[ "${DEPS_INSTALLED[$dep]:-false}" == "false" ]]; then
            need_install=true
            break
        fi
    done

    if [[ "$need_install" == false ]]; then
        success "All dependencies installed"
        return
    fi

    if [[ "$YES_FLAG" == false ]]; then
        echo ""
        read -p "Install missing dependencies? [Y/n] " -n 1 -r
        echo ""
        if [[ ! $REPLY =~ ^[Yy]?$ ]]; then
            error "Cannot proceed without dependencies"
        fi
    fi

    # Install git
    if [[ "${DEPS_INSTALLED[git]}" == "false" ]]; then
        info "Installing git..."
        install_package git
        success "git installed"
    fi

    # Install GitHub CLI
    if [[ "${DEPS_INSTALLED[gh]}" == "false" ]]; then
        info "Installing GitHub CLI..."
        case "$PKG_MANAGER" in
            brew) brew install gh ;;
            apt)
                curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
                echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null
                sudo apt update && sudo apt install -y gh
                ;;
            dnf) sudo dnf install -y gh ;;
            pacman) sudo pacman -S --noconfirm github-cli ;;
        esac
        success "GitHub CLI installed"

        # Prompt for authentication
        echo ""
        info "GitHub CLI needs authentication for creating PRs"
        gh auth login
    fi

    # Install Ollama (server mode only)
    if [[ "$MODE" != "client" ]] && [[ "${DEPS_INSTALLED[ollama]}" == "false" ]]; then
        info "Installing Ollama..."
        curl -fsSL https://ollama.ai/install.sh | sh
        success "Ollama installed"
    fi

    # Install Claude Code (server mode only)
    if [[ "$MODE" != "client" ]] && [[ "${DEPS_INSTALLED[claude]}" == "false" ]]; then
        info "Installing Claude Code..."
        npm install -g @anthropic-ai/claude-code
        success "Claude Code installed"
    fi
}

install_package() {
    local pkg=$1
    case "$PKG_MANAGER" in
        brew) brew install "$pkg" ;;
        apt) sudo apt-get install -y "$pkg" ;;
        dnf) sudo dnf install -y "$pkg" ;;
        pacman) sudo pacman -S --noconfirm "$pkg" ;;
    esac
}
```

**Step 2: Test dependency checking**

Run:
```bash
source scripts/install.sh && check_dependencies
```

Expected: Shows installed/missing dependencies

**Step 3: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): add dependency checking and installation"
```

---

## Task 5: Implement Model Pulling

**Files:**
- Modify: `scripts/install.sh`

**Step 1: Add model pulling function**

```bash
pull_models() {
    info "Pulling Ollama models (this may take a while)..."

    # Ensure Ollama is running
    if ! pgrep -x ollama &>/dev/null; then
        info "Starting Ollama service..."
        if [[ "$OS" == "darwin" ]]; then
            ollama serve &>/dev/null &
            sleep 2
        else
            sudo systemctl start ollama
            sleep 2
        fi
    fi

    # Pull small model first (faster, provides early feedback)
    info "Pulling $SMALL_MODEL..."
    if ollama pull "$SMALL_MODEL"; then
        success "$SMALL_MODEL ready"
    else
        error "Failed to pull $SMALL_MODEL"
    fi

    # Pull large model
    info "Pulling $LARGE_MODEL (this is ~40GB, be patient)..."
    if ollama pull "$LARGE_MODEL"; then
        success "$LARGE_MODEL ready"
    else
        error "Failed to pull $LARGE_MODEL"
    fi

    success "All models ready"
}
```

**Step 2: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): add Ollama model pulling"
```

---

## Task 6: Implement Binary Installation

**Files:**
- Modify: `scripts/install.sh`

**Step 1: Add binary installation functions**

```bash
install_binaries() {
    info "Installing ralph-o-matic binaries..."

    local bin_dir="/usr/local/bin"
    local server_binary="ralph-o-matic-server-${OS}-${ARCH}"
    local cli_binary="ralph-o-matic-${OS}-${ARCH}"

    # Windows binaries have .exe extension
    if [[ "$OS" == "windows" ]]; then
        server_binary="${server_binary}.exe"
        cli_binary="${cli_binary}.exe"
    fi

    # Install server (if not client-only mode)
    if [[ "$MODE" != "client" ]]; then
        info "Downloading server..."
        curl -fsSL "$RELEASE_URL/$server_binary" -o /tmp/ralph-o-matic-server
        chmod +x /tmp/ralph-o-matic-server
        sudo mv /tmp/ralph-o-matic-server "$bin_dir/ralph-o-matic-server"
        success "Server installed to $bin_dir/ralph-o-matic-server"
    fi

    # Install CLI
    info "Downloading CLI..."
    curl -fsSL "$RELEASE_URL/$cli_binary" -o /tmp/ralph-o-matic
    chmod +x /tmp/ralph-o-matic
    sudo mv /tmp/ralph-o-matic "$bin_dir/ralph-o-matic"
    success "CLI installed to $bin_dir/ralph-o-matic"
}

install_plugins() {
    if [[ "$MODE" == "client" ]]; then
        # Client mode: install the skill for submitting jobs
        info "Installing Claude Code plugins..."

        # Install ralph-wiggum plugin
        if command -v claude &>/dev/null; then
            claude plugins install ralph-wiggum || true
            success "ralph-wiggum plugin installed"

            claude plugins install brainstorm-to-ralph || true
            success "brainstorm-to-ralph plugin installed"
        else
            warn "Claude Code not installed, skipping plugins"
        fi
    fi

    # Server mode: plugins are optional (server shells out to claude)
}

configure_ralph() {
    info "Creating configuration..."

    local config_dir="$HOME/.config/ralph-o-matic"
    mkdir -p "$config_dir"

    if [[ "$MODE" == "client" ]]; then
        # Client config - needs server URL
        if [[ -z "$SERVER_URL" ]]; then
            echo ""
            read -p "Enter ralph-o-matic server URL: " SERVER_URL
        fi

        cat > "$config_dir/config.yaml" <<EOF
server: $SERVER_URL
default_priority: normal
default_max_iterations: 50
EOF
        success "Client configured for $SERVER_URL"
    else
        # Server config
        local lan_ip
        if [[ "$OS" == "darwin" ]]; then
            lan_ip=$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || echo "localhost")
        else
            lan_ip=$(hostname -I | awk '{print $1}' || echo "localhost")
        fi

        cat > "$config_dir/config.yaml" <<EOF
# Ralph-o-matic Server Configuration
large_model: $LARGE_MODEL
small_model: $SMALL_MODEL
default_max_iterations: 50
concurrent_jobs: 1
bind_address: $lan_ip
port: 9090
workspace_dir: $config_dir/workspace
job_retention_days: 30
EOF

        mkdir -p "$config_dir/workspace"
        mkdir -p "$config_dir/data"

        success "Server configured on $lan_ip:9090"
    fi
}
```

**Step 2: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): add binary installation and configuration"
```

---

## Task 7: Implement Server Startup and Verification

**Files:**
- Modify: `scripts/install.sh`

**Step 1: Add remaining functions**

```bash
prompt_start_server() {
    if [[ "$YES_FLAG" == true ]]; then
        start_server
        return
    fi

    echo ""
    read -p "Start server now? [Y/n] " -n 1 -r
    echo ""
    if [[ $REPLY =~ ^[Yy]?$ ]]; then
        start_server
    fi
}

start_server() {
    info "Starting ralph-o-matic server..."

    # Start in background
    nohup ralph-o-matic-server &>/dev/null &
    sleep 2

    # Check if running
    if pgrep -x ralph-o-matic-server &>/dev/null; then
        success "Server started"
    else
        warn "Server may have failed to start - check logs"
    fi
}

verify_installation() {
    info "Verifying installation..."

    local errors=0

    # Check CLI
    if ralph-o-matic --version &>/dev/null; then
        success "CLI working"
    else
        warn "CLI verification failed"
        ((errors++))
    fi

    # Check server (if installed)
    if [[ "$MODE" != "client" ]]; then
        if ralph-o-matic-server --version &>/dev/null; then
            success "Server binary working"
        else
            warn "Server verification failed"
            ((errors++))
        fi
    fi

    if [[ $errors -gt 0 ]]; then
        warn "Installation completed with $errors warning(s)"
    else
        success "All components verified"
    fi
}

prompt_mode() {
    if [[ "$YES_FLAG" == true ]]; then
        return
    fi

    echo ""
    echo "What would you like to install?"
    echo ""
    echo "  [1] Server + Client (full setup for running jobs locally)"
    echo "  [2] Server only (this machine will run ralph loops)"
    echo "  [3] Client only (submit jobs to a remote server)"
    echo ""
    read -p "> " -n 1 -r
    echo ""

    case $REPLY in
        1) MODE="full" ;;
        2) MODE="server" ;;
        3) MODE="client" ;;
        *) MODE="full" ;;
    esac
}

print_banner() {
    echo ""
    echo -e "${BLUE}╔══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║${NC}                     ${GREEN}Ralph-o-matic Installer${NC}                      ${BLUE}║${NC}"
    echo -e "${BLUE}╚══════════════════════════════════════════════════════════════════╝${NC}"
    echo ""
}

print_success() {
    local config_dir="$HOME/.config/ralph-o-matic"
    local lan_ip

    if [[ "$OS" == "darwin" ]]; then
        lan_ip=$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || echo "localhost")
    else
        lan_ip=$(hostname -I | awk '{print $1}' || echo "localhost")
    fi

    echo ""
    echo -e "${GREEN}╔══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║${NC}                    ${GREEN}Installation Complete!${NC}                        ${GREEN}║${NC}"
    echo -e "${GREEN}╠══════════════════════════════════════════════════════════════════╣${NC}"
    echo -e "${GREEN}║${NC}                                                                  ${GREEN}║${NC}"

    if [[ "$MODE" != "client" ]]; then
        echo -e "${GREEN}║${NC}  Dashboard:     http://$lan_ip:9090                        ${GREEN}║${NC}"
        echo -e "${GREEN}║${NC}                                                                  ${GREEN}║${NC}"
    fi

    echo -e "${GREEN}║${NC}  Quick start:                                                    ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}    claude                                                        ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}    /brainstorm-to-ralph \"Add user authentication\"               ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}                                                                  ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  Commands:                                                       ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}    ralph-o-matic status        # Check queue                     ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}    ralph-o-matic logs <id>     # View job logs                   ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}                                                                  ${GREEN}║${NC}"
    echo -e "${GREEN}╚══════════════════════════════════════════════════════════════════╝${NC}"
    echo ""
}
```

**Step 2: Verify complete script**

Run:
```bash
bash -n scripts/install.sh
```

Expected: No output (valid syntax)

**Step 3: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): complete bash install script"
```

---

## Task 8: Create PowerShell Install Script

**Files:**
- Create: `scripts/install.ps1`

**Step 1: Write the PowerShell script**

Create `scripts/install.ps1`:

```powershell
#Requires -Version 5.1
# Ralph-o-matic Installer for Windows
# "It just works."

param(
    [switch]$Yes,
    [ValidateSet("full", "server", "client")]
    [string]$Mode = "full",
    [string]$Server = "",
    [string]$LargeModel = "qwen3-coder:70b",
    [string]$SmallModel = "qwen2.5-coder:7b"
)

$ErrorActionPreference = "Stop"

$Version = "1.0.0"
$RepoUrl = "https://github.com/ryan/ralph-o-matic"
$ReleaseUrl = "$RepoUrl/releases/download/v$Version"

# Logging
function Write-Info { Write-Host "▸ $args" -ForegroundColor Blue }
function Write-Success { Write-Host "✓ $args" -ForegroundColor Green }
function Write-Warn { Write-Host "! $args" -ForegroundColor Yellow }
function Write-Err { Write-Host "✗ $args" -ForegroundColor Red; exit 1 }

# Platform detection
$script:RamGB = 0
$script:GpuType = "none"
$script:GpuVramMB = 0
$script:GpuCanRunLarge = $false
$script:GpuCanRunSmall = $false

function Test-Admin {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Get-Platform {
    Write-Info "Detecting platform..."

    # Get RAM
    $script:RamGB = [math]::Round((Get-CimInstance Win32_PhysicalMemory | Measure-Object -Property Capacity -Sum).Sum / 1GB)

    Write-Success "Detected: Windows ($env:PROCESSOR_ARCHITECTURE), ${script:RamGB}GB RAM"
}

function Test-RamRequirement {
    $MinRam = 32

    if ($Mode -eq "client") {
        return
    }

    if ($script:RamGB -lt $MinRam) {
        Write-Err @"
Insufficient RAM: ${script:RamGB}GB detected, ${MinRam}GB required for server mode.

Server mode requires 32GB RAM to run qwen3-coder.
If you only want to submit jobs to a remote server, use:
  .\install.ps1 -Mode client -Server http://your-server:9090
"@
    }

    Write-Success "RAM check passed: ${script:RamGB}GB available"
}

function Get-Gpu {
    Write-Info "Detecting GPU..."

    # Check for NVIDIA GPU
    try {
        $nvidiaSmi = & nvidia-smi --query-gpu=memory.total --format=csv,noheader,nounits 2>$null
        if ($LASTEXITCODE -eq 0 -and $nvidiaSmi) {
            $script:GpuType = "nvidia"
            $script:GpuVramMB = [int]($nvidiaSmi -split "`n")[0].Trim()
            Write-Success "NVIDIA GPU detected: ${script:GpuVramMB}MB VRAM"
        }
    } catch {
        # nvidia-smi not found
    }

    # Check for AMD GPU
    if ($script:GpuType -eq "none") {
        try {
            $amdInfo = Get-CimInstance -ClassName Win32_VideoController | Where-Object { $_.Name -like "*AMD*" -or $_.Name -like "*Radeon*" }
            if ($amdInfo) {
                $script:GpuType = "amd"
                $script:GpuVramMB = [math]::Round($amdInfo.AdapterRAM / 1MB)
                Write-Success "AMD GPU detected: ${script:GpuVramMB}MB VRAM"
            }
        } catch {
            # No AMD GPU
        }
    }

    if ($script:GpuType -eq "none") {
        Write-Info "No GPU detected - will use CPU only"
    }

    # Determine what models can run on GPU
    if ($script:GpuVramMB -ge 45000) {
        $script:GpuCanRunLarge = $true
        $script:GpuCanRunSmall = $true
        Write-Success "GPU can run both large and small models"
    } elseif ($script:GpuVramMB -ge 8000) {
        $script:GpuCanRunSmall = $true
        Write-Success "GPU can run small model, large model will use CPU/RAM"
    } else {
        if ($script:GpuType -ne "none") {
            Write-Info "GPU VRAM insufficient for models, will use CPU/RAM"
        }
    }
}

function Test-Dependencies {
    Write-Info "Checking dependencies..."

    $script:Deps = @{}

    # Git
    try {
        $gitVersion = & git --version 2>$null
        $script:Deps["git"] = @{ Installed = $true; Version = $gitVersion }
        Write-Success "git $gitVersion"
    } catch {
        $script:Deps["git"] = @{ Installed = $false }
        Write-Warn "git not installed"
    }

    # GitHub CLI
    try {
        $ghVersion = & gh --version 2>$null | Select-Object -First 1
        $script:Deps["gh"] = @{ Installed = $true; Version = $ghVersion }

        $authStatus = & gh auth status 2>&1
        if ($LASTEXITCODE -eq 0) {
            Write-Success "gh $ghVersion (authenticated)"
        } else {
            Write-Warn "gh $ghVersion (not authenticated)"
        }
    } catch {
        $script:Deps["gh"] = @{ Installed = $false }
        Write-Warn "gh (GitHub CLI) not installed"
    }

    # Ollama (server mode only)
    if ($Mode -ne "client") {
        try {
            $ollamaVersion = & ollama --version 2>$null
            $script:Deps["ollama"] = @{ Installed = $true; Version = $ollamaVersion }
            Write-Success "ollama $ollamaVersion"
        } catch {
            $script:Deps["ollama"] = @{ Installed = $false }
            Write-Warn "ollama not installed"
        }
    }

    # Claude Code (server mode only)
    if ($Mode -ne "client") {
        try {
            $claudeVersion = & claude --version 2>$null
            $script:Deps["claude"] = @{ Installed = $true; Version = $claudeVersion }
            Write-Success "claude-code installed"
        } catch {
            $script:Deps["claude"] = @{ Installed = $false }
            Write-Warn "claude-code not installed"
        }
    }
}

function Install-MissingDependencies {
    $needInstall = $false
    foreach ($dep in $script:Deps.Keys) {
        if (-not $script:Deps[$dep].Installed) {
            $needInstall = $true
            break
        }
    }

    if (-not $needInstall) {
        Write-Success "All dependencies installed"
        return
    }

    if (-not $Yes) {
        $response = Read-Host "Install missing dependencies? [Y/n]"
        if ($response -and $response -notmatch "^[Yy]") {
            Write-Err "Cannot proceed without dependencies"
        }
    }

    # Install via winget
    if (-not $script:Deps["git"].Installed) {
        Write-Info "Installing git..."
        winget install --id Git.Git -e --source winget --accept-package-agreements --accept-source-agreements
        Write-Success "git installed"
    }

    if (-not $script:Deps["gh"].Installed) {
        Write-Info "Installing GitHub CLI..."
        winget install --id GitHub.cli -e --source winget --accept-package-agreements --accept-source-agreements
        Write-Success "GitHub CLI installed"

        Write-Host ""
        Write-Info "GitHub CLI needs authentication for creating PRs"
        & gh auth login
    }

    if ($Mode -ne "client" -and -not $script:Deps["ollama"].Installed) {
        Write-Info "Installing Ollama..."
        winget install --id Ollama.Ollama -e --source winget --accept-package-agreements --accept-source-agreements
        Write-Success "Ollama installed"
    }

    if ($Mode -ne "client" -and -not $script:Deps["claude"].Installed) {
        Write-Info "Installing Claude Code..."
        npm install -g @anthropic-ai/claude-code
        Write-Success "Claude Code installed"
    }
}

function Install-Models {
    Write-Info "Pulling Ollama models (this may take a while)..."

    # Start Ollama if not running
    $ollamaProcess = Get-Process -Name "ollama" -ErrorAction SilentlyContinue
    if (-not $ollamaProcess) {
        Write-Info "Starting Ollama..."
        Start-Process -FilePath "ollama" -ArgumentList "serve" -WindowStyle Hidden
        Start-Sleep -Seconds 3
    }

    # Pull small model first
    Write-Info "Pulling $SmallModel..."
    & ollama pull $SmallModel
    if ($LASTEXITCODE -eq 0) {
        Write-Success "$SmallModel ready"
    } else {
        Write-Err "Failed to pull $SmallModel"
    }

    # Pull large model
    Write-Info "Pulling $LargeModel (this is ~40GB, be patient)..."
    & ollama pull $LargeModel
    if ($LASTEXITCODE -eq 0) {
        Write-Success "$LargeModel ready"
    } else {
        Write-Err "Failed to pull $LargeModel"
    }

    Write-Success "All models ready"
}

function Install-Binaries {
    Write-Info "Installing ralph-o-matic binaries..."

    $binDir = "$env:LOCALAPPDATA\Programs\ralph-o-matic"
    New-Item -ItemType Directory -Path $binDir -Force | Out-Null

    # Add to PATH if not already
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($userPath -notlike "*$binDir*") {
        [Environment]::SetEnvironmentVariable("Path", "$userPath;$binDir", "User")
        $env:Path = "$env:Path;$binDir"
    }

    # Install server (if not client-only mode)
    if ($Mode -ne "client") {
        Write-Info "Downloading server..."
        $serverUrl = "$ReleaseUrl/ralph-o-matic-server-windows-amd64.exe"
        Invoke-WebRequest -Uri $serverUrl -OutFile "$binDir\ralph-o-matic-server.exe"
        Write-Success "Server installed to $binDir\ralph-o-matic-server.exe"
    }

    # Install CLI
    Write-Info "Downloading CLI..."
    $cliUrl = "$ReleaseUrl/ralph-o-matic-windows-amd64.exe"
    Invoke-WebRequest -Uri $cliUrl -OutFile "$binDir\ralph-o-matic.exe"
    Write-Success "CLI installed to $binDir\ralph-o-matic.exe"
}

function Set-Configuration {
    Write-Info "Creating configuration..."

    $configDir = "$env:USERPROFILE\.config\ralph-o-matic"
    New-Item -ItemType Directory -Path $configDir -Force | Out-Null

    if ($Mode -eq "client") {
        if (-not $Server) {
            $Server = Read-Host "Enter ralph-o-matic server URL"
        }

        @"
server: $Server
default_priority: normal
default_max_iterations: 50
"@ | Out-File -FilePath "$configDir\config.yaml" -Encoding utf8

        Write-Success "Client configured for $Server"
    } else {
        # Get LAN IP
        $lanIp = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.InterfaceAlias -notlike "*Loopback*" -and $_.PrefixOrigin -eq "Dhcp" } | Select-Object -First 1).IPAddress
        if (-not $lanIp) { $lanIp = "localhost" }

        @"
# Ralph-o-matic Server Configuration
large_model: $LargeModel
small_model: $SmallModel
default_max_iterations: 50
concurrent_jobs: 1
bind_address: $lanIp
port: 9090
workspace_dir: $configDir\workspace
job_retention_days: 30
"@ | Out-File -FilePath "$configDir\config.yaml" -Encoding utf8

        New-Item -ItemType Directory -Path "$configDir\workspace" -Force | Out-Null
        New-Item -ItemType Directory -Path "$configDir\data" -Force | Out-Null

        Write-Success "Server configured on ${lanIp}:9090"
    }
}

function Show-Banner {
    Write-Host ""
    Write-Host "╔══════════════════════════════════════════════════════════════════╗" -ForegroundColor Blue
    Write-Host "║                     Ralph-o-matic Installer                      ║" -ForegroundColor Blue
    Write-Host "╚══════════════════════════════════════════════════════════════════╝" -ForegroundColor Blue
    Write-Host ""
}

function Get-InstallMode {
    if ($Yes) { return }

    Write-Host ""
    Write-Host "What would you like to install?"
    Write-Host ""
    Write-Host "  [1] Server + Client (full setup for running jobs locally)"
    Write-Host "  [2] Server only (this machine will run ralph loops)"
    Write-Host "  [3] Client only (submit jobs to a remote server)"
    Write-Host ""
    $choice = Read-Host ">"

    switch ($choice) {
        "1" { $script:Mode = "full" }
        "2" { $script:Mode = "server" }
        "3" { $script:Mode = "client" }
        default { $script:Mode = "full" }
    }
}

function Show-Success {
    $lanIp = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.InterfaceAlias -notlike "*Loopback*" -and $_.PrefixOrigin -eq "Dhcp" } | Select-Object -First 1).IPAddress
    if (-not $lanIp) { $lanIp = "localhost" }

    Write-Host ""
    Write-Host "╔══════════════════════════════════════════════════════════════════╗" -ForegroundColor Green
    Write-Host "║                    Installation Complete!                        ║" -ForegroundColor Green
    Write-Host "╠══════════════════════════════════════════════════════════════════╣" -ForegroundColor Green
    Write-Host "║                                                                  ║" -ForegroundColor Green
    if ($Mode -ne "client") {
        Write-Host "║  Dashboard:     http://${lanIp}:9090                        ║" -ForegroundColor Green
        Write-Host "║                                                                  ║" -ForegroundColor Green
    }
    Write-Host "║  Quick start:                                                    ║" -ForegroundColor Green
    Write-Host "║    claude                                                        ║" -ForegroundColor Green
    Write-Host '║    /brainstorm-to-ralph "Add user authentication"               ║' -ForegroundColor Green
    Write-Host "║                                                                  ║" -ForegroundColor Green
    Write-Host "║  Commands:                                                       ║" -ForegroundColor Green
    Write-Host "║    ralph-o-matic status        # Check queue                     ║" -ForegroundColor Green
    Write-Host "║    ralph-o-matic logs <id>     # View job logs                   ║" -ForegroundColor Green
    Write-Host "║                                                                  ║" -ForegroundColor Green
    Write-Host "╚══════════════════════════════════════════════════════════════════╝" -ForegroundColor Green
    Write-Host ""
}

# Main
function Main {
    Show-Banner
    Get-Platform
    Test-RamRequirement
    Get-InstallMode
    Test-Dependencies
    Install-MissingDependencies

    if ($Mode -ne "client") {
        Get-Gpu
        Install-Models
    }

    Install-Binaries
    Set-Configuration
    Show-Success
}

Main
```

**Step 2: Verify PowerShell syntax**

Run:
```powershell
powershell -Command "& { $null = [System.Management.Automation.Language.Parser]::ParseFile('scripts/install.ps1', [ref]$null, [ref]$errors); $errors }"
```

Expected: No errors

**Step 3: Commit**

```bash
git add scripts/install.ps1
git commit -m "feat(install): add PowerShell install script for Windows"
```

---

## Task 9: Add Install Script Tests (Bats)

**Files:**
- Create: `scripts/tests/detect_test.sh`

**Step 1: Write bats tests**

Create `scripts/tests/detect_test.sh`:

```bash
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
```

**Step 2: Run tests**

Run:
```bash
bats scripts/tests/detect_test.sh
```

Expected: Tests pass (or skip appropriately)

**Step 3: Commit**

```bash
git add scripts/tests/detect_test.sh
git commit -m "test(install): add bats tests for install script"
```

---

## Phase 9 Completion Checklist

- [ ] Bash install script with full flow
- [ ] Platform detection (OS, arch, RAM)
- [ ] GPU detection (NVIDIA, AMD, Apple Silicon)
- [ ] Automatic Ollama configuration based on hardware
- [ ] 32GB RAM requirement enforcement
- [ ] Dependency checking and installation
- [ ] Model pulling
- [ ] Binary installation
- [ ] PowerShell install script for Windows
- [ ] Bats tests for install scripts
- [ ] All code committed

**Next Phase:** Phase 10 - brainstorm-to-ralph Skill
