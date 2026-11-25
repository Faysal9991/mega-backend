# Greenfield Implementation Roadmap

Date: 2026-11-24 (Asia/Dhaka)

## Authority and current scope

The owner's greenfield instruction and current `AGENTS.md` supersede the earlier migration framing. Approved requirements define new behavior; Django is optional read-only reference evidence. No legacy schema, API adapter, data import or Python runtime dependency is planned. `docs/migration/` contains superseded historical notes.

Current authorized task: Step 8 identity/authentication only, per the owner's concrete Google-only signup, password/alias, session/restriction and durable abuse-control rules. Preserve scoped Step 7 optional-cache architectural acceptance at 51514aaa567e4a252732f1f0494261f63ca54d20; other review statuses remain unchanged. Implementation and acceptance plan: docs/architecture/IDENTITY_AUTH.md and ADR 0008. Step 9 onward and deferred business/infrastructure work remain unauthorized.

## Initial architecture inspection (historical)

- Git is initialized on `master` but has no commits or tracked files. Preserve existing untracked work; no commit or push is authorized.
- `go.mod` declares `github.com/Faysal9991/biho-live-backend`, Go `1.26.3`; installed toolchain reports `go1.26.3 darwin/arm64`.
- `cmd/api/main.go` prints a startup message. There is no HTTP server, runtime configuration, logging setup, database connection, business module or endpoint.
- Existing layout provides empty directories for platform, modules, contracts, migrations, queries, tests and deployment artifacts. Empty directories do not establish implementation and Git will not preserve them alone.
- `.gitignore` excludes common local secrets, build outputs and data; `.env.example` remains eligible for tracking. This is hygiene, not a secret-scanning guarantee.
- There are no existing dependency pins beyond Go, sqlc configuration/generated queries, Goose migrations, OpenAPI contracts, CI workflows or executable tests.
- Reference source is unavailable in this workspace. This does not block independent architecture or foundation work; relevant business behavior remains unverified.

## Execution sequence and gates

Each row requires owner authorization before execution. Step 8 identity is authorized; later capabilities must not be bundled into it.

| Step | Scope | Acceptance evidence |
| --- | --- | --- |
| 0 | Foundation inspection and architecture | Baseline prepared; owner authorized Step 1 using this baseline |
| 1 | Typed runtime configuration | Implemented and source-level architectural review approved by owner at bf409ff; test evidence recorded separately; no production behavior claim |
| 2 | Structured logging | Implemented; owner source-level architectural approval recorded against 7dc9d97. Reported tests and production behavior are separate evidence |
| 3 | HTTP lifecycle and middleware | Implemented with Chi v5.5.0, constructor-injected transport, request IDs/logging/recovery, resource limits and bounded shutdown. Final verification recorded in FOUNDATION_VERIFICATION.md; stop for architectural review |
| 4 | Response/error/validation foundation | Scoped owner review complete at c875d876; previous baseline full checks and fix-specific review evidence are distinguished in FOUNDATION_VERIFICATION.md; no production-performance approval |
| 5 | Health & readiness | Implemented for review: explicit atomic lifecycle, operational GET/HEAD probes independent of ordinary admission, no-store/minimal JSON, startup/drain/error contracts and deterministic tests; final evidence in FOUNDATION_VERIFICATION.md |
| 6 | PostgreSQL foundation | Implemented for review: owner testing-server bootstrap, isolated dev/test roles, bounded direct pgxpool, startup identity/schema checks, direct Goose migrations/locking and sqlc consistency. Actual checks in FOUNDATION_VERIFICATION.md; no proxy/deployment or production-performance approval |
| 7 | Redis optional disposable cache | Implemented for review: typed TLS-only configuration, bounded shared client/workers/sockets, optional startup diagnostic, Get/Set/Delete with versioned namespaces/size/TTL bounds. Focused source/options audit, real TLS Redis operations/cleanup and API lifecycle in all three modes passed; one admission-release defect fixed with a failing-before regression. Bounded benchmark and full check evidence in FOUNDATION_VERIFICATION.md; scoped optional-cache architectural acceptance recorded at 51514aaa; no durable authority |
| 8 — current | Identity | Owner-approved rules and concrete contracts/defaults in IDENTITY_AUTH.md; migration 2, Google-only signup, aliases/password proofs, opaque rotating sessions, installation restrictions and PostgreSQL abuse state; actual verification in STEP8_VERIFICATION.md; architectural review pending |
| 9 | Profiles and permissions | Approved resource ownership/roles, contract fixtures and unauthorized-access tests |
| 10 | Wallet | Approved units, limits, rounding, eligibility and accounting policies; reconciliation, concurrency, retry and crash-recovery acceptance |
| 11 | Remaining business capabilities | Feature-specific requirements, dependencies, API/schema review and acceptance per coverage ID; order refined after discovery |

Runtime/dependency versions other than the observed Go version are not selected in this task. Check official documentation and compatible tooling before adding and pinning them. Introduce River when durable jobs first require it; do not add unused dependencies now.

## Step 1 repository consistency

Inspection baseline: commit `55b4db29977d6c646bd41955bacb14ac94ce47a2`. Git now has this initial commit. At task start, the only working-tree change was the owner's correction of `go.mod` to `github.com/Faysal9991/mega-backend`; it was preserved. New imports use that module path. The former repository identity in the historical inspection above is not the current Go module.

Configuration uses the standard library only. HTTP settings are loaded but not enforced by a server yet. Step 1 approval was supplied by the owner at reviewed commit bf409ff738a3e61bb535b06e99cab9102005f594.

## Step 2 verification plan and handoff (historical)

Review constructor/configuration/startup behavior, With/WithGroup redaction, nested and arbitrary values, reserved/duplicate keys, injection, UTC/JSON output and concurrency. Correct only Step 2 defects, preserve Step 1 regression checks, then run all owner-requested uncached commands and record actual outcomes. No external dependencies, runtime globals or asynchronous infrastructure are introduced.

Final review tightened duplicate-key handling to first accepted occurrence per JSON object and retained sensitive context for invalid group names. Security tests inspect JSON tokens rather than relying on map decoding. `FND-003` tracks implementation and local evidence; source-level review was subsequently approved by the owner against the inspected 7dc9d97 baseline. That approval authorizes the separate Step 3 work below.

## Step 3 implementation plan and gate

1. Inspect instructions, source, approved configuration/logging/API standards and Git state at 7dc9d97; verify the stable Chi tag and compatibility before pinning.
2. Add only validated bounded header, cooperative request-timeout and admission settings; preserve existing defaults and regression scenarios.
3. Implement constructor-injected lifecycle and centralized routing, then request ID, completion logging, recovery and resource middleware in that order.
4. Verify isolated test routes, real loopback transport, cancellation, concurrency, body/header limits, partial panics, bind failure and graceful/forced drain. Keep real executable routing empty.
5. Complete docs, module integrity/format/build/test/race/vet/coverage checks, report actual evidence and stop for review. No Step 4 response engine or health endpoint.

During implementation a loopback lifecycle test exposed Chi's empty-mux middleware bypass. A dedicated infrastructure mount fixes it without fake routes; explicit empty-router regression coverage remains. The new request timeout cooperatively cancels context, and force-close does not claim to terminate arbitrary Go code. Deployment capacity and performance are unmeasured. FND-004 tracks implementation, local evidence and the pending architectural-review gate.

Step 4 was subsequently authorized explicitly; that implementation is recorded below.

## Stop conditions and deferred work

Stop after reporting this task. Do not implement unresolved financial or permission rules. Media/LiveKit, dynamic server registration, infrastructure provisioning/autoscaling, Flutter, admin UI, historical data import, legacy API compatibility and production cutover remain deferred. Stateless design and multi-instance test planning belong to the architecture now; actual scaling deployment does not.

## Step 4 scope and acceptance gate

1. Inspect clean Git/HEAD and existing helpers, HTTP/logging tests and contracts; reuse the helpers and shared request correlation.
2. Complete safe envelopes, typed application errors, bounded strict JSON/validation/pagination and integrate existing middleware without changing writer capabilities or lifecycle.
3. Test contracts and failure boundaries using isolated test-only routes; preserve existing regressions. Add common OpenAPI schemas and fixtures without runtime endpoints.
4. Format, verify modules/build/tests/coverage/race/vet and OpenAPI; record actual evidence, limitations and review status. Stop after Step 4.

Baseline: 0caa8e2c752e8f1bb0502ffe76c00ac882b98aba, initially clean. Existing helper packages were present but unintegrated and untested. FND-005 is implemented with local acceptance evidence, awaiting architectural review. No new module dependency, business rule, schema or compatibility adapter is introduced. The next proposed capability is Step 5 health endpoints, only after explicit authorization.

Step 4 review follow-up at 5598a2b4ec2e8f89af2f2dd35c7c2fbeddfdef35: correct completion logging to distinguish completed/aborted execution and represent an unsent final status as 0. Focused loopback and regression evidence is recorded in FOUNDATION_VERIFICATION.md. This finding fix does not change review approval or authorize Step 5.


## Step 5 plan and acceptance gate

1. Inspect Git, lifecycle, middleware/response contracts and prior review evidence; preserve existing roadmap/feature-matrix edits.
2. Inject one initially unready atomic lifecycle into the router and server; mark ready at Serve entry with a bound listener after initialization, and terminally unready before shutdown/close or on Serve exit.
3. Add only GET/HEAD /health/live and /health/ready with shared protections, JSON/no-store/405 semantics and no ordinary admission slot or external checks.
4. Verify readiness transitions, concurrent access, saturated admission, already-accepted draining probes, graceful/forced shutdown and existing abort logs. Add a separate operational OpenAPI document referencing unchanged common schemas.
5. Complete requested checks and documentation, then stop for Step 5 review. No Step 6 implementation is authorized.

Step 5 baseline: c875d876e3782b9cd672bf092c6ce57febe645b5. Initial owner edits to this roadmap and the feature matrix were preserved. FND-006 is implemented pending architectural review. The next proposed capability is PostgreSQL foundation (Step 6), only after explicit authorization.


## Step 6 secure-connectivity gate

Owner authorization covers the existing Coolify PostgreSQL testing endpoint, separate biho_live_dev/biho_live_test databases and dedicated users/migration roles, with the bootstrap password read only from Git-ignored .env. Credentials must never traverse an unencrypted public connection. Destructive integration tests must never target postgres or development.

At inspected HEAD e99d4fed2176ae3899eedd90e81e76823d98be86, a credential-free PostgreSQL SSLRequest received N (TLS refused). The .env ignore rule was confirmed; its password was not read or transmitted. No SQL, database/role creation, migrations or tests against the remote server were executed. Step 6 is blocked before implementation/version selection. Enable server-side PostgreSQL TLS and supply a trusted, identity-valid certificate (plus a trusted CA/DNS name if needed) before retrying with verification enabled. Do not weaken verification or alter server infrastructure as a workaround. Detailed evidence is in FOUNDATION_VERIFICATION.md; FND-007 remains blocked.


## Step 6 resumption plan — encrypted testing exception

The owner now permits sslmode=require without CA/identity verification only for the temporary development/testing environment. sslmode=disable remains forbidden; production must enforce verify-full. A credential-free probe now successfully negotiates TLS 1.3, so the earlier SSLRequest refusal is historical. Authentication must succeed before remote database/role bootstrap.

Execution order: locate the designated Git-ignored secret file without exposing its contents; authenticate over TLS; inspect server version/capabilities and existing target names without destructive changes; create isolated biho_live_dev and biho_live_test with separate runtime and migration roles; pin compatible pgx v5/Goose/sqlc and implement bounded pool, startup initialization and migration/query foundation; verify upgrades, permissions, cancellation and generation consistency exclusively in the dedicated test database; run required Go checks, document actual results and stop at Step 6. No schema or business rule outside foundation is authorized.

Current resumption preflight: TLS 1.3 passes with identity verification intentionally disabled under owner authorization. The workspace currently contains .env.example only; .env is absent and no Git-ignored environment file was found by the scoped filename inventory. No password has been read and authentication/database creation has not been attempted. The credential file path or creation is required before the next step.

Credential-file follow-up: a local .env template was created with owner-only mode 0600, the approved bootstrap connection settings and an empty PGPASSWORD. Git ignores the file. The owner must populate the password locally before authentication can be attempted. No password is requested through chat.


## Step 6 implementation gate and handoff

Password availability resolved the historical authentication blocker. The owner additionally requires direct pgxpool now, future transaction-pool compatibility without deploying PgBouncer, and direct Goose connections. Authenticated PostgreSQL 18.7/TLS 1.3; target names absent; created two isolated databases/four roles without changing existing databases or administrator credentials. FND-007 is implemented with local/remote testing acceptance evidence, pending architectural review.

Application startup loads explicit runtime settings, initializes a bounded pool and validates authenticated identity/app schema before binding; cleanup follows HTTP drain. Test environment selection and destructive migration guards reject default/development targets. Development migrations are upgrade-only; integration resets occur only on biho_live_test. Module/schema/API scope remains foundation only. See POSTGRESQL_FOUNDATION.md, ADR 0006 and verification evidence. Stop after Step 6; Redis or any business module requires separate authorization and justification.


## Step 6 two-gap follow-up plan and gate

At cf296a9ff0f6d27042a4f961178a6ea4e5cfc9a8, the working tree was clean. Preserve existing behavior and approved temporary TLS rules. Add a small BeginTx consumer interface/helper with context propagation, one success-only commit, independent bounded rollback, error classification, panic propagation and no retry. Add one injected pool availability checker with timeout/cache/coalescing/waiter bounds; combine it with the running lifecycle without changing terminal shutdown state. Verify unit/HTTP/concurrent and isolated PostgreSQL transaction/outage/recovery/shutdown cases, execute full/race/integration checks, update evidence and stop for review. No schema, credentials, runtime TLS policy or infrastructure mutation is needed beyond named isolated test fixtures.


## Step 7 implementation and acceptance gate

Baseline 63992e40ae1ccf28dddc7dea1948ac1c65635a2d, initially clean. Inspect pinned stable go-redis semantics, add separate typed optional configuration and revalidation, enforce TLS/resource/retry/operation policy, wire one optional client after PostgreSQL initialization and before serving, close after HTTP drain, and provide only disposable Get/Set/Delete. Use local controlled TLS doubles for disruptive tests and unique tracked TTL keys on the authorized shared endpoint for explicitly opted-in real verification. Preserve previous review statuses, execute full required checks, and stop at Step 7. No business-data cache or future module is authorized.

FND-008 tracks implementation with the real-integration gate visible. Saved .env had no REDIS_URL/REDIS_* settings at inspection; the missing local file setting was requested without asking for credentials through chat. A credential-free TLS preflight reached the server but default certificate/IP verification failed; no bypass/authentication was attempted. Real verification requires the owner-supplied URL plus verified trust/identity or an explicit development/test bypass setting. Do not infer that a missing/failed real test passed.


### Step 7 focused acceptance audit — 2026-11-25

Audited baseline 8f32219 with an initially clean tree. Fixed completed-operation admission release before publishing its result; the new single-slot sequential regression failed before the fix. Added concurrent close/operation, timeout/panic admission regressions, opt-in actual API lifecycle tests and bounded benchmarks. Real Redis operations and tracked-key cleanup now pass; the earlier missing-credential blocker is superseded. Actual API/PostgreSQL/Redis startup, health/readiness and context-driven graceful shutdown pass enabled, disabled and locally unavailable modes. Full default/race/build/vet/module/sqlc checks pass. Detailed PASS/NOT TESTED matrix, measurements and limits are appended to FOUNDATION_VERIFICATION.md. FND-008 remains implemented pending owner architectural acceptance. Stop at Step 7; no Step 8 authorization, commit, push or deployment.

### Step 7 deadline classification follow-up — baseline 44a2f5e

Scoped fix only: preserve published context errors or an already-reached absolute deadline at cache admission/completion, without converting genuinely earlier socket timeouts. Deterministic unpublished-deadline regressions failed before the fix; final focused race verification passed 100 repetitions, followed by full uncached tests/race, build, vet, formatting and diff checks. Evidence is appended to FOUNDATION_VERIFICATION.md. Existing admission ordering, limits, TLS and optional behavior are preserved; FND-008 remains implemented for owner review, no Step 8 authorization.


### Step 8 authorized plan and gate

Baseline 51514aaa567e4a252732f1f0494261f63ca54d20. Existing roadmap/feature-matrix deadline-follow-up edits were present and preserved. Only Step 7 optional-cache architectural acceptance is recorded; all other scoped approvals/pending reviews remain intact. Document identity schema/contracts/defaults first (IDENTITY_AUTH.md), then implement pure service boundaries, bounded provider/password adapters, generated transactional storage and existing HTTP composition. Execute signed fixtures, Argon2/generic failures, concurrent PostgreSQL signup/refresh/revocation/alias/restriction/cleanup tests; explicit guarded isolated migration suite; real API Redis modes; final complete tool checks/OpenAPI validation. Evidence and limitations go to STEP8_VERIFICATION.md. Stop for Step 8 architectural review; no later modules or infrastructure authorized.

Step 8 implementation and checks completed for review: full uncached/race/build/vet/module/sqlc/OpenAPI checks pass; guarded real migration/identity tests and actual API Redis-mode tests pass. Live Google mobile end-to-end remains NOT TESTED; no fake production verifier. See docs/architecture/STEP8_VERIFICATION.md. Next gate is owner Step 8 architectural review, not Step 9 implementation.
