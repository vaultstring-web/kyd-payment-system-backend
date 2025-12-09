# Start forex service with .env loaded
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$repoRoot = Split-Path -Parent $scriptDir
Set-Location $repoRoot

# Load .env file
if (Test-Path ".env") {
    Get-Content ".env" | ForEach-Object {
        $line = $_.Trim()
        if (-not $line -or $line.StartsWith('#')) { return }
        if ($line.Contains('=')) {
            $idx = $line.IndexOf('=')
            $key = $line.Substring(0, $idx).Trim()
            $val = $line.Substring($idx + 1).Trim()
            if ($val.StartsWith('"') -and $val.EndsWith('"')) { $val = $val.Substring(1, $val.Length - 2) }
            if ($key) { [System.Environment]::SetEnvironmentVariable($key, $val, "Process") }
        }
    }
}

$env:SERVER_PORT = "3002"
Write-Host "Starting forex service on port 3002..."
& ".\build\forex.exe"
