# KYD Payment System – Setup Guide

This guide helps teammates clone, configure, and run the backend.
**Recommended:** Use Docker for a consistent, "bank-grade" environment.

## Prerequisites
- **Docker Desktop** (includes Docker Compose)
- (Optional) **Go 1.21+** (only if you want to run scripts locally without Docker)
- (Optional) **Node.js 20+** (for frontend development)

## Quick Start (Docker)

1. **Clone the repository**
   ```bash
   git clone <repo-url>
   cd kyd-payment-system
   ```

2. **Configure Environment**
   Copy `env.example` to `.env` (optional, as `docker-compose.yml` has defaults):
   ```bash
   cp env.example .env
   ```
   *Note: For local dev, `DB_SSL_MODE=disable` is used. For production, set to `verify-full`.*

3. **Run Everything**
   ```bash
   docker-compose up --build
   ```
   This starts:
   - PostgreSQL (Database)
   - Redis (Cache)
   - API Gateway (:9000)
   - Microservices (Auth, Payment, Wallet, Forex, Settlement)
   - *Frontends are not started* by default. Use `--profile frontend` and ensure sibling dirs `admin` and `vaultstring-frontend` exist.

4. **Run Migrations and Seed** (first-time or after schema changes)
   Wait for postgres to be ready, then:
   ```powershell
   docker compose --profile tools run --rm migrate-runner
   docker compose --profile tools run --rm seed-runner
   ```

5. **Verify Deployment**
   ```powershell
   ./scripts/verify-fixes.ps1
   ```
   This script:
   - Registers/Logins users
   - Checks wallet balances
   - Performs a test transaction
   - Verifies security settings

## Manual Setup (Not Recommended)

If you must run services locally without Docker:

1.  **Start Dependencies**:
    ```bash
    docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=kyd_password postgres:15
    docker run -d -p 6379:6379 redis:7
    ```

2.  **Run Migrations**:
    ```bash
    go run cmd/migrate/main.go up
    ```

3.  **Seed Data**:
    ```bash
    docker compose --profile tools run --rm seed-runner
    ```

4.  **Run Services** (in separate terminals):
    ```bash
    go run cmd/auth/main.go
    go run cmd/payment/main.go
    # ... etc
    ```

## Security Notes

- **PII Encryption**: Data in DB is encrypted. If you inspect the DB directly, you'll see ciphertext for emails/phones.
- **SSL/TLS**: Enforced in production. Local dev uses `disable` mode for convenience.
- **Passwords**: Must include Upper, Lower, Number, Special Char.

## Production Readiness & Environment

The system is configured for production-grade security and reliability.

### Core Security Keys
The following keys are **MANDATORY** and must be kept consistent across all services and the seed runner:
- `JWT_SECRET`: Signing key for authentication tokens.
- `ENCRYPTION_KEY`: 32-byte (64-char hex) key for AES-GCM data encryption.
- `HMAC_KEY`: 32-byte (64-char hex) key for blind indexing (PII searches).

### Google OAuth Integration
- `GOOGLE_MOCK_MODE`: Set to `false` for production to enable real Google authentication.
- `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET`: Required when mock mode is disabled.

### Database Schema & Migrations
Migrations are **not** run automatically. Run them explicitly before seeding:
```powershell
# With Docker (recommended): ensure postgres is running first, then:
docker compose up -d postgres redis
docker compose --profile tools run --rm migrate-runner

# Or locally (with Go installed and postgres reachable):
$env:DATABASE_URL = "postgres://kyd_user:kyd_password@localhost:5432/kyd_dev?sslmode=disable"
go run ./cmd/migrate/main.go up
```

### Seeding Data
To populate the database with production-ready test accounts (including admin):
```bash
docker compose --profile tools run --rm seed-runner
```
Default Admin: `admin@kyd.com` / `password123`

### Frontend Services
Frontends (Admin Portal, Customer App) are optional and use the `frontend` compose profile:
```bash
docker-compose --profile frontend up --build
```
**Expectations**: The compose file expects sibling directories:
- `../admin` – Admin portal (Next.js, port 3016)
- `../vaultstring-frontend` – Customer app (Next.js, port 3012)

If these directories are missing, the frontend build will fail. For backend-only development, omit the `--profile frontend` flag.

## Risk Engine Configuration

The Payment Service includes a configurable Risk Engine. You can tune these in `.env` or `docker-compose.yml`:

| Variable | Default | Description |
|----------|---------|-------------|
| `RISK_ENABLE_CIRCUIT_BREAKER` | `true` | Global kill-switch for payments |
| `RISK_MAX_DAILY_LIMIT` | `100000000` | Max daily volume per user (Atomic Units) |
| `RISK_HIGH_VALUE_THRESHOLD` | `100000` | Threshold for velocity checks (Atomic Units) |
| `RISK_ADMIN_APPROVAL_THRESHOLD` | `500000` | Transactions above this amount require admin approval |
| `RISK_RESTRICTED_COUNTRIES` | `KP,IR,SY,CU` | Comma-separated list of blocked country codes (ISO 2-letter) |
| `RISK_ENABLE_DISPUTE_RESOLUTION` | `true` | Enables dispute/reversal flows |
