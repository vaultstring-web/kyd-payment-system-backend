## Docker Stability Checklist

Use this sequence for reliable local startup and quick recovery.

### Standard startup

1. `docker compose build`
2. `docker compose up -d`
3. `docker compose ps`
4. Verify all services show `healthy`.

### Health verification commands

- `docker compose ps`
- `docker compose logs --tail=200 gateway-service`
- `docker compose logs --tail=200 migrate-runner`
- `docker compose logs --tail=200 seed-runner`

### Fast recovery playbook

If `docker compose build` fails with `EOF` (buildx/buildkit instability):

1. `docker buildx ls` and check builder status.
2. Restart Docker Desktop.
3. Retry `docker compose build`.

If services are up but unhealthy:

1. Confirm dependencies:
   - Postgres healthy
   - Redis healthy
2. Check migration/seed status:
   - `docker compose logs migrate-runner`
   - `docker compose logs seed-runner`
3. Restart dependent services:
   - `docker compose restart auth-service payment-service wallet-service settlement-service forex-service gateway-service`

If gateway health fails:

1. Confirm upstream services are healthy in `docker compose ps`.
2. Check gateway logs:
   - `docker compose logs --tail=200 gateway-service`
3. Restart gateway:
   - `docker compose restart gateway-service`

### Security/session validation after startup

- Verify secure auth cookie behavior in browser devtools:
  - `HttpOnly` enabled
  - `Secure` enabled in HTTPS/deployed env
  - `SameSite` set to `Lax` (dev) or `None` with secure transport
- Verify CORS preflight accepts:
  - `Authorization`
  - `X-CSRF-Token`
  - `Idempotency-Key`
  - `X-Device-ID`

