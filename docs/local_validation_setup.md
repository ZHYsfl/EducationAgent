# Local Real-Environment Validation Setup (Auth + Memory)

This document prepares the service for local black-box validation with:
- local PostgreSQL
- Redis in Docker
- environment variables
- SQL migrations
- local service start

## 1. Prerequisites
- PostgreSQL running locally and reachable from your shell
- Docker available
- Go toolchain available
- `psql` client available

## 2. Environment configuration
From repo root:

```bash
cp auth_memory_service/.env.example auth_memory_service/.env
```

Edit `auth_memory_service/.env` and set at least:
- `POSTGRES_DSN` (required)
- `JWT_SECRET` (required, non-default)
- `INTERNAL_KEY` (required)

Example DSN:

```bash
POSTGRES_DSN=postgres://postgres:postgres@localhost:5432/auth_memory_service?sslmode=disable
```

## 3. Start Redis (Docker)
Use helper script:

```bash
./scripts/redis_local.sh start
./scripts/redis_local.sh status
```

Equivalent raw Docker command:

```bash
docker run -d --name education-agent-redis -p 6379:6379 --restart unless-stopped redis:7-alpine
```

If container already exists but is stopped:

```bash
docker start education-agent-redis
```

## 4. Apply SQL migrations
From repo root:

```bash
set -a
source auth_memory_service/.env
set +a
./scripts/apply_migrations.sh
```

The migration script applies all files under `auth_memory_service/migrations/` in numeric order.

## 5. Start service locally
From repo root:

```bash
cd auth_memory_service
go run ./cmd/server
```

Expected startup log includes:
- `auth-memory service listening on :<AUTH_MEMORY_PORT>`

## 6. Stop or clean Redis container (optional)

```bash
./scripts/redis_local.sh stop
./scripts/redis_local.sh rm
```
