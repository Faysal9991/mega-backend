# Greenfield Implementation Roadmap

Date: 2026-11-24 (Asia/Dhaka)

## Authority and current scope

The owner's greenfield instruction and current `AGENTS.md` supersede the earlier migration framing. Approved requirements define new behavior; Django is optional read-only reference evidence. No legacy schema, API adapter, data import or Python runtime dependency is planned. `docs/migration/` contains superseded historical notes.

Current authorized task: Step 1, typed runtime configuration only, including startup integration, isolated tests and documentation. Architecture preparation is the completed preceding task. Steps 2 onward and all business modules remain outside current authorization. See `architecture/RUNTIME_CONFIGURATION.md` and the Step 1 evidence in `architecture/FOUNDATION_VERIFICATION.md`.

## Initial architecture inspection (historical)

- Git is initialized on `master` but has no commits or tracked files. Preserve existing untracked work; no commit or push is authorized.
- `go.mod` declares `github.com/Faysal9991/biho-live-backend`, Go `1.26.3`; installed toolchain reports `go1.26.3 darwin/arm64`.
- `cmd/api/main.go` prints a startup message. There is no HTTP server, runtime configuration, logging setup, database connection, business module or endpoint.
- Existing layout provides empty directories for platform, modules, contracts, migrations, queries, tests and deployment artifacts. Empty directories do not establish implementation and Git will not preserve them alone.
- `.gitignore` excludes common local secrets, build outputs and data; `.env.example` remains eligible for tracking. This is hygiene, not a secret-scanning guarantee.
- There are no existing dependency pins beyond Go, sqlc configuration/generated queries, Goose migrations, OpenAPI contracts, CI workflows or executable tests.
- Reference source is unavailable in this workspace. This does not block independent architecture or foundation work; relevant business behavior remains unverified.

## Execution sequence and gates

Each row requires owner authorization before execution. Step 1 is now authorized; later capabilities must not be bundled into it.

| Step | Scope | Acceptance evidence |
| --- | --- | --- |
| 0 | Foundation inspection and architecture | Baseline prepared; owner authorized Step 1 using this baseline |
| 1 — current | Typed runtime configuration | Implemented; bounded typed values, required production/staging identity/bind settings, safe errors and isolated tests. Actual verification is recorded in `architecture/FOUNDATION_VERIFICATION.md`; stop for architectural review |
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

## Step 1 repository consistency

Inspection baseline: commit `55b4db29977d6c646bd41955bacb14ac94ce47a2`. Git now has this initial commit. At task start, the only working-tree change was the owner's correction of `go.mod` to `github.com/Faysal9991/mega-backend`; it was preserved. New imports use that module path. The former repository identity in the historical inspection above is not the current Go module.

Configuration uses the standard library only. HTTP settings are loaded but not enforced by a server yet. No subsequent roadmap capability, dependency or infrastructure was introduced. Next proposed step is structured logging, only after architectural review and explicit authorization.

## Stop conditions and deferred work

Stop after reporting this task. Do not implement unresolved financial or permission rules. Media/LiveKit, dynamic server registration, infrastructure provisioning/autoscaling, Flutter, admin UI, historical data import, legacy API compatibility and production cutover remain deferred. Stateless design and multi-instance test planning belong to the architecture now; actual scaling deployment does not.
