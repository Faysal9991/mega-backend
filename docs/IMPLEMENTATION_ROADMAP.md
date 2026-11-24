# Greenfield Implementation Roadmap

Date: 2026-11-24 (Asia/Dhaka)

## Authority and current scope

The owner's greenfield instruction and current `AGENTS.md` supersede the earlier migration framing. Approved requirements define new behavior; Django is optional read-only reference evidence. No legacy schema, API adapter, data import or Python runtime dependency is planned. `docs/migration/` contains superseded historical notes.

Current task: inspect the initialized project, verify its existing foundation and prepare system architecture. Implementation of new foundation capabilities and business modules is outside this task. Architecture documents describe intended behavior, not deployed or tested capabilities.

## Current inspection

- Git is initialized on `master` but has no commits or tracked files. Preserve existing untracked work; no commit or push is authorized.
- `go.mod` declares `github.com/Faysal9991/biho-live-backend`, Go `1.26.3`; installed toolchain reports `go1.26.3 darwin/arm64`.
- `cmd/api/main.go` prints a startup message. There is no HTTP server, runtime configuration, logging setup, database connection, business module or endpoint.
- Existing layout provides empty directories for platform, modules, contracts, migrations, queries, tests and deployment artifacts. Empty directories do not establish implementation and Git will not preserve them alone.
- `.gitignore` excludes common local secrets, build outputs and data; `.env.example` remains eligible for tracking. This is hygiene, not a secret-scanning guarantee.
- There are no existing dependency pins beyond Go, sqlc configuration/generated queries, Goose migrations, OpenAPI contracts, CI workflows or executable tests.
- Reference source is unavailable in this workspace. This does not block independent architecture or foundation work; relevant business behavior remains unverified.

## Execution sequence and gates

Each row is a separate future step, requiring owner authorization before execution. Do not bundle later capabilities into the next step.

| Step | Scope | Acceptance evidence |
| --- | --- | --- |
| 0 — current | Foundation inspection and architecture | Document current limitations, ownership, data/transaction boundaries, API standards, feature coverage and verification results; report and stop |
| 1 | Typed runtime configuration | Required settings fail fast, secrets stay out of logs, bounded values validated, isolated tests; no DB or business module |
| 2 | Structured logging | Explicit slog injection, request-safe fields, redaction policy and tests for sensitive fields |
| 3 | HTTP lifecycle and middleware | Pin Chi when introduced; server deadlines/body limits, request IDs, panic handling, shutdown/drain behavior and cancellation verified |
| 4 | Response/error engine | Reviewed OpenAPI common schemas and error registry; response, 204, invalid input and error contract tests |
| 5 | Health endpoints | Separate liveness/readiness semantics; draining/unavailable behavior tested without exposing internals |
| 6 | PostgreSQL foundation | Pin pgx/Goose/sqlc versions after compatibility verification; isolated Compose DB, bounded pool, migration upgrade checks, generated-query consistency and cancellation tests |
| 7 | Redis, when justified | Explicit use case, bounded clients/memory/TTLs, eviction separation, timeout and outage tests; no durable financial authority |
| 8 | Identity | Approved login/session/verification requirements and permission policy before implementation; abuse, token revocation and recovery tests |
| 9 | Profiles and permissions | Approved resource ownership/roles, contract fixtures and unauthorized-access tests |
| 10 | Wallet | Approved units, limits, rounding, eligibility and accounting policies; reconciliation, concurrency, retry and crash-recovery acceptance |
| 11 | Remaining business capabilities | Feature-specific requirements, dependencies, API/schema review and acceptance per coverage ID; order refined after discovery |

Runtime/dependency versions other than the observed Go version are not selected in this task. Check official documentation and compatible tooling before adding and pinning them. Introduce River when durable jobs first require it; do not add unused dependencies now.

## Stop conditions and deferred work

Stop after reporting this task. Do not implement unresolved financial or permission rules. Media/LiveKit, dynamic server registration, infrastructure provisioning/autoscaling, Flutter, admin UI, historical data import, legacy API compatibility and production cutover remain deferred. Stateless design and multi-instance test planning belong to the architecture now; actual scaling deployment does not.
