# Auth + Memory Service

## 1. Overview
This service implements the delivered **User/Auth + Memory** scope for the teaching-agent system.


```
auth_memory_service/
├── .env.example
├── .gitignore
├── README.md
├── MVP_BASELINE_SUMMARY.md
├── go.mod
├── go.sum
├── server
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── contract/
│   │   ├── codes.go
│   │   └── response.go
│   ├── http/
│   │   ├── router.go
│   │   ├── handlers/
│   │   │   ├── auth_handler.go
│   │   │   ├── auth_handler_test.go
│   │   │   ├── memory_handler.go
│   │   │   └── memory_handler_test.go
│   │   └── middleware/
│   │       └── auth_middleware.go
│   ├── infra/
│   │   ├── extractor/
│   │   │   └── extractor.go
│   │   ├── jwt/
│   │   │   └── token_manager.go
│   │   └── mailer/
│   │       └── mailer.go
│   ├── model/
│   │   ├── consumed_verification_token.go
│   │   ├── issued_verification_token.go
│   │   ├── memory_entry.go
│   │   ├── pending_registration.go
│   │   ├── session.go
│   │   ├── user.go
│   │   └── working_memory.go
│   ├── repository/
│   │   ├── auth_repository.go
│   │   ├── memory_repository.go
│   │   └── working_memory_repository.go
│   ├── service/
│   │   ├── auth_service.go
│   │   ├── auth_service_test.go
│   │   ├── memory_service.go
│   │   └── memory_service_test.go
│   └── util/
│       ├── id.go
│       ├── time.go
│       └── token.go
└── migrations/
    ├── 0001_users.sql
    ├── 0002_pending_registrations.sql
    ├── 0003_consumed_verification_tokens.sql
    ├── 0004_issued_verification_tokens.sql
    ├── 0005_sessions.sql
    └── 0006_memory_entries.sql

```

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
- The extractor is specialized for teacher lesson/PPT requirement-collection dialogue and keeps session-scoped lesson requirements in working memory / summary unless they are clearly durable teacher facts or preferences.
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
- The Phase 2 hybrid extractor keeps the public extract contract unchanged while internally normalizing `TeachingElements` and merging them into Redis working memory during the existing extract flow.
- When enabled, the extractor uses a real DeepSeek chat-completions client behind the internal `LLMClient` seam and still falls back deterministically to rules on transport, timeout, or validation failure.

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
- Extractor:
  - `EXTRACTOR_LLM_ENABLED` (default `false`)
  - `EXTRACTOR_LLM_MODEL` (default `deepseek-chat`)
  - `EXTRACTOR_LLM_TIMEOUT_MS` (default `2000`)
  - `EXTRACTOR_MAX_TURNS` (default `16`)
  - `DEEPSEEK_API_KEY` (required only when LLM is enabled)
  - `DEEPSEEK_BASE_URL` (default `https://api.deepseek.com`)

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

## 10. DeepSeek smoke test
Manual provider verification with a real key:

1. Set env vars:
   - `EXTRACTOR_LLM_ENABLED=true`
   - `DEEPSEEK_API_KEY=<real key>`
   - optional: `DEEPSEEK_BASE_URL=https://api.deepseek.com`
   - optional: `EXTRACTOR_LLM_MODEL=deepseek-chat`
   - optional: lower `EXTRACTOR_LLM_TIMEOUT_MS` to force timeout fallback testing
2. Run the service:
   - `cd auth_memory_service`
   - `go run ./cmd/server`
3. Trigger extract with an internal-key request:
```bash
curl -X POST http://localhost:9300/api/v1/memory/extract \
  -H "Content-Type: application/json" \
  -H "X-Internal-Key: $INTERNAL_KEY" \
  -d '{
    "user_id": "user_001",
    "session_id": "sess_manual_001",
    "messages": [
      {"role": "user", "content": "Please make a PPT on Newtons laws for grade 8 students. The teaching goals are to connect force and motion, the key difficulty is friction misconceptions, it should fit into 40 minutes, and across my lessons I prefer clean minimalist slides."},
      {"role": "user", "content": "Use the uploaded textbook PDF only for diagrams and keep the lesson flow as concept first, examples second, quiz third."}
    ]
  }'
```
4. Expected outcomes:
   - success: response is still the same public wrapper; durable facts/preferences may be returned, summary is richer, and working memory for the session contains normalized `extracted_elements`
   - auth failure: provider call fails internally, service still falls back to rules, and startup/runtime logs show the DeepSeek failure reason
   - timeout: with a very small `EXTRACTOR_LLM_TIMEOUT_MS`, the provider call fails and rule fallback still returns a valid extract response
   - fallback-to-rules: any invalid provider output or transport failure should still return contract-safe extract results without exposing provider internals in the API

## 11. Validation summary
- Auth validation: passed.
- Memory validation: passed.
- Full validation: passed.
- Final status: **READY FOR HANDOFF** / ready for PR preparation.

## 12. Known non-blocking notes
- Auth pending retry refresh remains slightly looser than strict “pending + unexpired” gating.
- Memory tests do not live-integration-assert Redis key/TTL against real Redis.
- Memory auth tests do not explicitly enumerate every internal-key endpoint separately.

## 13. Final status
This implementation is validated, no blocking issues remain, and the service is ready for handoff / PR preparation.
