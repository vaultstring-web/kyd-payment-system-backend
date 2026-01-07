<#
scripts/start-backend.ps1

Unified backend startup script for VaultString (KYD Payment System).
Features:
- Checks and starts Docker dependencies (Postgres, Redis)
- Rebuilds Go binaries to ensure latest code is running
- Runs database migrations and seeds
- Starts all microservices with automatic restart supervision
- Logs output to logs/ directory

Usage:
  powershell -ExecutionPolicy Bypass -File .\scripts\start-backend.ps1
#>

$ErrorActionPreference = "Stop"

# ==============================================================================
# 1. HELPER FUNCTIONS
# ==============================================================================

function Load-EnvFile {
    param([string]$Path)
    if (-not (Test-Path $Path)) { 
        Write-Warning "Env file not found: $Path"
        return 
    }

    Get-Content $Path | ForEach-Object {
        $line = $_.Trim()
        if (-not $line -or $line.StartsWith('#') -or $line.StartsWith(';')) { return }
        if ($line.Contains('#')) { $line = $line.Substring(0, $line.IndexOf('#')).Trim() }
        if ($line.Contains('=')) {
            $idx = $line.IndexOf('=')
            $key = $line.Substring(0, $idx).Trim()
            $val = $line.Substring($idx + 1).Trim()
            if ($val.StartsWith('"') -and $val.EndsWith('"')) { $val = $val.Substring(1, $val.Length - 2) }
            if ($val.StartsWith("'") -and $val.EndsWith("'")) { $val = $val.Substring(1, $val.Length - 2) }
            if ($key) { [System.Environment]::SetEnvironmentVariable($key, $val, "Process") }
        }
    }
}

function Start-MonitoredService {
    param([hashtable]$svc)
    $name = $svc.name
    $cmd = $svc.cmd
    $port = $svc.port
    $logFile = Join-Path $repoRoot "logs\$($name).log"

    Start-Job -Name "svc-$name" -ScriptBlock {
        param($repoRoot, $svc, $port, $logFile, $DATABASE_URL, $REDIS_URL, $JWT_SECRET, $name)
        
        # Ensure we are in the repo root
        Set-Location $repoRoot

        # Set Env Vars for this job scope
        $env:DATABASE_URL = $DATABASE_URL
        $env:REDIS_URL = $REDIS_URL
        $env:JWT_SECRET = $JWT_SECRET
        $env:SERVER_PORT = $port
        
        # Service specific envs
        if ($name -eq 'settlement') {
            $env:STELLAR_NETWORK_URL = 'https://horizon-testnet.stellar.org'
            $env:RIPPLE_SERVER_URL = 'wss://s.altnet.rippletest.net:51233'
        }

        while ($true) {
            try {
                $ts = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'
                Add-Content -Path $logFile -Value "`n===== [$ts] START ====="
                
                # Execute Command Directly
                # Use PowerShell invocation to avoid cmd.exe paging file issues
                # This blocks until the process exits and appends all streams to the log
                & "$($svc.cmd)" *>> "$logFile"
                
                $exitTs = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'
                Add-Content -Path $logFile -Value "===== [$exitTs] EXIT (restart in 2s) ====="
            } catch {
                $errMsg = $_.Exception.Message
                Add-Content -Path $logFile -Value "ERROR: $errMsg"
            }
            Start-Sleep -Seconds 2
        }
    } -ArgumentList $repoRoot, $svc, $port, $logFile, $env:DATABASE_URL, $env:REDIS_URL, $env:JWT_SECRET, $name | Out-Null
    
    Write-Host "  + Started monitor for $name (Port $port) -> logs/$name.log" -ForegroundColor Green
}

function Test-ServiceHealth {
    param(
        [string]$name,
        [string]$url,
        [int]$maxAttempts = 20,
        [int]$delaySeconds = 1
    )
    $attempt = 0
    while ($attempt -lt $maxAttempts) {
        try {
            $resp = Invoke-WebRequest -UseBasicParsing -Uri $url -Method GET -TimeoutSec 3
            if ($resp.StatusCode -eq 200) {
                Write-Host "  OK: $name healthy at $url" -ForegroundColor Green
                return $true
            }
        } catch {
        }
        Start-Sleep -Seconds $delaySeconds
        $attempt++
    }
    Write-Warning "  WARN: $name not healthy at $url after $maxAttempts attempts"
    return $false
}

function Test-TcpPort {
    param(
        [string]$name,
        [string]$tcpHost = "127.0.0.1",
        [int]$port,
        [int]$maxAttempts = 30,
        [int]$delaySeconds = 1
    )
    $attempt = 0
    while ($attempt -lt $maxAttempts) {
        $conn = Test-NetConnection -ComputerName $tcpHost -Port $port -InformationLevel Quiet
        if ($conn) {
            Write-Host ("  OK: {0} reachable on {1}:{2}" -f $name, $tcpHost, $port) -ForegroundColor Green
            return $true
        }
        Start-Sleep -Seconds $delaySeconds
        $attempt++
    }
    Write-Warning ("  WARN: {0} not reachable on {1}:{2} after {3} attempts" -f $name, $tcpHost, $port, $maxAttempts)
    return $false
}

function Wait-ForDatabase {
    param([int]$port = 5432)
    Write-Host "Waiting for Postgres to accept connections..." -ForegroundColor Yellow
    $ready = Test-TcpPort -name "postgres" -tcpHost "127.0.0.1" -port $port -maxAttempts 60 -delaySeconds 1
    if (-not $ready) {
        Write-Warning "Postgres did not become ready in time; migrations/seed may fail."
    }
}

function Wait-ServiceHealthy {
    param(
        [string]$containerName,
        [int]$maxAttempts = 60,
        [int]$delaySeconds = 2
    )
    $attempt = 0
    while ($attempt -lt $maxAttempts) {
        try {
            $status = docker inspect -f "{{.State.Health.Status}}" $containerName 2>$null
            if ($status -and $status.Trim() -eq "healthy") {
                Write-Host ("  OK: {0} is healthy" -f $containerName) -ForegroundColor Green
                return $true
            }
        } catch {
        }
        Start-Sleep -Seconds $delaySeconds
        $attempt++
    }
    Write-Warning ("  WARN: {0} not healthy after {1} attempts" -f $containerName, $maxAttempts)
    return $false
}

# ==============================================================================
# 2. SETUP & CLEANUP
# ==============================================================================

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$repoRoot  = Split-Path -Parent $scriptDir
Set-Location $repoRoot

Write-Host "=== VaultString Backend Starter ===" -ForegroundColor Cyan

# Check Docker
Write-Host "Checking Docker..." -ForegroundColor Yellow
try {
    docker ps | Out-Null
    Write-Host "  Docker is running." -ForegroundColor Green
} catch {
    Write-Error "Docker is NOT running. Please start Docker Desktop and try again."
    exit 1
}

# Cleanup existing
Write-Host "Cleaning up old processes..." -ForegroundColor Yellow
Get-Process -Name "auth", "payment", "forex", "wallet", "settlement", "gateway" -ErrorAction SilentlyContinue | Stop-Process -Force
Get-Job -Name "svc-*" -ErrorAction SilentlyContinue | Remove-Job -Force
# Clean launcher scripts
Remove-Item "scripts\launcher-*.ps1" -ErrorAction SilentlyContinue

# Load Env
# Load environment variables
    Load-EnvFile "$repoRoot\env.example"
    Load-EnvFile "$repoRoot\.env"

if (-not $env:DATABASE_URL) {
    Write-Warning "DATABASE_URL is not set! Services will likely fail."
} else {
    Write-Host "Loaded DATABASE_URL: $env:DATABASE_URL" -ForegroundColor Cyan
}

# Ensure Logs Dir
if (-not (Test-Path logs)) { New-Item -ItemType Directory -Path logs | Out-Null }

# ==============================================================================
# 3. INFRASTRUCTURE & BUILD
# ==============================================================================

Write-Host "Starting Database and Cache..." -ForegroundColor Yellow
docker compose up -d postgres redis
if ($LASTEXITCODE -ne 0) { Write-Error "Failed to start Docker containers"; exit 1 }

# Wait for container health and DB readiness
Write-Host "Waiting for container health checks..." -ForegroundColor Yellow
$null = Wait-ServiceHealthy -containerName "kyd-postgres" -maxAttempts 60 -delaySeconds 2
$null = Wait-ServiceHealthy -containerName "kyd-redis" -maxAttempts 60 -delaySeconds 2
Wait-ForDatabase -port 5432

Write-Host "Building Services..." -ForegroundColor Yellow
$builds = @(
    @{ name="auth";       path="cmd/auth/main.go" },
    @{ name="payment";    path="cmd/payment/main.go" },
    @{ name="forex";      path="cmd/forex/main.go" },
    @{ name="wallet";     path="cmd/wallet/main.go" },
    @{ name="settlement"; path="cmd/settlement/main.go" },
    @{ name="gateway";    path="cmd/gateway/main.go" }
)

foreach ($b in $builds) {
    Write-Host "  Building $($b.name)..." -NoNewline
    go build -o "build/$($b.name).exe" $b.path
    if ($LASTEXITCODE -eq 0) { Write-Host " OK" -ForegroundColor Green }
    else { Write-Host " FAILED" -ForegroundColor Red; exit 1 }
}

# ==============================================================================
# 4. MIGRATION & SEED
# ==============================================================================

Write-Host "Running Migrations..." -ForegroundColor Yellow
$prevDbUrl = $env:DATABASE_URL
# Use admin connection if possible, else fall back to default
$migrateUrl = "postgres://kyd_admin:admin_secure_pass@localhost:5432/kyd_dev?sslmode=disable"
$migrateUrl = $migrateUrl -replace "localhost","127.0.0.1"
$env:DATABASE_URL = $migrateUrl
go run ./cmd/migrate/main.go up
if ($LASTEXITCODE -ne 0) { 
    Write-Warning "Migration with admin user failed, trying with default user..."
    $env:DATABASE_URL = $prevDbUrl
    go run ./cmd/migrate/main.go up
}
$env:DATABASE_URL = $prevDbUrl

Write-Host "Seeding Database..." -ForegroundColor Yellow
go run ./cmd/seed/main.go

# ==============================================================================
# 5. START SERVICES
# ==============================================================================

Write-Host "Starting Services..." -ForegroundColor Cyan

# Default ports if not set
if (-not $env:AUTH_PORT) { $env:AUTH_PORT = '3000' }
if (-not $env:PAYMENT_PORT) { $env:PAYMENT_PORT = '3001' }
if (-not $env:FOREX_PORT) { $env:FOREX_PORT = '3002' }
if (-not $env:WALLET_PORT) { $env:WALLET_PORT = '3003' }
if (-not $env:SETTLEMENT_PORT) { $env:SETTLEMENT_PORT = '3004' }
if (-not $env:GATEWAY_PORT) { $env:GATEWAY_PORT = '9000' }

$services = @(
    @{ name='auth';       cmd='.\build\auth.exe';       port=$env:AUTH_PORT },
    @{ name='payment';    cmd='.\build\payment.exe';    port=$env:PAYMENT_PORT },
    @{ name='forex';      cmd='.\build\forex.exe';      port=$env:FOREX_PORT },
    @{ name='wallet';     cmd='.\build\wallet.exe';     port=$env:WALLET_PORT },
    @{ name='settlement'; cmd='.\build\settlement.exe'; port=$env:SETTLEMENT_PORT },
    @{ name='gateway';    cmd='.\build\gateway.exe';    port=$env:GATEWAY_PORT }
)

foreach ($svc in $services) { Start-MonitoredService -svc $svc }

Write-Host "`nALL SERVICES STARTED!" -ForegroundColor Green
Write-Host "Gateway: http://localhost:$($env:GATEWAY_PORT)"
Write-Host "Logs are being written to the 'logs/' directory."
Write-Host "Press Ctrl+C to stop all services."

Write-Host "`nChecking service health..." -ForegroundColor Yellow
$null = Test-TcpPort -name "postgres" -tcpHost "127.0.0.1" -port 5432 -maxAttempts 30 -delaySeconds 1
$null = Test-TcpPort -name "redis" -tcpHost "127.0.0.1" -port 6379 -maxAttempts 30 -delaySeconds 1
$authHealthy = Test-ServiceHealth -name "auth" -url ("http://127.0.0.1:{0}/health" -f $env:AUTH_PORT)
$payHealthy = Test-ServiceHealth -name "payment" -url ("http://127.0.0.1:{0}/health" -f $env:PAYMENT_PORT)
$forexHealthy = Test-ServiceHealth -name "forex" -url ("http://127.0.0.1:{0}/health" -f $env:FOREX_PORT)
$walletHealthy = Test-ServiceHealth -name "wallet" -url ("http://127.0.0.1:{0}/health" -f $env:WALLET_PORT)
$settHealthy = Test-ServiceHealth -name "settlement" -url ("http://127.0.0.1:{0}/health" -f $env:SETTLEMENT_PORT)
$gwHealthy = Test-ServiceHealth -name "gateway" -url ("http://127.0.0.1:{0}/health" -f $env:GATEWAY_PORT)
if (-not ($authHealthy -and $gwHealthy)) { Write-Warning "Auth or Gateway not healthy; admin login may return 502. Check logs/auth.log and logs/gateway.log." }

# Keep alive
while ($true) { Start-Sleep -Seconds 60 }
