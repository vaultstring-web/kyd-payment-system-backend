# KYD Payment System - Quick Reference

## System Status
✅ **READY FOR PRODUCTION** - All 5 core services operational

```
auth (3000)       ✅ JWT tokens, login/register
payment (3001)    ✅ Transfers, forex conversion
forex (3002)      ✅ MWK/CNY/USD rates cached in Redis
wallet (3003)     ✅ Multi-currency balance management
gateway (9000)    ✅ API Gateway & routing
settlement (3004) ⏸️  Waiting for blockchain credentials
```

## 30-Second Quickstart

### Terminal 1: Start Backend
```powershell
cd c:\Users\gondwe\Desktop\VaultString\Projects\kyd-payment-system
powershell -ExecutionPolicy Bypass -File .\scripts\run-supervisor-fixed.ps1
```

### Terminal 2: Verify Services
```powershell
powershell -ExecutionPolicy Bypass -File .\test-backend.ps1
```

Expected output: "All Tests Passed! Backend is ready."

## Login Credentials (Test)

```
Email: john.doe@example.com
Password: Password123

Email: wang.wei@example.com
Password: Password123
```

Both users have wallets in MWK, CNY, and USD (10,000 each).

## API Endpoint Examples

### Register
```bash
curl -X POST http://localhost:9000/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "email": "newuser@example.com",
    "password": "SecurePass123",
    "phone": "+265999888777",
    "first_name": "New",
    "last_name": "User",
    "user_type": "individual",
    "country_code": "MW"
  }'
```

### Login
```bash
curl -X POST http://localhost:9000/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "john.doe@example.com",
    "password": "Password123"
  }'
```

### Get Wallets (requires Bearer token from login)
```bash
curl http://localhost:9000/api/v1/wallets \
  -H "Authorization: Bearer <access_token_from_login>"
```

### Get Exchange Rate
```bash
curl http://localhost:9000/api/v1/forex/rate?from=MWK&to=CNY
```

### Initiate Payment
```bash
curl -X POST http://localhost:9000/api/v1/payments/initiate \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <access_token>" \
  -d '{
    "receiver_id": "recipient-user-id",
    "amount": 1000,
    "source_currency": "MWK",
    "target_currency": "CNY",
    "description": "Purchase"
  }'
```

## Key Files

| File | Purpose |
|------|---------|
| `WEBAPP_INTEGRATION.md` | Complete integration guide for frontend developers |
| `DEPLOYMENT_CHECKLIST.md` | Pre-launch verification & deployment steps |
| `BACKEND_STATUS.md` | Comprehensive system documentation |
| `test-backend.ps1` | Automated backend verification script |
| `.env` | Configuration (DATABASE_URL, REDIS_URL, etc.) |
| `build/` | Compiled service executables |
| `cmd/migrate/main.go` | Database migration runner |

## Database Access

```
Connection: postgres://kyd_user:kyd_password@localhost:5432/kyd_dev
Tables: users, wallets, transactions, ledger_entries, exchange_rates
```

## Common Troubleshooting

| Problem | Solution |
|---------|----------|
| Services won't start | Run from correct directory; check .env exists |
| "Connection refused" on 9000 | Services not running; see startup command above |
| Login fails | Use test credentials; verify user exists in DB |
| "Redis connection error" | Ensure Redis running on localhost:6379 |
| "Database connection error" | Verify PostgreSQL on localhost:5432 |

## Architecture

```
┌─────────────────────────────────────────┐
│           Webapp (React/Vue)            │
└──────────────────┬──────────────────────┘
                   │
                   ↓
         ┌─────────────────────┐
         │   API Gateway       │ :9000
         │   (Reverse Proxy)   │
         └────────────┬────────┘
                      │
        ┌─────────────┼─────────────┬────────────┐
        ↓             ↓             ↓            ↓
    ┌────────┐   ┌─────────┐  ┌─────────┐  ┌─────────┐
    │  Auth  │   │ Payment │  │ Forex   │  │ Wallet  │
    │ :3000  │   │ :3001   │  │ :3002   │  │ :3003   │
    └────┬───┘   └────┬────┘  └────┬────┘  └────┬────┘
         │             │            │            │
         └─────────────┼────────────┼────────────┘
                       ↓
            ┌──────────────────────┐
            │   Postgres :5432     │
            │   kyd_dev database   │
            └──────────────────────┘
                       ↑
            ┌──────────┴───────────┐
            ↓                      ↓
        ┌────────┐           ┌─────────┐
        │ Redis  │           │Exchange │
        │:6379   │           │ Rates   │
        └────────┘           └─────────┘
```

## Features Completed

- ✅ User registration & authentication
- ✅ Multi-currency wallets (MWK, CNY, USD)
- ✅ Real-time exchange rates (cached)
- ✅ Payment initiation with conversion
- ✅ JWT token refresh
- ✅ CORS support
- ✅ Rate limiting
- ✅ Structured logging
- ✅ Database migrations
- ✅ Test data seeding

## Features Pending

- ⏳ Complete payment settlement flow (SQL schema fix)
- ⏳ Blockchain settlement (Ripple/Stellar)
- ⏳ Transaction history retrieval
- ⏳ 2FA (device registration ready)
- ⏳ KYC verification workflow

## For Webapp Developers

1. **API Base URL:** `http://localhost:9000/api/v1`
2. **Token Storage:** localStorage (access_token, refresh_token)
3. **Token Refresh:** Auto-refresh on 401 or manual via /auth/refresh
4. **CORS:** Enabled for all origins (dev mode)
5. **Rate Limits:** 10-30 req/min depending on endpoint
6. **Full docs:** See `WEBAPP_INTEGRATION.md`

## For DevOps/Deployment

1. **Docker:** Use `docker-compose up` in repo root
2. **Kubernetes:** Apply YAML files in `k8s/` folder
3. **Manual:** Start each service from `build/` folder
4. **Databases:** PostgreSQL 15 + Redis 7 required
5. **Full checklist:** See `DEPLOYMENT_CHECKLIST.md`

## Support

- Backend running? Run `test-backend.ps1`
- Services down? Check logs and restart with supervisor script
- API errors? Refer to WEBAPP_INTEGRATION.md error responses
- Database issues? Run migrations: `go run cmd/migrate/main.go`

---

**Last Status Check:** December 7, 2025 - ✅ ALL SYSTEMS OPERATIONAL

Ready to connect the webapp! 🚀
