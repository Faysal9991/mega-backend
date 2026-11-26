# Consolidated capabilities, packages and fixes — verification

Execution dates: 2026-11-25–26 Asia/Dhaka. Baseline e94b765, initially clean; prior delivery implementation is committed in that baseline. No production/development database mutation, infrastructure change, unrelated-session termination, commit, push or deployment. Wallet, LiveKit and financial independent reviews remain PENDING.

## Scope and traceability

- CAP-001: Access resolver/catalog, immutable room policy/CAS, direct grants/denials, immediate PostgreSQL authorization and staff preset API.
- MOD-002 / ROM-001 extension: global moderation, hidden projection/SDK grants, distinct live bans, atomic audio seats/video calls, admission locks, settings and scoped moderation history; durable privilege recovery and lease fencing.
- ENT-001 / WAL-002: arbitrary versioned packages, direct/current entitlements, Wallet-owned purchases and separate immutable platform profit; no new wallet balance writer or fiat settlement.
- DSC-001: actual host-package mobile discovery ranking, availability filters, bounded indexed SQL, actor/revision/time-bound cursors and restart response.
- Existing delivery/profile/Identity/Agency/Wallet modules are reused. Package references extend existing media GC protection; optional Redis/provider behavior and owner-confirmed agency reset/history rules remain.

## Investigation and confirmed fixes

Guarded read-only diagnostic: PASS 1.78s (2.287s package): one database session, zero lock waiters/blocked sessions, zero older open-transaction time; server max_connections=100. No unrelated process/session was terminated or limit changed. This does not establish the cause of historical sanitized failures.

Historical migration errors lost their causes. Migration/pool failures now preserve private inspection while printing only a safe operation and deadline/cancellation/SQLSTATE category. Startup removes a redundant Ping: the existing identity/readiness query proves connectivity in one round trip. The same configured startup deadline/TLS/role checks remain.

The foundation fault-injection fixture enumerates all embedded migrations and tests failure after version11, rather than a stale hardcoded list/version. Its original five-minute scenario deadline remains; guarded cleanup can complete the schema and remove only its exact synthetic ID/name/avatar after proving no auth/financial use. New DDL is grouped; historical migration statements, rollback data guards, connection limits and runtime deadlines are not weakened.

Provider recovery previously completed by operation ID/state alone; a reclaimed lease could be completed by an older attempt. New claim/complete/retry fencing uses a fresh lease token. The private legacy Wallet posting routine is not directly executable by runtime. Ranking invalidation ignores unrelated mute and audience-subscription changes; row limits, snapshot expiration and end-of-query revision/time checks prevent stale pagination claims.

## Actual targeted checks

| Check | Outcome |
| --- | --- |
| Guarded migration/session/limit diagnostic | PASS, figures above |
| Pure capability/package/image-preserving integration edits, HTTP strictness, SDK hidden-token, parser/cursor, schema/tag compile checks | PASS at development checkpoints; final source set below is authoritative |
| Initial new PostgreSQL/race run | Global moderation/hidden/seat/revocation PASS 195.08s; subsequent deny check and package creation returned SERVICE_UNAVAILABLE; cause not proven in that run; total FAIL 263.46s |
| Focused PostgreSQL/race follow-up | PASS 249.62s test / 251.214s package: direct/scoped denies and dynamic policy (68.66s), package expiry/revocation/seat race (66.73s), atomic concurrent purchase/profit (31.37s), host ranking/filters/cursors (47.58s). Later refinements are covered by the final runs below |
| Final full local verification set | PASS: tidy/verify, build, uncached default/race tests, default/tagged vet, integration tag compile, sqlc consistency, all six OpenAPI validators, git diff --check; `/tmp/biho-consolidated-final-local.log` |
| Final serialized PostgreSQL/race acceptance | PASS on final attempts for all eight packages; complete migration cycle and financial reconciliation passed. Two fixture-only failures/corrections are retained below. Log: `/tmp/biho-consolidated-final-postgres.log` |
| Credential/tracked-secret checks | PASS: zero sensitive tracked-file paths; nine ignored configured secret values scanned against tracked/new source with zero matches; zero unformatted Go files; API dependency graph contains no Firebase SDK |

The final foundation scenario passed within its original five-minute context. Complete up/down/up, populated backfill, concurrent re-upgrade, fault-injection rollback, version11, runtime role/schema/TLS checks, pool bounds and cancellation checks passed. The initial final package pass exposed a new test precondition defect: the global-live-ban scenario correctly removed the target from both rooms but rejoined only room2; the later room1 mute assertion received CONFLICT because its target was left. The test now explicitly rejoins its room1 target before checking authorization; production rules and assertions are unchanged. The first pass completed all remaining subtests successfully and scoped cleanup finished (665.84s scenario / 666.456s package). The corrected complete package suite passed once (541.35s scenario / 542.963s package), including the previously interrupted policy/CAS/deny assertions. Remaining packages completed serially; foundation was not unnecessarily repeated. Post-local-set Go edits are limited to documented test fixtures: the restored membership precondition (corrected entire package passed with race detection) and the API assignment context correction below. No runtime Go, SQL or contract changed after the local set. No assertion was weakened to hide initial failures. Safe cause classification was subsequently retained for future errors.

The initial final API suite reached all 18 successful route checks, then failed at `assign(false)` after 122.59s: its shared fixture captured the two-minute setup context for later role changes, while HTTP requests used a separate context. The expired setup context therefore prevented the revocation call. Setup remains bounded to two minutes; each subsequent assignment now has its own 30-second context tied to the test lifecycle, instead of reusing expired setup authority. No runtime/startup/statement timeout or assertion was enlarged. After scoped cleanup, only the API package was rerun: PASS 173.060s package, with the actual outage/revocation scenario PASS 166.69s. The corrected fixture also passed integration-tag vet and redis_integration-tag compile. Passed business suites were not repeated; no unexplained financial reconciliation differences or remaining local test blockers were reported.

## Final PostgreSQL outcomes

All commands used race detection, uncached execution and `-p=1`; no suites overlapped. Times are test-package runtimes, **not endpoint/load benchmarks**.

| Package | Final result | Seconds |
| --- | --- | ---: |
| internal/platform/postgres | PASS, complete migration cycle | 208.353 |
| internal/modules/packages/postgres | PASS, all nine scenarios after fixture correction | 542.963 |
| internal/modules/wallet/postgres | PASS, conserved ledger/final reconciliation | 796.818 |
| internal/modules/live/postgres | PASS, activity reset/main/reconciliation batch | 711.497 |
| internal/modules/access/postgres | PASS, Profile/Access authorization | 224.237 |
| internal/modules/identity/postgres | PASS, Identity security | 146.168 |
| internal/modules/delivery/postgres | PASS, gifting/media/push/dispatcher recovery | 586.789 |
| cmd/api | PASS, actual composed routes/revocation/local Redis outage | 173.060 |

## Changed areas

Access catalog/resolver/policy/override code and migration10; new packages domain/application/HTTP/PostgreSQL/activation module and migration11; Wallet-owned purchase adapter; existing Live room/provider/event/gifting/discovery/recovery paths; bounded worker composition; API composition; platform migration/pool diagnostics and test fixtures. SQL queries and sqlc outputs, OpenAPI contracts/fixtures and all six validators, ADR0015, architecture/business/permission/coverage/roadmap/testing documentation are included. No dependency version changes. Migrations10–11 preserve guarded rollback; empty fixture rollback/upgrade is tested, while production data-retaining rollout/backup/restore remains a separate operational gate.

## Final commands

Use the existing guarded runner: literal ignored TEST_DB_* / TEST_MIGRATION_DB_* only, BIHO_INTEGRATION_ACK=biho_live_test, dedicated authenticated app/migrator database/role checks, no credential argv/output. Schema-mutating and consumer suites serialize with -p=1; no overlapping database suites. Synthetic fixtures and controlled providers only.

Local: gofmt checks; go mod tidy; go mod verify; go build ./...; go test -count=1 ./...; go test -race -count=1 ./...; go vet ./...; tagged integration vet/compile; make check-generated; all six OpenAPI validators; git diff --check; credential/tracked-secret checks.

Final database chain: the guarded runner invokes this command separately for each package in the listed order and stops on failure (no overlapping suites):

```sh
go test -race -tags=integration -p=1 -count=1 -timeout=30m -v ./internal/platform/postgres ./internal/modules/packages/postgres ./internal/modules/wallet/postgres ./internal/modules/live/postgres ./internal/modules/access/postgres ./internal/modules/identity/postgres ./internal/modules/delivery/postgres ./cmd/api
```

## Acceptance limits and outstanding decisions

Owner-approved accounting is 100% platform diamond profit with no VAT/commission. The owner also approved queued renewal: new paid periods append after the prior non-revoked same-package term. Existing periods and prior-key outcomes remain immutable. Distinct packages coexist independently. Final tests cover renewal period continuity and duplicate prevention.

Real LiveKit hidden/client/call/reconnect E2E, Garage/S3 and registered FCM-device E2E are NOT TESTED. SDK/mock tests do not establish provider/client visibility, inaudibility, token recall or receipt by every participant. The actual API test uses a closed local Redis endpoint; no shared cache is stopped. Full independent architectural/security/financial review, provider configuration, operational retention/dead-letter ownership, backup/restore and production load/capacity remain gates.

No production p95/p99, throughput, CPU/RSS or VPS-capacity result is claimed. The retained global security lock serializes authority. Discovery returns at most51 rows but SQL aggregation cost grows with eligible rooms/terms; connection and query deadlines remain bounded. List snapshots last at most30 seconds; package/direct benefits may coexist up to100 unexpired terms per account; pages max50, capability sweep10, pending candidates100/rooms10. Existing image/device/outbox bounds remain as recorded in the delivery report. No frontend, clustering, provisioning, games or unrelated social module was added.
