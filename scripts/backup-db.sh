#!/bin/bash
# Database backup script for KYD Payment System
# Usage: ./backup-db.sh [staging|production]
# Requires: DATABASE_URL environment variable
set -e
ENV="${1:-production}"
if [ -z "$DATABASE_URL" ]; then
  echo "DATABASE_URL is required"
  exit 1
fi
OUTPUT="${BACKUP_OUTPUT:-backup-$ENV-$(date +%Y%m%d-%H%M%S).sql}"
echo "Backing up database for $ENV to $OUTPUT"
pg_dump "$DATABASE_URL" -F p -f "$OUTPUT" && echo "Backup saved to $OUTPUT" || { echo "Backup failed"; exit 1; }
