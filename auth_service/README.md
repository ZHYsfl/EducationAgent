# Auth Service

This directory contains only Auth APIs:

- `POST /api/v1/auth/register`
- `POST /api/v1/auth/verify`
- `POST /api/v1/auth/login`
- `GET /api/v1/auth/profile`

## Run

```bash
cd auth_service
go mod tidy
go run ./cmd/server
```

## Test

```bash
cd auth_service
GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache go test ./...
```

## Key env vars

- `AUTH_PORT` (fallback: `AUTH_MEMORY_PORT`, default `9300`)
- `POSTGRES_DSN`
- `JWT_SECRET`
- `JWT_TTL_HOURS` (default `24`)
- `VERIFY_TOKEN_TTL_HOURS` (default `24`)
- `FRONTEND_VERIFY_URL`
- `INTERNAL_KEY` (optional)

## Migrations in this folder

- `0001_users.sql`
- `0002_pending_registrations.sql`
- `0003_consumed_verification_tokens.sql`
- `0004_issued_verification_tokens.sql`
