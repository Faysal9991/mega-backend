# LiveKit / agency / verified-activity batch verification

Date: 2026-11-25 (Asia/Dhaka). Baseline: main 645c75c, initially clean worktree. Status: implemented for independent review; schema-8 lifecycle/provider follow-up verification completed; acceptance gates below remain open. Wallet independent review remains pending; no approval is inferred from tests. No commit, push, deployment, provisioning, credential rotation, production/development reset or legacy import is performed.

## Completed scope and changed files

- `internal/modules/live/`: pure compatibility/role/limit policy, application orchestration, mobile/admin strict DTOs, official SDK provider, signed webhook verifier, PostgreSQL room/seat/agency/application state, command fingerprints, durable ordered provider intent, verified presence, account-restriction effects and reconciliation.
- `internal/modules/agency/postgres/`: one transaction-bound association write owner reused by Live workflows and the existing Wallet admin association operation. The approved Wallet authorization/effective dates/idempotency/audit/financial posting remain intact.
- `internal/modules/identity/postgres/`: identity-owned restriction paths atomically append an accounting ceiling, leave media memberships/seats, queue removals and retain existing session revocation. No provider calls under identity locks. Unrestrict does not revive sessions or history.
- `cmd/api`, typed Live config, `.env.example`, four new scoped permissions, additive migrations 5–8, explicit SQL/sqlc generation, OpenAPI/fixtures/validators, architecture/business/API/roadmap/coverage documents and focused regression tests. Generated model additions in older modules reflect the new schema; they do not introduce new wallet/profile/identity endpoints.

Feature IDs AGY-001/002/003, ROM-001, MED-001 and ADM-004 remain **implemented**, not globally verified/approved. Room lock/host handover, independent-host publishing, ownership transfer, historical proration, media provisioning/clustering, frontend/UI, uploads, delivery notifications, games and commercial payout settlement are not claimed complete.

## Commands and actual outcomes

| Check | Outcome |
| --- | --- |
| `gofmt -l` on changed Go implementation/tests | PASS: all changed/untracked Go implementation/test files checked after final code changes; no formatting output |
| `go mod tidy`, `go mod verify` | PASS; all modules verified; existing Go 1.26.3 declaration retained |
| `go build ./...` | PASS |
| `go test -count=1 ./...` | PASS, uncached default-tag tests |
| `go test -race -count=1 ./...` | PASS, including controlled SDK/HTTP/config/agency/identity/resource tests |
| `go vet ./...` and targeted `go vet -tags=integration` | PASS |
| `make check-generated` | PASS, sqlc v1.31.1 diff clean |
| `validate_identity.py`, `validate_profiles.py`, `validate_wallet.py`, `validate_live.py` | PASS using the existing isolated `/tmp/biho-current-month-openapi` Python environment |
| `git diff --check` | PASS after final code/document changes |
| `go test -tags=integration -p=1 -run '^TestIntegrationFoundation$' -count=1 -timeout=5m -v ./internal/platform/postgres` | PASS on final migrations 1–8: 145.81s test / 146.023s package (earlier schema-7 checkpoint: 118.68s / 119.115s); upgrade, down/up, populated profile backfill/rollback, concurrent re-upgrade, failed migration atomicity and runtime role/pool checks |
| Full `go test -race -tags=integration -p=1 -count=1 -timeout=20m -v ./internal/modules/live/postgres ./internal/modules/wallet/postgres ./internal/modules/access/postgres ./internal/modules/identity/postgres ./cmd/api` | PASS on schema 7 before owner lifecycle follow-up: Live 388.114s, Wallet 862.528s (zero reconciliation differences), Access 167.273s, Identity 161.266s, API 85.392s; includes actual API unavailable-cache 79.05s and startup-bound 4.57s |
| Schema-8 targeted activity-context reset, batched reconciliation and actual API (exact command below) | PASS for context reset 20.80s and batched reconciliation 35.64s / 58.155s package; actual API unavailable-cache/scoped-revocation/graceful-shutdown PASS 82.90s / 84.559s package |
| Schema-8 full Live/agency PostgreSQL/race follow-up | Pre-fix attempts FAILED: first 317.58s (new TLS connection timeout while draining provider work); retry 313.35s (provider backlog did not converge). Six preceding subtests including capacity/seat races passed; tracked cleanup reported no errors. Investigation found the provider timeout was started before local participant authorization reads. Fixed the timeout boundary and combined up to four desired-participant reads into one indexed join; final fixed-source full suite PASS: 482.48s test / 483.693s package, all 13 scenarios including webhook ordering, Wallet source, restriction cutoff, history, provider absence/reincarnation and concurrent closure |
| Provider budget / desired-participant regression (`TestIntegrationLiveReconciliationBatch`) | PASS after fix 36.56s / 38.007s package: unknown-member removal, active publisher update, restricted-agency removal, each receives at least 90% of its configured provider budget at call entry |

Final targeted schema-8 command (through the same guarded runner):

```sh
go test -race -tags=integration -p=1 -run '^(TestIntegrationAgencyActivityContextReset|TestIntegrationLiveReconciliationBatch|TestIntegrationLiveAPIUnavailableCache)$' -count=1 -timeout=6m -v ./internal/modules/live/postgres ./cmd/api
go test -race -tags=integration -p=1 -run '^TestIntegrationLiveAgency$' -count=1 -timeout=15m -v ./internal/modules/live/postgres
```

The guarded runner parses only ignored TEST_DB_* and TEST_MIGRATION_DB_* values literally, without shell evaluation or credential argv/output. It requires exact biho_live_test roles/name and BIHO_INTEGRATION_ACK. Tests verify actual authenticated targets. Schema-mutating suites finish before consumer suites; packages serialize with -p=1. The testing TLS `require` exception encrypts traffic but does not verify server identity; no stronger TLS/production claim follows.

After the provider fix, the complete default/race/build/vet/module/sqlc/all-four-OpenAPI command chain passed again. Final formatting and diff checks passed, with HEAD still 645c75c. No PostgreSQL test process remained after the joined full-suite completion.

The final lifecycle change adds only activity-context/association writes and source filtering; the subsequent recovery fix changes only provider timeout placement and desired-state SQL. Neither changes monetary posting, rounding, rates or ledger mutation. The full Wallet/Access/Identity package suite above was not repeated after those changes; the schema-8 actual API check preceded the provider-only fix, which changed no routing/configuration/schema; final-source default/race checks and the schema-8 source/API/migration regressions are recorded separately. Earlier Live PostgreSQL/race checkpoints passed at 384.78s and 403.38s, before final restriction/provider hardening. These establish earlier regression evidence, not final-source approval. Final checks above supersede them only where explicitly marked PASS.

## Test coverage and corrections

Default tests exercise host-type deny defaults/no implicit inheritance, current room-role publishing, strict snake_case DTOs/unknown field and actor injection rejection, bounds, pending provider outcomes, least-privilege official JWT claims, signature/checksum/tamper/duplicate-header rejection, provider timeout/admission cancellation and empty-versus-missing provider snapshots. Runtime HTTP responses are compared with executable OpenAPI fixtures; schemas additionally reject byte/integer/null/source-policy boundary violations.

The guarded Live suite uses two independent pools and controlled provider state for room/idempotency/closure races, capacity/seat contention, conflicting approvals, role/mute/kick/ban gates, audit failure rollback, durable target/fingerprint, immutable policy predecessor CAS, exact/contradictory event IDs, stale/out-of-order joins/left tombstones, reconnect/missing disconnect, provider absence/reincarnation and activity-source injection into Wallet. It also verifies restriction/audit rollback, immutable restriction evidence, cutoff clipping after a signed disconnect, no revival by unrestrict, fresh authentication and preserved historical activity. Existing agency tests cover duplicate/overlapping intervals, midnight/month/timezone/DST boundaries and resource bounds; existing Wallet suites cover acceptance-time revalidation, concurrent reservations, idempotency, immutable policy/ledger and reconciliation.

The dedicated activity-context regression uses tracked synthetic history to verify initial source evidence, same-agency classification continuity, leave clearing, zero carryover into another agency, new-only duration, preserved prior interval/context history and denied runtime history UPDATE/DELETE. It exercises the central writer and real source; it is not a real-provider E2E test.

The actual API suite uses a closed loopback Redis endpoint, real guarded PostgreSQL, disabled media provider, bounded TCP requests, scoped-grant revocation and joined graceful shutdown. It requires no real Redis/LiveKit credentials. Real Redis service E2E is not rerun: its explicit endpoint acknowledgement is absent. Optional-cache outage behavior is exercised without stopping or reconfiguring a shared service.

Corrections found during work: duplicate host approval initially surfaced the history trigger as 503, now checked as a state/association conflict; banned membership is checked before pending-provider status; audit targets identify the actual affected resource and command; current account restriction cuts future accounting even after unrestrict; provider absence differs from empty presence; unexpected SID recreation cannot reopen admission. Reconciliation uses bounded batched SQL for missing presences/member corrections rather than per-participant lookups/updates. A final slow-database run exposed provider timeout consumption by preceding local reads; provider contexts now begin immediately before each remote call, and desired member/association/agency/account state is fetched in one query. Runtime/connection deadlines and test assertions were retained. The observed query reduction is a static SQL-call count, not a measured latency improvement.

## Test-environment recovery

An interrupted pre-final financial run left orphaned test connections/one transaction and its six-account synthetic fixture. The existing rollback guard correctly refused to discard financial records; it was not removed or weakened. Cleanup matched exact test role/database, known PID/start windows and application name. Fixture recovery required a single original nonce, exact six synthetic Google/profile/email accounts in that run's window, operation-key namespace and no foreign counterparties. Existing fixture teardown arithmetic reversed only those 10 operations' deltas and removed their nine test policies. Reconciliation differences were zero before and after. No business/production data or role/server limits changed. Temporary diagnostic/recovery test files were removed, and final isolated migration/regression checks rerun.

The remote isolated database reproduced 5s/8s guard deadlines under serialized multi-instance work. The new typed database budget defaults to 8s and may be explicitly set to 100ms–30s; earlier request cancellation wins. Correctness integration uses 30s rather than treating network latency as a concurrency correctness result. Goose's bounded lock contention budget is now 60s inside the unchanged 2-minute migration CLI context; the expanded migration fixture has a 3-minute scenario context and retains both concurrent-success assertions.

## Remaining acceptance gates and limitations

**Real LiveKit server/mobile E2E: NOT TESTED.** Controlled SDK HTTP/JWT/webhook tests do not prove deployed SFU permissions, network reconnects, media muting, token revocation or production webhook delivery/order. No real LiveKit credentials/server were configured or provisioned. Independent architecture/security review and the pending Wallet review remain open.

No production-like load benchmark, p50/p95/p99, throughput, CPU/RSS/allocation/pool-wait or VPS capacity measurement is claimed. Remote correctness-test durations are not performance baselines or proof of the 200ms goal. The existing global security lock serializes guarded work. Supported bounds include 50 memberships/account, 500 participants/room, 32 audio seats, 500-member policy correction batches, 10,000 monthly source intervals, eight concurrent SDK calls/process, 30s operation leases and 100 attempts before explicit audited reset. Pending operations/retention require operational monitoring and a reviewed production plan. The SDK uses its shared standard HTTP transport; no media client/worker loop is started and global idle sockets are not closed on behalf of unrelated clients.

Physical media revocation remains pending until provider work applies. One ordered operation advances per mutation/webhook retry/reconciliation attempt; outages/backlogs require retries or scoped reconciliation. Only completed verified intervals qualify; no automatic continuous-presence checkpoint worker or guessed disconnect time exists. Receipt-time starts and terminal tombstones can undercount delayed/uncertain activity; reconnect is required to establish new qualifying evidence after role/seat/classification changes. Saved financial decisions and completed pre-restriction history remain immutable.

Before release, approve explicit compatibility/timezone/activity policy configuration, independent-host/ownership-transfer policy, historical transition proration. Existing Wallet current-association selection is preserved; no unapproved post-leave withdrawal/spending rule, rate, VAT, commission, payment provider or settlement is invented. Next phase is review and controlled provider E2E/operational measurement, not production deployment.

Owner clarification is recorded in ADR 0013: active agency context clears on leave and historical records remain. This confirms current-association selection; same-month rejoin starts a fresh qualifying context per the owner's follow-up.
