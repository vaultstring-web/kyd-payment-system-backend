# KYD Payment System - System Verification Script
# Verifies gateway health, wallet lifecycle, and admin monitoring endpoints.
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

# 2. Login (customer + admin)
Write-Host "`n2. Logging in as demo users..." -ForegroundColor Yellow
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
    $customerLoginBody = @{
        email = "customer@kyd.com"
        password = "password123"
    } | ConvertTo-Json
    $customerResp = Invoke-RestMethod -Uri "$baseUrl/auth/login" -Method Post -Body $customerLoginBody -ContentType "application/json"
    $customerToken = $customerResp.access_token

    $adminLoginBody = @{
        email = "admin@kyd.com"
        password = "password123"
    } | ConvertTo-Json
    $adminResp = Invoke-RestMethod -Uri "$baseUrl/auth/login" -Method Post -Body $adminLoginBody -ContentType "application/json"
    $adminToken = $adminResp.access_token

    if (-not $customerToken -or -not $adminToken) {
        Write-Host "   FAILED: Missing customer/admin tokens." -ForegroundColor Red
        exit 1
    }

    Write-Host "   Login OK (john/customer/admin)" -ForegroundColor Green
} catch {
    Write-Host "   FAILED: $($_.Exception.Message). Run: docker compose --profile tools run --rm seed-runner" -ForegroundColor Red
    exit 1
}

# 3. Get wallets + lookup number
Write-Host "`n3. Fetching wallets and validating digital lookup..." -ForegroundColor Yellow
$headers = @{
    Authorization = "Bearer $token"
}
$customerHeaders = @{
    Authorization = "Bearer $customerToken"
}
$adminHeaders = @{
    Authorization = "Bearer $adminToken"
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
    if ($count -eq 0) {
        Write-Host "   FAILED: No wallets for john." -ForegroundColor Red
        exit 1
    }

    $johnWallet = $wallets.wallets[0]
    $johnWalletId = if ($johnWallet.wallet_id) { $johnWallet.wallet_id } else { $johnWallet.id }
    $johnWalletNumber = $johnWallet.wallet_address
    $lookup = Invoke-RestMethod -Uri "$baseUrl/wallets/lookup?address=$johnWalletNumber" -Method Get -Headers $headers
    Write-Host "   Lookup OK: $($lookup.name) -> $($lookup.address)" -ForegroundColor Green
} catch {
    Write-Host "   FAILED: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}

# 4. Top-up john wallet
Write-Host "`n4. Top-up wallet (+2500)..." -ForegroundColor Yellow
try {
    $before = [decimal]$johnWallet.available_balance
    $depositBody = @{
        amount = 2500
        source_id = "verify-script"
        currency = $johnWallet.currency
    } | ConvertTo-Json
    $idempotency = [guid]::NewGuid().ToString()
    $topupHeaders = @{
        Authorization = "Bearer $token"
        "Idempotency-Key" = $idempotency
    }
    $null = Invoke-RestMethod -Uri "http://localhost:3001/api/v1/wallets/$johnWalletId/deposit" -Method Post -Body $depositBody -ContentType "application/json" -Headers $topupHeaders
    $walletsAfterTopup = Invoke-RestMethod -Uri "http://localhost:3001/api/v1/wallets" -Method Get -Headers $headers
    $johnAfterTopup = [decimal]($walletsAfterTopup.wallets | Where-Object { $_.wallet_id -eq $johnWalletId } | Select-Object -First 1).available_balance
    Write-Host "   Top-up OK: $before -> $johnAfterTopup" -ForegroundColor Green
} catch {
    Write-Host "   FAILED top-up: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}

# 5. Send money john -> customer
Write-Host "`n5. Sending money (john -> customer)..." -ForegroundColor Yellow
try {
    $customerWallets = Invoke-RestMethod -Uri "http://localhost:3001/api/v1/wallets" -Method Get -Headers $customerHeaders
    $receiver = $customerWallets.wallets[0]
    $receiverWalletNumber = $receiver.wallet_address
    $sendBody = @{
        receiver_wallet_number = $receiverWalletNumber
        amount = 1000
        currency = $johnWallet.currency
        destination_currency = $receiver.currency
        description = "verify script transfer"
        channel = "web"
        category = "transfer"
        device_id = "system-scheduler"
        location = "MW"
    } | ConvertTo-Json
    $sendHeaders = @{
        Authorization = "Bearer $token"
        "Idempotency-Key" = [guid]::NewGuid().ToString()
    }
    $sendResp = Invoke-RestMethod -Uri "http://localhost:3001/api/v1/payments/initiate" -Method Post -Body $sendBody -ContentType "application/json" -Headers $sendHeaders
    Write-Host "   Send OK: tx=$($sendResp.transaction.id)" -ForegroundColor Green
} catch {
    Write-Host "   FAILED send-money: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}

# 6. Admin monitoring endpoints
Write-Host "`n6. Verifying admin monitoring APIs..." -ForegroundColor Yellow
try {
    $usersResp = Invoke-RestMethod -Uri "$baseUrl/admin/users?limit=5&offset=0" -Method Get -Headers $adminHeaders
    $txResp = Invoke-RestMethod -Uri "$baseUrl/admin/transactions?limit=5&offset=0" -Method Get -Headers $adminHeaders
    $riskResp = Invoke-RestMethod -Uri "$baseUrl/admin/risk/alerts?limit=5&offset=0" -Method Get -Headers $adminHeaders
    $walletsResp = Invoke-RestMethod -Uri "$baseUrl/admin/wallets?limit=5&offset=0" -Method Get -Headers $adminHeaders

    $usersCount = if ($usersResp.users) { $usersResp.users.Count } else { 0 }
    $txCount = if ($txResp.transactions) { $txResp.transactions.Count } else { 0 }
    $riskCount = if ($riskResp.alerts) { $riskResp.alerts.Count } else { 0 }
    $walletCount = if ($walletsResp.addresses) { $walletsResp.addresses.Count } elseif ($walletsResp.wallets) { $walletsResp.wallets.Count } else { 0 }

    Write-Host "   Admin users: $usersCount | transactions: $txCount | risk alerts: $riskCount | wallets: $walletCount" -ForegroundColor Green
} catch {
    Write-Host "   FAILED admin APIs: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}

Write-Host "`n=== Verification Complete ===" -ForegroundColor Cyan
