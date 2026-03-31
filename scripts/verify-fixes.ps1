# KYD Payment System - System Verification Script
# Verifies gateway health, login, and wallet access.
# Prerequisite: docker-compose up (services running), seed-runner executed.

$ErrorActionPreference = "Stop"
$baseUrl = "http://localhost:9000/api/v1"

Write-Host "=== KYD System Verification ===" -ForegroundColor Cyan

# 1. Gateway health
Write-Host "`n1. Checking Gateway health..." -ForegroundColor Yellow
try {
    $health = Invoke-RestMethod -Uri "http://localhost:9000/health" -Method Get
    Write-Host "   Gateway: $($health.status)" -ForegroundColor Green
} catch {
    Write-Host "   FAILED: Gateway not reachable. Ensure docker-compose is running." -ForegroundColor Red
    exit 1
}

# 2. Login
Write-Host "`n2. Logging in as john.doe@example.com..." -ForegroundColor Yellow
$loginBody = @{
    email = "john.doe@example.com"
    password = "password123"
} | ConvertTo-Json

try {
    $loginResp = Invoke-RestMethod -Uri "$baseUrl/auth/login" -Method Post -Body $loginBody -ContentType "application/json"
    $token = $loginResp.access_token
    if (-not $token) {
        Write-Host "   FAILED: No access_token in response. Run seed-runner first." -ForegroundColor Red
        exit 1
    }
    Write-Host "   Login OK" -ForegroundColor Green
} catch {
    Write-Host "   FAILED: $($_.Exception.Message). Run: docker compose --profile tools run --rm seed-runner" -ForegroundColor Red
    exit 1
}

# 3. Get wallets
Write-Host "`n3. Fetching wallets..." -ForegroundColor Yellow
$headers = @{
    Authorization = "Bearer $token"
}
try {
    $wallets = Invoke-RestMethod -Uri "$baseUrl/wallets" -Method Get -Headers $headers
    $count = if ($wallets.wallets) { $wallets.wallets.Count } else { 0 }
    Write-Host "   Wallets: $count" -ForegroundColor Green
    if ($count -gt 0 -and $wallets.wallets) {
        foreach ($w in $wallets.wallets) {
            $wid = if ($w.wallet_id) { $w.wallet_id } else { $w.id }
            Write-Host "     - $($w.currency): $($w.available_balance) (ID: $wid)" -ForegroundColor Gray
        }
    }
} catch {
    Write-Host "   FAILED: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}

Write-Host "`n=== Verification Complete ===" -ForegroundColor Cyan
