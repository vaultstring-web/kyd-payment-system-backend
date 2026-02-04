# Database Schema Documentation

## Entity Relationship Diagram (ERD)

This diagram represents the consolidated schema for the KYD Payment System.

```mermaid
erDiagram
    %% User Management & Security (customer_schema)
    USERS ||--o{ WALLETS : "owns"
    USERS ||--o{ LOGIN_HISTORY : "has"
    USERS ||--o{ DEVICES : "uses"
    USERS {
        uuid id PK
        string email UK
        string phone_number UK
        string password_hash
        string full_name
        string role
        boolean is_verified
        boolean is_locked
        jsonb security_settings
        timestamp created_at
    }

    WALLETS ||--o{ TRANSACTIONS_SENT : "sends"
    WALLETS ||--o{ TRANSACTIONS_RECEIVED : "receives"
    WALLETS {
        uuid id PK
        uuid user_id FK
        string wallet_number UK
        decimal balance
        string currency
        string status
        string tier
        decimal daily_limit
        timestamp last_transaction_at
    }

    TRANSACTIONS {
        uuid id PK
        string reference UK
        uuid sender_wallet_id FK
        uuid receiver_wallet_id FK
        decimal amount
        decimal fee
        string currency
        string status
        string type
        string description
        string category
        jsonb metadata
        timestamp created_at
    }
    
    %% Relationships for Transactions (Double reference for sender/receiver)
    WALLETS ||--o{ TRANSACTIONS : "sender"
    WALLETS ||--o{ TRANSACTIONS : "receiver"

    %% Security & Audit (audit_schema)
    TRANSACTIONS ||--|| TRANSACTION_LEDGER : "recorded_in"
    TRANSACTION_LEDGER {
        uuid id PK
        uuid transaction_id FK
        string previous_hash
        string current_hash
        jsonb data_snapshot
        timestamp created_at
    }

    USERS ||--o{ AUDIT_LOGS : "triggers"
    AUDIT_LOGS {
        uuid id PK
        uuid user_id FK
        string action
        string entity_type
        string entity_id
        jsonb changes
        string ip_address
        string user_agent
        timestamp created_at
    }

    %% Admin Management (admin_schema)
    ADMIN_USERS {
        uuid id PK
        string email UK
        string role
        string permissions
    }

    %% Views
    SECURE_USERS_VIEW {
        uuid id
        string email
        string full_name
        string role
    }
```

## Schema Structure

### 1. Customer Schema (`customer_schema`)
Contains all user-facing data.
- **users**: Core user profiles with RLS enabled.
- **wallets**: Financial accounts associated with users.
- **transactions**: Financial records between wallets.
- **devices**: Trusted devices for 2FA/Security.

### 2. Admin Schema (`admin_schema`)
Contains internal administrative data.
- **admin_users**: Staff accounts with elevated privileges.
- **system_configs**: Global settings (fees, limits).

### 3. Audit Schema (`audit_schema`)
Immutable records for compliance and security.
- **audit_logs**: Tracks all critical user actions (password changes, login attempts).
- **transaction_ledger**: Hash-chained ledger for transaction immutability (Blockchain-like integrity).

## Security Features
- **Row Level Security (RLS)**: Enforced on all customer tables. Users can only see their own data.
- **Hash Chaining**: `transaction_ledger` ensures financial data cannot be tampered with without breaking the chain.
- **Encrypted Columns**: Sensitive fields (PII) are encrypted at rest using PGP.
