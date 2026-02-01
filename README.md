# KYD Payment System

A secure, bank-grade payment backend with multi-currency support, blockchain settlement, and Next.js frontends.

## 🚀 Quick Start for Team

We have moved to a **100% Docker-based workflow**. No local Go or Postgres installation is required.

**👉 [Read the TEAM_SETUP.md Guide](TEAM_SETUP.md) for full setup instructions.**

### Short Version:
1.  `docker-compose up --build`
2.  `./scripts/seed.ps1` (to load test data)
3.  `./scripts/verify-fixes.ps1` (to verify system health)

## Access Points
*   **Customer Frontend**: [http://localhost:3012](http://localhost:3012)
*   **Admin Portal**: [http://localhost:3016](http://localhost:3016)
*   **API Gateway**: [http://localhost:9000](http://localhost:9000)

## Architecture
*   **Backend**: Go (Microservices: Auth, Payment, Wallet, Forex, Settlement)
*   **Database**: PostgreSQL 15
*   **Cache**: Redis 7
*   **Frontend**: Next.js 14 (Customer & Admin)

## License
Proprietary.
