# API Reference

## Base URL
`http://localhost:9000/api/v1`

## Authentication

### Login
**POST** `/auth/login`
```json
{
  "email": "john.doe@example.com",
  "password": "Password123"
}
```
**Response**
```json
{
  "access_token": "ey...",
  "refresh_token": "ey...",
  "user": { ... }
}
```

## Payments

### Initiate Payment
**POST** `/payments/initiate`
*Headers*: `Authorization: Bearer <token>`

```json
{
  "sender_id": "uuid",
  "receiver_id": "uuid",
  "amount": 1000,
  "currency": "MWK",
  "description": "Payment for services",
  "reference": "unique-idempotency-key"
}
```

**Security Notes**:
- `amount`: Must be positive.
- `reference`: Used for idempotency.
- **Velocity Checks**: Max 3 transactions > $1000 per hour.

### Get Wallets
**GET** `/wallets`
*Headers*: `Authorization: Bearer <token>`

Returns all wallets associated with the authenticated user.
