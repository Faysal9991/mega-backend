# Testing Strategy

Status: Foundation regression and Step 4 contract tests are executable. Business tests and database/infrastructure fixtures remain deferred.

## Acceptance layers

- Domain units: approved rule fixtures, range/overflow, exact rounding and state transitions; no database dependencies in pure domain logic.
- PostgreSQL integration: fresh isolated schema, Goose upgrades, constraints, sqlc queries, cancellation, locking, concurrency and rollback. Verify behavior against real PostgreSQL, not SQL mocks alone.
- Redis integration when introduced: TTL/invalidation, bounded failure behavior, eviction assumptions and multi-instance coordination. Use isolated fixtures.
- API contracts: compare code with reviewed OpenAPI; success, empty/null fields, validation, auth, forbidden, conflict, throttling, timeouts, request IDs and safe errors.
- Security: resource ownership, scoped admin permissions, refresh replay/revocation, challenge verification, provider signature/replay and bounded input. Never use production credentials or personal data.
- Financial: balanced ledger and per-wallet reconciliation including holds; duplicate keys/payload conflicts, multi-recipient rounding, role/config changes, concurrent spending, provider ambiguity, refund/release/capture and crash points before/after commit and event acknowledgement. All monetary writers must go through Wallet.
- Durable work/realtime: duplicate delivery, exhausted retries, restart recovery, stale sequence rejection, missed Pub/Sub, authorized snapshots and reconnect across instances. At-least-once delivery must not duplicate financial effects.
- Selective reference comparisons only when an old behavior is adopted as an approved new requirement. Do not enforce vulnerable behavior or old response formats for parity.

## Commands and gates

For Go changes run gofmt, `go test ./...`, `go vet ./...`, and applicable `go test -race ./...`. When sqlc is introduced regenerate with its pinned version and verify no drift. Add lint/security/CI checks when configured; record unavailable checks explicitly. No integration check may silently pass because a dependency was skipped.

Current tests cover configuration, logging, lifecycle and API response/error/validation boundaries. Step 4 adds synthetic fixtures compared to real writer output and separate OpenAPI/JSON Schema validation; reproduction commands are in contracts/openapi/README.md. Common auth/error codes are contract fixtures, not proof of authentication/authorization implementation. Final runs include uncached coverage and race checks.

## Performance protocol

Define workload mix, dataset size/skew, payload sizes, concurrency, replica count, tool versions, host CPU/RAM, database settings/pools and warmup/run duration. Use fixed sanitized/synthetic data and label it accurately. Capture p50/p95/p99, throughput, errors, SQL count/time/rows, CPU/RSS/allocations, pool wait/lock waits, network bytes, job depth/age and outbox lag. Include overload, slow client, dependency outage and drain scenarios.

General API p95 ≤200 ms is an initial goal, not a guarantee. Zero duplicate monetary effects and zero unexplained reconciliation differences are acceptance invariants. Compare like workloads; never claim an unmeasured improvement. No load baseline has been measured for the current foundation.

## Current task evidence

The implementation roadmap records inspection facts. Record actual current command outcomes in the foundation verification report. Real local HTTP lifecycle and test-only contract routes are exercised. Database, Redis, SQL generation, provider, distributed multi-instance and load verification are deferred; no such production capability is implemented. Step 4 tests cover wrapped/unknown errors, decoder precision/duplicate/case policies, known/chunked overflow, deterministic diagnostics/pagination, HEAD/204, informational/Flush/final commitment, pre/post-commit and encoding panics, short/failed writes, private-data protection and shared-helper concurrency. Existing configuration/logging/lifecycle regressions remain enabled.

## Step 5 operational acceptance

Health fixtures are compared with router responses and validated against a separate operational OpenAPI referencing common schemas. Deterministic channel gates cover startup, accepted probes draining after readiness clears, ordinary admission saturation and graceful/forced shutdown. Atomic lifecycle access and terminal stop behavior are exercised concurrently. HEAD/405/Allow/cache/request-ID/JSON/privacy contracts and executable startup probes are included. Existing abort-logging/TCP tests remain active. No future dependency check or production capacity is inferred.


## Step 6 PostgreSQL acceptance

Default unit/race suites need no live credentials; lifecycle tests inject a database starter. Explicit build-tag integration runs fail on missing settings instead of silently skipping. They require test environment/name/roles and BIHO_INTEGRATION_ACK=biho_live_test, then verify authenticated identity before migrations. No automatic reset/default database selection or destructive development test exists. Synthetic fixtures are confined to app in the test database and cleaned up. Goose up/down/up, concurrent upgrades and transactional failure rollback, privilege/cross-database isolation, pool wait bounds, running-query cancellation, parameter precision and transactions/constraints are tested against the owner's real testing server. Generated bindings are checked via pinned sqlc diff. API smoke uses the development runtime role and exercises startup-before-bind plus probes and SIGTERM. No load/production-performance or populated-schema disaster-recovery claim follows from these checks.

Step 6 follow-up acceptance: injected transaction stubs plus actual isolated PostgreSQL transactions cover commit-on-success, rollback on callback error/panic/cancellation, independently bounded cleanup and original error/SQLSTATE classification. Availability tests use a controlled clock/channel barriers, bounded genuine waiters and concurrent HTTP probes. Real test-pool exhaustion simulates dependency unavailability without server/infrastructure mutation; recovery and a successful gated check during real shutdown are tested. Existing dedicated test credentials and destructive-target guards remain unchanged.

## Step 7 Redis acceptance

Default unit/race tests use controlled isolated TLS RESP doubles, not a shared server, for outage/auth recovery, TLS verification/default bypass rejection, cancellation/deadline behavior, hard socket/worker caps, no retry replay, cache hit/miss/expiry/limits, redaction and lifecycle. Real tests use the redis_integration tag, explicit authorized-endpoint acknowledgment and existing local REDIS_URL; missing settings/TLS/auth/ACL are reported blocked. Each run uses crypto-random versioned test prefixes, short TTLs and cleanup of tracked keys only with an independent deadline. No global key enumeration/deletion or server/ACL mutation is allowed. Step 6 tests/behavior remain preserved.

## Step 8 identity verification

Default tests use signed RSA/controlled TLS JWKS fixtures against the production Google validation logic, fixed Argon2 policy/limits, negative application authorization and actual common-router DTO/response fixtures. Real state/concurrency semantics are tested explicitly with `go test -race -tags=integration -p=1 -count=1 ./internal/platform/postgres ./internal/modules/identity/postgres` under existing guarded test settings. Do not parallelize foundation schema resets with identity consumers. Two separate pools model API instances for signup/refresh/revocation/durable rate state; local pool close models security-state loss without changing the server. Random tracked fixture cleanup and strict database/role acknowledgment remain mandatory. Actual API Redis mode checks also assert 401 for untrusted current-identity requests. See STEP8_VERIFICATION.md for executed results and untested live Google flow. Architecture acceptance is not implied by passing tests.
