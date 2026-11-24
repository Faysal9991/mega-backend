# Greenfield Implementation Roadmap

Date: 2026-11-24 (Asia/Dhaka)

## Authority and current scope

The owner's greenfield instruction and current `AGENTS.md` supersede the earlier migration framing. Approved requirements define new behavior; Django is optional read-only reference evidence. No legacy schema, API adapter, data import or Python runtime dependency is planned. `docs/migration/` contains superseded historical notes.

Current authorized task: Step 5 Health & Readiness only. Step 4 scoped review is complete at c875d876e3782b9cd672bf092c6ce57febe645b5: the previous Step 4 baseline passed full checks; the completion-logging fix passed source review, focused TCP regression tests, HTTP/startup race tests, vet and formatting/diff checks. This is not production-performance approval. Step 6 onward remains unauthorized. Step 3 review status is not changed by this scoped approval.

## Initial architecture inspection (historical)

- Git is initialized on `master` but has no commits or tracked files. Preserve existing untracked work; no commit or push is authorized.
- `go.mod` declares `github.com/Faysal9991/biho-live-backend`, Go `1.26.3`; installed toolchain reports `go1.26.3 darwin/arm64`.
- `cmd/api/main.go` prints a startup message. There is no HTTP server, runtime configuration, logging setup, database connection, business module or endpoint.
- Existing layout provides empty directories for platform, modules, contracts, migrations, queries, tests and deployment artifacts. Empty directories do not establish implementation and Git will not preserve them alone.
- `.gitignore` excludes common local secrets, build outputs and data; `.env.example` remains eligible for tracking. This is hygiene, not a secret-scanning guarantee.
- There are no existing dependency pins beyond Go, sqlc configuration/generated queries, Goose migrations, OpenAPI contracts, CI workflows or executable tests.
- Reference source is unavailable in this workspace. This does not block independent architecture or foundation work; relevant business behavior remains unverified.

## Execution sequence and gates

Each row requires owner authorization before execution. Step 5 is authorized; later capabilities must not be bundled into it.

| Step | Scope | Acceptance evidence |
| --- | --- | --- |
| 0 | Foundation inspection and architecture | Baseline prepared; owner authorized Step 1 using this baseline |
| 1 | Typed runtime configuration | Implemented and source-level architectural review approved by owner at bf409ff; test evidence recorded separately; no production behavior claim |
| 2 | Structured logging | Implemented; owner source-level architectural approval recorded against 7dc9d97. Reported tests and production behavior are separate evidence |
| 3 | HTTP lifecycle and middleware | Implemented with Chi v5.5.0, constructor-injected transport, request IDs/logging/recovery, resource limits and bounded shutdown. Final verification recorded in FOUNDATION_VERIFICATION.md; stop for architectural review |
| 4 | Response/error/validation foundation | Scoped owner review complete at c875d876; previous baseline full checks and fix-specific review evidence are distinguished in FOUNDATION_VERIFICATION.md; no production-performance approval |
| 5 — current | Health & readiness | Implemented for review: explicit atomic lifecycle, operational GET/HEAD probes independent of ordinary admission, no-store/minimal JSON, startup/drain/error contracts and deterministic tests; final evidence in FOUNDATION_VERIFICATION.md |
| 6 | PostgreSQL foundation | Pin pgx/Goose/sqlc versions after compatibility verification; isolated Compose DB, bounded pool, migration upgrade checks, generated-query consistency and cancellation tests |
| 7 | Redis, when justified | Explicit use case, bounded clients/memory/TTLs, eviction separation, timeout and outage tests; no durable financial authority |
| 8 | Identity | Approved login/session/verification requirements and permission policy before implementation; abuse, token revocation and recovery tests |
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
