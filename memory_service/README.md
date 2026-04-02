# Memory Service

This directory contains only Memory APIs:

- `POST /api/v1/memory/extract`
- `POST /api/v1/memory/recall`
- `GET /api/v1/memory/profile/{user_id}`
- `PUT /api/v1/memory/profile/{user_id}`
- `POST /api/v1/memory/working/save`
- `GET /api/v1/memory/working/{session_id}`

## Run

```bash
cd memory_service
go mod tidy
go run ./cmd/server
```

## Test

```bash
cd memory_service
GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache go test ./...
```

## Key env vars

- `MEMORY_PORT` (fallback: `AUTH_MEMORY_PORT`, default `9300`)
- `POSTGRES_DSN`
- `REDIS_ADDR`
- `REDIS_PASSWORD`
- `REDIS_DB`
- `WORKING_MEMORY_TTL_HOURS` (default `4`)
- `JWT_SECRET`
- `JWT_TTL_HOURS` (default `24`)
- `INTERNAL_KEY`
- `EXTRACTOR_LLM_ENABLED`
- `EXTRACTOR_LLM_MODEL`
- `EXTRACTOR_LLM_TIMEOUT_MS`
- `EXTRACTOR_MAX_TURNS`
- `DEEPSEEK_API_KEY`
- `DEEPSEEK_BASE_URL`

## Migrations in this folder

- `0001_users.sql`
- `0005_sessions.sql`
- `0006_memory_entries.sql`

## Recall behavior notes (internal)

- `POST /api/v1/memory/recall` response fields remain unchanged.
- Recall ranking is deterministic and service-layer owned (no vector DB).
- `top_k` is treated as a compactness budget across facts + preferences for prompt-ready output.
- Working memory remains the primary session-state carrier; `profile_summary` stays durable-first with only a minimal session addon for clear continuation/current-task queries.
