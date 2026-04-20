# e2e-validate.ps1 - Environment validation for live E2E tests
# Checks all prerequisites before running live integration tests.
#
# Usage: powershell -ExecutionPolicy Bypass -File scripts/e2e-validate.ps1
#
# SECURITY: This script NEVER prints token values - only status.

$ErrorActionPreference = "Continue"

Write-Host ""
Write-Host "=====================================================" -ForegroundColor Cyan
Write-Host "  opencode-fallback - E2E Environment Validation"
Write-Host "=====================================================" -ForegroundColor Cyan
Write-Host ""

$allPassed = $true

# --- Helper ---

function Write-Check {
    param(
        [string]$Label,
        [bool]$Passed,
        [string]$Detail = ""
    )
    if ($Passed) {
        $icon = "[PASS]"
        $color = "Green"
    } else {
        $icon = "[FAIL]"
        $color = "Red"
        $script:allPassed = $false
    }
    $msg = "  $icon $Label"
    if ($Detail) { $msg += " - $Detail" }
    Write-Host $msg -ForegroundColor $color
}

function Write-Skip {
    param([string]$Label, [string]$Detail = "")
    $msg = "  [SKIP] $Label"
    if ($Detail) { $msg += " - $Detail" }
    Write-Host $msg -ForegroundColor Yellow
}

# --- 1. auth.json exists ---

Write-Host "Environment Checks:" -ForegroundColor White
Write-Host ""

$authPath = ""
if ($env:OPENCODE_DATA_DIR) {
    $authPath = Join-Path $env:OPENCODE_DATA_DIR "auth.json"
} else {
    $local = $env:LOCALAPPDATA
    if (-not $local) {
        $local = Join-Path $env:USERPROFILE "AppData\Local"
    }
    $authPath = Join-Path $local "opencode\auth.json"
}

$authExists = Test-Path $authPath
Write-Check "auth.json exists" $authExists "Path: $authPath"

# --- 2. auth.json has valid JSON with anthropic entry ---

$authData = $null
$anthropicEntry = $null
$hasAnthropicOAuth = $false

if ($authExists) {
    try {
        $raw = Get-Content $authPath -Raw -ErrorAction Stop
        $authData = $raw | ConvertFrom-Json -ErrorAction Stop
        Write-Check "auth.json is valid JSON" $true
    } catch {
        Write-Check "auth.json is valid JSON" $false "Parse error: $($_.Exception.Message)"
    }

    if ($authData) {
        $anthropicEntry = $authData.anthropic
        if ($anthropicEntry -and $anthropicEntry.type -eq "oauth") {
            $hasAnthropicOAuth = $true
            Write-Check "Anthropic OAuth entry present" $true "type=oauth"
        } elseif ($anthropicEntry) {
            Write-Check "Anthropic OAuth entry present" $false "type=$($anthropicEntry.type), expected 'oauth'"
        } else {
            Write-Check "Anthropic OAuth entry present" $false "No 'anthropic' key in auth.json"
        }
    }
} else {
    Write-Check "auth.json is valid JSON" $false "File not found"
    Write-Check "Anthropic OAuth entry present" $false "File not found"
}

# --- 3. Token not expired ---

if ($hasAnthropicOAuth -and $anthropicEntry.expires) {
    $expiresMs = [long]$anthropicEntry.expires
    $nowMs = [long]([DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds())
    $remainingMs = $expiresMs - $nowMs

    if ($remainingMs -gt 0) {
        $remainingSpan = [TimeSpan]::FromMilliseconds($remainingMs)
        $hours = [math]::Floor($remainingSpan.TotalHours)
        $minutes = $remainingSpan.Minutes
        Write-Check "Anthropic token not expired" $true "Expires in: ${hours}h ${minutes}m"
    } else {
        $agoMs = -$remainingMs
        $agoSpan = [TimeSpan]::FromMilliseconds($agoMs)
        $agoMinutes = [math]::Floor($agoSpan.TotalMinutes)
        Write-Check "Anthropic token not expired" $false "Expired ${agoMinutes}m ago. Run OpenCode once to trigger token refresh, then try again."
    }
} elseif ($hasAnthropicOAuth) {
    Write-Check "Anthropic token not expired" $true "No expiry set (expires=0)"
} else {
    Write-Check "Anthropic token not expired" $false "No OAuth entry to check"
}

# --- 4. Binary builds ---

Write-Host ""
Write-Host "Build Checks:" -ForegroundColor White
Write-Host ""

try {
    $buildOutput = & go build ./cmd/opencode-fallback 2>&1
    $buildSuccess = $LASTEXITCODE -eq 0
    if ($buildSuccess) {
        Write-Check "go build succeeds" $true
    } else {
        Write-Check "go build succeeds" $false "$buildOutput"
    }
} catch {
    Write-Check "go build succeeds" $false "go not found in PATH"
}

# --- 5. Port 18888 available ---

$portInUse = $false
try {
    $listeners = Get-NetTCPConnection -LocalPort 18888 -ErrorAction SilentlyContinue
    if ($listeners) { $portInUse = $true }
} catch {
    # Port is available if Get-NetTCPConnection fails
}

$portDetail = ""
if ($portInUse) { $portDetail = "Port 18888 is in use - stop the process or use a different port" }
Write-Check "Port 18888 available (test port)" (-not $portInUse) $portDetail

# --- 6. Bridge token file (optional) ---

Write-Host ""
Write-Host "Optional Checks:" -ForegroundColor White
Write-Host ""

$bridgeTokenPath = ""
if ($env:OPENCODE_DATA_DIR) {
    $bridgeTokenPath = Join-Path $env:OPENCODE_DATA_DIR "fallback-bridge-token"
} else {
    $local = $env:LOCALAPPDATA
    if (-not $local) {
        $local = Join-Path $env:USERPROFILE "AppData\Local"
    }
    $bridgeTokenPath = Join-Path $local "opencode\fallback-bridge-token"
}

$bridgeTokenExists = Test-Path $bridgeTokenPath
if ($bridgeTokenExists) {
    $tokenContent = (Get-Content $bridgeTokenPath -Raw).Trim()
    if ($tokenContent.Length -gt 0) {
        Write-Check "Bridge token file present" $true "Token loaded (length: $($tokenContent.Length) chars)"
    } else {
        Write-Skip "Bridge token file present" "File exists but is empty"
    }
} else {
    Write-Skip "Bridge token file present" "Not found at $bridgeTokenPath (bridge tests will be skipped)"
}

# --- 7. GitHub Copilot entry (optional) ---

if ($authData) {
    $copilotEntry = $authData.'github-copilot'
    if ($copilotEntry -and $copilotEntry.type -eq "oauth") {
        Write-Check "GitHub Copilot configured" $true "type=oauth"
    } else {
        Write-Skip "GitHub Copilot configured" "No github-copilot OAuth entry in auth.json"
    }
} else {
    Write-Skip "GitHub Copilot configured" "auth.json not available"
}

# --- Summary ---

Write-Host ""
Write-Host "=====================================================" -ForegroundColor Cyan
if ($allPassed) {
    Write-Host "  All required checks passed! Ready for E2E tests." -ForegroundColor Green
} else {
    Write-Host "  Some required checks failed. Fix issues above." -ForegroundColor Red
}
Write-Host "=====================================================" -ForegroundColor Cyan
Write-Host ""

# Exit with error code if any required check failed.
if (-not $allPassed) { exit 1 }
