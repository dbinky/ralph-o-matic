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

function Install-Plugins {
    Write-Info "Installing Claude Code plugins..."

    try {
        $null = & claude --version 2>$null
    } catch {
        Write-Warn "Claude Code not installed, skipping plugins"
        return
    }

    # Install ralph-wiggum plugin
    try {
        & claude plugins install ralph-wiggum 2>$null
        Write-Success "ralph-wiggum plugin installed"
    } catch {
        Write-Warn "Failed to install ralph-wiggum (may already be installed)"
    }

    # Install brainstorm-to-ralph skill
    $skillsDir = "$env:USERPROFILE\.claude\skills"
    New-Item -ItemType Directory -Path $skillsDir -Force | Out-Null

    $skillUrl = "$ReleaseUrl/brainstorm-to-ralph-skill.zip"
    try {
        Invoke-WebRequest -Uri $skillUrl -OutFile "$env:TEMP\skill.zip"
        Expand-Archive -Path "$env:TEMP\skill.zip" -DestinationPath $skillsDir -Force
        Remove-Item "$env:TEMP\skill.zip"
        Write-Success "brainstorm-to-ralph skill installed"
    } catch {
        Write-Warn "Could not install brainstorm-to-ralph skill"
    }
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
    Install-Plugins
    Set-Configuration
    Show-Success
}

Main
