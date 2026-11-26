# Greenfield Implementation Roadmap

Date: 2026-11-24 (Asia/Dhaka)

## Authority and current scope

The owner's greenfield instruction and current `AGENTS.md` supersede the earlier migration framing. Approved requirements define new behavior; Django is optional read-only reference evidence. No legacy schema, API adapter, data import or Python runtime dependency is planned. `docs/migration/` contains superseded historical notes.

Current authorized task (2026-11-25): the cohesive LiveKit / agency / verified-activity backend batch at main baseline 645c75c, per the owner's attached request. LiveKit backend implementation is authorized within this batch; provisioning/clustering, frontend/UI and unrelated modules remain deferred. Preserve earlier scoped acceptances and pending reviews, including Wallet independent review. Current plan: architecture/LIVE_AGENCY_IMPLEMENTATION_PLAN.md; current evidence: architecture/LIVE_AGENCY_VERIFICATION.md. Earlier phase stop statements below are historical and are superseded only within this explicit scope.

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

### Step 8 scoped review corrections — baseline 55ead83f

Bound post-Open identity schema checks with existing DB_CONNECT_TIMEOUT, propagate identity response write failures through the existing abort pattern, and correct OpenAPI password byte assertions without changing runtime policy. Add deterministic startup/response and shared Unicode contract regressions; correct PostgreSQL version/integration serialization guidance. Targeted repeated race, full uncached tests/race/build/vet/OpenAPI/sqlc/format/diff and a read-only isolated startup integration check passed; evidence is appended to STEP8_VERIFICATION.md. No auth/session/route/Step 7 behavior change; architectural review remains pending and no later phase is authorized.


### Step 9 authorized plan and gate

Inspection baseline: `069175538e11575d5c5b23f97b6541cc3ca4e7c7`; working tree initially clean. Owner explicitly authorizes Step 9 and supplies scoped Step 8 architectural/code-review acceptance against that baseline. This supersedes the pending source-review status only; live Google mobile E2E and production verification remain pending, and all other review statuses are preserved.

1. Specify profile ownership, populated schema evolution, exact permission matrix, minimal routes and operator bootstrap in PROFILES_AUTHORIZATION.md / ADR 0009.
2. Implement profile-owned transaction initialization/projections, generated SQL, scoped PostgreSQL authority, protected owner bootstrap, owner-only roles/assignments and audited Identity-owned restriction changes.
3. Verify Unicode/strict HTTP/contracts; isolated populated upgrades/rollback and concurrent bootstrap; permission revocation/mutation ordering across instances, audit/session rollback, identity regressions and Redis modes.
4. Record actual checks in STEP9_VERIFICATION.md and stop at Step 9 implemented/review pending. No Wallet/later modules, infrastructure, deployment, commit or push.

PRO-001, ADM-001 and ADM-002 cover this bounded Step 9 scope. Avatar upload and broader support/admin operations remain deferred. No production performance measurement or acceptance is inferred. Independent Step 9 review is the next gate; Step 10 is not authorized.

Step 9 implementation and actual verification are complete for independent review: all local build/default/race/vet/sqlc/OpenAPI checks and guarded populated migration, identity/authorization, real API Redis-mode and startup timeout suites passed. See docs/architecture/STEP9_VERIFICATION.md for commands, initial fixture corrections, final results and limitations. PRO-001/ADM-001/ADM-002 remain implemented, review pending. No later phase is authorized.


### Step 9 directory response-budget review correction — baseline 442a8d8a

Scope: only the supplied admin-directory response-budget finding, against clean HEAD `442a8d8ad6b3410bda2aed08c91561e56d364b0f`. Reproduce the escaped 100-user envelope failure, fit complete leading rows within the existing shared cap at the HTTP boundary, preserve requested limit/cursor/permission/SQL behavior, then verify cursor traversal and existing regressions. Concrete validation and compatibility semantics are recorded in STEP9_VERIFICATION.md and PROFILES_AUTHORIZATION.md. No schema, authorization, bootstrap, session or other module change; no later phase authorization. ADM-001 remains implemented / independent review pending; all other review statuses remain unchanged.

Directory review correction verification passed: full local build/tests/race/vet/sqlc/OpenAPI/format/diff checks; regression reproduced the baseline HTTP 500, then verified bounded complete-row pagination through all users without skips. Real DB/Redis checks were not rerun for this HTTP-only change. Avatar and other not-yet-supplied review findings remain pending per owner follow-up; Step 9 independent review is not complete.


### Authorized cohesive financial batch — baseline 442a8d8a

The current owner request explicitly authorizes remaining Step 9 findings plus the single-wallet engine, diamond transfers, gated gifting policies, review-only withdrawals, minimal agency classification/activity eligibility and narrowly scoped financial administration. This supersedes the earlier stop-at-Step-9 gate only for this batch. Initial HEAD was `442a8d8ad6b3410bda2aed08c91561e56d364b0f`; existing uncommitted directory-budget code/contracts/docs were preserved. No prior review/production approval is inferred.

Compact plan: close the exact Step 9 escaping/newline/verification-text findings; document single-owner accounting/policy/provider boundaries; implement immutable ledger/policies and actor idempotency with shared security transaction; compose bounded APIs and minimum agency support; run pure/HTTP/race/contracts plus serial guarded PostgreSQL upgrade/concurrency/reconciliation and optional Redis-mode regression; consolidate actual results in FINANCIAL_VERIFICATION.md. Owner-authorized parallel workers own disjoint validator/transport, pure activity and integration tests; one root owns shared migrations/sqlc/composition.

Implemented capabilities are tracked by WAL-001, TRF-001, GFT-001, WDR-001, ADM-003 and AGY-003. Status remains implemented / independent review pending, never production-ready. No economy policy is seeded active. Unsupported VAT/fiat settlement, absent transfer/gift policy, payout providers and missing authoritative host activity remain explicitly gated. The final owner decision of 2026-11-25 (ADR 0011) supersedes the former previous-month proposal: activity uses the current business calendar month through check time, with default 15 distinct days / 120 verified minutes and immediate qualification. Timezone and immutable audio/video/shared policy configuration remain mandatory; no policy is silently activated. The monthly window resets without rewriting history, and acceptance rechecks eligibility under the financial transaction. Follow-up checks are recorded separately in FINANCIAL_VERIFICATION.md; independent review remains pending. Existing global security locking is unchanged. No frontend, LiveKit/activity collection, provider payout, infrastructure, commit, push or deployment. Next gate is independent review of the whole authorized batch, including its configuration-dependent/untested production boundaries.

Final batch verification: uncached full local tests/race/build/vet, module checks, sqlc/OpenAPI/format/diff and all guarded PostgreSQL plus Redis cache/API/startup regressions passed. Actual commands, initial failures/corrections, synthetic timings and limitations are consolidated in FINANCIAL_VERIFICATION.md. Independent review remains the next gate; no production readiness, provider settlement or load/capacity acceptance is inferred.

## Authorized LiveKit / agency backend batch (2026-11-25)

Owner request authorizes agency workflows, audio/video room management, signed provider callbacks, verified activity and Wallet eligibility-source integration beyond the historical deferral gate. Baseline 645c75c. Implementation: migrations 5–8, internal/modules/live, typed optional LiveKit composition, scoped catalog/contracts and tests. Plan: architecture/LIVE_AGENCY_IMPLEMENTATION_PLAN.md; design: ADR 0012 and architecture/LIVE_AGENCY.md; actual verification and remaining gates: architecture/LIVE_AGENCY_VERIFICATION.md. Status: implemented for independent review; do not infer Wallet approval. Stop after this backend batch. Next gate: resolve any remaining policy dependencies, independent architecture/security review and controlled real-provider E2E before production consideration. Provisioning/clustering, frontend/UI, uploads, notifications, games and commercial settlement remain outside this scope.

Owner lifecycle clarification (ADR 0013) is implemented in migration 8 and the central association writer: leave clears current context/history remains; another agency or a join after leave starts day/minute calculation from zero. Same-agency classification changes preserve context. Targeted context/source/runtime-immutability regression and schema 1–8 checks supplement the full batch suite. Independent review and provider E2E remain the next gate.

## Owner-authorized next backend delivery batch — c749cec

Scope/plan: architecture/DELIVERY_BATCH_PLAN.md; decisions: ADR 0014; design: architecture/DELIVERY_BATCH.md; consolidated checks: architecture/DELIVERY_BATCH_VERIFICATION.md. Implement catalog/live gift outbox via existing Wallet/Live, private validated S3 images/Profile attachments, encrypted FCM bindings/preferences/delivery, minimal scoped admin and cmd/worker. Migration 9 is additive; no active commercial/provider policy seeded. GFT-002/MED-002/NTF-002/ADM-005 and PRO-001 extension remain implemented/review pending. Wallet/Live reviews stay pending. Stop at this backend batch; next gate is independent review, explicit runtime configuration, controlled Garage/FCM/LiveKit data E2E and production-like operational/load checks. No provisioning, UI, deployment, commit or push.

Final migration acceptance: initial schema9 cycle passed; final index-cycle runs failed at startup identity verification / five-minute fixture deadline. Schema9, both predicate indexes and tracked-fixture cleanup were separately verified. Complete final rollback-cycle revalidation remains pending; see architecture/DELIVERY_BATCH_VERIFICATION.md.

## Owner-authorized consolidated implementation/fix batch — e94b765

Plan: architecture/CONSOLIDATED_BATCH_PLAN.md; approved rules: ADR0015; actual evidence/remaining gates: architecture/CONSOLIDATED_BATCH_VERIFICATION.md. Extend existing Access and Live rather than duplicating them; add configurable packages, Wallet-owned full-platform profit/purchases and queued periods, actual ranked discovery and necessary delivery/GC/schema integration. New permission/package/room capabilities remain implemented/review pending until acceptance evidence is complete. Full migration cycle and the final serial regression set are recorded once in the consolidated report; historical failures are not erased. Stop after this backend batch. Next gates: independent architecture/security/financial reviews, controlled actual LiveKit/FCM/Garage client/provider E2E, retention/drain/backup ownership and production-like load tests. No UI, production deployment, infrastructure changes, commit or push.

2026-11-26 consolidated acceptance: complete migration11 up/down/up/fault/concurrency cycle and all eight final serialized PostgreSQL/race package attempts PASS; full local/build/race/vet/sqlc/OpenAPI/secret checks PASS. Two fixture corrections and historical failures remain documented in architecture/CONSOLIDATED_BATCH_VERIFICATION.md. CAP-001, MOD-002, ENT-001, WAL-002, DSC-001 and ROM-001 extension remain implemented with independent review/real provider E2E/production acceptance pending; no blanket verified/production approval.

Focused source-review fix at 62975d5: global live-ban moderation/replay enforcement, owner-approved restricted Room Admin defaults, complete global-lock inventory and controlled synthetic HTTP benchmark. Plan/evidence/proposal: architecture/AUTHORIZATION_SOURCE_REVIEW.md. No lock replacement or expanded feature phase; independent reviews, real E2E and production load remain gates.

Focused review execution complete: migration12/custom-policy preservation, corrected cross-instance global ban/role-policy cases, package financial/ranking regressions, Access and actual API PostgreSQL/race checks PASS; full local checks PASS. Synthetic global-lock/pool contention measured; lock replacement remains a separately reviewed future change. Stop at this fix boundary.
