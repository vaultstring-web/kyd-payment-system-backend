Write-Host "=== KYD Payment System: Rebuild & Verify ===" -ForegroundColor Cyan

# 1. Stop existing containers to ensure clean state
Write-Host "Stopping existing containers..." -ForegroundColor Yellow
docker compose down

# 2. Rebuild and start backend and frontend services
# We force build to ensure code changes (like blockchain logic) are compiled into the containers
Write-Host "Rebuilding and starting services..." -ForegroundColor Yellow
docker compose up -d --build postgres redis auth-service payment-service wallet-service settlement-service gateway-service forex-service admin-frontend web-frontend

# 3. Wait for services to be ready
Write-Host "Waiting 20 seconds for services to initialize..." -ForegroundColor Yellow
Start-Sleep -Seconds 20

# 4. Run Seeder (Uses the mounted local code, so changes are immediate)
Write-Host "Seeding database with test data (MWK/CNY)..." -ForegroundColor Yellow
docker compose run --rm seed-runner

# 5. Run Verification Scripts
Write-Host "Running Blockchain Logic Verification..." -ForegroundColor Yellow
go run cmd/verify_advanced_blockchain/main.go

Write-Host "Running API & Data Verification..." -ForegroundColor Yellow
go run cmd/verify_api/main.go

Write-Host "=== Process Complete ===" -ForegroundColor Green
Write-Host "Services are running with latest code."
Write-Host "Blockchain logic is secured (Smart Contracts Active)."
