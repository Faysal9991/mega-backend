# Biho Live 2.0 — Engineering Rules
## 1. Mission and current scope
- Act with the rigor of a principal software architect, senior Go engineer, database engineer and security reviewer. Prioritize correctness, security, reliability, measured performance, maintainability and cost efficiency.

- Build Biho Live 2.0 as a new, greenfield, production-grade Go backend from scratch. This is NOT a Python-to-Go code migration or port. Design fresh architecture, database schema, APIs, contracts and business services. Use the old system only as a read-only reference for feature discovery, approved business rules and lessons learned.

- Current phase: greenfield project foundation and requirements/architecture design. Confirm the approved scope and implement only the authorized step. Do not block basic project setup on a complete legacy audit; verify the relevant requirements before implementing each business module. Stop at each acceptance gate.

- Backend first. Defer Flutter changes, LiveKit implementation/configuration, media clustering, provisioning, autoscaling and admin UI implementation. Inventory their existing contracts and dependencies now; keep extension points for them. Do not claim deferred functionality is complete.

- The new backend must be Go. Do not depend on Django, legacy database tables, legacy API formats, or Python runtime. Any historical data import or compatibility bridge is a separate future decision requiring explicit owner approval.

## 2. Requirements, reference analysis and feature coverage
- Before work, inspect repository structure, git status, relevant instructions, architecture decisions, current plan, and, for the relevant feature when available, actual reference source at a recorded commit. Never assume a path, endpoint, table or rule exists.

- Approved Biho Live 2.0 requirements and architecture decisions are the implementation source of truth. Old code, configuration and tests establish only observed old-system behavior, not mandatory design. Production configuration and mobile behavior require separate evidence. Record conflicts, proposed improvements and unknowns; do not invent business rules.

- Maintain `docs/business/FEATURE_COVERAGE_MATRIX.md` with stable feature IDs, reference evidence where available, approved requirements, roles, inputs/outputs, side effects, data dependencies, edge cases, new API/code/tests, status and intentional changes. Use statuses: discovered, specified, implemented, verified, deferred, blocked, excluded (owner-approved only). A feature is verified only when acceptance evidence exists.

- Discover features from existing documentation and reference code, including routes, models, permissions, admin operations, events, jobs, integrations, flags and recovery paths. Distinguish incomplete, disabled and working behavior. Use findings to specify new capabilities, not to reproduce old internals or redundant endpoints.

- Cover identity, profiles, follows/blocks, levels, agency/host classification and live time, reseller stock, gifts/commissions, wallet, transfer, withdrawal/refund, membership (VIP/VVIP/King/Queen), themes, entry effects, room permissions/seats/lock/host handover/reconnect, leaderboards, Gift Chain, Fireworks, Rocket, games, messenger/chat audit, feed, moderation/device bans, notifications, analytics, support, admin and any additional discovered behavior. Verify PK battle scope rather than assuming it is implemented.

- Implement exact approved business rules and rounding; document and test deliberate improvements and business changes. Escalate ambiguous financial/permission rules before implementing them. Never silently remove or change a feature.

## 3. Architecture and technology
- Use a domain-driven modular monolith, not premature microservices. Go with `net/http` + Chi, PostgreSQL, pgx v5/pgxpool, sqlc, Redis, Goose, River for durable jobs, Docker Compose, OpenAPI, structured `slog`, OpenTelemetry/Prometheus where useful. Pin compatible versions; introduce additional dependencies only with a justified decision record.

- Organize `cmd/`, `internal/platform/`, `internal/modules/`, `internal/shared/`, `database/migrations/`, `contracts/`, `docs/`, `tests/`, `deployments/`. Create packages only when needed. Use `cmd/api`, `cmd/realtime`, `cmd/worker` when their phases require them; share business services, not duplicate logic.

- Modules own domain rules and writes. Flow: transport -> application service -> domain/repository interfaces -> infrastructure implementations. Keep HTTP, SQL, Redis and third-party SDK concerns out of pure domain logic. Use explicit constructor injection and small interfaces at consumer boundaries; avoid circular imports, global mutable state, generic repository frameworks and speculative abstractions.

- Use stateless HTTP instances. Durable state in PostgreSQL; temporary presence/cache in Redis. Local WebSocket connection state is allowed, but cross-instance delivery and recovery must be designed explicitly. No sticky session requirement for correctness.

- Preserve future interfaces for media token issuance, room operations, cluster registry and node health. Keep credentials server-side; no hardcoded server count. In the later LiveKit phase use native cluster room placement, immutable session identity and graceful drain, not custom SFU routing.

## 4. API and response contract
- Design a clean new API contract independently of legacy routes and response shapes. Study old endpoints only to discover use cases and integration requirements. Do not implement legacy adapters or dual API versions unless explicitly authorized. Honor externally required provider callback contracts.

- New APIs start at `/api/v1/`, resource-based routes, appropriate HTTP methods, snake_case JSON, UTC RFC3339 timestamps, documented ID and amount types, stable machine-readable codes, consistent list and error semantics. Maintain OpenAPI as a reviewed contract; test code against it.

- Standard success: `{"success":true,"code":"PROFILE_FETCHED","data":{},"request_id":"..."}`. Optional `message` is human-readable; clients must branch on `code`. Include `meta.pagination` when relevant (`limit`, `next_cursor`, `has_more`); empty collections are `[]`, not null. Define optional/null fields deliberately.

- Standard error: `{"success":false,"code":"VALIDATION_FAILED","message":"Invalid request data","error":{"fields":[{"field":"amount","code":"INVALID_AMOUNT","message":"Must be positive"}]},"request_id":"..."}`. Do not expose SQL, stack traces, internal IDs or secrets unless explicitly part of an authorized contract.

- Use centralized response/error/validation helpers. Use HTTP 200/201/202/204 for appropriate successes; 400/401/403/404/409/422/429/500/503 for appropriate errors. 204 has no body. Pending financial operations must not be reported as settled. Return Retry-After when appropriate. Support request IDs via response header; handle cancellations and timeouts consistently.

- Separate request/response DTOs from persistence models. Validate input sizes, types, ranges, unknown fields according to version contract; authorize per resource and action. Apply bounded pagination and rate limits. Never expose a full model by default.

- Use a stable error-code registry and OpenAPI schemas for every new endpoint. Preserve integration-specific signatures and raw request-body semantics. Add contract fixtures and tests for success, validation, auth, forbidden, conflict and error cases.

## 5. Fast queries and low resource consumption
- Measure before optimizing: endpoint p50/p95/p99, throughput, error rate, SQL calls/time, rows, CPU, RSS, allocation, pool wait, network bytes and job backlog. Use production-like sanitized datasets and workload; label synthetic results. Do not claim unmeasured speedups.

- Use explicit, parameterized SQL with sqlc; select only needed fields, avoid N+1 and per-row remote calls, use batches and bounded joins, and cursor/keyset pagination with stable ordering where suitable. No unbounded list queries or expensive COUNT by default.

- Derive composite/partial indexes from actual predicates, joins and ordering. Inspect `EXPLAIN (ANALYZE, BUFFERS)` on safe staging queries, pg_stat_statements and lock waits. Avoid redundant indexes; account for write amplification. Never run EXPLAIN ANALYZE on production mutations.

- Reuse a bounded pgxpool per process; budget total connections across all replicas/workers. Use context deadlines, cancellation, short transactions and deterministic lock order. Never hold a database transaction open during external HTTP or media calls. Introduce PgBouncer only after measuring need and checking prepared-statement compatibility.

- Cache only suitable read data; version cache keys, set TTLs, invalidate after commit and prevent stampedes. Handle cache outage gracefully where safe. Never authorize spending from cached balances or stale privileges. Keep Redis memory bounded; separate cache eviction from durable coordination. Use Redis Pub/Sub for ephemeral delivery only; critical events need durable outbox/replay or snapshot recovery.

- Minimize response payloads and duplicate requests; use appropriate HTTP compression/ETags for eligible responses. Defer media optimization to its phase. Do not assume switching languages reduces WebRTC bandwidth.

- Bound goroutines, queues, request bodies and retries; implement backpressure, jittered retries, graceful shutdown and readiness/liveness checks. Profile before low-level optimization. Use structured logs with sensitive fields redacted; avoid unbounded metric labels.

## 6. Financial invariants (non-negotiable)
- One authoritative Wallet Engine and one write owner per operation, including admin adjustments, gifts, reseller recharge, games, membership, transfers, withdrawal and reservations. PostgreSQL is authoritative. Do not introduce independent writers to the same wallet or dual writes.

- Use int64 for whole diamonds with overflow/range checks; exact decimal or integer-rate arithmetic for fractional currency/commission. Implement approved per-operation rounding (reference gift floor and transfer ceil only when confirmed as requirements), lifetime counters, commission/VAT snapshots and role-specific spending/withdrawal policy. Do not equate lifetime gift counters with current balances.

- Server resolves catalog price, recipients, session, eligibility, commission and exchange configuration; never trust client prices, totals or actor IDs. Reject invalid/negative amounts and insufficient funds.

- Debit/credit, ledger, settlement, idempotency and outbox records commit atomically where applicable. Balanced append-only ledger with explicit issuance/burn/platform accounts; corrections via compensating entries. Enforce constraints, deterministic locking and unique references.

- Scope idempotency keys to actor and operation, bind to canonical request fingerprint, return prior outcome on retry, reject mismatched payloads. Protect concurrent spending and duplicate jobs. At-least-once delivery requires idempotent consumers; never claim exactly-once messaging.

- Define explicit transaction states (pending, settled, failed; reserve/capture/release/refund as applicable) with valid transitions and recovery. Publish financial success only after commit; animations are not settlement evidence. Keep authorization, pending funds and refund behavior explicit.

- Test balances and ledger reconciliation, multi-recipient settlement, rounding, duplicate requests, concurrent transactions, process failure, timeout, retry, refund, role changes and unauthorized actions. No release with unexplained reconciliation differences.

## 7. Security and privacy
- Verify Google identity tokens server-side (signature, issuer, audience, expiry and applicable claims); enforce real OTP challenge verification where enabled, secure password reset and rate limits. No phone-as-password fallback, device-ID password override, plaintext password copy or client API key as user authentication. Use a reviewed secure password hashing policy; any historical hash import requires a separate approved plan.

- Use short-lived access tokens, rotating/revocable refresh sessions as designed; any future compatibility must not bypass security. Verify WebSocket identity and resource membership; never trust caller-provided actor IDs or device IDs as proof of identity.

- Centralize permission checks: account privileges, business roles, room roles and premium entitlements are distinct. Deny by default; check object ownership, staff privileges and cross-tenant access. Require explicit scoped admin permissions and audit sensitive actions.

- Use TLS, least-privilege DB/service credentials, secret injection, redacted logs, validated uploads and safe media access. Never commit or print credentials, personal data, tokens or production dumps. Protect external callback signatures/replay; avoid SSRF on future server enrollment and integrations.

- Do not replicate known old-system vulnerabilities. Record changes and compatibility implications; escalate unresolved security blockers before release.

## 8. Events, workers and future realtime
- Define versioned event envelopes with event ID, type, session/aggregate ID, appropriate revision/sequence, time and minimal data. Do not reuse a room/channel ID as a live-session identity. Keep snapshots and delta events consistent; support reconnect, deduplication and stale event rejection.

- Use transactional outbox and durable jobs for critical effects. Bound retries and define failure/dead-letter recovery; jobs must be idempotent. Coordinate scheduled jobs across replicas. Separate latency-sensitive financial/recovery tasks from slow media/analytics work when implemented.

- Do not send full profile or room snapshots for every small update. Enforce room membership and server-only event authorship. Document ordering guarantees per aggregate/stream, not globally.

## 9. Fresh database design and operational safety
- Design a new PostgreSQL schema from approved domain invariants and access patterns. Use normalized relationships, appropriate constraints, explicit units and timestamp semantics, stable identifiers and indexes justified by queries. Do not reuse or alter the legacy schema. Start with an isolated empty database and deterministic development/test fixtures.

- Work on local/staging or sanitized copies. Never connect to or mutate production, deploy, rotate credentials, run destructive SQL, reset data, force push or change infrastructure without explicit owner approval. Do not overwrite unrelated user edits.

- Version new database schema changes with Goose; review locking, compatibility, backup/restore and rollback. Once deployed, prefer expand/backfill/validate/contract for safe changes. Test schema upgrades from supported versions. Never delete the old system or its data.

- Historical data import, existing client compatibility and production cutover are out of current scope. If approved later, create a separate plan covering identifiers, accounts, balances and held funds, histories, checksums, reconciliation, single-writer handover, backups and rollback including post-cutover writes. Do not assume application rollback restores data.

## 10. Tests and definition of done
- Write unit, PostgreSQL/Redis integration, API contract, approved business-rule fixtures, selective reference comparisons, regression, authorization, concurrency and failure-recovery tests as applicable. Use isolated fixtures; never rely on production data. Test with multiple instances when relevant.

- Run `gofmt`, `go test ./...`, `go vet ./...`, applicable `go test -race ./...`, sqlc generation consistency and configured lint/security checks. Record actual commands and outcomes; state unrun/failed tests and reasons. Do not weaken or delete tests to make them pass.

- Add load scenarios with fixed dataset, workload, concurrency, hardware and measured baseline; report p95/p99, throughput, errors and resource consumption. Initial goals (not measured guarantees): general API p95 <=200 ms, zero duplicate financial effects, zero unexplained reconciliation differences. Adjust targets with owner approval based on evidence.

- A feature is done only when approved requirement traceability, business rules, security, implementation, API documentation, tests, schema/rollback implications and coverage status are complete. Deferred/blocked items remain visible. No false claims of full feature coverage.

## 11. Working protocol and handoff
- At task start: read this file, current approved plan/phase, relevant ADRs and feature coverage matrix; inspect git status. Identify scope, dependencies, acceptance checks and risks. For complex changes, write a short implementation plan before edits.

- Make small, reviewable changes within the requested scope. Do not rename or restructure unrelated packages, switch technology or add dependencies without justification. Use idiomatic Go, clear names and comments for non-obvious business invariants. Handle and wrap errors; never silently ignore failures.

- Codex and Claude share this file and repository documentation. Keep decisions in `docs/decisions/`, not tool-specific chat memory. Do not edit these rules, approved contracts or scope to bypass a failing check. Avoid simultaneous edits to the same files; inspect changes and coordinate ownership when using both agents.

- After each task report: scope completed, changed files, tests/results, measured performance (if any), business/compatibility changes, feature IDs/status, open risks and recommended next phase. Update the repository plan and feature coverage matrix. Stop at the requested phase; no unauthorized commit/push/deploy.

- Explain decisions to the owner in clear Bengali when appropriate; use English for code, identifiers, API contracts and repository engineering documentation.

## 12. First phase deliverables — greenfield foundation

Create or update these documents incrementally as the approved phase requires:

- `docs/architecture/SYSTEM_ARCHITECTURE.md` — greenfield architecture, module boundaries, dependency direction and scaling design.
- `docs/architecture/DATABASE_DESIGN.md` — fresh schema, constraints, access patterns and index strategy.
- `docs/business/FEATURE_COVERAGE_MATRIX.md` — all discovered features with approved scope, deferred items and verification evidence.
- `docs/business/BUSINESS_RULES.md` and `PERMISSION_MATRIX.md` — approved rules, ambiguity and decisions.
- `docs/business/REFERENCE_ANALYSIS.md` — old-system evidence, known gaps and lessons learned; not a migration specification.
- `docs/api/API_STANDARDS.md`, `ERROR_CODES.md` and `contracts/openapi/` — new API contracts.
- `docs/IMPLEMENTATION_ROADMAP.md` and `docs/testing/TESTING_STRATEGY.md` — phases, dependencies, benchmarks and acceptance gates.
- `docs/decisions/` — approved architecture and business decision records.

First authorized implementation steps: Go module and Git setup, configuration, structured logging, HTTP server and middleware, response/error engine, health endpoints, PostgreSQL/pgxpool/sqlc, Goose, and then Redis when needed. Build one foundation capability at a time; add dependencies only when needed. Follow with identity, profiles/permissions, wallet, and business modules in dependency order. LiveKit-specific implementation and infrastructure management remain deferred.

If reference source is unavailable, record unverified requirements rather than fabricating evidence. Continue authorized independent foundation work. Obtain approval before implementing unresolved business behavior or proceeding beyond the current phase.
