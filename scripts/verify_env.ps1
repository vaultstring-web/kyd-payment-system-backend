# Check if Docker is running
$dockerStatus = docker info 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host "❌ Docker is NOT running. Please start Docker Desktop." -ForegroundColor Red
    exit 1
} else {
    Write-Host "✅ Docker is running." -ForegroundColor Green
}

# Check if containers are running
$containers = docker-compose ps --services --filter "status=running"
if ($containers -notcontains "db") {
    Write-Host "⚠️  Database container is not running. Attempting to start..." -ForegroundColor Yellow
    docker-compose up -d
}

# Wait for DB to be ready (simple sleep for now, could be pg_isready)
Write-Host "⏳ Waiting for services to stabilize..."
Start-Sleep -Seconds 5

# Run Seed
Write-Host "🌱 Running Seed Script..."
go run cmd/seed/main.go
if ($LASTEXITCODE -eq 0) {
    Write-Host "✅ Database seeded successfully." -ForegroundColor Green
} else {
    Write-Host "❌ Seeding failed." -ForegroundColor Red
    exit 1
}

# Run Tests
Write-Host "🧪 Running Tests..."
go test ./internal/repository/postgres/...
