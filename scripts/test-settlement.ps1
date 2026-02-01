# Scripts/test-settlement.ps1
# Tests the settlement service manual trigger

$ErrorActionPreference = "Stop"
$settlementUrl = "http://localhost:3004/api/v1/settlements/process"

Write-Host "Triggering Settlement Process..." -ForegroundColor Yellow

try {
    $response = Invoke-RestMethod -Uri $settlementUrl -Method Post -ErrorAction Stop
    Write-Host "Response:" -ForegroundColor Green
    $response | ConvertTo-Json -Depth 5
    
    if ($response.status -eq "processing") {
        Write-Host "Settlement trigger successful!" -ForegroundColor Green
    } else {
        Write-Error "Settlement trigger returned unexpected status."
    }
} catch {
    Write-Error "Failed to trigger settlement: $_"
    exit 1
}
