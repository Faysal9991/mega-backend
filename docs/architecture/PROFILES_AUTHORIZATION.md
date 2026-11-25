# Step 9: profiles and scoped authorization

Authorized against 069175538e11575d5c5b23f97b6541cc3ca4e7c7. The owner supplies scoped Step 8 architectural/code-review acceptance at this baseline. Live Google mobile E2E and production verification remain pending; unrelated review statuses are unchanged.

## Implementation plan

1. Specify schema, ownership, permissions and routes here before code.
2. Evolve/backfill profile ownership; integrate transaction-scoped initialization and identity read composition.
3. Implement PostgreSQL authorization, owner bootstrap, audited mutations and HTTP contracts.
4. Verify isolated migrations, security/concurrency/failure cases, contracts and regressions; stop at implemented/review pending.

## Schema and ownership

Migration 3 creates profiles(account_id PK/FK, display_name text <=256 UTF-8 bytes, bio text <=2048 bytes, avatar_url text <=2048 bytes). It copies every existing identity display_name/avatar verbatim, then drops those columns from identity_accounts. Empty strings are preserved. Identity owns contact_email/status and login/session data. Profiles alone writes profiles; Identity calls a narrow transaction-bound initializer on signup. Identity reads an explicit profile projection for /identity/me. Returning Google authentication never initializes/updates an existing profile. Missing profile/security artifacts fail closed.

Profiles use non-null strings: empty means unset, never null. PATCH allows display_name (0..80 Unicode code points AND <=256 UTF-8 bytes) and bio (0..500 code points AND <=2048 bytes). No trimming/normalization. Display names reject controls; bio allows LF/tab, other controls rejected. JSON null, empty patches and unknown keys are rejected. Omission preserves a field, empty string clears it. Atomic SQL partial updates preserve unrelated fields; concurrent edits of the same field use last committed update wins. Avatar is read-only, seeded from Google/default data.

Access owns access_owners(account_id PK/FK), access_roles(id PK, name UNIQUE <=64 bytes), access_role_permissions(role_id/permission PK, fixed CHECK catalog), access_assignments(account_id/role_id PK) and access_audit(id PK, actor/target text, action, fields text[], reason, correlation, occurred_at). Role IDs are random 32-character hex IDs; role names never confer authority. No owner permission exists in the editable catalog. Owner bootstrap inserts a single protected owner; any repeat attempt conflicts. Bootstrap requires an active EXISTING Google-linked account and a migration credential; runtime cannot insert/update/delete protected owners. No account creation or implicit promotion.

Runtime may INSERT audit only; it cannot UPDATE/DELETE history. Audits contain field names/permission catalog values, reason and request/operation ID, never raw profile/contact/credential values. Reason is a required operator explanation (1..256 UTF-8 bytes, no controls); operators must not put private values/secrets in it.

## Permission matrix

| Action | Required authority |
| --- | --- |
| GET/PATCH /profiles/me | Live bound session; only own display_name/bio |
| GET /profiles/{account_id} | Live bound session; four public fields only |
| GET /identity/me/permissions | Live bound session; own effective fixed permissions and protected_owner flag |
| GET /admin/users, /admin/users/{account_id} | users.read; public fields + account status |
| GET /admin/users/{account_id}/private | users.read AND users.private.read; contact_email/username/phone/phone_verified only |
| PATCH /admin/users/{account_id}/profile | users.profile.update; profile allowlist + reason |
| PUT /admin/users/{account_id}/restriction | users.restrict; restricted boolean + reason; protected owner always denied |
| GET/POST /admin/roles; PUT/DELETE /admin/roles/{role_id} | Protected owner only; POST/PUT name + exact permission array + reason; DELETE reason |
| GET/PUT /admin/users/{account_id}/roles | Protected owner only; PUT replaces exact role_ids + reason; self/protected-owner assignment denied |

Ordinary users have no staff authority. Protected owner receives the fixed catalog, independent of roles. Private permission is effective only with users.read; private-only role definitions are rejected. Custom role permissions are allowlisted, assignments reference existing roles. Agency/host classification, premium entitlements and account restrictions are separate.

## Concurrency and failure boundaries

Every access transaction first acquires one transaction-scoped advisory security lock (fixed documented namespace), then locks the bound installation, verifies live identity and permissions, locks actor and target accounts in lexicographic ID order, and revalidates the live session. Role changes, assignments, bootstrap and privileged reads/mutations share the lock: permission revocation orders before or after an in-flight action and is visible to the next action on any instance. Conservative global serialization suits this minimal administration phase; no Redis authority. Identity restriction service runs on the same transaction, updates account status and revokes all sessions atomically. Lifting never unrevokes. Audit insertion and mutations commit together; unavailable state/audit rolls back/fails closed. No external provider/cache I/O inside transactions. Authenticated profile/admin requests share an Identity-owned durable per-account budget of 120 requests/minute; rate failures return 429 with Retry-After: 60. Runtime profile rows follow account deletion via FK cascade; no account deletion API is introduced.

Directory sorts immutable account_id ascending, limit default 20 / maximum 100, cursor is last account_id (32 lower hex). Fetch limit+1 without counts; concurrent new accounts before cursor appear only on restart. No joins per result/N+1; private aliases use a single bounded projection. Role/assignment APIs cap lists at 100 roles/100 assignments and reject overflow; permission catalog has four entries.

## Upgrade and rollback

Explicit Goose only, never startup migration. Migration 3 requires a coordinated API stop/upgrade/start: old binaries reference removed columns. Back up before rollout; backfill takes an accounts table lock, so measure lock duration on a sanitized staging dataset before deployment. Down restores latest editable display_name/avatar to Identity before dropping Step 9 tables; bio/role/audit data would be lost, so populated rollback needs separate backup/review and is not an operational recovery plan. Migration tests use only acknowledged isolated fixtures. Startup checks require profile and access schema within the existing connect timeout. No production changes are authorized.

## Bootstrap operation

Explicit command: `go run ./cmd/bootstrap-owner -account-id <existing-id> -reason <safe-explanation> -operation-id <correlation>` with APP_ENV and separate MIGRATION_DB_* direct credentials. Uses verified environment policy, bounded context and advisory serialization; verifies actual database/role against configured dedicated migrator. It never runs migrations. Runtime DB_* credentials cannot grant owner. Do not execute on a deployed database without selected account and explicit owner instruction. Only isolated fixtures exercise it in this task.

Deferred: media uploads, general moderation, agency/entitlements, support workflows, admin UI, Wallet, LiveKit, infrastructure and later steps. Step 9 remains implemented/review pending until independent review.

## Implementation evidence and limits

`profiles/postgres` is the only application writer of profile rows. Identity's transaction adapter receives only `profiles.Initializer`; root repository signup writes fail before mutation. Identity SQL composes an explicit read projection by joining profile-owned data, preserving the original /identity/me fields and empty-string semantics. Access application policy uses a transaction-scoped Unit; storage adapters recheck required permissions at each privileged method and reject commit after a privileged write without a persisted audit. The fixed lock ID is 772341909; all instances must retain the same namespace. Shared immutable PostgreSQL owner state is read directly.

This intentionally conservative implementation serializes profile/admin transactions globally and uses multiple bounded security queries. No throughput/latency improvement is claimed. Benchmark representative sanitized workloads before production capacity approval; scope review should assess the contention tradeoff. Backfill and drop-column locks require a coordinated upgrade rather than mixed old/new binary rollout. Current local/synthetic verification is in STEP9_VERIFICATION.md; live Google mobile E2E and production remain pending.
