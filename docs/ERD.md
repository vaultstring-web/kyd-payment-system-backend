# Database Schema Documentation

## Entity Relationship Diagram (ERD)

The following diagram represents the consolidated schema for the VaultString Payment System.

```mermaid
erDiagram
    %% Core Tables (Customer Schema)
    USERS ||--o{ WALLETS : "owns"
    USERS ||--o{ TRANSACTIONS : "sends/receives"
    USERS ||--o{ NOTIFICATIONS : "receives"
    USERS ||--o{ KYC_DOCUMENTS : "submits"
    USERS ||--o{ AUDIT_LOGS : "generates"

    WALLETS ||--o{ TRANSACTIONS : "funds/credits"

    TRANSACTIONS ||--|| TRANSACTION_LEDGER : "recorded_in"
    TRANSACTIONS ||--o{ SETTLEMENTS : "settled_via"

    SETTLEMENTS ||--o{ BLOCKCHAIN_TRANSACTIONS : "executed_on"

    %% Table Definitions
    USERS {
        uuid id PK
        varchar email "Unique"
        varchar phone "Unique"
        varchar password_hash
        varchar user_type "individual, merchant, etc."
        varchar kyc_status
        boolean email_verified
        boolean is_active
        integer failed_login_attempts
        timestamp locked_until
        decimal risk_score
        timestamp created_at
    }

    USER_DEVICES {
        uuid id PK
        uuid user_id FK
        varchar device_name
        varchar ip_address
        boolean is_trusted
        timestamp last_seen_at
    }

    WALLETS {
        uuid id PK
        uuid user_id FK
        varchar currency "MWK, CNY, USD"
        decimal available_balance
        decimal ledger_balance
        varchar status
    }

    TRANSACTIONS {
        uuid id PK
        varchar reference "Unique"
        uuid sender_id FK
        uuid receiver_id FK
        decimal amount
        decimal fee_amount
        varchar status "pending, completed, etc."
        varchar transaction_type
        timestamp initiated_at
    }

    NOTIFICATIONS {
        uuid id PK
        uuid user_id FK
        varchar type
        varchar title
        text message
        boolean is_read
        boolean is_archived
        timestamp created_at
    }

    TRANSACTION_LEDGER {
        uuid id PK
        uuid transaction_id FK
        varchar hash "SHA-256"
        varchar previous_hash "SHA-256"
        varchar event_type
    }

    KYC_DOCUMENTS {
        uuid id PK
        uuid user_id FK
        varchar document_type
        varchar verification_status
        string image_urls
    }

    SETTLEMENTS {
        uuid id PK
        varchar network "stellar, ripple"
        decimal total_amount
        varchar status
    }

    BLOCKCHAIN_TRANSACTIONS {
        uuid id PK
        uuid settlement_id FK
        varchar tx_hash
        varchar from_address
        varchar to_address
    }

    AUDIT_LOGS {
        uuid id PK
        uuid user_id FK
        varchar action
        jsonb old_values
        jsonb new_values
        varchar ip_address
    }
```

## Schema Organization

The database is organized into schemas for security and logical separation:

1.  **customer_schema**: Contains core business data (Users, Wallets, Transactions, Notifications).
    *   **RLS (Row Level Security)** is enabled on all sensitive tables to ensure users can only access their own data.
2.  **admin_schema**: Contains high-level administrative logs (Audit Logs).
3.  **audit_schema**: Contains low-level data change logs (triggers capture every INSERT/UPDATE/DELETE).
4.  **privacy_schema**: Contains differential privacy budgets and query logs.

## Security Features

*   **Immutable Ledger**: `transaction_ledger` uses a SHA-256 hash chain (`previous_hash` -> `hash`) to prevent tampering with transaction history.
*   **Audit Logging**: Triggers automatically log all data changes to `audit_schema.data_changes`.
*   **Roles**: `kyd_system` (app backend) and `kyd_admin` (DBA/admin tool) roles are defined with least-privilege access.
