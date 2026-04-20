# Quick check of auth.json token status (no secrets printed)
$path = Join-Path $env:LOCALAPPDATA "opencode\auth.json"
$a = Get-Content $path -Raw | ConvertFrom-Json
$entry = $a.anthropic

Write-Host "type: $($entry.type)"
Write-Host "has_access: $($entry.access.Length -gt 0)"
Write-Host "has_refresh: $($entry.refresh.Length -gt 0)"
Write-Host "expires_ms: $($entry.expires)"

$nowMs = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
Write-Host "now_ms: $nowMs"

$diff = $entry.expires - $nowMs
if ($diff -gt 0) {
    $span = [TimeSpan]::FromMilliseconds($diff)
    Write-Host "STATUS: VALID - expires in $([math]::Floor($span.TotalHours))h $($span.Minutes)m"
} else {
    $span = [TimeSpan]::FromMilliseconds(-$diff)
    Write-Host "STATUS: EXPIRED - expired $([math]::Floor($span.TotalHours))h $($span.Minutes)m ago"
}

# Show all provider keys in auth.json
Write-Host ""
Write-Host "Providers in auth.json:"
$a.PSObject.Properties | ForEach-Object { Write-Host "  - $($_.Name) (type: $($_.Value.type))" }
