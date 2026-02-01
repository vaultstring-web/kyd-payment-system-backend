$ErrorActionPreference = "Stop"

# Load Env
$repoRoot = Split-Path -Parent $PSScriptRoot
if (Test-Path "$repoRoot\.env") {
    Get-Content "$repoRoot\.env" | ForEach-Object {
        if ($_ -match '^[#;]' -or $_ -notmatch '=') { return }
        $parts = $_ -split '=', 2
        $k = $parts[0].Trim()
        $v = $parts[1].Trim()
        if ($k -ne '') { [System.Environment]::SetEnvironmentVariable($k, $v, 'Process') }
    }
}

$port = $env:GATEWAY_PORT
if (-not $port) { $port = "9000" }
$gatewayHost = "127.0.0.1"

Write-Host ("Targeting Gateway at http://{0}:{1}" -f $gatewayHost, $port) -ForegroundColor Cyan

# Prepare session and obtain CSRF cookie (double-submit pattern)
$baseUri = ("http://{0}:{1}/" -f $gatewayHost, $port)
$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
# Any safe GET should trigger CSRF cookie issuance; try a public endpoint first
try {
    Invoke-WebRequest -Method Get -Uri ($baseUri + "forex/rates") -WebSession $session | Out-Null
} catch {
    try {
        Invoke-WebRequest -Method Get -Uri $baseUri -WebSession $session -ErrorAction SilentlyContinue | Out-Null
    } catch {
        # ignore
    }
}
$csrfCookie = $session.Cookies.GetCookies($baseUri) | Where-Object { $_.Name -eq 'csrf_token' } | Select-Object -First 1
$csrfToken = $null
if ($csrfCookie) { $csrfToken = $csrfCookie.Value }
if ($csrfToken) {
    Write-Host "CSRF token acquired: yes" -ForegroundColor Cyan
} else {
    Write-Host "CSRF token acquired: no" -ForegroundColor Yellow
}

try {
    # 1. Login John
    Write-Host "Logging in John..." -NoNewline
    $loginJohn = Invoke-RestMethod -Method Post -WebSession $session -Uri ("http://{0}:{1}/api/v1/auth/login" -f $gatewayHost, $port) -ContentType 'application/json' -Body (@{email='john.doe@example.com'; password='Password123!'} | ConvertTo-Json)
    Write-Host " OK" -ForegroundColor Green
    $tokenJohn = $loginJohn.access_token
    $johnID = $loginJohn.user.id

    # 2. Login Wang
    Write-Host "Logging in Wang..." -NoNewline
    $loginWang = Invoke-RestMethod -Method Post -WebSession $session -Uri ("http://{0}:{1}/api/v1/auth/login" -f $gatewayHost, $port) -ContentType 'application/json' -Body (@{email='wang.wei@example.com'; password='Password123!'} | ConvertTo-Json)
    Write-Host " OK" -ForegroundColor Green
    $tokenWang = $loginWang.access_token
    $wangID = $loginWang.user.id

    # 2c. Login Admin and validate /auth/me
    Write-Host "Logging in Admin..." -NoNewline
    $loginAdmin = Invoke-RestMethod -Method Post -WebSession $session -Uri ("http://{0}:{1}/api/v1/auth/login" -f $gatewayHost, $port) -ContentType 'application/json' -Body (@{email='admin@example.com'; password='AdminPassword123!'} | ConvertTo-Json)
    Write-Host " OK" -ForegroundColor Green
    $tokenAdmin = $loginAdmin.access_token
    $adminUser = $loginAdmin.user
    Write-Host ("  Admin user_type: {0}" -f ($adminUser.user_type)) -ForegroundColor Cyan
    $meResp = Invoke-RestMethod -Method Get -WebSession $session -Uri ("http://{0}:{1}/api/v1/auth/me" -f $gatewayHost, $port) -Headers @{ Authorization = "Bearer $tokenAdmin" }
    Write-Host "  /auth/me validated" -ForegroundColor Green

    # 2b. Verify Wang wallets (CNY funded, MWK zero)
    Write-Host "Verifying Wang's wallets..." -ForegroundColor Yellow
    $wangWallets = Invoke-RestMethod -Method Get -WebSession $session -Uri ("http://{0}:{1}/api/v1/wallets" -f $gatewayHost, $port) -Headers @{ Authorization = "Bearer $tokenWang" }
    $hasCNY = $false
    $mkwZero = $true
    foreach ($w in $wangWallets.wallets) {
        if ($w.currency -eq 'CNY') {
            $hasCNY = ($w.available_balance -as [decimal]) -gt 0
        }
        if ($w.currency -eq 'MWK') {
            $mkwZero = (($w.available_balance -as [decimal]) -eq 0)
        }
    }
    Write-Host ("  CNY funded: {0}" -f $hasCNY) -ForegroundColor Cyan
    Write-Host ("  MWK zero:   {0}" -f $mkwZero) -ForegroundColor Cyan
    if (-not $hasCNY -or -not $mkwZero) {
        Write-Warning "Wallet verification failed (expected CNY funded and MWK zero)"
    }

    # 3. Initiate Payment (MWK -> CNY)
    Write-Host "Initiating Payment 1000 MWK -> CNY..." -ForegroundColor Yellow
    $paymentBody = @{ 
        sender_id = $johnID; 
        receiver_id = $wangID; 
        amount = 1000; 
        currency = 'MWK'; 
        destination_currency = 'CNY'; # Explicitly testing destination currency selection
        description = 'Verification Test'; 
        channel='mobile'; 
        category='transfer' 
    } | ConvertTo-Json

    $idemKey = "verify-$(Get-Random)-$(Get-Date -Format 'yyyyMMddHHmmss')"
    $resp = Invoke-RestMethod -Method Post -WebSession $session -Uri ("http://{0}:{1}/api/v1/payments/initiate" -f $gatewayHost, $port) -ContentType 'application/json' -Headers @{ Authorization = "Bearer $tokenJohn"; 'Idempotency-Key' = $idemKey; 'X-CSRF-Token' = $csrfToken } -Body $paymentBody
    
    Write-Host "Payment Response:" -ForegroundColor Cyan
    $resp | ConvertTo-Json -Depth 5 | Write-Host

    if ($resp.transaction.status -eq 'completed') {
        Write-Host "PAYMENT SUCCESSFUL" -ForegroundColor Green
    } else {
        Write-Host "PAYMENT STATUS: $($resp.transaction.status)" -ForegroundColor Yellow
    }

    # 4. Initiate Payment using wallet number (address)
    Write-Host "Initiating Payment using receiver wallet number..." -ForegroundColor Yellow
    $cnyWalletAddr = $null
    foreach ($w in $wangWallets.wallets) {
        if ($w.currency -eq 'CNY' -and $w.wallet_address) {
            $cnyWalletAddr = $w.wallet_address
            break
        }
    }
    if ($null -eq $cnyWalletAddr) {
        Write-Warning "No CNY wallet address found for Wang; skipping wallet-number test"
    } else {
        $paymentBody2 = @{
            sender_id = $johnID;
            amount = 500;
            currency = 'MWK';
            destination_currency = 'CNY';
            receiver_wallet_number = $cnyWalletAddr;
            description = 'Wallet number test';
            channel='mobile';
            category='transfer'
        } | ConvertTo-Json
        $idemKey2 = "verify-walletnum-$(Get-Random)-$(Get-Date -Format 'yyyyMMddHHmmss')"
        $resp2 = Invoke-RestMethod -Method Post -WebSession $session -Uri ("http://{0}:{1}/api/v1/payments/initiate" -f $gatewayHost, $port) -ContentType 'application/json' -Headers @{ Authorization = "Bearer $tokenJohn"; 'Idempotency-Key' = $idemKey2; 'X-CSRF-Token' = $csrfToken } -Body $paymentBody2
        Write-Host "Wallet Number Payment Response:" -ForegroundColor Cyan
        $resp2 | ConvertTo-Json -Depth 5 | Write-Host
        if ($resp2.transaction.status -eq 'completed') {
            Write-Host "PAYMENT (WALLET NUMBER) SUCCESSFUL" -ForegroundColor Green
        } else {
            Write-Host "PAYMENT (WALLET NUMBER) STATUS: $($resp2.transaction.status)" -ForegroundColor Yellow
        }
    }
} catch {
    Write-Error "Test Failed: $_"
    if ($_.Exception.Response) {
        $reader = New-Object System.IO.StreamReader $_.Exception.Response.GetResponseStream()
        $errBody = $reader.ReadToEnd()
        Write-Error "Response Body: $errBody"
    }
    exit 1
}
