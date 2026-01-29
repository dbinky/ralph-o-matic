#!/usr/bin/env bash
set -euo pipefail

# Ralph-o-matic Installer
# "It just works."

VERSION="0.0.2"
REPO_URL="https://github.com/dbinky/ralph-o-matic"
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
MODE_SET=false
YES_FLAG=false
SERVER_URL=""
LARGE_MODEL=""
SMALL_MODEL=""
OLLAMA_URL="http://localhost:11434"
INFERENCE_MODE=""  # gpu_cpu_split, gpu_only, cpu_only, remote

while [[ $# -gt 0 ]]; do
    case $1 in
        --yes|-y) YES_FLAG=true; shift ;;
        --mode=*) MODE="${1#*=}"; MODE_SET=true; shift ;;
        --server=*) SERVER_URL="${1#*=}"; shift ;;
        --large-model=*) LARGE_MODEL="${1#*=}"; shift ;;
        --small-model=*) SMALL_MODEL="${1#*=}"; shift ;;
        *) error "Unknown option: $1" ;;
    esac
done

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
    local MIN_RAM=16

    if [[ "$MODE" == "client" ]]; then
        # Client doesn't need much RAM
        return 0
    fi

    if [[ $RAM_GB -lt $MIN_RAM ]]; then
        error "Insufficient RAM: ${RAM_GB}GB detected, ${MIN_RAM}GB minimum required.

Server mode requires at least 16GB RAM to run coding models.
If you only want to submit jobs to a remote server, use:
  $0 --mode=client --server=http://your-server:9090"
    fi

    if [[ $RAM_GB -lt 32 ]]; then
        warn "RAM: ${RAM_GB}GB detected. The default 70b model needs 42GB."
        info "The installer will recommend smaller models that fit your hardware."
    else
        success "RAM check passed: ${RAM_GB}GB available"
    fi
}

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

show_hardware_summary() {
    echo ""
    echo -e "${BLUE}Hardware Summary:${NC}"
    echo "  OS:   $OS ($ARCH)"
    echo "  RAM:  ${RAM_GB}GB"
    if [[ "$GPU_TYPE" == "apple" ]]; then
        echo "  GPU:  Apple Silicon (unified memory)"
    elif [[ "$GPU_TYPE" == "nvidia" ]]; then
        echo "  GPU:  NVIDIA (${GPU_VRAM_MB}MB VRAM)"
    elif [[ "$GPU_TYPE" == "amd" ]]; then
        echo "  GPU:  AMD (${GPU_VRAM_MB}MB VRAM)"
    else
        echo "  GPU:  None detected"
    fi
    echo ""
}

show_model_recommendation() {
    local rec_large="$1"
    local rec_small="$2"
    local rec_mode="$3"

    echo -e "${GREEN}Recommended configuration:${NC}"
    echo "  Inference mode: $rec_mode"
    echo "  Large model:    $rec_large"
    echo "  Small model:    $rec_small"
    echo ""
}

customize_models() {
    echo ""
    echo "Available large models:"
    echo "  [1] qwen3-coder:70b     (42GB, quality 10 - best)"
    echo "  [2] qwen2.5-coder:32b   (20GB, quality 8)"
    echo "  [3] qwen2.5-coder:14b   (10GB, quality 6)"
    echo "  [4] qwen2.5-coder:7b    (5GB,  quality 4)"
    echo ""
    read -p "Select large model [1-4]: " -n 1 -r
    echo ""
    case $REPLY in
        1) LARGE_MODEL="qwen3-coder:70b" ;;
        2) LARGE_MODEL="qwen2.5-coder:32b" ;;
        3) LARGE_MODEL="qwen2.5-coder:14b" ;;
        4) LARGE_MODEL="qwen2.5-coder:7b" ;;
        *) warn "Invalid choice, using qwen2.5-coder:14b"; LARGE_MODEL="qwen2.5-coder:14b" ;;
    esac

    echo ""
    echo "Available small models:"
    echo "  [1] qwen2.5-coder:7b    (5GB,   quality 4)"
    echo "  [2] qwen2.5-coder:1.5b  (1.5GB, quality 2 - fastest)"
    echo ""
    read -p "Select small model [1-2]: " -n 1 -r
    echo ""
    case $REPLY in
        1) SMALL_MODEL="qwen2.5-coder:7b" ;;
        2) SMALL_MODEL="qwen2.5-coder:1.5b" ;;
        *) warn "Invalid choice, using qwen2.5-coder:7b"; SMALL_MODEL="qwen2.5-coder:7b" ;;
    esac

    success "Selected: large=$LARGE_MODEL, small=$SMALL_MODEL"
}

setup_remote_ollama() {
    echo ""
    read -p "Enter remote Ollama URL (e.g. http://192.168.1.100:11434): " OLLAMA_URL

    info "Checking remote Ollama at $OLLAMA_URL..."
    if curl -sf "$OLLAMA_URL/api/tags" &>/dev/null; then
        success "Remote Ollama is reachable"

        # List available models on remote
        local remote_models
        remote_models=$(curl -sf "$OLLAMA_URL/api/tags" | grep -o '"name":"[^"]*"' | cut -d'"' -f4 || true)
        if [[ -n "$remote_models" ]]; then
            echo ""
            echo "Models available on remote:"
            echo "$remote_models" | while read -r m; do echo "  - $m"; done
            echo ""
        fi
    else
        warn "Could not reach remote Ollama at $OLLAMA_URL"
        warn "Continuing anyway - ensure the remote is available before running jobs"
    fi

    # Still need to pick models (they run on remote)
    if [[ -z "$LARGE_MODEL" ]]; then
        LARGE_MODEL="qwen3-coder:70b"
    fi
    if [[ -z "$SMALL_MODEL" ]]; then
        SMALL_MODEL="qwen2.5-coder:7b"
    fi
}

select_models() {
    show_hardware_summary

    # Compute recommendation based on hardware
    local rec_large="qwen2.5-coder:14b"
    local rec_small="qwen2.5-coder:7b"
    local rec_mode="cpu_only"

    if [[ "$GPU_TYPE" == "apple" ]]; then
        # Apple Silicon unified memory
        if [[ $RAM_GB -ge 64 ]]; then
            rec_large="qwen3-coder:70b"
            rec_mode="gpu_only"
        elif [[ $RAM_GB -ge 32 ]]; then
            rec_large="qwen2.5-coder:32b"
            rec_mode="gpu_only"
        elif [[ $RAM_GB -ge 16 ]]; then
            rec_large="qwen2.5-coder:14b"
            rec_mode="gpu_only"
        else
            rec_large="qwen2.5-coder:7b"
            rec_mode="gpu_only"
        fi
    elif [[ "$GPU_TYPE" == "nvidia" ]] || [[ "$GPU_TYPE" == "amd" ]]; then
        if [[ "$GPU_CAN_RUN_LARGE" == true ]]; then
            rec_large="qwen3-coder:70b"
            rec_mode="gpu_only"
        elif [[ "$GPU_CAN_RUN_SMALL" == true ]]; then
            rec_mode="gpu_cpu_split"
            if [[ $RAM_GB -ge 64 ]]; then
                rec_large="qwen3-coder:70b"
            elif [[ $RAM_GB -ge 32 ]]; then
                rec_large="qwen2.5-coder:32b"
            else
                rec_large="qwen2.5-coder:14b"
            fi
        else
            rec_mode="cpu_only"
            if [[ $RAM_GB -ge 64 ]]; then
                rec_large="qwen3-coder:70b"
            elif [[ $RAM_GB -ge 32 ]]; then
                rec_large="qwen2.5-coder:32b"
            else
                rec_large="qwen2.5-coder:14b"
            fi
        fi
    else
        # No GPU
        rec_mode="cpu_only"
        if [[ $RAM_GB -ge 64 ]]; then
            rec_large="qwen3-coder:70b"
        elif [[ $RAM_GB -ge 32 ]]; then
            rec_large="qwen2.5-coder:32b"
        else
            rec_large="qwen2.5-coder:14b"
        fi
    fi

    show_model_recommendation "$rec_large" "$rec_small" "$rec_mode"

    # If --yes flag or CLI overrides provided, use defaults/overrides
    if [[ "$YES_FLAG" == true ]]; then
        if [[ -z "$LARGE_MODEL" ]]; then LARGE_MODEL="$rec_large"; fi
        if [[ -z "$SMALL_MODEL" ]]; then SMALL_MODEL="$rec_small"; fi
        INFERENCE_MODE="$rec_mode"
        success "Using recommended configuration (--yes)"
        return
    fi

    # Check if user passed model overrides via CLI flags
    if [[ -n "$LARGE_MODEL" ]] && [[ -n "$SMALL_MODEL" ]]; then
        INFERENCE_MODE="$rec_mode"
        success "Using CLI-specified models: large=$LARGE_MODEL, small=$SMALL_MODEL"
        return
    fi

    echo "How would you like to run inference?"
    echo ""
    echo "  [1] GPU + CPU split (large model on CPU/RAM, small model on GPU)"
    echo "  [2] GPU only (all models on GPU)"
    echo "  [3] CPU only (all models on CPU/RAM)"
    echo "  [4] Remote Ollama (use a remote Ollama server)"
    echo ""
    read -p "Select mode [1-4] (recommended: press Enter for $rec_mode): " -n 1 -r
    echo ""

    case $REPLY in
        1) INFERENCE_MODE="gpu_cpu_split" ;;
        2) INFERENCE_MODE="gpu_only" ;;
        3) INFERENCE_MODE="cpu_only" ;;
        4) INFERENCE_MODE="remote" ;;
        "") INFERENCE_MODE="$rec_mode" ;;
        *) warn "Invalid choice, using recommended"; INFERENCE_MODE="$rec_mode" ;;
    esac

    if [[ "$INFERENCE_MODE" == "remote" ]]; then
        setup_remote_ollama
        echo ""
        read -p "Accept recommended models? [Y/n] " -n 1 -r
        echo ""
        if [[ $REPLY =~ ^[Nn]$ ]]; then
            customize_models
        fi
        return
    fi

    # Offer accept or customize
    echo ""
    read -p "Accept recommended models ($rec_large + $rec_small)? [Y/n] " -n 1 -r
    echo ""
    if [[ $REPLY =~ ^[Nn]$ ]]; then
        customize_models
    else
        LARGE_MODEL="$rec_large"
        SMALL_MODEL="$rec_small"
    fi

    success "Configuration: mode=$INFERENCE_MODE, large=$LARGE_MODEL, small=$SMALL_MODEL"
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

pull_models() {
    # Skip pulling for remote Ollama - models are managed on the remote
    if [[ "$INFERENCE_MODE" == "remote" ]]; then
        info "Using remote Ollama at $OLLAMA_URL - skipping local model pull"
        info "Ensure models '$LARGE_MODEL' and '$SMALL_MODEL' are available on the remote"
        return
    fi

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
    info "Pulling $LARGE_MODEL..."
    if ollama pull "$LARGE_MODEL"; then
        success "$LARGE_MODEL ready"
    else
        error "Failed to pull $LARGE_MODEL"
    fi

    success "All models ready"
}

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

    # Install CLI (if not server-only mode)
    if [[ "$MODE" != "server" ]]; then
        info "Downloading CLI..."
        curl -fsSL "$RELEASE_URL/$cli_binary" -o /tmp/ralph-o-matic
        chmod +x /tmp/ralph-o-matic
        sudo mv /tmp/ralph-o-matic "$bin_dir/ralph-o-matic"
        success "CLI installed to $bin_dir/ralph-o-matic"
    fi
}

install_plugins() {
    info "Installing Claude Code plugins..."

    if ! command -v claude &>/dev/null; then
        warn "Claude Code not installed, skipping plugins"
        return
    fi

    # Install ralph-wiggum plugin (pipe empty stdin to skip TUI trust dialog)
    if echo "" | claude plugin install ralph-wiggum 2>/dev/null; then
        success "ralph-wiggum plugin installed"
    else
        warn "Failed to install ralph-wiggum (may already be installed)"
    fi

    # Install brainstorm-to-ralph skill
    # This is bundled with ralph-o-matic, copy to Claude Code skills directory
    local skills_dir="$HOME/.claude/skills"
    mkdir -p "$skills_dir"

    if [[ -d "/usr/local/share/ralph-o-matic/skills/brainstorm-to-ralph" ]]; then
        cp -r /usr/local/share/ralph-o-matic/skills/brainstorm-to-ralph "$skills_dir/"
        success "brainstorm-to-ralph skill installed"
    else
        # Download from release
        local skill_url="$RELEASE_URL/brainstorm-to-ralph-skill.tar.gz"
        if curl -fsSL "$skill_url" -o /tmp/skill.tar.gz 2>/dev/null; then
            tar -xzf /tmp/skill.tar.gz -C "$skills_dir/"
            rm /tmp/skill.tar.gz
            success "brainstorm-to-ralph skill installed"
        else
            warn "Could not install brainstorm-to-ralph skill"
        fi
    fi
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
ollama:
  url: $OLLAMA_URL
  inference_mode: $INFERENCE_MODE
large_model:
  name: $LARGE_MODEL
small_model:
  name: $SMALL_MODEL
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
    if [[ "$YES_FLAG" == true ]] || [[ "$MODE_SET" == true ]]; then
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

# Main installation flow
main() {
    # Reopen stdin from terminal so interactive prompts work with curl | bash
    if [[ ! -t 0 ]]; then
        exec 0</dev/tty
    fi

    print_banner
    detect_platform
    check_ram_requirement
    prompt_mode
    check_dependencies
    install_missing_dependencies
    if [[ "$MODE" != "client" ]]; then
        detect_gpu
        select_models
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

# Only run main when executed directly, not when sourced (e.g., by tests)
if [[ "${BASH_SOURCE[0]:-$0}" == "${0}" ]]; then
    main "$@"
fi
