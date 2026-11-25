# Step 9 verification

Status: implementation / independent review pending. Baseline `069175538e11575d5c5b23f97b6541cc3ca4e7c7`, initially clean. The owner supplied scoped Step 8 architectural/code-review acceptance; this does not verify live Google mobile E2E or production, or change other review statuses. Only Step 9 is implemented; no commit/push/deployment.

## Scope and review map

- PRO-001: `internal/modules/profiles`, populated migration 3, Identity transaction initializer / explicit read projection, own/public profile and allowlisted partial edits.
- ADM-001 / ADM-002: `internal/modules/access`, PostgreSQL-authoritative fixed permissions, separate protected owner, role definitions/assignments, directory/private projections, Identity-owned restriction/session revocation, atomic audit and explicit `cmd/bootstrap-owner`.
- Contracts: `contracts/openapi/profiles.openapi.json`, strict response fixtures and shared Unicode byte/rune cases. Existing identity contract remains intact.
- Design / upgrade / rollback / permission matrix: PROFILES_AUTHORIZATION.md, ADR 0009, DATABASE_DESIGN.md, BUSINESS_RULES.md and PERMISSION_MATRIX.md. No dependencies added. No legacy feature behavior inferred; owner Step 9 request supplies approved requirements.

## Local checks

Final recorded check batch uses `/tmp/biho-step9-checks.py` as a nonsecret subprocess runner (individual logs `/tmp/biho-step9-*.log`; no environment/credential output). All passed: `go build ./...`; `go test -count=1 ./...`; `go test -race -count=1 ./...`; `go vet ./...`; `go vet -tags=integration ./cmd/api ./internal/modules/access/postgres ./internal/modules/identity/postgres ./internal/platform/postgres`; `go vet -tags=redis_integration ./cmd/api`; `make check-generated`; `/tmp/biho-step8-openapi/bin/python contracts/openapi/validate_identity.py`; the same Python for `validate_profiles.py`; `gofmt -l cmd internal database` (empty); `git diff --check` (empty).

HTTP tests compare real mounted route responses against shared fixtures and test private-data exclusion, own update/omission/clearing, null/empty/unknown/credential/alias/status/role/avatar injection, duplicate folded keys, unauthorized admin and invalid pagination. Bootstrap invocation tests reject missing/invalid account, reason, correlation and dedicated credentials before any network work. Profile tests share Bengali/emoji/ASCII/control byte/code-point fixtures with the executable contract validator. Identity/OpenAPI password fixtures retain all 18 prior Unicode cases. These are synthetic tests, not Google mobile E2E.

## Isolated PostgreSQL evidence

Only explicitly acknowledged `biho_live_test` with dedicated `biho_live_test_app` and `biho_live_test_migrator` is used. Existing ignored TEST_DB_* / TEST_MIGRATION_DB_* are supplied with an explicit text parser, never shell evaluation, credential argv or printed values. `BIHO_INTEGRATION_ACK=biho_live_test` and authenticated database/role checks precede schema/fixture operations. Goose tests are serial and do not overlap API consumers. TLS is encrypted under the existing temporary require policy; server identity and production TLS are not newly verified.

Command: `go test -race -tags=integration -p=1 -count=1 -timeout=10m -v ./internal/platform/postgres ./internal/modules/identity/postgres ./internal/modules/access/postgres`.

Initial run passed schema/behavior assertions but exposed an obsolete runtime DELETE-profile cleanup statement (runtime deliberately lacks DELETE profile permission). It was removed: Identity fixture cleanup deletes tracked accounts, and the explicit profile FK cascade handles their profile lifecycle. A duplicate role-delete assertion was also removed. No grants were broadened or assertions weakened. The full serial rerun passed: platform/postgres 72.566s; identity/postgres 146.340s; access/postgres completed its assertions and tracked cleanup successfully (final focused follow-up below adds the restriction/login race and closed-pool check). Durations describe remote synthetic test execution, not API performance.

The guarded migration suite covers fresh up/down/up, seeded version-2 profile backfill, down-to-2 restoration of the latest edited/empty display name/avatar, concurrent upgrades, failing transactional migration 4 with neither surviving DDL nor version change, runtime/migrator separation and transaction/cancellation/pool/readiness regressions. Identity integration retains two-instance signup/refresh/alias/password/restriction and safe failure tests. Access integration covers synthetic owner bootstrap guards/concurrent single winner, runtime owner/audit immutability, no implicit promotion, exact profile initialization, returning login/identity projection, atomic failed signup, concurrent partial edits, role/assignment revocation, escalation/injection/owner protection, lock ordering against in-flight mutation, missing/failed audit rollback, restriction/session revocation/lifting, bounded keyset pages and tracked cleanup.

## Limits and next gate

No production database access/bootstrap, deployed migration, real owner selection, live Google mobile E2E, production/load/latency/throughput benchmark or capacity approval. Test durations are not endpoint performance measurements. Global profile/admin transaction serialization is a documented contention risk to assess before production; populated migration requires coordinated binary upgrade and reviewed backup/rollback. Generic schema tools may ignore the UTF-8 byte extension; runtime and the executable validator enforce it. Media uploads, agency/host/premium workflows, Wallet, LiveKit, support workflows, admin UI, infrastructure and later phases remain deferred. Stop at independent Step 9 review; Step 10 is not authorized.


## Final follow-up checks (completion recorded incrementally)

A separate serial runner `/tmp/biho-step9-api.py` uses only existing ignored test settings supplied without shell evaluation/credential argv. It runs final migration rollback constraints, access races, real Redis-mode API composition, then read-only schema/startup timeout. A failing stage stops the runner; no stage silently skips.

- `go test -race -tags=integration -p=1 -run '^TestIntegrationFoundation$' -count=1 -timeout=5m -v ./internal/platform/postgres`: PASS; test 49.06s / package 51.228s. Down-to-2 restores original name/avatar byte CHECK constraints and latest profile values. Version 3 remains installed only on the acknowledged test database.
- New HTTP DTO tests execute every minimal supporting admin route (including roles/assignments, restrictions, profile changes, basic/private/directory reads), 201 creation, 409 conflict and empty array pagination. Focused `go test -race -count=1 ./internal/modules/access/httpapi`: PASS (1.898s).
- Final access/Redis/startup results: all PASS (details below).


## Final isolated acceptance results

The final guarded runner `/tmp/biho-step9-api.py` completed with exit 0 and `/tmp/biho-step9-api-result.txt` = PASS. It supplied only existing ignored isolated test database settings and the authorized testing Redis settings through an explicit literal parser, never shell evaluation or credential command arguments. Tests ran sequentially; no schema reset overlapped consumers. Logs are `/tmp/biho-step9-{migration-final,access-final,redis-api,startup}.log`.

| Actual command / scenario | Outcome |
| --- | --- |
| `go test -race -tags=integration -p=1 -run '^TestIntegrationFoundation$' -count=1 -timeout=5m -v ./internal/platform/postgres` | PASS, test 49.06s / package 51.228s; final rollback restores original name/avatar length constraints and current profile values |
| `go test -race -tags=integration -p=1 -count=1 -timeout=10m -v ./internal/modules/access/postgres` | PASS, test 164.23s / package 166.294s; includes account-lock ordering against an in-flight session creation, which restriction then revokes; empty directory arrays/final cursor, audit failure/missing-audit rollback, no raw private audit fields, runtime immutable-history/owner grants and final closed-pool fail-closed behavior; all tracked fixtures cleaned |
| `go test -race -tags=redis_integration -p=1 -count=1 -timeout=10m -v ./cmd/api` | PASS, test 91.21s / package 93.404s; enabled 23.90s, disabled 24.58s, locally unavailable 23.48s; actual API authenticated profile projection and admin read, PostgreSQL assignment revocation causes 403, restored assignment causes 200 in every mode; isolated synthetic bootstrap/fixtures cleaned; shared Redis server unchanged |
| `go test -race -tags=integration -p=1 -run '^TestIntegrationIdentitySchemaStartupBound$' -count=1 -timeout=10m -v ./cmd/api` | PASS, test 4.01s / package 6.144s; current migration-3 artifact checks and bounded cancellation/pool cleanup, no migrations in startup |

The prior full serial integration command `go test -race -tags=integration -p=1 -count=1 -timeout=10m -v ./internal/platform/postgres ./internal/modules/identity/postgres ./internal/modules/access/postgres` passed the final foundation and unchanged full identity scenarios (72.566s and 146.340s package durations); final access evidence above includes later test additions. Initial fixture-cleanup failure is retained above with its correction. TLS 1.3 was observed; the existing temporary test `require` policy encrypts without verifying server identity. Production TLS/server identity and live Google mobile E2E are not certified by this work.

Final source review also caught a pure-domain aliasing edge: if an internal caller supplied the same pointer for name and bio, the bio newline allowance could apply to the name. Validation now chooses the control policy by the field being checked, and `TestSharedPointerDoesNotAllowNameControls` plus shared Unicode/HTTP fixtures pass under race. Full build/default/race/vet/sqlc/OpenAPI/format/diff checks were repeated after this correction; final check summary remains PASS. No extra configured lint/security scanner exists in this repository; no scanner pass is claimed.

Step 9 status: **implemented / independent review pending**, covering PRO-001, ADM-001 and ADM-002. Scoped Step 8 owner architectural/code acceptance at the supplied baseline is recorded separately and other review statuses remain intact. No commit, push, deployment, infrastructure change or bootstrap outside isolated synthetic fixtures occurred.
