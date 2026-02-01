# Team Workflow & Pull Plan

This document outlines the standard operating procedure for the team to pull changes, run the environment, and maintain security compliance.

## 🔄 Daily Pull Routine

Because we are actively hardening the security architecture (Schema changes, Encryption, Ledger), it is critical to **reset your environment** frequently.

### 1. Pull Latest Changes
```bash
git pull origin main
```

### 2. Automated Reset & Verify (Recommended)
We have added a helper script to automate the cleanup, build, seed, and test process.
**PowerShell:**
```powershell
./scripts/verify_env.ps1
```
*This script will: Stop containers, remove volumes, rebuild images, start services, run migrations, seed data, and run integration tests.*

### 3. Manual Reset (Alternative)
If you prefer to run steps manually:


**Every time you pull schema changes:**
```bash
# Stop containers and remove volumes (clears DB data)
docker-compose down -v

# Rebuild and start
docker-compose up -d --build
```

### 3. Verify Startup
Wait for logs to show `database system is ready to accept connections`.
```bash
docker-compose logs -f postgres
```

### 4. Run Seed Data
Populate the fresh database with test users (Admin, Merchant, Individuals):
```bash
go run cmd/seed/main.go
```

## 🛡️ Security Development Workflow

### Coding Standards (Bank-Grade)
- **No Hardcoded Secrets**: Always use `os.Getenv` and `.env` file.
- **Structured Logging**: Use `logger.Info` / `logger.Error` with context fields. Do not use `fmt.Println`.
- **Input Validation**: All request structs must have `validate` tags. Use `validator.ValidateStructured`.
- **Audit Trails**: Critical actions (Money movement, Auth) are automatically audited. Ensure your handlers use the `AuditMiddleware`.

### Running Tests
Run the full suite, including the new Ledger Immutability tests:
```bash
go test ./...
```
*Note: Some integration tests require the DB to be running.*

## 🚨 Troubleshooting

**"Port 5432 already in use"**
- Check if a local Postgres service is running on your machine.
- Check if another Docker container is running: `docker ps`.

**"Database connection refused"**
- Ensure Docker Desktop is running.
- Ensure you ran `docker-compose up` successfully.

**"Schema mismatch" or "Relation does not exist"**
- Run `docker-compose down -v` to clear the old schema and let migrations re-run.
