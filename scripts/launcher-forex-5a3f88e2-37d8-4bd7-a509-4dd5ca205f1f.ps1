Write-Host 'LAUNCHER ENV:'
Write-Host "  DATABASE_URL=postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
Write-Host "  REDIS_URL=localhost:6379"
Write-Host "  JWT_SECRET=sk_4f9b8c1e7a23d94f0b6de1a28cd94f71d9f3b0c28c6fa47e9b12df67c8e41a25"
Write-Host "  SERVER_PORT=8080"
Write-Host "  CMD=.\\build\\forex.exe"
$env:DATABASE_URL = 'postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable'
$env:REDIS_URL = 'localhost:6379'
$env:JWT_SECRET = 'sk_4f9b8c1e7a23d94f0b6de1a28cd94f71d9f3b0c28c6fa47e9b12df67c8e41a25'
$env:SERVER_PORT = '3002'
Set-Location 'C:\Users\gondwe\Desktop\VaultString\Projects\kyd-payment-system'
& '.\\build\\forex.exe'
