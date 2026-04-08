# Memory Service

This directory contains Auth + Memory repository memory routes.

Canonical outward-facing routes:

- `POST /api/v1/memory/recall`
- `POST /api/v1/memory/context/push`

Deprecated compatibility routes during migration:

- `POST /api/v1/memory/extract`
- `GET /api/v1/memory/profile/{user_id}`
- `PUT /api/v1/memory/profile/{user_id}`
- `POST /api/v1/memory/working/save`
- `GET /api/v1/memory/working/{session_id}`

Internal-only routes:

- `POST /api/internal/memory/recall/sync`
- `POST /api/internal/memory/extract`
- `GET /api/internal/memory/profile/{user_id}`
- `PUT /api/internal/memory/profile/{user_id}`
- `POST /api/internal/memory/working/save`
- `GET /api/internal/memory/working/{session_id}`

## Run

```bash
cd memory_service
go mod tidy
go run ./cmd/server
```

### Smoke bootstrap

```bash
cd memory_service
cp .env.smoke.example .env
set -a
source .env
set +a
go run ./cmd/server
```

For deprecated `/api/v1/memory/profile/{user_id}` bearer-self smoke checks, ensure:

- bearer token is minted by this repository `auth_service` JWT logic
- `JWT_SECRET` matches between `auth_service` and `memory_service`
- token claims include `user_id`, `username`, and millisecond `exp` matching repo token format

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
- `MEMORY_ASYNC_MODE` (default `in_process`)
- `VOICE_AGENT_BASE_URL`
- `VOICE_AGENT_PPT_MESSAGE_PATH` (default `/api/v1/voice/ppt_message`)
- `VOICE_AGENT_INTERNAL_KEY`
- `VOICE_AGENT_TIMEOUT_MS` (default `2000`)
- `VOICE_AGENT_RETRY_MAX` (default `3`)
- `EXTRACTOR_LLM_ENABLED`
- `EXTRACTOR_LLM_MODEL`
- `EXTRACTOR_LLM_TIMEOUT_MS`
- `EXTRACTOR_MAX_TURNS`
- `DEEPSEEK_API_KEY`
- `DEEPSEEK_BASE_URL`

For smoke tests, `.env.smoke.example` is the supported template.

## Migrations in this folder

- `0001_users.sql`
- `0005_sessions.sql`
- `0006_memory_entries.sql`

## Route classes and auth

- Canonical `/api/v1/memory/*` routing is middleware-mounted at the Gin route-group level.
- Current implementation mounts canonical `recall` and `context/push` with trusted internal-service auth.
- Deprecated `/extract` and `/working/*` preserve the existing trusted/internal access pattern during migration.
- Deprecated `/profile/{user_id}` is the only compatibility route that temporarily supports either trusted internal access or bearer self-access.
- Deprecated compatibility routes emit deprecation headers and structured logs so removal readiness can be measured.

## Recall and callback behavior

- `POST /api/v1/memory/recall` now returns only an accepted response and dispatches async processing.
- Async recall reuses deterministic synchronous recall logic internally, then callbacks Voice Agent via `POST /api/v1/voice/ppt_message`.
- The canonical callback payload uses `task_id`, `session_id`, `event_type = "get_memory"`, `summary`, and optional `request_id`.
- The callback summary is compact and prompt-ready rather than serializing the old sync recall payload.
- `task_id = session_id` in the callback is a transport compatibility mapping for the current Voice Agent contract, not a memory-domain identity rule.
- `/api/internal/memory/recall/sync` exists only for smoke testing, deterministic verification, and callback generation support. It is not a future integration target.

## Context push behavior

- `POST /api/v1/memory/context/push` accepts the documented `user_id`, `session_id`, and ordered `messages[]` payload 1:1.
- Phase 1 reuses extraction, durable write filtering, working-memory update, and summary/profile persistence.
- Archival and indexing remain interface-backed in phase 1. A stub/no-op implementation is acceptable.
- If OSS archival is wired now, it must reuse repository code under `oss/` rather than adding a second OSS implementation.

## Internal recall behavior notes

- Recall ranking remains deterministic and service-layer owned.
- `top_k` is treated as a compactness budget across facts + preferences (default `5` when omitted or non-positive).
- Working memory remains the primary session-state carrier; `profile_summary` stays durable-first with only a minimal session addon for clear continuation/current-task queries.
