# Fresh Database Design

Status: Design principles and ownership proposal. No schema, table names, migrations or indexes are approved for implementation by this document.

Start with an isolated empty PostgreSQL database and deterministic synthetic fixtures. Do not copy or alter Django tables. PostgreSQL is durable authority; Redis holds only explicitly temporary/cache state. Choose identifiers, physical schemas, currencies/units, range limits and retention after reviewing requirements and access patterns.

## Constraints and ownership

- Each module owns its tables and writes. Use normalized relationships, explicit foreign keys and deliberate deletion policies; avoid cascading removal of financial/audit history.
- Distinguish absent values from zero/empty values. Represent instants with timezone-aware PostgreSQL timestamps and expose UTC RFC3339. Define precision and expiry boundary semantics per contract.
- Represent whole diamonds as checked signed 64-bit integers with appropriate range constraints. Fractional amounts/rates require a chosen exact representation and explicit units. No floating-point money.
- Ledger entries are append-only with balanced transactions and explicit issuance, burn and platform accounts. A row CHECK cannot enforce balance across rows: review transaction-finalization validation, privileges and reconciliation together. Do not claim cross-row accounting safety from row constraints alone.
- Balance projection and ledger writes commit together under deterministic locks. Use uniqueness for actor/operation/idempotency scope and durable external references. Bind idempotency to a canonical request fingerprint. Hold/release/capture state transitions must preserve available-funds invariants.
- Persist configuration/rate/commission snapshots needed to explain historical settlement. Preserve lifetime counters separately from spendable balance. No runtime business rules are inferred from legacy names.
- Durable operation, outbox and job/consumer references must support safe retries, conflict detection and recovery. Define retention before pruning idempotency records; deletion must not re-enable duplicate settlement.

## Query and index design process

For each use case record projection, filter/join predicates, authorization scope, stable ordering, page bounds, expected selectivity and transaction/lock needs. Then choose indexes and query shape. Keyset pagination requires a deterministic tie-breaker and documented behavior under concurrent changes. Avoid unbounded COUNT and SELECT *.

Use explicit parameterized sqlc queries. Put generated bindings inside module infrastructure with one owner and documented generation commands when sqlc is introduced. Review generated transaction handling and errors. Do not generate SQL until the schema and relevant use case are authorized.

Use EXPLAIN (ANALYZE, BUFFERS) only in safe staging/synthetic workloads, account for write amplification, inspect pg_stat_statements and lock waits, and measure N+1/round-trip behavior. Never execute production mutation analysis.

## Schema lifecycle and operations

Goose owns versioned schema changes. Review lock duration, backward compatibility, backup/restore and rollback. After deployment use expand/backfill/validate/contract where appropriate; rollback cannot erase settled financial history. Test upgrades from supported schema versions and clean setup on isolated databases.

Bound pgx pools across all processes/replicas and reserve capacity for migrations and recovery. Runtime credentials get least privilege; migration credentials are separate. Transaction code must propagate cancellation and never hold locks across external HTTP calls.

Historical imports, old identifiers/hashes, existing-client compatibility and production cutover require separate owner-approved plans. No import or production connection is part of this foundation.
