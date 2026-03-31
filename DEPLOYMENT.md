# Deployment & Operations Guide

This guide covers the procedures for deploying and operating the KYD Payment System in a production environment.

## 1. Infrastructure Requirements

- **Database**: PostgreSQL 15+ (RDS or self-hosted)
- **Cache**: Redis 7+ (ElastiCache or self-hosted)
- **Container Orchestration**: Kubernetes (EKS/GKE) or Docker Swarm
- **Ingress**: Nginx or AWS ALB with TLS termination

## 2. Environment Configuration

Ensure all production environment variables are set in your orchestration layer. Do NOT use a `.env` file in production.

### Core Services
- `ENV`: set to `production`
- `JWT_SECRET`: use a strong random string (64+ chars)
- `ENCRYPTION_KEY`: 32-byte hex string for AES-256-GCM
- `HMAC_KEY`: 32-byte hex string for request signing
- `DATABASE_URL`: production postgres connection string
- `REDIS_URL`: production redis connection string

### Google Services Integration
- `GOOGLE_CLIENT_ID`: OAuth 2.0 Client ID from GCP Console
- `GOOGLE_CLIENT_SECRET`: OAuth 2.0 Client Secret from GCP Console
- `GOOGLE_REDIRECT_URI`: Must match GCP Console (e.g., `https://api.vaultstring.com/api/v1/auth/google/callback`)
- `GOOGLE_MOCK_MODE`: set to `false`
- `GOOGLE_API_KEY`: API key for restricted Google services
- `GOOGLE_PROJECT_ID`: Your GCP Project ID
- `GOOGLE_SERVICE_ACCOUNT_PATH`: Path to mounted JSON credentials for Service Account

### Email (Gmail API)
- `SMTP_HOST`: `smtp.gmail.com`
- `SMTP_PORT`: `587`
- `SMTP_USERNAME`: your service email
- `SMTP_PASSWORD`: Gmail App Password (if using SMTP)
- `GMAIL_API_ENABLED`: set to `true` for enhanced security (requires `GOOGLE_SERVICE_ACCOUNT_PATH`)

## 3. Monitoring & Alerting

### Structured Logging
All services emit JSON logs to `stdout`. Use a log aggregator like:
- **ELK Stack** (Elasticsearch, Logstash, Kibana)
- **CloudWatch Logs** (AWS)
- **Google Cloud Logging**

### Health Checks
Each service provides a `/health` endpoint:
- Gateway: `http://gateway:8080/health`
- Auth: `http://auth:8080/health`
- Payment: `http://payment:8080/health`

### Alerting Policies
Recommended alerts:
- 5xx error rate > 1% over 5 minutes
- Database connection pool exhaustion
- High latency (> 500ms) on `/payments/initiate`
- Unauthorized OAuth callback attempts

## 4. Deployment Procedures

1. **Migrations**: Run the migration tool before deploying new service versions.
   ```bash
   ./kyd-migrate up
   ```
2. **Blue/Green Deployment**: Recommended for zero-downtime updates.
3. **Verification**: Run the automated test suite against the staging environment before flipping traffic.

## 5. Security Best Practices

- **TLS**: Use TLS 1.3 for all external traffic.
- **Request Signing**: Set `SIGNING_REQUIRED=true` to enforce HMAC signatures between frontend and gateway.
- **Secret Rotation**: Rotate `JWT_SECRET` and `GOOGLE_CLIENT_SECRET` every 90 days.
- **Audit Logs**: Regularly review `audit` table for suspicious login patterns.
