# Testing Strategy

Status: Verification plan; no business tests or infrastructure fixtures currently exist.

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

The current bootstrap contains no test functions: a successful `go test` verifies package compilation but provides no business, race-safety or API acceptance evidence. Formatting-only inspection uses `gofmt -l` without changing owner files.

## Performance protocol

Define workload mix, dataset size/skew, payload sizes, concurrency, replica count, tool versions, host CPU/RAM, database settings/pools and warmup/run duration. Use fixed sanitized/synthetic data and label it accurately. Capture p50/p95/p99, throughput, errors, SQL count/time/rows, CPU/RSS/allocations, pool wait/lock waits, network bytes, job depth/age and outbox lag. Include overload, slow client, dependency outage and drain scenarios.

General API p95 ≤200 ms is an initial goal, not a guarantee. Zero duplicate monetary effects and zero unexplained reconciliation differences are acceptance invariants. Compare like workloads; never claim an unmeasured improvement. No benchmark is meaningful against the current print-only bootstrap.

## Current task evidence

The implementation roadmap records inspection facts. Record actual current command outcomes in the foundation verification report. No runtime service, database, Redis, SQL generation, provider, multi-instance or load verification is possible in this task because those capabilities are not implemented. Do not add tests that simply mirror the startup print statement.
