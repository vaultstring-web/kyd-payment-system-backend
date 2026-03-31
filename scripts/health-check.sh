#!/bin/bash
# Health check for KYD Payment System deployment
# Usage: ./health-check.sh [green|blue]
# Or: HEALTH_BASE_URL=https://... ./health-check.sh
set -e
ENV="${1:-green}"
BASE="${HEALTH_BASE_URL:-http://localhost:9000}"
echo "Running health check for: $ENV at $BASE"
curl -sf "$BASE/health" -o /dev/null && echo "Gateway health OK" || { echo "Health check failed"; exit 1; }
echo "Health checks passed"
