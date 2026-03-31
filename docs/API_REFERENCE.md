# API Reference

## Base URL
`http://localhost:9000/api/v1`

All endpoints (except public auth routes) require `Authorization: Bearer <token>`.

---

## Authentication

### Register
**POST** `/auth/register`
```json
{
  "email": "user@example.com",
  "password": "SecurePass123!",
  "first_name": "John",
  "last_name": "Doe",
  "phone_number": "+265991234567"
}
```

### Login
**POST** `/auth/login`
```json
{
  "email": "john.doe@example.com",
  "password": "password123"
}
```
**Response**
```json
{
  "access_token": "ey...",
  "refresh_token": "ey...",
  "user": { "id": "...", "email": "...", "user_type": "individual", ... }
}
```

### Get Current User
**GET** `/auth/me`  
Returns the authenticated user profile.

### Update Profile
**PUT** `/auth/me`  
Update current user's profile (name, phone, etc.).

### Change Password
**POST** `/auth/me/password`
```json
{
  "current_password": "old",
  "new_password": "NewSecure123!"
}
```

### Forgot Password
**POST** `/auth/forgot-password`
```json
{ "email": "user@example.com" }
```

### Reset Password
**POST** `/auth/reset-password`
```json
{
  "token": "jwt-reset-token",
  "new_password": "NewSecure123!"
}
```

### TOTP (2FA)
- **POST** `/auth/totp/setup` – Initiate TOTP setup
- **POST** `/auth/totp/verify` – Verify TOTP code
- **POST** `/auth/totp/disable` – Disable TOTP
- **GET** `/auth/totp/status` – Check TOTP status

### Google OAuth
- **GET** `/auth/google/start` – Get Google OAuth URL
- **POST** `/auth/google/callback` – Exchange code for tokens
- **GET** `/auth/google/mock-login` – Mock login (when `GOOGLE_MOCK_MODE=true`)

---

## Wallets

### List Wallets
**GET** `/wallets`  
Returns all wallets for the authenticated user.

### Get Wallet
**GET** `/wallets/{id}`  
Get a single wallet by ID (must belong to user).

### Get Balance
**GET** `/wallets/{id}/balance`

### Create Wallet
**POST** `/wallets`
```json
{
  "currency": "MWK"
}
```

### Deposit
**POST** `/wallets/{id}/deposit`
```json
{
  "amount": 1000,
  "reference": "deposit-ref-123"
}
```

### Lookup Wallet
**GET** `/wallets/lookup?address=<wallet_address>`  
Look up a wallet by address/number.

### Search Wallets
**GET** `/wallets/search?q=<partial_address>&limit=10`

### Transaction History
**GET** `/wallets/{id}/history?limit=50&offset=0`

---

## Payments

### Initiate Payment
**POST** `/payments/initiate` or **POST** `/payments`  
*Headers*: `Idempotency-Key` (optional; gateway injects if missing)
```json
{
  "receiver_id": "uuid",
  "receiver_wallet_id": "uuid",
  "amount": 1000,
  "currency": "MWK",
  "destination_currency": "CNY",
  "description": "Payment for services",
  "reference": "unique-idempotency-key"
}
```
**Security Notes**:
- `amount`: Must be positive.
- `reference`: Used for idempotency.
- Velocity checks apply (e.g. max 3 high-value transactions per hour).

### Get Transaction by ID
**GET** `/payments/{id}`  
Returns a single transaction (user must be sender or receiver).

### Get Transactions (List)
**GET** `/payments?limit=50&offset=0&wallet_id=<uuid>`  
Paginated list of transactions for the authenticated user.

### Get Receipt
**GET** `/payments/{id}/receipt` or **GET** `/transactions/{id}/receipt`

### Cancel Payment
**POST** `/payments/{id}/cancel`  
Cancel a pending transaction (sender only).

### Bulk Payment
**POST** `/payments/bulk`
```json
{
  "payments": [
    {
      "receiver_id": "uuid",
      "amount": 100,
      "currency": "MWK",
      "destination_currency": "MWK",
      "description": "Bulk item 1"
    }
  ]
}
```
Returns `{ "successful": [...], "failed": [...], "total_count": N }`.

### Initiate Dispute
**POST** `/disputes`
```json
{
  "transaction_id": "uuid",
  "reason": "Unauthorized transaction"
}
```

---

## Forex

### Get All Rates
**GET** `/forex/rates`

### Get Rate
**GET** `/forex/rate/{from}/{to}` or **GET** `/forex/rate?from=MWK&to=USD`

### Calculate
**POST** `/forex/calculate`
```json
{
  "from_currency": "MWK",
  "to_currency": "USD",
  "amount": 1000
}
```

### Get History
**GET** `/forex/history?from=MWK&to=USD&days=7`

---

## Compliance (KYC)

### Submit KYC
**POST** `/compliance/kyc/submit`  
Submit KYC documents and data.

### Get KYC Status
**GET** `/compliance/kyc/status`

---

## Notifications

### List Notifications
**GET** `/notifications?limit=50&offset=0`  
Returns paginated notifications for the authenticated user.  
Notifications are persisted for payment events, security alerts, and KYC status changes.

---

## Admin Endpoints

All admin routes require `user_type: admin` in the JWT.

Base path: `/admin`

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/admin/users` | GET | List users |
| `/admin/users/{id}` | GET, PATCH, DELETE | User CRUD |
| `/admin/users/{id}/block`, `/unblock` | POST | Block/unblock user |
| `/admin/users/{id}/activity` | GET | User activity |
| `/admin/transactions` | GET | All transactions |
| `/admin/transactions/pending` | GET | Pending transactions |
| `/admin/transactions/{id}` | GET | Single transaction |
| `/admin/transactions/{id}/review` | POST | Approve/reject |
| `/admin/transactions/{id}/flag` | POST | Flag for review |
| `/admin/risk/alerts` | GET | Risk alerts |
| `/admin/risk/metrics` | GET | Risk metrics |
| `/admin/disputes` | GET | List disputes |
| `/admin/disputes/resolve` | POST | Resolve dispute |
| `/admin/analytics/metrics` | GET | System stats |
| `/admin/analytics/earnings` | GET | Earnings report |
| `/admin/analytics/volume` | GET | Transaction volume |
| `/admin/api-keys` | GET, POST | API key management |
| `/admin/api-keys/{id}` | DELETE | Revoke API key |
| `/admin/compliance/applications` | GET | KYC applications |
| `/admin/compliance/applications/{id}/review` | POST | Review application |
| `/admin/compliance/reports` | GET | Compliance reports |
| `/admin/system/status` | GET | System status |
| `/admin/audit-logs` | GET | Audit logs |
| `/admin/security/events` | GET | Security events |
| `/admin/security/blocklist` | GET, POST | Blocklist |
| `/admin/wallets` | GET | All wallets |
| `/admin/blockchain/networks` | GET, POST | Blockchain networks |
| `/admin/banking/settlements` | GET | Settlements |
| `/admin/banking/accounts` | GET | Bank accounts |
| `/admin/banking/gateways` | GET | Payment gateways |

---

## Happy Path (End-to-End)

1. **Register**: `POST /auth/register` with `email`, `password`, `first_name`, `last_name`, `phone_number`.
2. **Login**: `POST /auth/login` with `email` and `password` to get `access_token`. Include `Authorization: Bearer <token>` in subsequent requests.
3. **Create/Get Wallet**: `POST /wallets` with `{ "currency": "MWK" }` or `GET /wallets` to list existing wallets.
4. **Lookup Receiver**: `GET /wallets/lookup?address=<wallet_address>` or `GET /wallets/search?q=<partial>` to find the receiver's wallet.
5. **Initiate Payment**: `POST /payments/initiate` with `receiver_id`, `receiver_wallet_id`, `amount`, `currency`, `destination_currency`. Include `Idempotency-Key` header for safe retries.
6. **Get Transaction**: `GET /payments/{id}` to fetch a single transaction (sender or receiver only).
7. **Get Receipt**: `GET /payments/{id}/receipt` for a printable receipt.
8. **List Transactions**: `GET /payments` or `GET /wallets/{id}/history` for paginated history.
9. **Cancel (if pending)**: `POST /payments/{id}/cancel` – sender can cancel pending transactions only.

**Admin approval**: For large amounts (above `RISK_ADMIN_APPROVAL_THRESHOLD`), the transaction stays `pending`. An admin must approve via `POST /admin/transactions/{id}/review` with `{ "action": "approve" }`.
