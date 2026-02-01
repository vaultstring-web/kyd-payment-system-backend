# scripts/test-payment-flow.ps1

$ErrorActionPreference = "Stop"

$gatewayPort = "9000"
$gatewayUrl = "http://localhost:$gatewayPort"

Write-Host "Running Payment Flow Test against Gateway at $gatewayUrl"

# 1. Login John (Sender)
Write-Host "Logging in John..."
try {
    $loginJohn = Invoke-RestMethod -Method Post -Uri "$gatewayUrl/api/v1/auth/login" -ContentType 'application/json' -Body (@{email='john.doe@example.com'; password='Password123'} | ConvertTo-Json)
    $johnToken = $loginJohn.access_token
    $johnID = $loginJohn.user.id
    Write-Host "John Logged In. ID: $johnID"
} catch {
    Write-Error "Failed to login John. Is the seed data loaded?"
    exit 1
}

# 2. Login Wang (Receiver)
Write-Host "Logging in Wang..."
try {
    $loginWang = Invoke-RestMethod -Method Post -Uri "$gatewayUrl/api/v1/auth/login" -ContentType 'application/json' -Body (@{email='wang.wei@example.com'; password='Password123'} | ConvertTo-Json)
    $wangToken = $loginWang.access_token
    $wangID = $loginWang.user.id
    Write-Host "Wang Logged In. ID: $wangID"
} catch {
    Write-Error "Failed to login Wang."
    exit 1
}

# 3. Initiate Payment MWK -> CNY
Write-Host "Initiating Payment 50000 MWK -> CNY..."
$paymentPayload = @{
    amount = 50000
    currency = "MWK"
    destination_currency = "CNY"
    receiver_id = $wangID
    description = "Test Cross-Border Payment"
    channel = "web"
    category = "trade"
}

try {
    $paymentRes = Invoke-RestMethod -Method Post -Uri "$gatewayUrl/api/v1/payments/initiate" -Headers @{Authorization="Bearer $johnToken"} -ContentType 'application/json' -Body ($paymentPayload | ConvertTo-Json)
    Write-Host "Payment Initiated Successfully!"
    $paymentRes | ConvertTo-Json -Depth 5 | Write-Host
} catch {
    Write-Error "Payment Initiation Failed: $($_.Exception.Message)"
    if ($_.Exception.Response) {
        $reader = New-Object System.IO.StreamReader $_.Exception.Response.GetResponseStream()
        $errBody = $reader.ReadToEnd()
        Write-Host "Error Body: $errBody"
    }
    exit 1
}

Write-Host "Test Passed!"
