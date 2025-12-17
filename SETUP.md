# KYD Payment System – Setup Guide

This guide helps teammates clone, configure, and run the backend on their machines or in Docker.

## Prerequisites
- Go `>= 1.24.11`
- Docker + Docker Compose (for local services)
- PostgreSQL 15+
- Redis 7+
- (Optional) Node.js 20+ if you build the Next.js web client later

## Clone and dependencies
```bash
git clone <your-repo-url>
cd kyd-payment-system
go mod download
```

## Environment variables
Set these before running any service (example values shown):
```
SERVER_HOST=0.0.0.0
SERVER_PORT=3000           # per service; see README or scripts
DATABASE_URL=postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable
REDIS_URL=localhost:6379
JWT_SECRET=replace-with-strong-secret

# Stellar
STELLAR_NETWORK_URL=https://horizon-testnet.stellar.org
STELLAR_ISSUER_ACCOUNT=GA...
STELLAR_SECRET_KEY=SA...

# Ripple
RIPPLE_SERVER_URL=wss://s.altnet.rippletest.net:51233
RIPPLE_ISSUER_ADDRESS=r...
RIPPLE_SECRET_KEY=s...
```

You can start from `env.example` (copy to `.env` if you use a dotenv loader).

## Database migrations
All migrations are merged into a single initial schema (`001_initial_schema.up.sql`) so fresh setups are simple.

Run using the built-in migration command:
```bash
# Windows PowerShell
$env:DATABASE_URL="postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
go run .\cmd\migrate\main.go up
```

Then seed test data:
```bash
go run .\cmd\seed\main.go
```

## Running locally (Docker Compose)
```bash
docker compose up -d postgres redis
```
Then start services (choose one):
- **PowerShell supervisor** (recommended):
  ```powershell
  powershell -ExecutionPolicy Bypass -File .\scripts\run-supervisor-fixed.ps1
  ```
- **Manual** (per service):
  ```powershell
  $env:SERVER_PORT="3000"; go run .\cmd\auth
  $env:SERVER_PORT="3001"; go run .\cmd\payment
  $env:SERVER_PORT="3002"; go run .\cmd\forex
  $env:SERVER_PORT="3003"; go run .\cmd\wallet
  $env:SERVER_PORT="9000"; go run .\cmd\gateway
  ```

Health checks:
- `GET http://localhost:3000/health` (auth)
- `GET http://localhost:3001/health` (payment)
- `GET http://localhost:3002/health` (forex)
- `GET http://localhost:3003/health` (wallet)
- API Gateway (if used): `http://localhost:9000`

## Verify backend
Quick health and basic flow test:
```powershell
powershell -ExecutionPolicy Bypass -File .\test-backend.ps1
```

## API docs
- Postman collection: `docs/KYD_API.postman_collection.json`

## Notes
- Binaries and logs are git-ignored (`.gitignore`). If you see `*.exe` in `build/`, they’re generated artifacts; delete locally as needed.
- Dependencies are fetched from upstream (no `vendor/`).

