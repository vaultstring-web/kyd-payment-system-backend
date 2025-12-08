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

# Define services to start (use compiled executables in build/ for reliability)
$services = @(
    @{ name='auth'; cmd='.\\build\\auth.exe'; port=$env:AUTH_PORT },
    @{ name='wallet'; cmd='.\\build\\wallet.exe'; port=$env:WALLET_PORT },
    @{ name='forex'; cmd='.\\build\\forex.exe'; port=$env:FOREX_PORT },
    @{ name='payment'; cmd='.\\build\\payment.exe'; port=$env:PAYMENT_PORT },
    @{ name='settlement'; cmd='.\\build\\settlement.exe'; port=$env:SETTLEMENT_PORT },
    @{ name='gateway'; cmd='.\\build\\gateway.exe'; port=$env:GATEWAY_PORT }
)

function Start-MonitoredService {
    param([hashtable]$svc)
    $name = $svc.name
    $cmd = $svc.cmd
    $port = $svc.port
    $logFile = Join-Path $repoRoot "logs\$($name).log"

    Start-Job -Name "svc-$name" -ScriptBlock {
        param($repoRoot, $svc, $port, $logFile, $DATABASE_URL, $REDIS_URL, $JWT_SECRET, $name)
        
        while ($true) {
            try {
                $ts = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'
                Add-Content -Path $logFile -Value "`n===== [$ts] START ====="
                
                # Create a small launcher script so environment variables are set in a child
                # PowerShell process. Use a unique filename per run and remove it after use.
                $safeName = ($name -replace '[^A-Za-z0-9_-]', '_')
                $uniq = [guid]::NewGuid().ToString()
                $launcherPath = Join-Path $repoRoot "scripts\launcher-$safeName-$uniq.ps1"
                $launcherContent = @"
Write-Host 'LAUNCHER ENV:'
Write-Host "  DATABASE_URL=$env:DATABASE_URL"
Write-Host "  REDIS_URL=$env:REDIS_URL"
Write-Host "  JWT_SECRET=$env:JWT_SECRET"
Write-Host "  SERVER_PORT=$env:SERVER_PORT"
Write-Host "  CMD=$($svc.cmd)"
`$env:DATABASE_URL = '$DATABASE_URL'
`$env:REDIS_URL = '$REDIS_URL'
`$env:JWT_SECRET = '$JWT_SECRET'
`$env:SERVER_PORT = '$port'
Set-Location '$repoRoot'
& '$($svc.cmd)'
"@
                $launcherContent | Out-File -FilePath $launcherPath -Encoding UTF8

                # Start the launcher hidden and redirect output to the log file
                try {
                    $powershellExe = "powershell.exe"
                    $proc = Start-Process -FilePath $powershellExe -ArgumentList @("-NoProfile", "-NoLogo", "-File", $launcherPath) `
                        -RedirectStandardOutput $logFile -RedirectStandardError $logFile -PassThru -WindowStyle Hidden

                    if ($proc -and $proc.Id) {
                        Wait-Process -Id $proc.Id
                    } else {
                        $msg = "Start-Process returned no process for launcher $launcherPath (powershellExe=$powershellExe, args='-NoProfile -NoLogo -File $launcherPath')"
                        Add-Content -Path $logFile -Value "===== ERROR: $msg ====="
                    }
                } catch {
                    $err = $_.Exception.Message
                    Add-Content -Path $logFile -Value "===== ERROR: Failed to start launcher: $err ====="
                } finally {
                    # Clean up launcher script
                    try { Remove-Item -Path $launcherPath -Force -ErrorAction SilentlyContinue } catch {}
                }

                $exitTs = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'
                Add-Content -Path $logFile -Value "===== [$exitTs] EXIT (restart in 2s) ====="
            } catch {
                $errMsg = $_.Exception.Message
                Add-Content -Path $logFile -Value "===== ERROR: $errMsg ====="
            }
            Start-Sleep -Seconds 2
        }
    } -ArgumentList @($repoRoot, $svc, $port, $logFile, $env:DATABASE_URL, $env:REDIS_URL, $env:JWT_SECRET, $name) | Out-Null
    
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
