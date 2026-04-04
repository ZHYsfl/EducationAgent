#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MIGRATIONS_DIR="${ROOT_DIR}/auth_memory_service/migrations"

if ! command -v psql >/dev/null 2>&1; then
  echo "psql command not found. Install PostgreSQL client tools first." >&2
  exit 1
fi

if [[ -z "${POSTGRES_DSN:-}" ]]; then
  echo "POSTGRES_DSN is required" >&2
  exit 1
fi

echo "Applying migrations from ${MIGRATIONS_DIR}"
for file in "${MIGRATIONS_DIR}"/*.sql; do
  echo "-> $(basename "${file}")"
  psql "${POSTGRES_DSN}" -v ON_ERROR_STOP=1 -f "${file}"
done

echo "Migrations applied successfully"
