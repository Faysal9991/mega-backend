# Next backend delivery batch — verification

Date: 2026-11-25 Asia/Dhaka. Baseline main c749cec, initially clean. Implemented for independent review; final command outcomes are recorded below. Wallet and LiveKit independent reviews remain PENDING. No production connection/mutation, deployment, provisioning, credential rotation, commit or push.

## Scope / files / traceability

- internal/modules/delivery: pure contracts/image/crypto/templates, strict HTTP, PostgreSQL catalog/media/push workflows, generated SQL, bounded dispatcher and official S3/FCM/LiveKit-data adapters.
- Wallet: optional transaction-bound live-gift resolution/intent hooks and omitted-zero room/session fingerprint fields. Arithmetic, posting function, immutable financial-policy JSON and prior non-room behavior retained.
- Live/Identity/Profiles: Live-owned gift eligibility, transactional notification producers and push unbinding; Profile-owned avatar attachment.
- Migration 9, delivery/profile/live SQL/sqlc, five new scoped permissions, optional typed config/.env.example, API/worker composition, OpenAPI/fixtures and docs.
- GFT-001/002, MED-002, PRO-001, NTF-001/002, ADM-005 tracked in feature matrix; status implemented/review pending, not blanket verified/accepted.

## Actual checks

| Check | Outcome |
| --- | --- |
| Targeted pure/image/crypto/config/HTTP/official SDK tests | PASS at implementation checkpoints; exact final full chain below |
| Initial schema-9 upgrade/down/up/failed-migration/runtime-role suite | PASS 186.08s test / 186.515s package with 5-minute fixture context |
| Final full local format/module/build/default/race/vet/sqlc/OpenAPI/diff chain | PASS on final source after consent/room-synchronization refinements: uncached default/race tests, module verification/build/vet, tagged vet, sqlc consistency, all five OpenAPI validators, gofmt and diff check |
| Final serialized isolated PostgreSQL/race delivery, Wallet, Live, Access, Identity and actual API suites | Final-source delivery/recovery PASS 564.256s package (batch 540.73s); Wallet PASS 859.817s; Live PASS 589.019s package; Access PASS 238.395s; API PASS 131.505s; Identity PASS 238.507s package / 236.96s test on restored schema9 |
| In-flight worker cancellation/restart PostgreSQL test | PASS 21.92s: in-flight provider cancellation joins workers; fenced expired lease restarts without another financial effect |
| Final schema/index upgrade/rollback check | NOT FULLY PASSED: final cycle failed at startup identity check (227.53s); retry hit the unchanged five-minute fixture deadline during backfill upgrade (300.00s). Schema9 recovery/index/cleanup read-only verification is recorded below; the final complete rollback cycle remains an acceptance gate |

Final security review additionally requires exact prior-session consent for cross-account binding changes; caller-controlled installation IDs never authorize clearing another account. Known pending room-provider corrections defer broadcast with ROOM_SYNCHRONIZING; committed money remains unchanged. New-session unbinding is same-account only; explicit switch consent and audit rollback have follow-up PostgreSQL assertions.

The long baseline chain compiled Identity/API while the final consent interface was being updated, producing mixed-source build errors (OwnInstallation/RegisterWithPrevious). Final-source full build/default/race/tagged-vet checks passed; Identity/API PostgreSQL tests are queued again after delivery/schema checks. No test assertion was weakened.

Initial implementation failures and corrections: PostgreSQL identity revision updates must use DEFAULT (fixed); live recipient authorization must run before distribution lookup (fixed without weakening the Forbidden assertion). Other delivery scenarios passed at the earlier 293.10s checkpoint, including committed money/outbox retry, leases, validated media/replacement/GC and push rotation/session switching. Final results supersede checkpoints only when marked PASS.

The expanded migration fixture exceeded its original 3-minute scenario budget during rollback. The transaction rolled back safely. A temporary guarded recovery test matched only the exact synthetic foundation ID/name/avatar, proved no Google binding, financial operation or nonzero wallet balance, and removed that fixture; PASS 3.12s. The temporary file was removed. Fixture budget is now five minutes, within an eight-minute Go harness; runtime/connection/provider deadlines and migration assertions remain intact. No role/server limit, runtime TLS policy or production/development data changed.

A baseline Wallet integration teardown had incorrectly passed two SQL strings to a single pgx Exec for context/account deletion. It now issues separate parameterized statements over the same tracked fixture IDs; no assertion or retention guard was weakened. Final money tests check zero reconciliation differences and reverse only their own synthetic postings during migrator-role teardown.

## Commands / environment

Guarded runner: only ignored TEST_DB_* / TEST_MIGRATION_DB_* configuration, literal parsing without shell evaluation or credential argv/output; BIHO_INTEGRATION_ACK=biho_live_test, exact authenticated database and app/migrator role checks. Schema packages serialize with -p=1 and finish before consumers. Test TLS require encrypts traffic but intentionally does not verify server identity; production configuration still requires verification. Synthetic Google identities/keys/devices and controlled providers only.

Final local chain: gofmt checks; go mod tidy; go mod verify; go build ./...; go test -count=1 ./...; go test -race -count=1 ./...; go vet ./...; tagged integration vet; make check-generated; validate_identity.py, validate_profiles.py, validate_wallet.py, validate_live.py and validate_delivery.py in the existing isolated Python environment; git diff --check.

Final PostgreSQL chain (same guarded runner, serialized):

```sh
go test -race -tags=integration -p=1 -count=1 -timeout=30m -v ./internal/modules/delivery/postgres ./internal/modules/wallet/postgres ./internal/modules/live/postgres ./internal/modules/access/postgres ./internal/modules/identity/postgres ./cmd/api
go test -race -tags=integration -p=1 -run '^(TestIntegrationDeliveryBatch|TestIntegrationDispatcherInflightShutdownRecovery)$' -count=1 -timeout=25m -v ./internal/modules/delivery/postgres
go test -tags=integration -p=1 -run '^TestIntegrationFoundation$' -count=1 -timeout=8m -v ./internal/platform/postgres
go test -race -tags=integration -p=1 -count=1 -timeout=20m -v ./internal/modules/identity/postgres ./cmd/api
```

The final schema/index cycle failed at runtime pool startup identity verification after 227.53s; the subsequent Identity suite failed at the same startup boundary after 11.45s. The source reports a sanitized failure, so the exact original cause is not proven. A temporary read-only guarded probe later verified matching database/role/schema usage: TLS connection 1.1366s and identity query 176.388542ms, PASS 1.31s; no secret/raw error was logged and the probe was removed. API then passed 131.505s. A serialized schema retry hit the five-minute fixture deadline at backfill upgrade (300.00s), so it is recorded as failed rather than repeatedly extending test budgets. A temporary guarded recovery completed schema9, matched only the exact synthetic foundation ID/name/avatar, proved no identity session/Google binding/financial operations/entries/nonzero balances, and removed that fixture atomically. Its final index check used an incorrect diagnostic table name (`media_assets` instead of `delivery_assets`), failing after cleanup committed (26.41s); a corrected read-only probe passed (6.42s), verifying schema9, both exact partial indexes and fixture absence separately. All temporary probes are removed. Runtime startup timeout, TLS, roles and migration assertions remain unchanged. Identity retry on restored schema9 passed (236.96s test / 238.507s package).

## Verification coverage

Tests cover catalog price authority/configuration/immutable versions/deactivation/audit rollback; room/session/recipient/sender restriction, closed-room, insufficient-funds and overflow denial; committed retry after catalog/room changes, duplicate/concurrent gifts, ledger reconciliation; provider failure after commit, outbox replay/fencing/crash recovery/concurrent workers/explicit audited retry; private upload intents, owner/purpose checks, actual MIME/checksum/image bounds, invalid bytes, thumbnail/pixel/alpha output, duplicate completion, provider outage/recovery, concurrent avatar CAS and retirement/reference-safe GC; encrypted registration, duplicates/rotation, invalid-token cleanup, preferences, multi-device isolation, bounded announcements/concurrent dispatch, revoked sessions, account switch, installation restriction and shutdown. HTTP fixtures/negatives cover price/actor injection, null/presence, canonical keys, pagination and central safe projections. The actual API regression uses a closed local Redis endpoint, disabled optional providers, real guarded PostgreSQL and scoped revocation, joining shutdown.

Official SDK tests use controlled HTTP servers to check S3 signature scope/expiry/private ACL/size/type/checksum behavior, conditional bounded reads and cancellation; LiveKit server-only reliable/topic/payload bounds; Firebase allowlists and unregistered-token classification. These do not establish real bucket/SFU/device delivery.

## Measurements / limits / unrun gates

Synthetic image allocation smoke: Apple M3, darwin/arm64, Go 1.26.4, fixed transparent 1024x1024 PNG, main/256 thumbnail, concurrency 1, three iterations (`go test -run '^$' -bench '^BenchmarkProcessImage$' -benchtime=3x -benchmem ./internal/modules/delivery`). Initial: 26,227,056 ns/op, 14,914,013 B/op, 1,114,206 allocs/op. After replacing per-pixel interface boxing on primary JPEG/PNG paths: 17,276,694 ns/op, 10,455,733 B/op, 92 allocs/op. Pixel/alpha and validation tests pass. This tiny synthetic sample is not a statistically stable performance baseline, RSS/peak memory measurement, VPS benchmark or p95/p99 claim. No production-like load test was run. The final pure image optimization is covered by current-source pixel/alpha tests and the allocation smoke; the final-source full delivery/recovery PostgreSQL run also passed after that pure conversion change.

Limits: 2 MiB input, 4M pixels/4096 dimensions, two image processors/API, 1024/256 variants, 8 MiB normalized output cap, 100 active assets/200 MiB declared input per owner, 10 push bindings/account, 100 announcement targets, 1–4 workers with one job each, 60s fenced leases, 30s jobs, 5s provider/OAuth calls, ten attempts and capped 5m backoff. Global security lock serializes authority; persistent backlog/history/retention and SDK/binary costs need operational review. SDK API dependency graph excludes Firebase; only worker imports it.

**Real Garage/S3 E2E: NOT TESTED. Real FCM test-device delivery: NOT TESTED. Real LiveKit data E2E: NOT TESTED.** No controlled bucket/service-account/device/SFU was configured or provisioned. Real Redis service E2E was not rerun without its endpoint acknowledgment; actual API cache-outage behavior is covered. External animation references are administrative allowlisted URLs, not byte-verified animation uploads. Banner model/renderer/UI, provider/CDN permissions/CORS, key recovery, retention/dead-letter operational ownership and production load/capacity remain separate acceptance gates.
