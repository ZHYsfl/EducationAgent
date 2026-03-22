# Auth + Memory Service

## 1. Overview
This service implements the delivered **User/Auth + Memory** scope for the teaching-agent system.

Included Auth endpoints:
- `POST /api/v1/auth/register`
- `POST /api/v1/auth/verify`
- `POST /api/v1/auth/login`
- `GET /api/v1/auth/profile`

Included Memory endpoints:
- `POST /api/v1/memory/extract`
- `POST /api/v1/memory/recall`
- `GET /api/v1/memory/profile/{user_id}`
- `PUT /api/v1/memory/profile/{user_id}`
- `POST /api/v1/memory/working/save`
- `GET /api/v1/memory/working/{session_id}`

Implementation is aligned to `system_interface_spec.md` and the approved `PLAN.md`.

## 2. Implemented API surface
Auth:
- `POST /api/v1/auth/register`
- `POST /api/v1/auth/verify`
- `POST /api/v1/auth/login`
- `GET /api/v1/auth/profile`

Memory:
- `POST /api/v1/memory/extract`
- `POST /api/v1/memory/recall`
- `GET /api/v1/memory/profile/{user_id}`
- `PUT /api/v1/memory/profile/{user_id}`
- `POST /api/v1/memory/working/save`
- `GET /api/v1/memory/working/{session_id}`

## 3. Contract highlights
- All HTTP responses use a unified JSON wrapper: `code`, optional `message`, optional `data`.
- JWT claims are exactly: `user_id`, `username`, `exp`.
- Timestamps are Unix milliseconds (`int64`).
- ID prefixes used by this service: `user_`, `sess_`, `mem_`.
- Email verification is one-time and deterministic for replay/concurrent loser behavior (`40903`), while preserving `40104` (expired) and `40103` (invalid/unknown) semantics.
- Memory extract persists/upserts long-term entries (including durable summary persistence).
- `PUT /api/v1/memory/profile/{user_id}` is partial update (omitted fields are preserved).
- Working memory is Redis-only with key `working_mem:{session_id}` and default 4-hour TTL reset on save.
- Dual-mode access policy for memory profile endpoints (Bearer + internal key) is retained as an approved integration-derived policy (not presented as a direct system-spec chapter mandate).

## 4. Internal implementation notes
The following are internal persistence mechanisms that support public contract behavior; they are **not additional public APIs**:
- `pending_registrations`: unverified registrations and hashed verification token state.
- `consumed_verification_tokens`: replay-evidence table for deterministic one-time verification behavior.
- `issued_verification_tokens`: issued token expiry evidence used for cleanup-independent error resolution.
- `memory_entries`: long-term fact/preference/summary storage with upsert and profile aggregation support.
- `sessions`: compatibility table for `memory_entries.source_session_id` FK with `ON DELETE SET NULL`.

## 5. Project structure
- `cmd/server/main.go`: service entrypoint and wiring.
- `internal/config/`: env/config loading.
- `internal/http/handlers/`: HTTP handlers (thin controller layer).
- `internal/http/middleware/`: Bearer/internal-key access middleware.
- `internal/service/`: Auth/Memory business logic.
- `internal/repository/`: PostgreSQL/Redis persistence logic.
- `internal/model/`: data models.
- `internal/contract/`: response wrapper and business error codes.
- `migrations/`: SQL schema migrations.
- `internal/service/*_test.go`, `internal/http/handlers/*_test.go`: key tests.

## 6. Environment and configuration
Configured via environment variables (see `internal/config/config.go`):

- Server:
  - `AUTH_MEMORY_PORT` (default `9300`)
- PostgreSQL:
  - `POSTGRES_DSN` (**required**)
- Redis:
  - `REDIS_ADDR` (default `localhost:6379`)
  - `REDIS_PASSWORD` (optional)
  - `REDIS_DB` (default `0`)
  - `WORKING_MEMORY_TTL_HOURS` (default `4`)
- JWT:
  - `JWT_SECRET` (set in non-dev; default fallback exists but should be overridden)
  - `JWT_TTL_HOURS` (default `24`)
- Internal auth:
  - `INTERNAL_KEY` (required for internal-key protected endpoints in deployment)
- Verification email settings:
  - `VERIFY_TOKEN_TTL_HOURS` (default `24`)
  - `FRONTEND_VERIFY_URL` (default `http://localhost:3000/verify-email`)

## 7. Database and migrations
Migrations included:
- `0001_users.sql`
- `0002_pending_registrations.sql`
- `0003_consumed_verification_tokens.sql`
- `0004_issued_verification_tokens.sql`
- `0005_sessions.sql`
- `0006_memory_entries.sql`

How to run migrations:
1. Create a PostgreSQL database.
2. Apply SQL files in strict numeric order (`0001` -> `0006`) using your migration tool or `psql`.

Key table purposes:
- `users`: verified accounts only.
- `pending_registrations`: pre-verification account state.
- `consumed_verification_tokens` and `issued_verification_tokens`: one-time verify determinism and expiry evidence.
- `sessions`: FK target for memory source session compatibility.
- `memory_entries`: long-term memory facts/preferences/summaries.

## 8. Running the service
From repo root:

```bash
cd auth_memory_service
go mod tidy
# apply migrations 0001..0006 to your POSTGRES_DSN database
go run ./cmd/server
```

## 9. Running tests
Validated test workflow:

```bash
cd auth_memory_service
GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache go test ./...
```

Optional targeted verify-race stress command used during blocker verification:

```bash
cd auth_memory_service
for i in $(seq 1 30); do
  GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache \
    go test ./internal/service -run '^TestVerifyConcurrentRace$' -count=1
done
```

## 10. Validation summary
- Auth validation: passed.
- Memory validation: passed.
- Full validation: passed.
- Final status: **READY FOR HANDOFF** / ready for PR preparation.

## 11. Known non-blocking notes
- Auth pending retry refresh remains slightly looser than strict “pending + unexpired” gating.
- Memory tests do not live-integration-assert Redis key/TTL against real Redis.
- Memory auth tests do not explicitly enumerate every internal-key endpoint separately.

## 12. Final status
This implementation is validated, no blocking issues remain, and the service is ready for handoff / PR preparation.
