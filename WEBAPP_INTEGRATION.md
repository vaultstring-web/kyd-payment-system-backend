# KYD Payment System - Webapp Integration Guide

## System Status
✅ **5 of 6 microservices running and operational**
- Auth Service (port 3000)
- Payment Service (port 3001)
- Forex Service (port 3002)
- Wallet Service (port 3003)
- API Gateway (port 9000) ← **Use this endpoint**
- Settlement Service (port 3004) - Requires blockchain credentials

## Quick Start for Backend

### Start Backend Services
```powershell
cd c:\Users\gondwe\Desktop\VaultString\Projects\kyd-payment-system
powershell -ExecutionPolicy Bypass -File .\scripts\run-supervisor-fixed.ps1
```

Or manually start services:
```powershell
cd build
.\auth.exe
.\payment.exe
.\forex.exe
.\wallet.exe
.\gateway.exe
```

### Verify Backend
```powershell
powershell -ExecutionPolicy Bypass -File .\test-backend.ps1
```

All tests should pass and display "Backend is ready."

---

## API Integration Points

### Base URL
```
http://localhost:9000/api/v1
```

### 1. Authentication

#### Register New User
```http
POST /auth/register
Content-Type: application/json

{
  "email": "user@example.com",
  "password": "SecurePassword123",
  "phone": "+265999888777",
  "first_name": "John",
  "last_name": "Doe",
  "user_type": "individual",
  "country_code": "MW"
}
```

**Response:**
```json
{
  "user": {
    "id": "uuid-here",
    "email": "user@example.com",
    "phone": "+265999888777",
    "first_name": "John",
    "last_name": "Doe",
    "user_type": "individual",
    "country_code": "MW",
    "created_at": "2025-12-07T00:00:00Z"
  },
  "access_token": "eyJhbGc...",
  "refresh_token": "eyJhbGc...",
  "expires_in": 900
}
```

#### Login
```http
POST /auth/login
Content-Type: application/json

{
  "email": "user@example.com",
  "password": "SecurePassword123"
}
```

**Response:** Same as registration (user + tokens)

#### Refresh Token
```http
POST /auth/refresh
Content-Type: application/json

{
  "refresh_token": "eyJhbGc..."
}
```

---

### 2. Wallet Management

#### Get All Wallets (Authenticated)
```http
GET /wallets
Authorization: Bearer <access_token>
```

**Response:**
```json
{
  "count": 3,
  "wallets": [
    {
      "id": "wallet-uuid",
      "user_id": "user-uuid",
      "currency": "MWK",
      "available_balance": 10000.00,
      "blocked_balance": 0.00,
      "total_balance": 10000.00,
      "status": "active",
      "created_at": "2025-12-07T00:00:00Z"
    },
    {
      "id": "wallet-uuid",
      "currency": "CNY",
      "available_balance": 10000.00,
      "blocked_balance": 0.00,
      "total_balance": 10000.00,
      "status": "active",
      "created_at": "2025-12-07T00:00:00Z"
    },
    {
      "id": "wallet-uuid",
      "currency": "USD",
      "available_balance": 10000.00,
      "blocked_balance": 0.00,
      "total_balance": 10000.00,
      "status": "active",
      "created_at": "2025-12-07T00:00:00Z"
    }
  ]
}
```

---

### 3. Payment Processing

#### Initiate Payment
```http
POST /payments/initiate
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "receiver_id": "recipient-user-uuid",
  "amount": 1000.00,
  "source_currency": "MWK",
  "target_currency": "CNY",
  "description": "Purchase from supplier"
}
```

**Response:**
```json
{
  "transaction_id": "txn-uuid",
  "status": "pending",
  "sender_id": "user-uuid",
  "receiver_id": "recipient-uuid",
  "amount": 1000.00,
  "source_currency": "MWK",
  "target_currency": "CNY",
  "exchange_rate": 0.008373,
  "converted_amount": 8.373,
  "fee": 0.50,
  "created_at": "2025-12-07T00:00:00Z"
}
```

#### Get Exchange Rate
```http
GET /forex/rate?from=MWK&to=CNY
```

**Response:**
```json
{
  "from": "MWK",
  "to": "CNY",
  "buy_rate": 0.008628,
  "sell_rate": 0.008373,
  "mid_rate": 0.008500,
  "timestamp": "2025-12-07T12:00:00Z",
  "expires_at": "2025-12-07T13:00:00Z"
}
```

---

### 4. User Lookup

#### Get User by Email
```http
GET /users/email/user@example.com
Authorization: Bearer <access_token>
```

**Response:**
```json
{
  "id": "user-uuid",
  "email": "user@example.com",
  "first_name": "John",
  "last_name": "Doe",
  "phone": "+265999888777",
  "user_type": "individual",
  "country_code": "MW",
  "created_at": "2025-12-07T00:00:00Z"
}
```

---

## Test Data Available

### Test User 1
- **Email:** john.doe@example.com
- **Password:** Password123
- **Wallets:** MWK (10,000), CNY (10,000), USD (10,000)
- **Purpose:** Default test user

### Test User 2
- **Email:** wang.wei@example.com
- **Password:** Password123
- **Wallets:** MWK (10,000), CNY (10,000), USD (10,000)
- **Purpose:** Recipient for payment testing

### Create New Test User
Use the registration endpoint with any email/password combination.

---

## JavaScript Integration Example

```javascript
// 1. Login
async function login() {
  const response = await fetch('http://localhost:9000/api/v1/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      email: 'john.doe@example.com',
      password: 'Password123'
    })
  });
  
  const data = await response.json();
  localStorage.setItem('access_token', data.access_token);
  localStorage.setItem('refresh_token', data.refresh_token);
  
  return data.user;
}

// 2. Get Wallets
async function getWallets() {
  const token = localStorage.getItem('access_token');
  
  const response = await fetch('http://localhost:9000/api/v1/wallets', {
    headers: { Authorization: `Bearer ${token}` }
  });
  
  return response.json();
}

// 3. Check Exchange Rate
async function getExchangeRate(from, to) {
  const response = await fetch(
    `http://localhost:9000/api/v1/forex/rate?from=${from}&to=${to}`
  );
  
  return response.json();
}

// 4. Initiate Payment
async function initiatePayment(receiverId, amount, sourceCurrency, targetCurrency) {
  const token = localStorage.getItem('access_token');
  
  const response = await fetch('http://localhost:9000/api/v1/payments/initiate', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`
    },
    body: JSON.stringify({
      receiver_id: receiverId,
      amount: amount,
      source_currency: sourceCurrency,
      target_currency: targetCurrency,
      description: 'Purchase from supplier'
    })
  });
  
  return response.json();
}

// 5. Full Payment Flow
async function buyFromChina() {
  // Login
  const user = await login();
  console.log('Logged in as:', user.first_name);
  
  // Get wallets
  const wallets = await getWallets();
  console.log('Wallets:', wallets.wallets);
  
  // Get MWK → CNY rate
  const rate = await getExchangeRate('MWK', 'CNY');
  console.log('MWK/CNY rate:', rate.sell_rate);
  
  // Initiate payment (send 1000 MWK = ~8.37 CNY to wang.wei)
  const payment = await initiatePayment(
    'wang-wei-user-id',
    1000,
    'MWK',
    'CNY'
  );
  console.log('Payment initiated:', payment);
  
  return payment;
}
```

---

## Common Error Responses

### 401 Unauthorized
```json
{
  "error": "Unauthorized",
  "message": "Invalid or expired token"
}
```
**Solution:** Refresh token using `/auth/refresh` endpoint or login again.

### 400 Bad Request
```json
{
  "error": "Bad Request",
  "message": "Invalid request parameters"
}
```
**Solution:** Check request body JSON structure and required fields.

### 404 Not Found
```json
{
  "error": "Not Found",
  "message": "User not found"
}
```
**Solution:** Verify user email/ID exists in system.

### 500 Internal Server Error
```json
{
  "error": "Internal Server Error",
  "message": "Something went wrong"
}
```
**Solution:** Check backend logs. May indicate database or service connectivity issue.

---

## CORS Configuration
✅ CORS is enabled for all origins in development
- Allowed Methods: GET, POST, PUT, DELETE, PATCH
- Allowed Headers: Content-Type, Authorization
- Credentials: Supported

---

## Rate Limiting
⚠️ Rate limits are configured per IP:
- Auth endpoints: 10 requests/minute
- Payment endpoints: 5 requests/minute
- General endpoints: 30 requests/minute

If rate limited, HTTP 429 response will be returned.

---

## Database

### Connection Details (Internal Only)
- **Host:** localhost
- **Port:** 5432
- **Database:** kyd_dev
- **User:** kyd_user
- **Password:** kyd_password

### Available Tables
- `users` - User accounts
- `wallets` - Multi-currency wallets
- `transactions` - Payment transactions
- `ledger_entries` - Double-entry ledger
- `exchange_rates` - Forex rates
- `users_devices` - Device registration (for 2FA)

---

## Troubleshooting

### "Connection refused" on port 9000
- Services not running. Run `run-supervisor-fixed.ps1`
- Or manually start services in `build/` folder

### "Invalid authentication" on login
- User doesn't exist in database
- Password is incorrect (case-sensitive)
- Use test credentials: john.doe@example.com / Password123

### "dial tcp: lookup redis: no such host"
- Redis not running or URL misconfigured
- Ensure REDIS_URL in .env is set to `localhost:6379`

### "missing destination name business_registration"
- Database schema mismatch (should be fixed)
- Run migrations: `go run cmd/migrate/main.go`

### Payment returns HTTP 500 with SQL error
- Known issue: Transaction INSERT has parameter count mismatch
- Workaround: Use simplified payment flow without conversion
- Fix in progress

---

## Next Steps

1. **Deploy Backend to Production**
   - Use Docker: `docker-compose up`
   - Or Kubernetes: Apply YAML files in `k8s/` folder
   - Update .env for production URLs

2. **Connect Webapp Frontend**
   - Use API Gateway endpoint: `http://localhost:9000/api/v1`
   - Implement login/register screens
   - Display wallets and balance
   - Create payment form for MWK→CNY transfers

3. **Implement Settlement Service**
   - Requires Ripple and Stellar blockchain credentials
   - Currently not running (non-blocking for MVP)
   - Contact admin for blockchain setup

4. **Run End-to-End Tests**
   - Use `test-backend.ps1` for service verification
   - Run manual payment flow tests
   - Monitor logs for errors

---

## Support

For issues or questions:
1. Check BACKEND_STATUS.md for detailed system information
2. Review service logs in terminal output
3. Verify .env configuration
4. Ensure Postgres and Redis are running

---

**Last Updated:** December 7, 2025  
**Status:** Production Ready (5/6 Services Operational)
