# PostgreSQL foundation — Step 6

Status: implemented for review. Uses the owner's existing Coolify testing server, PostgreSQL 18.7 observed after authentication. No local Compose service, proxy, infrastructure deployment or business schema is introduced.

## Connections, TLS and secrets

The API uses pgxpool directly. Runtime settings are loaded explicitly by config.LoadDatabase after ordinary configuration/logging and before listener binding; .env is never read automatically by API/migration/test code. Supply only each process's own required environment, not bootstrap or other roles' credentials. Local bootstrap and integration commands in verification used an explicit file-reading launcher with captured/sanitized diagnostics, without shell sourcing, credential URLs in arguments or raw error output. Local credentials remain in ignored .env, mode 0600.

The owner explicitly permits sslmode=require for this temporary development/testing endpoint. This encrypts transport but **does not verify server identity or CA trust**. TLS 1.3 was observed; minimum permitted TLS is 1.2. No fallback/plaintext connection is configured. sslmode=disable/allow/prefer/verify-ca are rejected. Staging/production require verify-full in both configuration loading and the connection constructor's revalidation. Production must have a trusted certificate valid for DB_HOST's IP/DNS identity; DB_SSLROOTCERT can add a trusted private CA. No production TLS connection or deployment is claimed.

Database configuration is separate from the HTTP configuration snapshot. It validates names, required credentials, ports, pool bounds and connect timeout without echoing input. Secrets and whole database configurations redact their standard string/Go/JSON formatting. Raw pgx/Goose connection errors are replaced by fixed safe failures; no raw SQL, password, DSN or error cause is logged. Consumers of future query errors must map them through application errors, never send them directly to logs/HTTP.

| Setting | Policy |
| --- | --- |
| DB_HOST / DB_NAME / DB_USER / DB_PASSWORD / DB_SSLMODE | Explicit, nonempty; no credential defaults |
| DB_PORT | Default 5432; 1–65535 |
| DB_MAX_CONNS | Default 8; 1–32 per process, subject to lower role/fleet limits |
| DB_CONNECT_TIMEOUT | Default 5s; 100ms–30s; also bounds startup ping/identity/schema checks |
| DB_SSLROOTCERT | Optional trusted CA file; required trust can also come from system roots |

Development may use only biho_live_dev; test mode only biho_live_test; staging/production reject these development/test database names and postgres/template targets. Runtime Open additionally requires the role `<database>_app`, verifies authenticated database/role through generated sqlc code, and checks app schema USAGE before returning a usable pool. Configuration strings are operator input, not client API parameters. A dedicated migration role cannot be supplied to the API.

## Database and role isolation

Bootstrap authenticated as the owner-supplied administrator over TLS before any SQL writes. Both target names and all four role names were absent. `database/bootstrap/testing.sql` created only the authorized new resources. It is a fresh-install operator artifact: its launcher must refuse existing names, generate passwords securely, save them locally before provisioning, and pass passwords as environment-backed psql variables, never CLI arguments. Do not rerun to rotate or overwrite existing credentials; partial bootstrap requires inspection and recovery using the saved secrets.

| Database | Runtime role | Migration/database-owner role |
| --- | --- | --- |
| biho_live_dev | biho_live_dev_app | biho_live_dev_migrator |
| biho_live_test | biho_live_test_app | biho_live_test_migrator |

All four are nonsuperusers without CREATEDB/CREATEROLE/REPLICATION. Runtime roles have a 12-connection aggregate role limit; migration roles have 2. PUBLIC CONNECT/TEMP is revoked on the two new databases; each runtime role receives CONNECT only to its own target. PUBLIC access to public schema is revoked. Existing unrelated databases and their PUBLIC privileges are unchanged: this is not a claim of a cluster-wide pg_hba policy or isolation from every preexisting cluster database.

The migration role owns its database and an existing private migration schema. It creates app via Goose; default privileges grant its runtime role USAGE on subsequently created schemas. The migration schema predates that grant and remains inaccessible to runtime. Runtime cannot create schema/table/temp objects or inspect Goose history. No blanket table DML grant is supplied: future module migrations must grant explicit required table/sequence privileges. Restrict future privileged schemas explicitly rather than accidentally inheriting the schema-USAGE default. Test credentials are confirmed unable to connect to the development target. Application development and the API smoke test use only the development runtime role.

## Pool and operation budgets

One reusable bounded pool per process: max configurable (8 default), min 0, idle lifetime 5m, connection lifetime 30m plus up to 5m jitter. Pool maintenance is pgx's bounded built-in behavior; no custom background checker or goroutine queue is added. Startup acquires a connection and confirms database/role/schema before binding. Failed initialization closes the pool; canceled initialization returns without binding. HTTP shutdown drains/closes transport before the deferred pool Close. Callers must release acquired connections and use deadlines: pgxpool.Close waits for leases and cannot stop non-cooperative code.

Each operation must supply a bounded context, including acquisition, transactions and external-I/O boundaries. Pool acquisition cancellation and actual server-running query cancellation/recovery are tested. No per-connection SET statement_timeout, search_path, temp state or session hook is used. Future SQL must qualify app-owned objects and define its own query/transaction deadline. No transaction should span external I/O. Default endpoint/request deadlines remain useful but are not an excuse to omit database operation budgets.

Observed server max_connections was 100 and pg_stat_activity had 9 entries at preflight (a snapshot, not available capacity). Budget `API replicas × max pool + workers + migrations + other workloads + admin reserve` below server and role caps. Role limits are aggregate, pool caps per process; the default does not authorize multiplying replicas or increasing pool settings. No workload/latency/capacity improvement has been measured.

## Goose and sqlc

Pinned pgx v5.13.0, Goose v3.30.0 and sqlc v1.31.1. Go requirements from exact module metadata are 1.25, 1.26 and 1.26 respectively; project Go 1.26.3/installed 1.26.4 is compatible in local checks. See ADR 0006 for source/version rationale. sqlc is a build tool, outside the application dependency graph; Makefile pins its invocation. Generated bindings are checked in under internal/platform/postgres/dbsql.

`go run ./cmd/migrate` consumes only explicit MIGRATION_DB_* settings (same suffixes as DB_*, separate direct host/port/name/user/password/TLS). No fallback to runtime settings is allowed. Goose uses pgx's database/sql bridge only because Goose requires it; API queries use pgxpool directly. Its direct connection is limited to one open/idle connection and uses session advisory locking with bounded retries inside a two-minute CLI context. No global Goose setters/registry or raw migration logging are used. cmd/api never runs migrations.

Migration 00001 creates app; Goose versions are stored in migration.goose_db_version. Up is repeatable and serialized. No business tables or speculative indexes exist. Down drops only empty app without CASCADE and is exposed only as DownTest, requiring test environment, exact biho_live_test database/migrator and authenticated identity verification before writes. No down CLI or development/production rollback command is provided. Future populated/deployed schemas need reviewed expand/backfill/validate/contract and backup/restore plans; this foundation is not a disaster-recovery or data-rollback proof.

Integration tests opt in through build tag integration and BIHO_INTEGRATION_ACK=biho_live_test, require TEST_DB_* and TEST_MIGRATION_DB_* settings, verify actual database/role, and fail rather than skip when explicit integration settings are absent/invalid. They create only named synthetic fixtures in the test database, remove them, and leave version 1/app present. Tests cover up/down/up, concurrent upgrades, a deliberately failing transactional migration with no surviving DDL/version effect, privilege enforcement, transactions/constraints, parameter precision, bounded acquisition and active-query cancellation. Neither postgres nor development receives destructive tests.

Commands after injecting the appropriate process environment securely:

```sh
go run ./cmd/migrate
make generate
make check-generated
go test -count=1 ./...
BIHO_INTEGRATION_ACK=biho_live_test go test -tags=integration -count=1 ./internal/platform/postgres
BIHO_INTEGRATION_ACK=biho_live_test go test -race -tags=integration -count=1 -cover ./...
```

## Future PgBouncer transaction pooling — requirements only

No PgBouncer is installed, configured or deployed. All present connections are direct PostgreSQL. Runtime uses QueryExecModeExec (extended protocol without named prepared-statement/description caches), with both caches disabled. It does not depend on session SET/search_path, temp tables, LISTEN, session advisory locks or session affinity. Keep explicit transactions on one acquired transaction object, schema-qualified SQL and cancellation/deadline handling. Future additions must preserve these properties or receive a reviewed exception.

Before introducing a proxy: verify the chosen PgBouncer version's protocol/reset/cancellation behavior; test multi-client transactions, prepared statement/cache policy, statement invalidation, authentication and TLS on both legs, timeouts, failover/reconnect and backpressure. Budget backend server pools separately from frontend client pgxpool sizes and include aggregate role limits. Avoid starting a new independent write owner. Runtime DB_HOST/PORT may later point to the proxy; MIGRATION_DB_HOST/PORT must remain the direct PostgreSQL endpoint because Goose's session advisory lock requires session affinity. Readiness probes remain lifecycle snapshots: startup now includes database connectivity/schema checks, but ongoing dependency outage detection is not implemented by this step.
