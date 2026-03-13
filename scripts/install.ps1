#Requires -Version 5.1
# Ralph-o-matic Installer for Windows
# "It just works."

param(
    [switch]$Yes,
    [switch]$Update,
    [ValidateSet("full", "server", "client")]
    [string]$Mode = "full",
    [ValidateSet("ollama", "anthropic", "openrouter")]
    [string]$Backend = "ollama",
    [string]$Server = "",
    [string]$LargeModel = "",
    [string]$SmallModel = ""
)

$ErrorActionPreference = "Stop"

$Version = "0.6.3"
$RepoUrl = "https://github.com/dbinky/ralph-o-matic"
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
    $MinRam = 16

    if ($Mode -eq "client" -or $Backend -eq "anthropic" -or $Backend -eq "openrouter") {
        return
    }

    if ($script:RamGB -lt $MinRam) {
        Write-Err @"
Insufficient RAM: ${script:RamGB}GB detected, ${MinRam}GB minimum required.

Server mode requires at least 16GB RAM to run coding models.
If you only want to submit jobs to a remote server, use:
  .\install.ps1 -Mode client -Server http://your-server:9090
"@
    }

    if ($script:RamGB -lt 32) {
        Write-Warn "RAM: ${script:RamGB}GB detected. Smaller models will be recommended."
        Write-Info "The installer will recommend smaller models that fit your hardware."
    } else {
        Write-Success "RAM check passed: ${script:RamGB}GB available"
    }
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
    # devstral needs ~15GB VRAM, qwen3:8b needs ~5GB VRAM
    if ($script:GpuVramMB -ge 16000) {
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

$script:InferenceMode = ""  # gpu_cpu_split, gpu_only, cpu_only, remote
$script:OllamaUrl = "http://localhost:11434"

function Show-HardwareSummary {
    Write-Host ""
    Write-Host "Hardware Summary:" -ForegroundColor Blue
    Write-Host "  OS:   Windows ($env:PROCESSOR_ARCHITECTURE)"
    Write-Host "  RAM:  ${script:RamGB}GB"
    if ($script:GpuType -eq "nvidia") {
        Write-Host "  GPU:  NVIDIA (${script:GpuVramMB}MB VRAM)"
    } elseif ($script:GpuType -eq "amd") {
        Write-Host "  GPU:  AMD (${script:GpuVramMB}MB VRAM)"
    } else {
        Write-Host "  GPU:  None detected"
    }
    Write-Host ""
}

function Show-ModelRecommendation {
    param([string]$RecLarge, [string]$RecSmall, [string]$RecMode)

    Write-Host "Recommended configuration:" -ForegroundColor Green
    Write-Host "  Inference mode: $RecMode"
    Write-Host "  Large model:    $RecLarge"
    Write-Host "  Small model:    $RecSmall"
    Write-Host ""
}

function Select-CustomModels {
    Write-Host ""
    Write-Host "Available large models (all support tool use):"
    Write-Host "  [1] devstral            (15GB, quality 9 - best)"
    Write-Host "  [2] qwen3-coder:30b    (19GB, quality 8)"
    Write-Host "  [3] qwen3:14b          (9.3GB, quality 6)"
    Write-Host "  [4] qwen3:8b           (5.2GB, quality 4)"
    Write-Host ""
    $choice = Read-Host "Select large model [1-4]"
    switch ($choice) {
        "1" { $script:LargeModel = "devstral" }
        "2" { $script:LargeModel = "qwen3-coder:30b" }
        "3" { $script:LargeModel = "qwen3:14b" }
        "4" { $script:LargeModel = "qwen3:8b" }
        default { Write-Warn "Invalid choice, using qwen3:14b"; $script:LargeModel = "qwen3:14b" }
    }

    Write-Host ""
    Write-Host "Available small models:"
    Write-Host "  [1] qwen3:8b    (5.2GB, quality 4 - tool use)"
    Write-Host "  [2] qwen3:4b    (2.5GB, quality 2 - fastest)"
    Write-Host ""
    $choice = Read-Host "Select small model [1-2]"
    switch ($choice) {
        "1" { $script:SmallModel = "qwen3:8b" }
        "2" { $script:SmallModel = "qwen3:4b" }
        default { Write-Warn "Invalid choice, using qwen3:8b"; $script:SmallModel = "qwen3:8b" }
    }

    Write-Success "Selected: large=$($script:LargeModel), small=$($script:SmallModel)"
}

function Set-RemoteOllama {
    Write-Host ""
    $script:OllamaUrl = Read-Host "Enter remote Ollama URL (e.g. http://192.168.1.100:11434)"

    Write-Info "Checking remote Ollama at $($script:OllamaUrl)..."
    try {
        $response = Invoke-RestMethod -Uri "$($script:OllamaUrl)/api/tags" -TimeoutSec 5
        Write-Success "Remote Ollama is reachable"

        if ($response.models) {
            Write-Host ""
            Write-Host "Models available on remote:"
            foreach ($m in $response.models) {
                Write-Host "  - $($m.name)"
            }
            Write-Host ""
        }
    } catch {
        Write-Warn "Could not reach remote Ollama at $($script:OllamaUrl)"
        Write-Warn "Continuing anyway - ensure the remote is available before running jobs"
    }

    # Default models for remote
    if (-not $script:LargeModel) { $script:LargeModel = "devstral" }
    if (-not $script:SmallModel) { $script:SmallModel = "qwen3:8b" }
}

function Select-Models {
    Show-HardwareSummary

    # Compute recommendation based on hardware (matches bash installer logic)
    $recLarge = "qwen3:14b"
    $recSmall = "qwen3:8b"
    $recMode = "cpu_only"

    if ($script:GpuType -eq "nvidia" -or $script:GpuType -eq "amd") {
        if ($script:GpuCanRunLarge) {
            $recLarge = "devstral"
            $recMode = "gpu_only"
        } elseif ($script:GpuCanRunSmall) {
            $recMode = "gpu_cpu_split"
            if ($script:RamGB -ge 48) {
                $recLarge = "devstral"
            } elseif ($script:RamGB -ge 32) {
                $recLarge = "qwen3-coder:30b"
            } elseif ($script:RamGB -ge 16) {
                $recLarge = "qwen3:14b"
            } else {
                $recLarge = "qwen3:8b"
            }
        } else {
            $recMode = "cpu_only"
            if ($script:RamGB -ge 48) {
                $recLarge = "devstral"
            } elseif ($script:RamGB -ge 32) {
                $recLarge = "qwen3-coder:30b"
            } elseif ($script:RamGB -ge 16) {
                $recLarge = "qwen3:14b"
            } else {
                $recLarge = "qwen3:8b"
            }
        }
    } else {
        # No GPU
        $recMode = "cpu_only"
        if ($script:RamGB -ge 48) {
            $recLarge = "devstral"
        } elseif ($script:RamGB -ge 32) {
            $recLarge = "qwen3-coder:30b"
        } elseif ($script:RamGB -ge 16) {
            $recLarge = "qwen3:14b"
        } else {
            $recLarge = "qwen3:8b"
        }
    }

    Show-ModelRecommendation -RecLarge $recLarge -RecSmall $recSmall -RecMode $recMode

    # If -Yes flag or CLI overrides provided, use defaults/overrides
    if ($Yes) {
        if (-not $script:LargeModel) { $script:LargeModel = $recLarge }
        if (-not $script:SmallModel) { $script:SmallModel = $recSmall }
        $script:InferenceMode = $recMode
        Write-Success "Using recommended configuration (-Yes)"
        return
    }

    # Check if user passed model overrides via CLI flags
    if ($script:LargeModel -and $script:SmallModel) {
        $script:InferenceMode = $recMode
        Write-Success "Using CLI-specified models: large=$($script:LargeModel), small=$($script:SmallModel)"
        return
    }

    Write-Host "How would you like to run inference?"
    Write-Host ""
    Write-Host "  [1] GPU + CPU split (large model on CPU/RAM, small model on GPU)"
    Write-Host "  [2] GPU only (all models on GPU)"
    Write-Host "  [3] CPU only (all models on CPU/RAM)"
    Write-Host "  [4] Remote Ollama (use a remote Ollama server)"
    Write-Host ""
    $choice = Read-Host "Select mode [1-4] (recommended: press Enter for $recMode)"

    switch ($choice) {
        "1" { $script:InferenceMode = "gpu_cpu_split" }
        "2" { $script:InferenceMode = "gpu_only" }
        "3" { $script:InferenceMode = "cpu_only" }
        "4" { $script:InferenceMode = "remote" }
        "" { $script:InferenceMode = $recMode }
        default { Write-Warn "Invalid choice, using recommended"; $script:InferenceMode = $recMode }
    }

    if ($script:InferenceMode -eq "remote") {
        Set-RemoteOllama
        Write-Host ""
        $response = Read-Host "Accept recommended models? [Y/n]"
        if ($response -match "^[Nn]") {
            Select-CustomModels
        }
        return
    }

    # Offer accept or customize
    Write-Host ""
    $response = Read-Host "Accept recommended models ($recLarge + $recSmall)? [Y/n]"
    if ($response -match "^[Nn]") {
        Select-CustomModels
    } else {
        $script:LargeModel = $recLarge
        $script:SmallModel = $recSmall
    }

    Write-Success "Configuration: mode=$($script:InferenceMode), large=$($script:LargeModel), small=$($script:SmallModel)"
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

    # Ollama (server mode + ollama backend only)
    if ($Mode -ne "client" -and $Backend -eq "ollama") {
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

    if ($Mode -ne "client" -and $Backend -eq "ollama" -and -not $script:Deps["ollama"].Installed) {
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
    # Skip pulling for remote Ollama - models are managed on the remote
    if ($script:InferenceMode -eq "remote") {
        Write-Info "Using remote Ollama at $($script:OllamaUrl) - skipping local model pull"
        Write-Info "Ensure models '$($script:LargeModel)' and '$($script:SmallModel)' are available on the remote"
        return
    }

    Write-Info "Pulling Ollama models (this may take a while)..."

    # Start Ollama if not running
    $ollamaProcess = Get-Process -Name "ollama" -ErrorAction SilentlyContinue
    if (-not $ollamaProcess) {
        Write-Info "Starting Ollama..."
        Start-Process -FilePath "ollama" -ArgumentList "serve" -WindowStyle Hidden
        Start-Sleep -Seconds 3
    }

    # Pull small model first (faster, provides early feedback)
    Write-Info "Pulling $($script:SmallModel)..."
    & ollama pull $script:SmallModel
    if ($LASTEXITCODE -eq 0) {
        Write-Success "$($script:SmallModel) ready"
    } else {
        Write-Err "Failed to pull $($script:SmallModel)"
    }

    # Pull large model
    Write-Info "Pulling $($script:LargeModel)..."
    & ollama pull $script:LargeModel
    if ($LASTEXITCODE -eq 0) {
        Write-Success "$($script:LargeModel) ready"
    } else {
        Write-Err "Failed to pull $($script:LargeModel)"
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

        if ($Backend -eq "anthropic" -or $Backend -eq "openrouter") {
            @"
server: http://localhost:9090
default_priority: normal
default_max_iterations: 50
"@ | Out-File -FilePath "$configDir\config.yaml" -Encoding utf8
        } else {
            @"
# Ralph-o-matic Server Configuration
ollama:
  url: $($script:OllamaUrl)
  inference_mode: $($script:InferenceMode)
large_model:
  name: $($script:LargeModel)
small_model:
  name: $($script:SmallModel)
default_max_iterations: 50
bind_address: $lanIp
port: 9090
workspace_dir: $configDir\workspace
job_retention_days: 30
"@ | Out-File -FilePath "$configDir\config.yaml" -Encoding utf8
        }

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

function Select-Backend {
    if ($Yes) { return }

    Write-Host ""
    Write-Host "How would you like to run ralph-o-matic?"
    Write-Host ""
    Write-Host "  [1] Local models via Ollama (GPU/CPU - free, private, requires hardware)"
    Write-Host "  [2] Anthropic API via Claude Code (uses your Claude subscription/API credits)"
    Write-Host "  [3] OpenRouter API (cloud, multi-provider - pay-per-token via openrouter.ai)"
    Write-Host ""
    $choice = Read-Host "Select [1-3]"

    switch ($choice) {
        "2" { $script:Backend = "anthropic" }
        "3" { $script:Backend = "openrouter" }
        default { $script:Backend = "ollama" }
    }
}

function Test-ClaudeAuth {
    Write-Info "Validating Claude Code installation..."

    if (-not (Get-Command claude -ErrorAction SilentlyContinue)) {
        Write-Err "Claude Code CLI not found. Install it first: npm install -g @anthropic-ai/claude-code"
    }
    Write-Success "Claude Code CLI found"

    Write-Info "Checking authentication (this makes a quick API call)..."
    try {
        $result = (& claude --print "respond with only the word OK" --model claude-haiku-4-5-20251001 2>$null) -join " "
        if ($result -notmatch "(?i)ok") {
            Write-Err "Claude Code authentication failed. Run 'claude' to log in first."
        }
    } catch {
        Write-Err "Claude Code authentication failed. Run 'claude' to log in first."
    }
    Write-Success "Claude Code authenticated"
}

function Select-AnthropicModels {
    # Auto-select defaults with -Yes flag
    if ($Yes) {
        $script:LargeModel = "claude-sonnet-4-6-20260218"
        $script:SmallModel = "claude-sonnet-4-6-20260218"
        return
    }

    Write-Host ""
    Write-Host "Select the LARGE model (used for main coding iterations):"
    Write-Host ""
    Write-Host "  [1] claude-opus-4-6               (most capable, slower, higher cost)"
    Write-Host "  [2] claude-sonnet-4-6-20260218    (fast and capable, recommended)"
    Write-Host "  [3] Custom model ID"
    Write-Host ""
    $choice = Read-Host "Select [1-3]"

    switch ($choice) {
        "1" { $script:LargeModel = "claude-opus-4-6" }
        "2" { $script:LargeModel = "claude-sonnet-4-6-20260218" }
        "3" {
            $script:LargeModel = Read-Host "Enter model ID"
            if (-not $script:LargeModel) {
                Write-Warn "Empty model ID, using claude-sonnet-4-6-20260218"
                $script:LargeModel = "claude-sonnet-4-6-20260218"
            }
        }
        default {
            Write-Warn "Invalid choice, using claude-sonnet-4-6-20260218"
            $script:LargeModel = "claude-sonnet-4-6-20260218"
        }
    }

    Write-Host ""
    Write-Host "Select the SMALL model (used for quick checks and lightweight tasks):"
    Write-Host ""
    Write-Host "  [1] claude-sonnet-4-6-20260218    (fast and capable, recommended)"
    Write-Host "  [2] claude-haiku-4-5-20251001     (faster, lower cost)"
    Write-Host "  [3] Custom model ID"
    Write-Host ""
    $choice = Read-Host "Select [1-3]"

    switch ($choice) {
        "1" { $script:SmallModel = "claude-sonnet-4-6-20260218" }
        "2" { $script:SmallModel = "claude-haiku-4-5-20251001" }
        "3" {
            $script:SmallModel = Read-Host "Enter model ID"
            if (-not $script:SmallModel) {
                Write-Warn "Empty model ID, using claude-sonnet-4-6-20260218"
                $script:SmallModel = "claude-sonnet-4-6-20260218"
            }
        }
        default {
            Write-Warn "Invalid choice, using claude-sonnet-4-6-20260218"
            $script:SmallModel = "claude-sonnet-4-6-20260218"
        }
    }

    Write-Success "Selected: large=$($script:LargeModel), small=$($script:SmallModel)"
}

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
            large_model = $script:LargeModel
            small_model = $script:SmallModel
        }
    } | ConvertTo-Json -Depth 3

    try {
        Invoke-RestMethod -Uri "http://localhost:9090/api/config" -Method Patch -ContentType "application/json" -Body $body | Out-Null
        Write-Success "Anthropic config applied (large=$($script:LargeModel), small=$($script:SmallModel))"
    } catch {
        Write-Warn "Failed to apply Anthropic config: $_"
    }
}

$script:OpenRouterApiKey = ""

function Test-OpenRouterKey {
    if ($Yes) {
        if (-not $env:OPENROUTER_API_KEY) {
            Write-Err "OpenRouter API key required. Set OPENROUTER_API_KEY env var with -Yes."
        }
        $script:OpenRouterApiKey = $env:OPENROUTER_API_KEY
        return
    }

    Write-Host ""
    Write-Host "Enter your OpenRouter API key (from https://openrouter.ai/keys):"
    $secureKey = Read-Host "API key" -AsSecureString
    $script:OpenRouterApiKey = [Runtime.InteropServices.Marshal]::PtrToStringAuto(
        [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureKey)
    )

    if (-not $script:OpenRouterApiKey) {
        Write-Err "API key cannot be empty"
    }

    # Validate by calling the models endpoint
    Write-Info "Validating API key..."
    try {
        $headers = @{ "Authorization" = "Bearer $($script:OpenRouterApiKey)" }
        $null = Invoke-RestMethod -Uri "https://openrouter.ai/api/v1/models" -Headers $headers -TimeoutSec 30
        Write-Success "API key validated"
    } catch {
        $statusCode = $_.Exception.Response.StatusCode.value__
        Write-Err "API key validation failed (HTTP $statusCode). Check your key at https://openrouter.ai/keys"
    }
}

function Select-OpenRouterModels {
    if ($Yes) {
        $script:LargeModel = "moonshotai/kimi-k2.5"
        $script:SmallModel = "mistralai/devstral-2-2512"
        return
    }

    Write-Host ""
    Write-Host "Select the LARGE model (used for main coding iterations):"
    Write-Host ""
    Write-Host "  [1] Kimi K2.5        (moonshotai/kimi-k2.5)"
    Write-Host "  [2] Grok 4.1 Fast    (x-ai/grok-4-1-fast)"
    Write-Host "  [3] Devstral 2 2512  (mistralai/devstral-2-2512)"
    Write-Host "  [4] Custom model ID"
    Write-Host ""
    $choice = Read-Host "Select [1-4]"

    switch ($choice) {
        "1" { $script:LargeModel = "moonshotai/kimi-k2.5" }
        "2" { $script:LargeModel = "x-ai/grok-4-1-fast" }
        "3" { $script:LargeModel = "mistralai/devstral-2-2512" }
        "4" {
            $script:LargeModel = Read-Host "Enter model ID"
            if (-not $script:LargeModel) {
                Write-Warn "Empty model ID, using moonshotai/kimi-k2.5"
                $script:LargeModel = "moonshotai/kimi-k2.5"
            }
        }
        default {
            Write-Warn "Invalid choice, using moonshotai/kimi-k2.5"
            $script:LargeModel = "moonshotai/kimi-k2.5"
        }
    }

    Write-Host ""
    Write-Host "Select the SMALL model (used for fast tasks and tool calls):"
    Write-Host ""
    Write-Host "  [1] Kimi K2.5        (moonshotai/kimi-k2.5)"
    Write-Host "  [2] Grok 4.1 Fast    (x-ai/grok-4-1-fast)"
    Write-Host "  [3] Devstral 2 2512  (mistralai/devstral-2-2512)"
    Write-Host "  [4] Custom model ID"
    Write-Host ""
    $choice = Read-Host "Select [1-4]"

    switch ($choice) {
        "1" { $script:SmallModel = "moonshotai/kimi-k2.5" }
        "2" { $script:SmallModel = "x-ai/grok-4-1-fast" }
        "3" { $script:SmallModel = "mistralai/devstral-2-2512" }
        "4" {
            $script:SmallModel = Read-Host "Enter model ID"
            if (-not $script:SmallModel) {
                Write-Warn "Empty model ID, using mistralai/devstral-2-2512"
                $script:SmallModel = "mistralai/devstral-2-2512"
            }
        }
        default {
            Write-Warn "Invalid choice, using mistralai/devstral-2-2512"
            $script:SmallModel = "mistralai/devstral-2-2512"
        }
    }

    Write-Success "Selected: large=$($script:LargeModel), small=$($script:SmallModel)"
}

function Push-OpenRouterConfig {
    Write-Info "Applying OpenRouter configuration to server..."

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
        Write-Warn "Server not responding - skipping OpenRouter config"
        return
    }

    $body = @{
        default_backend = "openrouter"
        openrouter = @{
            api_key = $script:OpenRouterApiKey
            base_url = "https://openrouter.ai/api/v1"
            large_model = $script:LargeModel
            small_model = $script:SmallModel
        }
    } | ConvertTo-Json -Depth 3

    try {
        Invoke-RestMethod -Uri "http://localhost:9090/api/config" -Method Patch -ContentType "application/json" -Body $body | Out-Null
        Write-Success "OpenRouter config applied (large=$($script:LargeModel), small=$($script:SmallModel))"
    } catch {
        Write-Warn "Failed to apply OpenRouter config: $_"
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

function Install-Skill {
    Write-Info "Installing Claude Code skills..."

    if (-not (Get-Command claude -ErrorAction SilentlyContinue)) {
        Write-Warn "Claude Code not installed, skipping skills"
        return
    }

    $skillsDir = "$env:USERPROFILE\.claude\skills"
    New-Item -ItemType Directory -Path $skillsDir -Force | Out-Null

    $skills = @("brainstorm-to-ralph", "direct-to-ralph", "plan-to-ralph")

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

function Install-Service {
    Write-Info "Installing ralph-o-matic as a scheduled task (auto-start on login)..."

    $binDir = "$env:LOCALAPPDATA\Programs\ralph-o-matic"
    $configDir = "$env:USERPROFILE\.config\ralph-o-matic"
    $logDir = "$configDir\logs"
    New-Item -ItemType Directory -Path $logDir -Force | Out-Null

    $taskName = "RalphOMaticServer"

    # Remove existing task if present
    $existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    if ($existing) {
        Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
    }

    # Create a scheduled task that runs at logon
    $action = New-ScheduledTaskAction `
        -Execute "$binDir\ralph-o-matic-server.exe" `
        -WorkingDirectory $configDir

    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME

    $settings = New-ScheduledTaskSettingsSet `
        -AllowStartIfOnBatteries `
        -DontStopIfGoingOnBatteries `
        -StartWhenAvailable `
        -RestartCount 3 `
        -RestartInterval (New-TimeSpan -Minutes 1) `
        -ExecutionTimeLimit (New-TimeSpan -Days 365)

    Register-ScheduledTask `
        -TaskName $taskName `
        -Action $action `
        -Trigger $trigger `
        -Settings $settings `
        -Description "Ralph-o-matic server - AI iterative development pipeline" `
        -RunLevel Limited | Out-Null

    Write-Success "Scheduled task '$taskName' installed (runs automatically on login)"
}

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

function Start-RalphServer {
    Stop-RalphServer

    Write-Info "Starting ralph-o-matic server..."

    $configDir = "$env:USERPROFILE\.config\ralph-o-matic"
    $taskName = "RalphOMaticServer"

    # Set environment for the server process
    $env:RALPH_DB = "$configDir\data\ralph.db"

    # Start via scheduled task
    Start-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2

    # Verify it's running
    $serverProcess = Get-Process -Name "ralph-o-matic-server" -ErrorAction SilentlyContinue
    if ($serverProcess) {
        Write-Success "Server started (runs automatically on login)"
    } else {
        Write-Warn "Server may have failed to start - check logs at $configDir\logs\"
        Write-Warn "You can start it manually: ralph-o-matic-server"
    }
}

function Request-StartServer {
    Install-Service

    if ($Yes) {
        Start-RalphServer
        return
    }

    Write-Host ""
    $response = Read-Host "Start server now? [Y/n]"
    if (-not $response -or $response -match "^[Yy]") {
        Start-RalphServer
    }
}

# Notification configuration
$script:NotifySmtpEnabled = $false
$script:NotifySmtpHost = ""
$script:NotifySmtpPort = "587"
$script:NotifySmtpUsername = ""
$script:NotifySmtpPassword = ""
$script:NotifySmtpFrom = ""
$script:NotifySmtpRecipients = ""
$script:NotifyTeamsEnabled = $false
$script:NotifyTeamsWebhookUrl = ""

function Set-NotificationConfig {
    # Only for server/full mode
    if ($Mode -eq "client") { return }

    # -Yes flag skips notification setup (notifications are optional)
    if ($Yes) { return }

    Write-Host ""
    $response = Read-Host "Would you like to configure notifications? [y/N]"
    if ($response -notmatch "^[Yy]") { return }

    # SMTP email notifications
    Write-Host ""
    $smtpResponse = Read-Host "Enable email (SMTP) notifications? [y/N]"
    if ($smtpResponse -match "^[Yy]") {
        $script:NotifySmtpEnabled = $true

        $script:NotifySmtpHost = Read-Host "SMTP host"
        $port = Read-Host "SMTP port [587]"
        if ($port) { $script:NotifySmtpPort = $port }
        $script:NotifySmtpUsername = Read-Host "SMTP username"
        $securePassword = Read-Host "SMTP password" -AsSecureString
        $script:NotifySmtpPassword = [Runtime.InteropServices.Marshal]::PtrToStringAuto(
            [Runtime.InteropServices.Marshal]::SecureStringToBSTR($securePassword)
        )
        $script:NotifySmtpFrom = Read-Host "From address"
        $script:NotifySmtpRecipients = Read-Host "Recipient addresses (comma-separated)"

        Write-Success "SMTP notifications configured"
    }

    # Teams webhook notifications
    Write-Host ""
    $teamsResponse = Read-Host "Enable Teams webhook notifications? [y/N]"
    if ($teamsResponse -match "^[Yy]") {
        $script:NotifyTeamsEnabled = $true

        $script:NotifyTeamsWebhookUrl = Read-Host "Teams webhook URL"

        Write-Success "Teams notifications configured"
    }
}

function Push-NotificationConfig {
    # Nothing to apply if no notifications configured
    if (-not $script:NotifySmtpEnabled -and -not $script:NotifyTeamsEnabled) { return }

    Write-Info "Applying notification configuration..."

    # Wait for server to be ready (up to 15 seconds)
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
        Write-Warn "Server not responding - skipping notification config"
        Write-Warn "You can set notification config later with: ralph-o-matic server-config set"
        return
    }

    # Push config via CLI (fall back to Invoke-RestMethod if CLI not in PATH).
    #
    # NOTE: When falling back to Invoke-RestMethod, credentials (SMTP password,
    # Teams webhook URL) are sent over unauthenticated HTTP to localhost. This is
    # acceptable for local installs where the server is on the same machine.
    $useCli = $null -ne (Get-Command ralph-o-matic -ErrorAction SilentlyContinue)

    function Set-ServerConfig($key, $value) {
        if ($useCli) {
            & ralph-o-matic server-config set $key $value
            if ($LASTEXITCODE -ne 0) {
                Write-Warn "Failed to set $key via CLI"
            }
        } else {
            try {
                $body = @{ $key = $value } | ConvertTo-Json
                Invoke-RestMethod -Uri "http://localhost:9090/api/config" -Method Patch -ContentType "application/json" -Body $body | Out-Null
            } catch {
                Write-Warn "Failed to set $key via API: $_"
            }
        }
    }

    if ($script:NotifySmtpEnabled) {
        Set-ServerConfig "notify.smtp.host" $script:NotifySmtpHost
        Set-ServerConfig "notify.smtp.port" $script:NotifySmtpPort
        Set-ServerConfig "notify.smtp.username" $script:NotifySmtpUsername
        Set-ServerConfig "notify.smtp.password" $script:NotifySmtpPassword
        Set-ServerConfig "notify.smtp.from" $script:NotifySmtpFrom
        Set-ServerConfig "notify.smtp.recipients" $script:NotifySmtpRecipients
        Write-Success "SMTP config applied"
    }

    if ($script:NotifyTeamsEnabled) {
        Set-ServerConfig "notify.teams.webhook_url" $script:NotifyTeamsWebhookUrl
        Write-Success "Teams config applied"
    }
}

function Test-NotificationConfig {
    # Nothing to test if no notifications configured
    if (-not $script:NotifySmtpEnabled -and -not $script:NotifyTeamsEnabled) { return }

    Write-Host ""

    if ($script:NotifySmtpEnabled) {
        $response = Read-Host "Send test email notification? [Y/n]"
        if ($response -notmatch "^[Nn]") {
            Write-Info "Sending test email..."
            try {
                & ralph-o-matic test-notify smtp
                if ($LASTEXITCODE -eq 0) {
                    Write-Success "Test email sent"
                } else {
                    Write-Warn "Test email failed - check SMTP settings with: ralph-o-matic server-config list"
                }
            } catch {
                Write-Warn "Test email failed - check SMTP settings with: ralph-o-matic server-config list"
            }
        }
    }

    if ($script:NotifyTeamsEnabled) {
        $response = Read-Host "Send test Teams notification? [Y/n]"
        if ($response -notmatch "^[Nn]") {
            Write-Info "Sending test Teams notification..."
            try {
                & ralph-o-matic test-notify teams
                if ($LASTEXITCODE -eq 0) {
                    Write-Success "Test Teams notification sent"
                } else {
                    Write-Warn "Test Teams notification failed - check webhook URL with: ralph-o-matic server-config list"
                }
            } catch {
                Write-Warn "Test Teams notification failed - check webhook URL with: ralph-o-matic server-config list"
            }
        }
    }
}

# Main
function Main {
    Show-Banner
    Get-Platform

    # -Update: quick software-only update path
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
        } elseif ($Backend -eq "openrouter") {
            Test-OpenRouterKey
            Select-OpenRouterModels
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
        } elseif ($Backend -eq "openrouter") {
            Push-OpenRouterConfig
        }
        Push-NotificationConfig
        Test-NotificationConfig
    }

    Show-Success
}

Main
