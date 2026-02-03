# Phase 8: PowerShell Setup Script Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create a guided PowerShell script that walks an EntraID admin through app registration setup using the Azure CLI (`az`). The script verifies prerequisites, creates the app registration with roles, and outputs configuration values.

**Architecture:** Single script `scripts/setup-entra.ps1` with four phases: pre-flight checks, gather details, create app registration (with per-step confirmation), and output summary. Each `az` command is shown to the user before execution. Failures are recoverable — the script reports what was already created and advises cleanup.

**Tech Stack:** PowerShell 5.1+, Azure CLI (`az`)

---

### Task 1: Create script skeleton with pre-flight checks

**Files:**
- Create: `scripts/setup-entra.ps1`

**Step 1: Write the script with pre-flight phase**

Create `scripts/setup-entra.ps1`:

```powershell
#!/usr/bin/env pwsh
#Requires -Version 5.1

<#
.SYNOPSIS
    Sets up an EntraID (Azure AD) app registration for ralph-o-matic SSO.

.DESCRIPTION
    Guides an EntraID admin through creating an app registration with:
    - User and Admin app roles
    - Web redirect URI for dashboard SSO
    - Public client redirect URI for CLI auth
    - Client secret for server-side token exchange

.EXAMPLE
    ./setup-entra.ps1
#>

[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# --- Helpers ---

function Write-Step {
    param([string]$Message)
    Write-Host ""
    Write-Host "=== $Message ===" -ForegroundColor Cyan
    Write-Host ""
}

function Write-Success {
    param([string]$Message)
    Write-Host "[OK] $Message" -ForegroundColor Green
}

function Write-Warning {
    param([string]$Message)
    Write-Host "[!] $Message" -ForegroundColor Yellow
}

function Write-Failure {
    param([string]$Message)
    Write-Host "[FAIL] $Message" -ForegroundColor Red
}

function Confirm-Step {
    param([string]$Description, [string]$Command)
    Write-Host ""
    Write-Host "Next step: $Description" -ForegroundColor White
    Write-Host "Command:   $Command" -ForegroundColor DarkGray
    Write-Host ""
    $response = Read-Host "Proceed? (Y/n)"
    if ($response -and $response -notin @("y", "Y", "yes", "Yes", "")) {
        return $false
    }
    return $true
}

# Track what we've created for cleanup guidance
$script:CreatedResources = @()

function Add-CreatedResource {
    param([string]$Type, [string]$Name, [string]$Id)
    $script:CreatedResources += [PSCustomObject]@{
        Type = $Type
        Name = $Name
        Id   = $Id
    }
}

function Show-CleanupAdvice {
    if ($script:CreatedResources.Count -eq 0) {
        Write-Host "No resources were created. Nothing to clean up." -ForegroundColor Gray
        return
    }
    Write-Host ""
    Write-Host "The following resources were created:" -ForegroundColor Yellow
    foreach ($resource in $script:CreatedResources) {
        Write-Host "  - $($resource.Type): $($resource.Name) ($($resource.Id))"
    }
    Write-Host ""
    Write-Host "To clean up, run:" -ForegroundColor Yellow
    foreach ($resource in $script:CreatedResources) {
        if ($resource.Type -eq "App Registration") {
            Write-Host "  az ad app delete --id $($resource.Id)"
        }
        elseif ($resource.Type -eq "Service Principal") {
            Write-Host "  az ad sp delete --id $($resource.Id)"
        }
    }
}

# --- Phase 1: Pre-flight Checks ---

Write-Step "Phase 1: Pre-flight Checks"

# Check az CLI is installed
try {
    $null = Get-Command az -ErrorAction Stop
    Write-Success "Azure CLI (az) is installed"
} catch {
    Write-Failure "Azure CLI (az) is not installed"
    Write-Host "Install it from: https://docs.microsoft.com/en-us/cli/azure/install-azure-cli"
    exit 1
}

# Check az CLI is logged in
try {
    $account = az account show 2>&1 | ConvertFrom-Json
    if (-not $account.tenantId) {
        throw "Not logged in"
    }
    Write-Success "Logged in to Azure"
    Write-Host "  Tenant:  $($account.tenantDisplayName) ($($account.tenantId))"
    Write-Host "  Account: $($account.user.name)"
} catch {
    Write-Failure "Not logged in to Azure CLI"
    Write-Host "Run: az login"
    exit 1
}

$tenantId = $account.tenantId
$tenantName = $account.tenantDisplayName

# Confirm tenant
Write-Host ""
$confirm = Read-Host "Continue with tenant '$tenantName' ($tenantId)? (Y/n)"
if ($confirm -and $confirm -notin @("y", "Y", "yes", "Yes", "")) {
    Write-Host "Aborted."
    exit 0
}

# --- Phase 2: Gather Details ---

Write-Step "Phase 2: Gather Details"

$appName = Read-Host "App display name (default: ralph-o-matic)"
if (-not $appName) { $appName = "ralph-o-matic" }

$serverUrl = Read-Host "Server URL (e.g., https://ralph.example.com)"
if (-not $serverUrl) {
    Write-Failure "Server URL is required"
    exit 1
}

# Validate HTTPS
if ($serverUrl -notlike "https://*") {
    Write-Warning "Server URL is not HTTPS. This is insecure for production use."
    $override = Read-Host "Continue anyway? (y/N)"
    if ($override -notin @("y", "Y", "yes", "Yes")) {
        Write-Host "Aborted. Use an HTTPS URL."
        exit 0
    }
}

# Remove trailing slash
$serverUrl = $serverUrl.TrimEnd("/")
$webRedirectUri = "$serverUrl/auth/callback"
$cliRedirectUri = "http://localhost"

Write-Host ""
Write-Host "Configuration:" -ForegroundColor White
Write-Host "  App name:         $appName"
Write-Host "  Server URL:       $serverUrl"
Write-Host "  Web redirect:     $webRedirectUri"
Write-Host "  CLI redirect:     $cliRedirectUri"

# --- Phase 3: Create App Registration ---

Write-Step "Phase 3: Create App Registration"

# Step 1: Create app registration
if (-not (Confirm-Step "Create app registration '$appName'" `
        "az ad app create --display-name '$appName' --sign-in-audience AzureADMyOrg")) {
    Show-CleanupAdvice
    exit 0
}

try {
    $appJson = az ad app create --display-name $appName --sign-in-audience AzureADMyOrg 2>&1
    $app = $appJson | ConvertFrom-Json
    $appId = $app.appId
    $appObjectId = $app.id
    Add-CreatedResource "App Registration" $appName $appObjectId
    Write-Success "App registration created: $appId"
} catch {
    Write-Failure "Failed to create app registration: $_"
    Show-CleanupAdvice
    exit 1
}

# Step 2: Define app roles
$rolesJson = @'
[
    {
        "allowedMemberTypes": ["User"],
        "description": "Submit and manage own jobs",
        "displayName": "User",
        "isEnabled": true,
        "value": "User"
    },
    {
        "allowedMemberTypes": ["User"],
        "description": "Full access including server configuration",
        "displayName": "Admin",
        "isEnabled": true,
        "value": "Admin"
    }
]
'@

if (-not (Confirm-Step "Define app roles (User, Admin)" `
        "az ad app update --id $appObjectId --app-roles '...'")) {
    Show-CleanupAdvice
    exit 0
}

try {
    $rolesFile = [System.IO.Path]::GetTempFileName()
    $rolesJson | Set-Content -Path $rolesFile -Encoding UTF8
    az ad app update --id $appObjectId --app-roles "@$rolesFile" 2>&1 | Out-Null
    Remove-Item $rolesFile -ErrorAction SilentlyContinue
    Write-Success "App roles defined: User, Admin"
} catch {
    Write-Failure "Failed to define app roles: $_"
    Show-CleanupAdvice
    exit 1
}

# Step 3: Add web redirect URI
if (-not (Confirm-Step "Add web redirect URI for dashboard SSO" `
        "az ad app update --id $appObjectId --web-redirect-uris '$webRedirectUri'")) {
    Show-CleanupAdvice
    exit 0
}

try {
    az ad app update --id $appObjectId --web-redirect-uris $webRedirectUri 2>&1 | Out-Null
    Write-Success "Web redirect URI added: $webRedirectUri"
} catch {
    Write-Failure "Failed to add web redirect URI: $_"
    Show-CleanupAdvice
    exit 1
}

# Step 4: Add CLI redirect URI (public client)
if (-not (Confirm-Step "Add CLI redirect URI (localhost)" `
        "az ad app update --id $appObjectId --public-client-redirect-uris '$cliRedirectUri'")) {
    Show-CleanupAdvice
    exit 0
}

try {
    az ad app update --id $appObjectId --public-client-redirect-uris $cliRedirectUri 2>&1 | Out-Null
    Write-Success "CLI redirect URI added: $cliRedirectUri"
} catch {
    Write-Failure "Failed to add CLI redirect URI: $_"
    Show-CleanupAdvice
    exit 1
}

# Step 5: Create client secret
if (-not (Confirm-Step "Create client secret (1 year validity)" `
        "az ad app credential reset --id $appObjectId --years 1")) {
    Show-CleanupAdvice
    exit 0
}

try {
    $credJson = az ad app credential reset --id $appObjectId --years 1 --display-name "ralph-o-matic server" 2>&1
    $cred = $credJson | ConvertFrom-Json
    $clientSecret = $cred.password
    $secretExpiry = (Get-Date).AddYears(1).ToString("yyyy-MM-dd")
    Write-Success "Client secret created (expires: $secretExpiry)"
} catch {
    Write-Failure "Failed to create client secret: $_"
    Show-CleanupAdvice
    exit 1
}

# Step 6: Create service principal
if (-not (Confirm-Step "Create service principal for user assignment" `
        "az ad sp create --id $appId")) {
    Show-CleanupAdvice
    exit 0
}

try {
    $spJson = az ad sp create --id $appId 2>&1
    $sp = $spJson | ConvertFrom-Json
    Add-CreatedResource "Service Principal" $appName $sp.id
    Write-Success "Service principal created"
} catch {
    Write-Failure "Failed to create service principal: $_"
    Show-CleanupAdvice
    exit 1
}

# --- Phase 4: Summary ---

Write-Step "Phase 4: Setup Complete!"

Write-Host "Configure your ralph-o-matic server with:" -ForegroundColor White
Write-Host ""
Write-Host "Environment variables:" -ForegroundColor Cyan
Write-Host "  RALPH_AUTH_MODE=entra"
Write-Host "  RALPH_ENTRA_TENANT_ID=$tenantId"
Write-Host "  RALPH_ENTRA_CLIENT_ID=$appId"
Write-Host "  RALPH_ENTRA_CLIENT_SECRET=$clientSecret"
Write-Host ""
Write-Host "Or add to /etc/ralph-o-matic/settings.json:" -ForegroundColor Cyan

$settingsJson = @"
{
  "auth": {
    "mode": "entra",
    "entra": {
      "tenant_id": "$tenantId",
      "client_id": "$appId",
      "client_secret": "$clientSecret"
    }
  }
}
"@
Write-Host $settingsJson
Write-Host ""
Write-Host "IMPORTANT:" -ForegroundColor Yellow
Write-Host "  1. Assign users/groups to the 'User' or 'Admin' role in Azure Portal:"
Write-Host "     Azure Portal > Enterprise applications > $appName > Users and groups"
Write-Host ""
Write-Host "  2. Client secret expires on $secretExpiry"
Write-Host "     Set a reminder to rotate it before then."
Write-Host ""
Write-Host "  3. Users with no assigned role will get 403 — being in the tenant is not sufficient."
```

**Step 2: Make script executable**

Run: `chmod +x scripts/setup-entra.ps1` (for Unix systems)

**Step 3: Commit**

```bash
git add scripts/setup-entra.ps1
git commit -m "feat: add PowerShell setup script for EntraID app registration"
```

---

### Task 2: Manual testing checklist

This script is interactive and uses the Azure CLI, so automated testing is limited. The following manual testing checklist should be run during initial deployment:

**Pre-flight checks:**
- [ ] Run without `az` installed — exits with install instructions
- [ ] Run without `az login` — exits with login instructions
- [ ] Run with valid login — shows tenant info and prompts

**Interactive flow:**
- [ ] Accept all defaults — creates app with all resources
- [ ] Decline at step 1 — nothing created, clean exit
- [ ] Decline at step 3 — reports steps 1-2 completed
- [ ] Invalid server URL — warns about HTTP

**Output validation:**
- [ ] Tenant ID matches logged-in tenant
- [ ] Client ID is a valid GUID
- [ ] Client secret is present and non-empty
- [ ] Both env var and settings.json formats displayed
- [ ] Expiry warning present

---

## Dependencies

- **Depends on:** Nothing (standalone script, can be developed in parallel)
- **Blocks:** Nothing (manual prerequisite for enabling auth)

## Reference Files

- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 346-403, "PowerShell Setup Script")
- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 695-713, "PowerShell Script Tests")
- Existing install script: `scripts/install.sh` (for script style reference)
