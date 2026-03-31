# Team Setup & Workflow Guide

This guide describes how to set up the KYD Payment System environment using Docker.
**Prerequisite:** Docker Desktop installed.

## 1. Initial Setup

1.  **Clone the repository**:
    ```bash
    git clone <repo-url>
    cd kyd-payment-system
    ```

2.  **Environment Configuration**:
    The `docker-compose.yml` comes with default values for local development.
    (Optional) You can copy `env.example` to `.env` if you need to override specific values.

## 2. Running the System

Start backend services (Database, Redis, Gateway, Auth, Payment, Wallet, Forex, Settlement):

```bash
docker-compose up --build
```

To include frontends (requires sibling directories `admin` and `vaultstring-frontend`):

```bash
docker-compose --profile frontend up --build
```

*   **API Gateway**: `http://localhost:9000`
*   **Customer Frontend**: `http://localhost:3012` (requires `--profile frontend`)
*   **Admin Portal**: `http://localhost:3016` (requires `--profile frontend`)

## 3. Migrations and Seeding

The database starts empty. Run migrations first, then seed:

**Run migrations** (creates schema):
```powershell
docker compose up -d postgres redis
docker compose --profile tools run --rm migrate-runner
```

**Run the seeder** (populates test users and wallets):
```powershell
docker compose --profile tools run --rm seed-runner
```
*Note: Ensure postgres is running before migrate and seed.*

### Clean DB reset workflow (recommended)

If your local data becomes inconsistent, use this reset flow:

```powershell
docker compose down
docker volume rm projects_postgres_data
docker compose up -d postgres redis
docker compose --profile tools run --rm migrate-runner
docker compose --profile tools run --rm seed-runner
docker compose up -d --build gateway-service auth-service payment-service wallet-service
```

Then run:

```powershell
./scripts/verify-fixes.ps1
```

Do **not** depend on `000_master_consolidated.down.sql` for local reset; use volume reset + `up.sql` + seeding.

## 4. Test Credentials

Use these pre-seeded accounts to test the system:

Test Accounts Created:
---------------------------------------------------
Admin User:
  Email:    admin@kyd.com
  Password: password123
  Role:     ADMIN
---------------------------------------------------
Customer User:
  Email:    customer@kyd.com
  Password: password123
  Role:     INDIVIDUAL
---------------------------------------------------
Additional Users:
  - john.doe@example.com (password123)
  - jane.smith@example.com (password123)

## 5. Frontend Testing Guide

Follow these steps to verify the "Happy Path":

### Step 1: Login as Sender
1.  Open **Customer Frontend**: [http://localhost:3012](http://localhost:3012)
2.  Login with **John Doe** (`john.doe@example.com` / `password123`).
3.  Note the **MWK** wallet balance.

### Step 2: Send Money
1.  Click **"Send Money"** in the dashboard.
2.  **Receiver**: Use a 16-digit wallet number from another seeded user (for example `customer@kyd.com`).
    *Tip: run `./scripts/verify-fixes.ps1` to validate wallet lookup and transfer endpoints automatically.*
3.  **Amount**: Enter `1000` MWK.
4.  **Currency**: Select `CNY` as destination currency.
5.  Click **"Confirm & Send"**.
6.  You should see a **Success** notification.

### Step 3: Verify Receipt
1.  Logout John.
2.  Login with **Customer User** (`customer@kyd.com` / `password123`).
3.  Check wallet and transaction history for the received transfer.

### Step 4: Admin Approval (For Large Amounts)
1.  Login as John again.
2.  Send **600,000** MWK (above the 500k threshold).
3.  The transaction will be **Pending**.
4.  Open **Admin Portal**: [http://localhost:3016](http://localhost:3016)
5.  Login with **Admin** (`admin@kyd.com` / `password123`).
6.  Go to **Transactions**. Find the pending one.
7.  Click **Approve**.
8.  Check John's history; it should now be **Completed**.

## 6. Automated Verification

We have provided scripts to verify the system is working correctly.

**Run System Verification:**
```powershell
./scripts/verify-fixes.ps1
```
This script will:
*   Log in as test users.
*   Check wallet balances.
*   Top up a wallet and perform a live transfer.
*   Verify key admin monitoring endpoints.

## 7. Development Workflow

*   **Code Changes**: The services use Docker. If you change Go code, you must rebuild the container:
    ```bash
    docker-compose up -d --build <service-name>
    # Example:
    docker-compose up -d --build payment-service
    ```
*   **Logs**:
    ```bash
    docker-compose logs -f <service-name>
    ```

## Troubleshooting

*   **Database connection failed**: Ensure `postgres` container is healthy (`docker-compose ps`).
*   **"relation does not exist"**: Run the seeder again to ensure migrations applied.

## 8. Git Hygiene Rules (Critical)

1. Never commit large migration artifacts:
   - `migrations/000_master_consolidated.down.sql` is intentionally ignored.
2. Before commit, always check:
   ```bash
   git status
   git diff --name-only --cached
   ```
3. If `seeds/consolidated/node_modules` appears in staging, unstage it:
   ```bash
   git restore --staged seeds/consolidated/node_modules
   ```
4. If push is rejected for historical large files, remove from history with `git-filter-repo` and push with `--force-with-lease` only after team confirmation.

est Accounts Created **

- Admin User : admin@kyd.com / password123
- Treasury User : fees@kyd.com / password123
- Standard Customers :
  - customer@kyd.com / password123
  - john.doe@example.com / password123
  - jane.smith@example.com / password123