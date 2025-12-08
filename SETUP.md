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
Migrations live in `migrations/`. You can run them using the migrate tool:
```bash
migrate -path migrations -database "$DATABASE_URL" up
```
(Install from https://github.com/golang-migrate/migrate if you don’t have it.)

## Running locally (Docker Compose)
```bash
docker compose up -d postgres redis
```
Then start services (choose one):
- **PowerShell scripts**: `./scripts/run-services.ps1` or `./scripts/run-supervisor-fixed.ps1`
- **Manual** (per service): `go run ./cmd/auth`, `go run ./cmd/payment`, etc. (set `SERVER_PORT` per service)

Health checks:
- `GET http://localhost:3000/health` (auth)
- `GET http://localhost:3001/health` (payment)
- `GET http://localhost:3002/health` (forex)
- `GET http://localhost:3003/health` (wallet)
- API Gateway (if used): `http://localhost:9000`

## Tests
```bash
go test ./...
```

## API docs
- Postman collection: `docs/KYD_API.postman_collection.json`
- Quick endpoints summary: `WEBAPP_INTEGRATION.md`

## Notes
- Binaries and logs are git-ignored (`.gitignore`). If you see `*.exe` in `build/`, they’re generated artifacts; delete locally as needed.
- Dependencies are fetched from upstream (no `vendor/`).

