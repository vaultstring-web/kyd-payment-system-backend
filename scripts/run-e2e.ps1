<#
Run this script from PowerShell in the repository root.

Usage:
  1. Copy `.env.example` to `.env` and edit values if needed.
     cp .env.example .env
  2. From the repo root run:
     powershell -ExecutionPolicy Bypass -File .\scripts\run-e2e.ps1

What it does:
  - Loads env vars from `.env` (if present) or `.env.example`.
  - Assumes services are already running (use `scripts/run-supervisor-fixed.ps1`).
  - Waits for the gateway health endpoint to be ready.
  - Runs a scripted test: logs in seeded users, initiates an MWK -> CNY payment,
    and prints the transaction response and recipient wallets.

Notes:
  - This script opens new terminal windows for each service using Start-Process.
  - Blockchain services use stubbed implementations locally (no real on-chain
    settlement) — the settlement flow is simulated by the stubs.
#>

function Set-EnvFromFile {
    param(
        [string]$Path
    )
    if (-not (Test-Path $Path)) { return }
    Get-Content $Path | ForEach-Object {
        if ($_ -match '^[#;]' -or $_ -notmatch '=') { return }
        $parts = $_ -split '=', 2
        $k = $parts[0].Trim()
        $v = $parts[1].Trim()
        if ($k -ne '') { Set-Item -Path Env:$k -Value $v }
    }
}

# Resolve repository root (parent of the scripts directory)
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $repoRoot

# Load .env if present, otherwise .env.example
if (Test-Path .env) { Write-Output "Loading .env"; Set-EnvFromFile -Path .env } else { Write-Output "Loading .env.example"; Set-EnvFromFile -Path .env.example }

# Defaults for local dev if missing
if (-not $env:DATABASE_URL) { $env:DATABASE_URL = 'postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable' }
if (-not $env:REDIS_URL) { $env:REDIS_URL = 'localhost:6379' }
if (-not $env:JWT_SECRET) { $env:JWT_SECRET = 'sk_dev_local' }
if (-not $env:AUTH_PORT) { $env:AUTH_PORT = '3000' }
if (-not $env:PAYMENT_PORT) { $env:PAYMENT_PORT = '3001' }
if (-not $env:FOREX_PORT) { $env:FOREX_PORT = '3002' }
if (-not $env:WALLET_PORT) { $env:WALLET_PORT = '3003' }
if (-not $env:SETTLEMENT_PORT) { $env:SETTLEMENT_PORT = '3004' }
if (-not $env:GATEWAY_PORT) { $env:GATEWAY_PORT = '9000' }

# Convenience local variables
$db = $env:DATABASE_URL
$redis = $env:REDIS_URL
$jwt = $env:JWT_SECRET

$ports = @{ auth = $env:AUTH_PORT; payment = $env:PAYMENT_PORT; forex = $env:FOREX_PORT; wallet = $env:WALLET_PORT; settlement = $env:SETTLEMENT_PORT; gateway = $env:GATEWAY_PORT }

Write-Output "Using DB: $db"
Write-Output "Using Redis: $redis"

Write-Output "Applying database migrations..."
go run ./cmd/migrate/main.go up

Write-Output "Seeding users and wallets..."
# Ensure seed uses compliant phone format and known credentials
$env:SEED_EMAIL = 'john.doe@example.com'
$env:SEED_PASSWORD = 'Password123'
$env:SEED_PHONE = '+265991234567'
$env:SEED_FIRST = 'John'
$env:SEED_LAST = 'Doe'
$env:SEED_COUNTRY = 'MW'
$env:SEED_WANG_EMAIL = 'wang.wei@example.com'
$env:SEED_WANG_PASSWORD = 'Password123'
$env:SEED_WANG_PHONE = '+8613800138000'
$env:SEED_WANG_FIRST = 'Wang'
$env:SEED_WANG_LAST = 'Wei'
$env:SEED_WANG_COUNTRY = 'CN'
go run ./cmd/seed/main.go

Write-Output "Waiting for gateway to become healthy on port $($ports.gateway)..."
$gatewayUrl = "http://localhost:$($ports.gateway)/health"
$attempt = 0
while ($attempt -lt 30) {
    try {
        $res = Invoke-RestMethod -Method Get -Uri $gatewayUrl -TimeoutSec 3
        if ($res -and $res.status) { Write-Output "Gateway healthy: $($gatewayUrl)"; break }
    } catch {
        Start-Sleep -Seconds 2
        $attempt++
    }
}

if ($attempt -ge 30) { Write-Output "Gateway did not become ready in time. Check service windows for errors."; exit 1 }

Write-Output "Running integration test via gateway..."

try {
    $loginJohn = Invoke-RestMethod -Method Post -Uri "http://localhost:$($ports.gateway)/api/v1/auth/login" -ContentType 'application/json' -Body (@{email='john.doe@example.com'; password='Password123'} | ConvertTo-Json) -ErrorAction Stop
    Write-Output "=== John Login Response ==="
    $loginJohn | ConvertTo-Json -Depth 6 | Write-Output

    $loginWang = Invoke-RestMethod -Method Post -Uri "http://localhost:$($ports.gateway)/api/v1/auth/login" -ContentType 'application/json' -Body (@{email='wang.wei@example.com'; password='Password123'} | ConvertTo-Json) -ErrorAction Stop
    Write-Output "=== Wang Login Response ==="
    $loginWang | ConvertTo-Json -Depth 6 | Write-Output

    $tokenJohn = $loginJohn.access_token
    $johnID = $loginJohn.user.id
    $tokenWang = $loginWang.access_token
    $wangID = $loginWang.user.id

    Write-Output "Initiating payment: John ($johnID) -> Wang ($wangID) 1000 MWK"

    $paymentBody = @{ sender_id = $johnID; receiver_id = $wangID; amount = '1000'; currency = 'MWK'; description = 'E2E test MWK->CNY'; channel='mobile'; category='transfer' } | ConvertTo-Json

    $idemKey = "e2e-$([System.Guid]::NewGuid().ToString())-$([DateTime]::UtcNow.ToString('yyyyMMddHHmmss'))"
    $resp = Invoke-RestMethod -Method Post -Uri "http://localhost:$($ports.gateway)/api/v1/payments/initiate" -ContentType 'application/json' -Headers @{ Authorization = "Bearer $tokenJohn"; 'Idempotency-Key' = $idemKey } -Body $paymentBody -ErrorAction Stop
    Write-Output "=== Payment Response ==="
    $resp | ConvertTo-Json -Depth 8 | Write-Output

    Write-Output "Fetching recipient (Wang) wallets to confirm balance changes..."
    $wallets = Invoke-RestMethod -Method Get -Uri "http://localhost:$($ports.gateway)/api/v1/wallets" -Headers @{ Authorization = "Bearer $tokenWang" } -ErrorAction Stop
    Write-Output "=== Wang Wallets ==="
    $wallets | ConvertTo-Json -Depth 8 | Write-Output

    Write-Output "E2E test completed. Inspect service windows for detailed logs and settlement confirmations (stubbed)."
} catch {
    Write-Output "Integration test failed: $_"
    exit 1
}
