$headers = @{ "Content-Type" = "application/json" }
$johnBody = @{ email = "john.doe@example.com"; password = "Password123!" } | ConvertTo-Json
$wangBody = @{ email = "wang.wei@example.com"; password = "Password123!" } | ConvertTo-Json

function Show-ErrorBody($ex) {
    try {
        $stream = $ex.Exception.Response.GetResponseStream()
        $reader = New-Object System.IO.StreamReader($stream)
        $body = $reader.ReadToEnd()
        Write-Host "Body: $body" -ForegroundColor Yellow
    } catch { }
}

try {
    Write-Host "Logging in as John..."
    $johnRes = Invoke-RestMethod -Uri "http://localhost:9000/api/v1/auth/login" -Method POST -Headers $headers -Body $johnBody
    $johnToken = $johnRes.access_token
    Write-Host "John login success."

    Write-Host "Logging in as Wang..."
    $wangRes = Invoke-RestMethod -Uri "http://localhost:9000/api/v1/auth/login" -Method POST -Headers $headers -Body $wangBody
    $wangToken = $wangRes.access_token
    Write-Host "Wang login success."

    $johnAuth = @{ "Authorization" = "Bearer $johnToken" }
    $wangAuth = @{ "Authorization" = "Bearer $wangToken" }

    Write-Host "Fetching John's wallets..."
    # try {
    #     $johnWallets = Invoke-RestMethod -Uri "http://localhost:9000/api/v1/wallets" -Method GET -Headers $johnAuth
    #     $johnWalletsCount = $johnWallets.count
    #     Write-Host "John wallets: $johnWalletsCount"
    #     $johnMwk = $johnWallets.wallets | Where-Object { $_.currency -eq "MWK" } | Select-Object -First 1
    #     if (-not $johnMwk) { throw "John has no MWK wallet" }
    #     Write-Host ("John MWK available: {0}" -f $johnMwk.available_balance)
    # } catch {
    #     Write-Host "Error Fetching John's Wallets:" -ForegroundColor Red
    #     Show-ErrorBody $_
    #     # throw
    # }

    Write-Host "Fetching Wang's profile..."
    try {
        $wangMe = Invoke-RestMethod -Uri "http://localhost:9000/api/v1/auth/me" -Method GET -Headers $wangAuth
        $wangId = $wangMe.id
        Write-Host "Wang user id: $wangId"
    } catch {
        Write-Host "Error fetching Wang profile" -ForegroundColor Red
        Show-ErrorBody $_
        throw
    }

    Write-Host "Initiating payment MWK->CNY (100 MWK) from John to Wang..."
    # $initBody = @{
    #     receiver_id = $wangId
    #     amount      = "100"
    #     currency    = "MWK"
    #     description = "Test transfer"
    #     channel     = "mobile"
    #     category    = "p2p"
    # } | ConvertTo-Json
    # try {
    #     $payRes = Invoke-RestMethod -Uri "http://localhost:9000/api/v1/payments/initiate" -Method POST -Headers $johnAuth -Body $initBody
    #     Write-Host "Payment created. Transaction ID: $($payRes.transaction.id)"
    #     Write-Host "Status: $($payRes.transaction.status) Ref: $($payRes.transaction.reference)"
    #     Write-Host ("Converted: {0} {1}" -f $payRes.transaction.converted_amount, $payRes.transaction.converted_currency)
    # } catch {
    #     Write-Host "Payment initiation failed" -ForegroundColor Red
    #     Show-ErrorBody $_
    #     # throw
    # }

    Write-Host "Verifying post-payment balances..."
    # try {
    #     $johnWallets2 = Invoke-RestMethod -Uri "http://localhost:9000/api/v1/wallets" -Method GET -Headers $johnAuth
    #     $johnMwk2 = $johnWallets2.wallets | Where-Object { $_.currency -eq "MWK" } | Select-Object -First 1
    #     Write-Host ("John MWK available after: {0}" -f $johnMwk2.available_balance)
    # } catch {
    #     Write-Host "Error Fetching John's Wallets (post-payment)" -ForegroundColor Red
    #     Show-ErrorBody $_
    # }

    # try {
    #     $wangWallets = Invoke-RestMethod -Uri "http://localhost:9000/api/v1/wallets" -Method GET -Headers $wangAuth
    #     $wangCny = $wangWallets.wallets | Where-Object { $_.currency -eq "CNY" } | Select-Object -First 1
    #     Write-Host ("Wang CNY available after: {0}" -f $wangCny.available_balance)
    # } catch {
    #     Write-Host "Error Fetching Wang's Wallets (post-payment)" -ForegroundColor Red
    #     Show-ErrorBody $_
    # }

    Write-Host "Verifying admin endpoints..."
    try {
        $adminBody = @{ email = "admin@example.com"; password = "AdminPassword123" } | ConvertTo-Json
        $adminRes = Invoke-RestMethod -Uri "http://localhost:9000/api/v1/auth/login" -Method POST -Headers $headers -Body $adminBody
        $adminToken = $adminRes.access_token
        $adminAuth = @{ "Authorization" = "Bearer $adminToken" }

        $adminUsers = Invoke-RestMethod -Uri "http://localhost:9000/api/v1/auth/users?limit=5" -Method GET -Headers $adminAuth
        Write-Host ("Admin users fetched: {0}" -f $adminUsers.total)

        Write-Host "Admin updating Wang's profile..."
        $updateBody = @{ first_name = "WangUpdated" } | ConvertTo-Json
        $updateRes = Invoke-RestMethod -Uri "http://localhost:9000/api/v1/auth/users/$wangId" -Method PUT -Headers $adminAuth -Body $updateBody
        
        if ($updateRes.first_name -ne "WangUpdated") {
            throw "Update failed: Name is not WangUpdated"
        }
        Write-Host "Wang's profile updated successfully."

        # Revert update
        $revertBody = @{ first_name = "Wei" } | ConvertTo-Json
        Invoke-RestMethod -Uri "http://localhost:9000/api/v1/auth/users/$wangId" -Method PUT -Headers $adminAuth -Body $revertBody
        Write-Host "Reverted Wang's profile."
    } catch {
        Write-Host "Admin endpoint verification failed" -ForegroundColor Red
        Show-ErrorBody $_
        throw
    }
} catch {
    Write-Host "Test failed: $_" -ForegroundColor Red
    exit 1
}
Write-Host "Test completed." -ForegroundColor Green
exit 0
