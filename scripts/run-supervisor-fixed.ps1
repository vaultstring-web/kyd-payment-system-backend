<#
scripts/run-supervisor.ps1 (fixed)

Supervisor for local development on Windows. It repeatedly launches each Go service
and restarts it if it exits (crash or manual stop). Logs are written to `logs/`.

Usage:
  1. Copy `.env.example` to `.env` and edit values as needed.
  2. Run this script in an elevated PowerShell or with ExecutionPolicy Bypass:
     powershell -ExecutionPolicy Bypass -File .\scripts\run-supervisor.ps1

Notes:
  - This is a development helper. For production use a proper Windows Service
    wrapper or container orchestration (Docker Compose, Kubernetes, systemd, etc.).
  - The script launches each service in its own background job. Each job monitors
    its process and restarts it on exit. Logs are appended to `logs/<service>.log`.
#>

function Load-EnvFile {
    param([string]$Path)
    if (-not (Test-Path $Path)) { 
        Write-Warning "Env file not found: $Path"
        return 
    }

    Get-Content $Path | ForEach-Object {
        $line = $_.Trim()
        # Skip empty lines and comments
        if (-not $line -or $line.StartsWith('#') -or $line.StartsWith(';')) { return }
        
        # Remove inline comments
        if ($line.Contains('#')) { $line = $line.Substring(0, $line.IndexOf('#')).Trim() }
        
        # Parse key=value
        if ($line.Contains('=')) {
            $idx = $line.IndexOf('=')
            $key = $line.Substring(0, $idx).Trim()
            $val = $line.Substring($idx + 1).Trim()
            
            # Remove quotes
            if ($val.StartsWith('"') -and $val.EndsWith('"')) { $val = $val.Substring(1, $val.Length - 2) }
            if ($val.StartsWith("'") -and $val.EndsWith("'")) { $val = $val.Substring(1, $val.Length - 2) }
            
            if ($key) { [System.Environment]::SetEnvironmentVariable($key, $val, "Process") }
        }
    }
}

# Setup paths
# Compute script directory and then repo root as the parent of the scripts folder
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$repoRoot = Split-Path -Parent $scriptDir
Set-Location $repoRoot

# Load environment
if (Test-Path .env) { 
    Write-Output "Loading .env"
    Load-EnvFile .env
} else {
    Write-Output "Loading .env.example"
    Load-EnvFile .env.example
}

# Set defaults for ports
if (-not $env:AUTH_PORT) { $env:AUTH_PORT = '3000' }
if (-not $env:PAYMENT_PORT) { $env:PAYMENT_PORT = '3001' }
if (-not $env:FOREX_PORT) { $env:FOREX_PORT = '3002' }
if (-not $env:WALLET_PORT) { $env:WALLET_PORT = '3003' }
if (-not $env:SETTLEMENT_PORT) { $env:SETTLEMENT_PORT = '3004' }
if (-not $env:GATEWAY_PORT) { $env:GATEWAY_PORT = '9000' }

# Check using direct env access (more reliable)
if (-not $env:DATABASE_URL -or -not $env:REDIS_URL -or -not $env:JWT_SECRET) {
    Write-Error "Missing required environment variables"
    if (-not $env:DATABASE_URL) { Write-Error "  - DATABASE_URL not set" }
    if (-not $env:REDIS_URL) { Write-Error "  - REDIS_URL not set" }
    if (-not $env:JWT_SECRET) { Write-Error "  - JWT_SECRET not set" }
    Write-Error "Please configure .env file before running."
    exit 1
}

Write-Output "Environment loaded successfully"
Write-Output "Ports: auth=$($env:AUTH_PORT) payment=$($env:PAYMENT_PORT) forex=$($env:FOREX_PORT) wallet=$($env:WALLET_PORT) settlement=$($env:SETTLEMENT_PORT) gateway=$($env:GATEWAY_PORT)"

if (-not (Test-Path logs)) { New-Item -ItemType Directory -Path logs | Out-Null }

# Define services to start
# Prefer compiled executables in build/. If missing, fall back to `go run`.
$services = @(
    @{ name='auth'; cmd='.\\build\\auth.exe'; fallback='go run .\\cmd\\auth'; port=$env:AUTH_PORT },
    @{ name='wallet'; cmd='.\\build\\wallet.exe'; fallback='go run .\\cmd\\wallet'; port=$env:WALLET_PORT },
    @{ name='forex'; cmd='.\\build\\forex.exe'; fallback='go run .\\cmd\\forex'; port=$env:FOREX_PORT },
    @{ name='payment'; cmd='.\\build\\payment.exe'; fallback='go run .\\cmd\\payment'; port=$env:PAYMENT_PORT },
    @{ name='settlement'; cmd='.\\build\\settlement.exe'; fallback='go run .\\cmd\\settlement'; port=$env:SETTLEMENT_PORT },
    @{ name='gateway'; cmd='.\\build\\gateway.exe'; fallback='go run .\\cmd\\gateway'; port=$env:GATEWAY_PORT }
)

function Start-MonitoredService {
    param([hashtable]$svc)
    $name = $svc.name
    $cmd = $svc.cmd
    $port = $svc.port
    $logFile = Join-Path $repoRoot "logs\$($name).log"

    Start-Job -Name "svc-$name" -ScriptBlock {
        param($repoRoot, $svc, $port, $logFile, $DATABASE_URL, $REDIS_URL, $JWT_SECRET, $name, $authPort, $paymentPort, $walletPort, $forexPort, $settlementPort)
        
        while ($true) {
            try {
                $ts = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'
                Add-Content -Path $logFile -Value "`n===== [$ts] START ====="
                
                Set-Location $repoRoot

                # Set core env vars in this job process so child inherits them
                $env:DATABASE_URL = $DATABASE_URL
                $env:REDIS_URL    = $REDIS_URL
                $env:JWT_SECRET   = $JWT_SECRET
                $env:SERVER_PORT  = $port

                # Gateway service targets (always localhost so VaultString works with http://localhost:9000)
                if ($name -eq 'gateway') {
                  $authUrl       = "http://127.0.0.1:$authPort"
                  $paymentUrl    = "http://127.0.0.1:$paymentPort"
                  $walletUrl     = "http://127.0.0.1:$walletPort"
                  $forexUrl      = "http://127.0.0.1:$forexPort"
                  $settlementUrl = "http://127.0.0.1:$settlementPort"

                  $env:AUTH_SERVICE_URL       = $authUrl
                  $env:PAYMENT_SERVICE_URL    = $paymentUrl
                  $env:WALLET_SERVICE_URL     = $walletUrl
                  $env:FOREX_SERVICE_URL      = $forexUrl
                  $env:SETTLEMENT_SERVICE_URL = $settlementUrl
                }

                # Resolve command with fallback when build exe is missing
                $cmdToRun = $svc.cmd
                $isExe = Test-Path $cmdToRun
                $exe = $null
                $args = @()
                if ($isExe) {
                    $exe = $cmdToRun
                    $args = @()
                } else {
                    # Use 'go run ./cmd/<service>' as fallback
                    $exe = 'go'
                    $args = @('run', "./cmd/$name")
                }

                try {
                    $argsString = ($args -join ' ')
                    if ([string]::IsNullOrWhiteSpace($argsString)) {
                        & $exe *> $logFile
                    } else {
                        & $exe @args *> $logFile
                    }
                } catch {
                    $err = $_.Exception.Message
                    Add-Content -Path $logFile -Value ("===== ERROR: Failed to start {0}: {1} =====" -f $name, $err)
                }

                $exitTs = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'
                Add-Content -Path $logFile -Value "===== [$exitTs] EXIT (restart in 2s) ====="
            } catch {
                $errMsg = $_.Exception.Message
                Add-Content -Path $logFile -Value "===== ERROR: $errMsg ====="
            }
            Start-Sleep -Seconds 2
        }
    } -ArgumentList @($repoRoot, $svc, $port, $logFile, $env:DATABASE_URL, $env:REDIS_URL, $env:JWT_SECRET, $name, $env:AUTH_PORT, $env:PAYMENT_PORT, $env:WALLET_PORT, $env:FOREX_PORT, $env:SETTLEMENT_PORT) | Out-Null
    
    Write-Output "Started monitor for $name -> $logFile"
}

# Start all services
foreach ($svc in $services) {
    Start-MonitoredService -svc $svc
}

Write-Output ""
Write-Output "All services started. Monitoring..."
Write-Output "View logs: Get-Content logs/<service>.log -Wait"
Write-Output "Stop all: Get-Job -Name 'svc-*' | Stop-Job; Get-Job -Name 'svc-*' | Remove-Job"
Write-Output ""

# Keep script running
while ($true) { Start-Sleep -Seconds 60 }
