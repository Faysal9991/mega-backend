# Focused authorization source review — 2026-11-26

Baseline: 62975d5, clean worktree. Preserve consolidated implementation; no lock replacement, dependency/module expansion, commit, push or deployment.

Plan: (1) deny live room/moderation/global-ban authority for globally live-banned actors, including mutation/replay boundaries; no protected-owner exemption is authorized; (2) reconcile role defaults with approved matrix and preserve explicit audited policy/staff grants; (3) inventory every global-lock API/worker/trigger path, run bounded synthetic concurrent HTTP requests with lock/pool measurements, propose finer-grained locking; (4) targeted PostgreSQL regressions plus build/uncached unit/race/vet/sqlc/OpenAPI/format/diff checks. Run DB suites serially against guarded biho_live_test only.

Acceptance: denied cross-room moderation/global bans and replay after restriction across two composed HTTP instances; safe reinstatement; protected-owner denial/protection; immediate role-policy revocation/grant; unchanged financial owner boundaries. Real providers/client E2E and production performance remain NOT TESTED. Results are recorded below.

## Confirmed behavior and approved correction

A persisted global live ban now removes all room.* grants plus live.rooms.moderate and live.participant.global_ban after source merging/legacy expansion, even for a protected owner. Execute checks the persisted ban under the unchanged security lock before room/member/seat/global-ban mutations and idempotency replay. Account/installation and financial/configuration authority remain distinct. Normal APIs still cannot target a protected owner with a live ban; a synthetic injected owner restriction tests fail-closed behavior. No protected-owner moderation exception was authorized.

Owner explicitly approved Room Admin defaults: kick, mute, seat removal, self-leave only. Room closure, role assignment and room bans require an explicit audited policy/staff grant. Host defaults and the previously approved co-host/room-moderator audience moderation defaults remain unchanged; Global Moderator defaults already exclude close/room-admin assignment/room bans. Migration12 appends a correction only when the head is the original seed01; custom published policies/assigned global capabilities are untouched. History is immutable; version12/previous01 records migration provenance rather than inventing a human audit actor. Runtime policy changes retain CAS and same-transaction audit. Rollback restores the previous seed (and its broader Admin defaults), and refuses retained child-policy history; production rollback requires review. Apply migration12 before using the corrected seed defaults; no deployment/cutover is performed here.

## Complete global-lock inventory

Source at baseline62975d5 plus this fix. Key772341909 is a transaction-scoped **exclusive bigint advisory lock**. A valid authenticated request reaching Access.Within, Authorized or Financial serializes with every other holder until commit/rollback, including read APIs. Initial token lookup/rate accounting precede lock acquisition. Validation/early authentication failures may exit before acquiring it. Media/provider HTTP is outside these transactions. Paths below are relative to /api/v1 and apply when mounted. All concrete mounted routes in these five handlers are listed, including map-expanded package GETs.

### access — 15 routes

| Method | Path |
| --- | --- |
| GET | `/admin/roles` |
| POST | `/admin/roles` |
| DELETE | `/admin/roles/{role_id}` |
| PUT | `/admin/roles/{role_id}` |
| GET | `/admin/users` |
| GET | `/admin/users/{account_id}` |
| GET | `/admin/users/{account_id}/private` |
| PATCH | `/admin/users/{account_id}/profile` |
| PUT | `/admin/users/{account_id}/restriction` |
| GET | `/admin/users/{account_id}/roles` |
| PUT | `/admin/users/{account_id}/roles` |
| GET | `/identity/me/permissions` |
| GET | `/profiles/me` |
| PATCH | `/profiles/me` |
| GET | `/profiles/{account_id}` |

### wallet — 20 routes

| Method | Path |
| --- | --- |
| GET | `/admin/agency-hosts/{account_id}` |
| PUT | `/admin/agency-hosts/{account_id}` |
| GET | `/admin/economy/policies` |
| POST | `/admin/economy/policies` |
| GET | `/admin/financial-operations/{operation_id}` |
| POST | `/admin/wallet-adjustments` |
| GET | `/admin/withdrawals` |
| POST | `/admin/withdrawals/{withdrawal_id}/review` |
| POST | `/gifts` |
| GET | `/wallet` |
| GET | `/wallet/policies` |
| GET | `/wallet/transactions` |
| GET | `/wallet/transactions/{operation_id}` |
| GET | `/wallet/transfers` |
| POST | `/wallet/transfers` |
| GET | `/wallet/withdrawal-eligibility` |
| GET | `/wallet/withdrawals` |
| POST | `/wallet/withdrawals` |
| GET | `/wallet/withdrawals/{withdrawal_id}` |
| POST | `/wallet/withdrawals/{withdrawal_id}/cancel` |

### live — 44 routes

| Method | Path |
| --- | --- |
| GET | `/admin/agencies` |
| PATCH | `/admin/agencies/{id}` |
| GET | `/admin/hosts/{id}/history` |
| PATCH | `/admin/hosts/{target}/classification` |
| GET | `/admin/live-activity/{id}` |
| GET | `/admin/live-participants/{id}/ban` |
| GET | `/admin/live-policies` |
| POST | `/admin/live-policies` |
| GET | `/admin/rooms` |
| GET | `/admin/rooms/{id}` |
| POST | `/admin/rooms/{id}/close` |
| POST | `/admin/rooms/{id}/reconcile` |
| POST | `/agencies` |
| GET | `/agencies/{id}` |
| GET | `/agencies/{id}/host-applications` |
| POST | `/agencies/{id}/host-applications` |
| DELETE | `/agencies/{id}/managers/{target}` |
| PUT | `/agencies/{id}/managers/{target}` |
| GET | `/agency-membership/history` |
| POST | `/agency-membership/leave` |
| POST | `/host-applications/{id}/decision` |
| PUT | `/live-participants/{target}/ban` |
| POST | `/providers/livekit/webhook` |
| GET | `/rooms` |
| POST | `/rooms` |
| GET | `/rooms/{id}` |
| POST | `/rooms/{id}/close` |
| POST | `/rooms/{id}/join` |
| POST | `/rooms/{id}/leave` |
| DELETE | `/rooms/{id}/members/{target}/ban` |
| POST | `/rooms/{id}/members/{target}/ban` |
| POST | `/rooms/{id}/members/{target}/kick` |
| PATCH | `/rooms/{id}/members/{target}/mute` |
| PATCH | `/rooms/{id}/members/{target}/role` |
| GET | `/rooms/{id}/moderation-history` |
| POST | `/rooms/{id}/seats/accept` |
| POST | `/rooms/{id}/seats/invitations/{target}` |
| POST | `/rooms/{id}/seats/lock` |
| POST | `/rooms/{id}/seats/release` |
| POST | `/rooms/{id}/seats/self` |
| POST | `/rooms/{id}/seats/unlock` |
| DELETE | `/rooms/{id}/seats/{target}` |
| PATCH | `/rooms/{id}/settings` |
| POST | `/rooms/{id}/token` |

### delivery — 22 routes

| Method | Path |
| --- | --- |
| POST | `/admin/announcements` |
| GET | `/admin/deliveries` |
| POST | `/admin/deliveries/{id}/retry` |
| GET | `/admin/gift-history` |
| POST | `/admin/gift-recipient-policies` |
| GET | `/admin/gifts` |
| POST | `/admin/gifts` |
| PUT | `/admin/gifts/{id}` |
| GET | `/gift-history` |
| GET | `/gifts` |
| GET | `/gifts/{id}` |
| GET | `/media/assets/{id}` |
| PUT | `/media/assets/{id}/attachment` |
| POST | `/media/assets/{id}/complete` |
| GET | `/media/attachments/{purpose}/{target}` |
| POST | `/media/upload-intents` |
| GET | `/notification-preferences` |
| PUT | `/notification-preferences` |
| GET | `/notifications` |
| DELETE | `/push/device` |
| PUT | `/push/device` |
| POST | `/rooms/{id}/gifts` |

### packages — 19 routes

| Method | Path |
| --- | --- |
| GET | `/admin/capabilities` |
| GET | `/admin/capabilities/room-policy` |
| PUT | `/admin/capabilities/room-policy` |
| POST | `/admin/global-moderator-roles` |
| POST | `/admin/package-entitlements/{id}/revocations` |
| GET | `/admin/package-purchases` |
| GET | `/admin/packages` |
| POST | `/admin/packages` |
| PUT | `/admin/packages/{id}` |
| POST | `/admin/packages/{id}/entitlements` |
| GET | `/admin/packages/{id}/history` |
| GET | `/admin/packages/{id}/subscribers` |
| PUT | `/admin/users/{account_id}/capabilities` |
| GET | `/identity/me/package-purchases` |
| GET | `/identity/me/packages` |
| GET | `/identity/me/powers` |
| GET | `/packages` |
| GET | `/packages/{id}` |
| POST | `/packages/{id}/purchases` |

### Additional holders and exclusions

- Signed POST /api/v1/providers/livekit/webhook uses Live.Ingest's explicit lock, rather than Access authentication; it is included in the Live route table. Invalid signatures do not enter it.
- POST /rooms/{id}/token uses the guarded Grant transaction; POST /admin/rooms/{id}/reconcile uses guarded authorization and further explicit provider reconciliation transactions. Provider grant/flush snapshot authorization (provider.go) acquires the same lock even when reached after an HTTP mutation/webhook or worker.
- Background RecoverCapabilities and Dispatcher.cleanupAsset use explicit locks. This is not every worker/job: notification sending, ordinary queue claim/ack, object/provider network calls are not globally locked merely because they are workers.
- Bootstrap uses the same lock through the operator-only Access.Bootstrap path; no bootstrap HTTP route exists.
- The app.agency_history_guard trigger acquires it on every association INSERT/UPDATE, including trusted direct SQL, Wallet PUT /admin/agency-hosts/{account_id}, Live host approval/leave/classification/account-restriction paths when they write associations. Trigger locking is additional to any surrounding Access lock (reentrant for the transaction).
- Migration12's guarded seed correction/rollback acquires it too; Goose's own migration lock is a different lock.
- All nine Identity HTTP routes in identity/httpapi/handler.go (challenge, Google/password login, refresh, logout/all, identifiers/password, identity/me) use Identity transactions/installation/account/subject locks, not key772341909. Identity restriction called inside Access administration remains under its caller's lock. Health probes, readiness/schema checks and optional Redis startup do not acquire it.

## Proposed finer-grained strategy — NOT implemented

Retain current locking in this fix. Design a separate reviewed ADR and rollout with these invariants:

1. Use a shared policy/owner epoch gate for ordinary authorization; reserve its exclusive form for global room-policy/owner/catalog changes. Shared readers may run concurrently. Current READ COMMITTED reads must occur **after** lock acquisition; stale transaction snapshots/caches are not authority.
2. Acquire principal locks in sorted account/installation order. Assignment, direct override, entitlement and restriction writers take exclusive locks for affected principals; readers hold shared locks until the business write and audit commit. Retain Identity's session/account/installation synchronization and deterministic wallet row locks; do not add a reversed row/advisory lock order.
3. Under stable principal membership, obtain sorted shared role/package locks before resolving grants. Role/current-package editors obtain the corresponding exclusive lock rather than unbounded subscriber locking. Then obtain sorted room/resource locks (including asset references for GC/attachment and association actors for the history trigger); host/member/admin mutation and eligibility checks use current values under those locks. Separate namespaces and deterministic server-derived key encoding are mandatory. Hash collisions may serialize unrelated work, never grant authority.
4. Establish one complete order: policy gate -> sorted principals/installations -> sorted role/package definitions -> sorted rooms/resources -> existing deterministic financial account rows. Cross-target admin actions and jobs must participate in the same order; audit/outbox/idempotency remain inside the same transaction as the domain write. Wall-clock expiry still requires current server-time validation at the operation's acceptance boundary.
5. Inventory *every* listed API, provider recovery, worker, bootstrap and association trigger before implementation. Mixed old/new processes cannot silently bypass each other's gates; use a compatibility rollout retaining the old gate until all writers/readers and SQL triggers support the new protocol. No production rollout is authorized here.
6. Prove blocked-writer/read ordering, immediate post-commit revocation, concurrent shared readers, role/package edit fanout, target/resource races, retry/deadlock/cancellation/process-failure recovery and audit rollback with multiple pools/processes. Repeat the identical benchmark before/after. No scalability improvement is claimed until measured.

## Controlled benchmark method

Synthetic, in-process TCP HTTP module composition with two independent Chi servers/Access+Live repositories and PostgreSQL pools; no shared authority cache and no external provider calls. GET /api/v1/profiles/me, four ordinary actors from six synthetic fixture accounts/two rooms; eight requests each at concurrency1/4/8, warm-up on both servers, max3 connections per pool plus a dedicated read-only observer. HTTP timeout30s. No injected lock hold/sleep. Client Mac15,12, 8 physical CPUs, 16GiB RAM, Go1.26.4 darwin/arm64; remote test DB hardware/network path unknown. Real shared-server network and race instrumentation affect latency. Results are small-sample diagnostics, not a capacity/SLO/p95<=200ms claim.

p50/p95/p99 use nearest-rank observed request duration. Pool wait is pgxpool.EmptyAcquireWaitTime delta summed across both pools, with empty/cancelled acquire counts. A 200ms scheduled observer (actual cadence includes SQL latency) samples only advisory key772341909 in current test database: peak ungranted holders and max observed server waitstart age; this is a sampled lower bound, not exact per-request lock-wait p95. Sampling adds a connection/observer workload. No SQL/credentials/actor payloads are logged. Measured results (race-enabled, eight samples per case; p95/p99 are the maximum sample here):

| Concurrency | p50 ms | p95/p99 ms | req/s | Errors | Peak lock waiters | Max observed lock wait ms | Empty pool acquires | Total pool wait ms | Cancelled acquires |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 4177.32 | 6592.29 | 0.219 | 0 | 0 | 0.00 | 0 | 0.00 | 0 |
| 4 | 10467.43 | 12203.46 | 0.331 | 0 | 3 | 8255.82 | 3 | 5022.46 | 0 |
| 8 | 12545.90 | 22736.04 | 0.352 | 0 | 5 | 12679.06 | 18 | 13353.16 | 0 |

Lock samples: 147/98/95 respectively. The exclusive global lock is a demonstrated serialization point: concurrent profile reads waited behind unrelated actors, and bounded pools also queued work. Baseline single-client latency already includes multiple remote SQL round trips; these measurements cannot attribute all latency to the lock or predict production VPS capacity. Pool wait includes any lazy connection construction. Requests use production middleware with explicit test HTTP deadline30s (matching existing API integration configuration), not a changed runtime default. No lock replacement was made. The new load-measurement subtest is opt-in via BIHO_AUTH_LOCK_BENCHMARK=1 for future runs, so normal financial/security regressions retain their existing time budget; the measured run above executed the identical benchmark body before that opt-in gate was added.


## Verification record

- Full local set PASS: `go build ./...`, `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`, integration-tag vet/compile, `make check-generated`, all six OpenAPI validators, formatting/diff and configured-secret scan; `go mod verify` PASS, no dependency updates. Log: /tmp/biho-review-local.log.
- Guarded serial PostgreSQL/race foundation PASS 298.879s package / 271.96s full cycle: version12 up/down/up, failure13 rollback, custom-policy preservation on migration12 and runtime privilege/cancellation/pool checks. Original five-minute scenario bound retained.
- Initial focused package run: FAIL 400.961s because new bare Chi test servers omitted required production request-ID middleware (all three new HTTP cases aborted responses). Existing package powers/expiry/seat race PASS124.00s, purchase/renewal/profit/reconciliation PASS97.59s, host ranking/cursors PASS90.63s. Test composition corrected to production NewRouter; no runtime rules/assertions weakened.
- Second HTTP-only run: two authorization cases stopped on invalid close request bodies (`reason` is unsupported there), yielding expected validation400 before authorization. Payloads corrected to canonical `{}` so they exercise forbidden403. Rejoin expects existing committed200 semantics. Fixture state restoration made local to each scenario. Benchmark completed all three cases with zero errors; measurements above are retained, not unnecessarily repeated.
- Final corrected authorization-only rerun PASS: 378.44s scenario / 380.175s package; global restriction across two HTTP instances PASS171.01s, role defaults/immediate audited policy changes PASS168.66s. Access/Profile PostgreSQL/race PASS218.983s; actual API PostgreSQL/race PASS173.825s, including startup/schema, new routes, revocation and closed-local-Redis outage. Final serialized continuation PASS; no remaining local verification blocker. All post-local-set Go edits are test fixture/composition/payload corrections; runtime Go/SQL/contracts remain the checked set. Tagged vet rechecked on corrected fixture. Actual real LiveKit/mobile/FCM/Garage E2E and production performance are NOT TESTED; independent architecture/security/financial reviews remain pending. No commit/push/deploy.

## Reproduction and handoff

Guarded runner loads only literal ignored TEST_DB_*/TEST_MIGRATION_DB_* values, checks biho_live_test and dedicated app/migrator roles, sets BIHO_INTEGRATION_ACK, and never prints credential argv/values. /tmp/biho-review-db.py is a temporary runner; use the same documented isolated configuration for future runs. Database suites were serialized, never overlapped. Commands (each run only after the previous completed):

```sh
python3 /tmp/biho-review-db.py go test -race -tags=integration -p=1 -count=1 -timeout=30m -v ./internal/platform/postgres
python3 /tmp/biho-review-db.py go test -race -tags=integration -p=1 -count=1 -timeout=30m -v -run 'TestIntegrationConsolidatedCapabilitiesPackages/(review|Wallet_purchase|package_policy|host_package)' ./internal/modules/packages/postgres
python3 /tmp/biho-review-db.py go test -race -tags=integration -p=1 -count=1 -timeout=30m -v -run 'TestIntegrationConsolidatedCapabilitiesPackages/review' ./internal/modules/packages/postgres
python3 /tmp/biho-review-db.py go test -race -tags=integration -p=1 -count=1 -timeout=30m -v -run 'TestIntegrationConsolidatedCapabilitiesPackages/review_(live|room)' ./internal/modules/packages/postgres
python3 /tmp/biho-review-db.py go test -race -tags=integration -p=1 -count=1 -timeout=30m -v ./internal/modules/access/postgres ./cmd/api
```

The first two package attempts contain the documented fixture failures; each necessary correction reran only affected cases. The middle review attempt measured the benchmark once; future benchmark reproduction additionally sets BIHO_AUTH_LOCK_BENCHMARK=1. No runtime timeout, old regression assertion or original scenario deadline was increased. Logs: /tmp/biho-review-postgres.log, /tmp/biho-review-local.log. Broad Wallet/Live/Delivery PostgreSQL suites were not repeated in this focused fix; financial package invariants and current permission/purchase/ranking/API regressions were exercised, while their uncached unit/race suites ran in full.

Changed runtime scope is Access live restriction filtering/default definitions, Live mutation/replay gate (moving the join-only restriction check to the common boundary), and migration12 seed correction; no Wallet ledger/posting, package price/settlement, gifting distribution, LiveKit SDK/grants or global lock protocol changed. Added tests, contract descriptions, permission/business/coverage/roadmap/testing documents and this inventory/proposal. Scope is complete locally; stop for independent review/provider E2E and measured production-like load acceptance before release. No new feature module, commit, push or deployment.
