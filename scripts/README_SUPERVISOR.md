# Supervisor script (Windows)

This folder contains `run-supervisor.ps1`, a small development helper that keeps the KYD backend services running on Windows by restarting any service that exits.

Usage

1. Copy `.env.example` to `.env` and edit values if needed:

```powershell
cp .env.example .env
```

2. Run the supervisor script (use ExecutionPolicy Bypass if needed):

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\run-supervisor.ps1
```

What it does

- Loads `.env` (or `.env.example` if `.env` is not present).
- Starts `auth`, `wallet`, `forex`, `payment`, `settlement`, and `gateway` services as background monitor jobs.
- Writes logs to `logs/<service>.log` and restarts a service if it exits.

Notes & next steps

- For production, use proper process supervisors: Windows Services, Docker Compose, or Kubernetes.
- You can register the script as a Scheduled Task to run at system startup or user login for persistent availability.
- If you want, I can add a script to register a Windows Scheduled Task that runs this supervisor at boot.
