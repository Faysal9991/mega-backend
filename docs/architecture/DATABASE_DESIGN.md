# Fresh Database Design

Status: Business schema remains a design/ownership proposal. Authorized Step 6 implements only app schema, private Goose migration history and the generated connection-identity/schema-access query; no business tables or indexes. See POSTGRESQL_FOUNDATION.md and ADR 0006.

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

## Step 6 foundation boundary

Use separate isolated development/integration databases and runtime/migration roles. App uses bounded direct pgxpool; Goose uses explicit direct migration settings and session advisory locking. Destructive test guards verify biho_live_test and its migration identity before writes; no development/default database reset is provided. Existing Coolify testing server is owner-authorized; no local Compose or PgBouncer deployment is introduced. Schema-only rollback is non-CASCADE; populated production rollback and backup/restore remain separately reviewed future work.

## Step 8 identity schema

Migration 00002_identity.sql adds ten identity-owned tables with explicit foreign keys, uniqueness and validation checks; generated query ownership is internal/modules/identity/postgres/identitysql. See IDENTITY_AUTH.md and ADR 0008 for concrete columns, units, expiry, locking and access patterns. Session/refresh lookup hashes and active account/expiry/installation indexes follow actual queries. PostgreSQL also owns fixed-window abuse counters and single-use challenges/proofs. All bearer secrets are hashes; runtime and migration roles remain separate. Explicit migrations are required before API startup; identity Ready checks table artifacts but never migrates. Rollback removes only identity tables in FK order before migration 1 removes the app schema; populated production rollback is not authorized. No wallet/agency/profile expansion.

## Step 9 populated profile evolution and scoped staff schema

Migration 3 creates app.profiles, backfills every existing display_name/avatar_url and drops those fields from identity_accounts. Profiles' account foreign key cascades only with account deletion; there is no account-deletion API in this step. The migration creates access_owners, access_roles, access_role_permissions, access_assignments and access_audit. Permission values have a fixed CHECK catalog; unique role names confer no authority. Role deletion cascades permission/assignment links, with an atomic audit. Account IDs order directory keyset queries using the account primary key; profile lookup uses its primary key; access_assignments(role_id,account_id) supports role-revocation/deletion joins. No count/N+1 directory query. Audit IDs are random hex, timestamps PostgreSQL timestamptz, metadata excludes raw changed values; runtime has INSERT only for audit and SELECT only for protected owners. Startup requires all 16 module artifacts in the existing timeout, never auto-migrates. Populated upgrade/rollback and coordinated binary compatibility are documented in PROFILES_AUTHORIZATION.md.


## Authorized financial schema

Migration 4 adds wallet_accounts, wallet_operations, wallet_entries, economy_policies, economy_commands, agency_host_associations, wallet_withdrawals and wallet_withdrawal_events. The single Wallet Engine owns all financial writes. Available and held accounts use exact bigint diamonds; issuance is an explicit bounded negative supply contra-account. SECURITY DEFINER posting with qualified names/fixed search_path and runtime privilege separation protect balances/entries; deferred conservation/audit checks reject incomplete commits. Composite owner/operation/page and account-entry indexes follow actual history/period queries. Policy publication revision and effective selection are separate; immutable command outcomes bind actor/kind/key/fingerprint. Associations retain nonoverlapping intervals. Reservation/review/release states never imply provider settlement. See FINANCIAL_ENGINE.md for full ownership, source metadata, query bounds, trusted-activity boundaries and safe upgrade/rollback limitations. Runtime initialization never migrates.

## Live / agency additive schema

Migrations 5–8 add agency profiles keyed by existing agency-owner account IDs, scoped managers, single-pending host applications, distinct room/session identities, memberships and unique occupied seats, ordered leased provider operations, immutable provider delivery evidence, connection-SID intervals, scoped command fingerprints and immutable live policy versions. Existing association history and Wallet foreign keys are preserved. Query-derived account/SID inspection, account/end-window activity, active agency-host and active publisher indexes support bounded lookups; no schema is copied from a legacy database. New tables/indexes are additive. Down guards preserve retained business evidence and policy history. No production migration/rollback has run; see LIVE_AGENCY.md and consolidated verification for the isolated test evidence.

Migration 7's account/time/id index supports the first immutable account-restriction ceiling inside each completed interval, including after account unrestriction. This is an indexed lateral lookup in the single bounded activity query, not per-row remote/SQL calls. The active room-members partial index matches the actual room_id/state/order predicates. Restriction history has no runtime UPDATE/DELETE grant. Existing permission CHECK extension in migration 5 takes an exclusive lock until that migration commits; no production lock-duration or rollout claim is made. Production migration and backup/restore execution remain separate approvals.

Migration 8 adds agency_activity_contexts, an append-only account/agency/effective-floor journal with a descending account/revision index. The latest context bounds qualifying source intervals; a NULL agency clears active qualification on leave. New agency membership starts from zero and prior history remains (owner-confirmed ADR 0013). Existing current associations are initialized from their effective start; no legacy activity is imported. Runtime grants permit SELECT/INSERT only; rollback refuses retained context evidence.
