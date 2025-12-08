# Backend Setup & Quick Start

## ✅ Status
All services are running and healthy on your local machine.

## Services Running
- **Auth Service** → http://localhost:3000/health ✓
- **Payment Service** → http://localhost:3001/health ✓
- **Forex Service** → http://localhost:3002/health ✓
- **Wallet Service** → http://localhost:3003/health ✓
- **Settlement Service** → http://localhost:3004/health ✓
- **API Gateway** → http://localhost:9000/health ✓

## Start the Backend

### Option 1: Use the Supervisor Script (Recommended)
Open PowerShell and run:
```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\run-supervisor-fixed.ps1
```

This will:
- Load environment variables from `.env`
- Start all services in monitored background jobs
- Auto-restart services if they crash
- Write logs to `logs/<service>.log`

### Option 2: Start Services Individually
```powershell
# Set environment variables
$env:SERVER_PORT = '3000'; $env:DATABASE_URL = 'postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable'; $env:REDIS_URL = 'localhost:6379'; $env:JWT_SECRET = 'sk_4f9b8c1e7a23d94f0b6de1a28cd94f71d9f3b0c28c6fa47e9b12df67c8e41a25'

# Run a service
.\build\auth.exe
```

## Test the API

### Health Check
```powershell
# Test all services at once
$ports = @{3000='auth';3001='payment';3002='forex';3003='wallet';3004='settlement';9000='gateway'}
foreach ($p in $ports.Keys) {
  try {
    $r = Invoke-RestMethod -Uri "http://localhost:$p/health" -TimeoutSec 3 -ErrorAction Stop
    Write-Host "[OK] $($ports[$p]) on port $p -> $r" -ForegroundColor Green
  } catch {
    Write-Host "[DOWN] $($ports[$p]) on port $p" -ForegroundColor Red
  }
}
```

### Example API Calls (via Gateway on Port 9000)
```powershell
# Login
$loginBody = @{
    email = "admin@example.com"
    password = "password123"
} | ConvertTo-Json
Invoke-RestMethod -Uri "http://localhost:9000/api/v1/auth/login" -Method POST -Body $loginBody -ContentType "application/json"

# Get wallets (requires JWT token)
$headers = @{ Authorization = "Bearer <TOKEN_FROM_LOGIN>" }
Invoke-RestMethod -Uri "http://localhost:9000/api/v1/wallets" -Headers $headers

# Initiate payment
$paymentBody = @{
    wallet_id = "wallet-uuid"
    amount = 100.0
    currency = "USD"
} | ConvertTo-Json
Invoke-RestMethod -Uri "http://localhost:9000/api/v1/payments/initiate" -Method POST -Body $paymentBody -ContentType "application/json" -Headers $headers
```

## View Logs

```powershell
# Monitor auth service in real-time
Get-Content logs\auth.log -Wait

# View recent logs (last 50 lines)
Get-Content logs\auth.log -Tail 50

# All available logs
ls logs\
```

## Stop All Services

```powershell
# Stop all monitored services
Get-Job -Name 'svc-*' | Stop-Job
Get-Job -Name 'svc-*' | Remove-Job
```

## Environment Configuration
Edit `.env` in the project root to modify:
- `DATABASE_URL` — Postgres connection string
- `REDIS_URL` — Redis cache location
- `JWT_SECRET` — Authentication secret
- `AUTH_PORT`, `PAYMENT_PORT`, etc. — Service ports

## Database
- **Host:** localhost:5432
- **Database:** kyd_dev
- **User:** kyd_user
- **Password:** kyd_password
- **SSL Mode:** disabled (sslmode=disable)

Migrations are auto-applied on service startup.

## Redis
- **Host:** localhost:6379
- **Password:** (empty)

## Troubleshooting

### Services not starting
- Check `.env` exists and contains `DATABASE_URL`, `REDIS_URL`, `JWT_SECRET`
- Verify Postgres and Redis are running: `Test-NetConnection -ComputerName localhost -Port 5432` and `Test-NetConnection -ComputerName localhost -Port 6379`
- Check logs: `Get-Content logs\<service>.log -Tail 100`

### Port conflicts
- Check if another process is using the port: `netstat -ano | findstr :3000`
- Kill the process: `Stop-Process -Id <PID> -Force`

### Health endpoints return DOWN
- Wait a few seconds (services may still be initializing)
- Check logs for errors
- Ensure firewall allows localhost:3000–9000

## API Documentation
See `docs/KYD_API.postman_collection.json` for the full API spec.

---

**Backend is ready for local development and testing!** 🚀
