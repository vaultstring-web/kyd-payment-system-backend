# Login Troubleshooting Guide

If you see **"Login Failed: Authentication service unavailable"**, **"Authorization header required"**, or **"relation customer_schema.users does not exist"**, follow these steps.

## Root causes

| Error | Cause |
|-------|-------|
| `Authentication service unavailable` | Backend services (gateway, auth) not running, or auth service can't reach DB |
| `relation "customer_schema.users" does not exist` | Database migrations have not been run |
| `Authorization header required` (on page load) | Expected when not logged in; can be noisy in console |

## Fix: run migrations and seed

The `customer_schema.users` table and other schema are created by migrations. Run them **before** testing login.

### 1. Start Postgres & Redis (if not already running)

```powershell
cd kyd-payment-system
docker compose up -d postgres redis
```

### 2. Run migrations (creates schema)

```powershell
docker compose --profile tools run --rm migrate-runner
```

Expected output: `✅ Migrations applied successfully`

### 3. Seed test data (creates test users)

```powershell
docker compose --profile tools run --rm seed-runner
```

### 4. Ensure full backend is running

```powershell
docker compose up -d
```

Or with frontends:

```powershell
docker compose --profile frontend up -d
```

## Test credentials (after seed)

| User | Email | Password |
|------|-------|----------|
| Admin | admin@kyd.com | password123 |
| Customer | customer@kyd.com | password123 |
| John Doe | john.doe@example.com | password123 |
| Jane Smith | jane.smith@example.com | password123 |

## Running frontend locally (pnpm dev)

If you run the frontend with `pnpm dev` (outside Docker):

1. Backend must be running (gateway on port 9000)
2. Create `.env.local` in `vaultstring-frontend`:

   ```
   NEXT_PUBLIC_GATEWAY_URL=http://localhost:9000
   ```

3. Restart the dev server after changing env

## Verify setup

```powershell
# Check services
docker compose ps

# Test gateway health
curl http://localhost:9000/health

# Test login via API
curl -X POST http://localhost:9000/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"john.doe@example.com","password":"password123"}'
```

## Google / social login (mock mode)

Mock Google login also needs migrations and the `users` table. The same steps above apply. Ensure `GOOGLE_MOCK_MODE: "true"` in your auth service env (default in docker-compose).
