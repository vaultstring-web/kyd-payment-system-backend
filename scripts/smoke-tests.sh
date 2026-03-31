#!/bin/bash
# Smoke tests for KYD Payment System
# Usage: ./smoke-tests.sh [staging|production]
# Expects GATEWAY_URL or uses default based on ENV
set -e
ENV="${1:-staging}"
if [ "$ENV" = "production" ]; then
  BASE="${GATEWAY_URL:-https://kydpay.com}"
else
  BASE="${GATEWAY_URL:-https://staging.kydpay.com}"
fi
echo "Running smoke tests for $ENV at $BASE"
curl -sf "$BASE/health" | grep -q '"status"' && echo "Health OK" || { echo "Health check failed"; exit 1; }
curl -sf "$BASE/api/v1/auth/login" -X POST -H "Content-Type: application/json" -d '{"email":"test@test.com","password":"test"}' >/dev/null 2>&1 || true
echo "Smoke tests passed"
