# KYD Backend Quick Test Script (PowerShell)
# Recreated helper to validate core service health and a sample registration flow.

param(
    [string]$AuthPort = "3000",
    [string]$PaymentPort = "3001",
    [string]$ForexPort = "3002",
    [string]$WalletPort = "3003",
    [string]$GatewayPort = "9000"
)

function Test-Endpoint($Name, $Url, $Method = "GET", $Body = $null) {
    try {
        if ($Body) {
            $response = Invoke-WebRequest -Uri $Url -Method $Method -Body ($Body | ConvertTo-Json) -ContentType "application/json" -UseBasicParsing -TimeoutSec 10
        } else {
            $response = Invoke-WebRequest -Uri $Url -Method $Method -UseBasicParsing -TimeoutSec 10
        }
        if ($response.StatusCode -ge 200 -and $response.StatusCode -lt 300) {
            Write-Host "  [OK] $Name"
            return $true
        }
        Write-Host "  [FAILED] $Name (HTTP $($response.StatusCode))"
        return $false
    } catch {
        Write-Host "  [FAILED] $Name ($_ )"
        return $false
    }
}

Write-Host ""
Write-Host "========================================"
Write-Host "KYD Payment System - Backend Test"
Write-Host "========================================"
Write-Host ""
Write-Host "Test 1: Service Health Checks"

$allOk = $true
$allOk = (Test-Endpoint "Auth (port $AuthPort)" "http://localhost:$AuthPort/health") -and $allOk
$allOk = (Test-Endpoint "Payment (port $PaymentPort)" "http://localhost:$PaymentPort/health") -and $allOk
$allOk = (Test-Endpoint "Forex (port $ForexPort)" "http://localhost:$ForexPort/health") -and $allOk
$allOk = (Test-Endpoint "Wallet (port $WalletPort)" "http://localhost:$WalletPort/health") -and $allOk
$allOk = (Test-Endpoint "Gateway (port $GatewayPort)" "http://localhost:$GatewayPort/health") -and $allOk

Write-Host ""
Write-Host "Test 2: User Registration (Auth)"

$randomSuffix = Get-Random -Minimum 1000000 -Maximum 9999999
$registrationBody = @{
    email        = "testuser_$(Get-Random)@example.com"
    password     = "P@ssw0rd123!"
    first_name   = "Test"
    last_name    = "User"
    phone        = "+26599$randomSuffix"
    user_type    = "individual"
    country_code = "MW"
}

$registrationOk = Test-Endpoint "Registration" "http://localhost:$AuthPort/api/v1/auth/register" "POST" $registrationBody

Write-Host ""
if ($allOk -and $registrationOk) {
    Write-Host "All tests passed."
    exit 0
} else {
    Write-Host "Some tests failed."
    exit 1
}

