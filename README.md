# mega-backend

Biho Live 2.0 is a greenfield Go modular monolith. The current authorized scope is Step 8 identity/authentication; only the documented identity routes are implemented.

Requires Go 1.26.3. Pinned runtime components are Chi v5.5.0, pgx v5.13.0, Goose v3.30.0 and go-redis v9.23.0; sqlc v1.31.1 is a build tool.

```sh
go run ./cmd/api
```

Supply dedicated DB_* runtime settings through an explicit secret/environment supplier before startup (see .env.example and the PostgreSQL guide). The API does not automatically read .env. Database initialization and schema/identity checks precede listener binding; migrations run separately with direct MIGRATION_DB_* settings. Development HTTP defaults bind to 127.0.0.1:8080. GET/HEAD `/health/live` and `/health/ready` return minimal common JSON (HEAD has no body), no-store and X-Request-ID. Liveness is independent of ordinary API admission; readiness follows successful initialization/binding at Serve entry and is cleared before shutdown. Other paths return safe 404 responses with structured completion events. SIGINT/SIGTERM initiate bounded graceful shutdown. Production/staging require explicit APP_ENV, APP_NAME, HTTP_HOST and INSTANCE_ID; JSON output is required. Database credentials are required; never use bootstrap/migration credentials for the API.

Go does not automatically load .env files. Supply variables through the process environment; .env.example is a reviewed non-secret development reference. See [runtime configuration](docs/architecture/RUNTIME_CONFIGURATION.md), [HTTP lifecycle](docs/architecture/HTTP_SERVER.md), [middleware](docs/architecture/MIDDLEWARE.md) and [structured logging](docs/architecture/STRUCTURED_LOGGING.md).

Verification commands:

```sh
go mod tidy
go mod verify
go build ./...
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go test -cover ./...
git diff --check
```

Actual outcomes and limitations are in [foundation verification](docs/architecture/FOUNDATION_VERIFICATION.md). Step 4 scoped review is complete at c875d876; previous full-check and fix-specific review evidence is recorded separately. Step 5 is implemented for architectural review. See [response engine](docs/architecture/API_RESPONSE_ENGINE.md) and [common OpenAPI](contracts/openapi/common.openapi.json). See [health/readiness guarantees](docs/architecture/HEALTH_READINESS.md) and [operational OpenAPI](contracts/openapi/operational.openapi.json). Step 6 is implemented for architectural review. See [PostgreSQL setup and limits](docs/architecture/POSTGRESQL_FOUNDATION.md). No PgBouncer is deployed; future transaction-pooling requirements are documented there. Scoped optional-cache architectural acceptance is recorded at 51514aaa567e4a252732f1f0494261f63ca54d20. Step 8 identity is authorized and remains for architectural review; other business/infrastructure work requires separate authorization.

Step 6 follow-up: [transaction and ongoing readiness semantics](docs/architecture/POSTGRESQL_FOUNDATION.md) now cover bounded canceled/panicking transaction cleanup and the shared pool readiness check (2s deadline, 1s cache, up to 32 coalesced waiters). Database failure returns readiness 503 and can recover only while the lifecycle is running; liveness remains independent. No TLS, proxy or deployment change is included.

Optional Redis is disabled by default. See [Redis setup, limits and test safety](docs/architecture/REDIS_CACHE.md): explicit REDIS_URL/rediss, verified TLS by default, at most eight total connections by default and disposable versioned/TTL keys only. Startup/cache outage is nonfatal and does not affect PostgreSQL readiness/liveness. No business-data cache, Redis server/proxy deployment or durable coordination is implemented.


## Step 8 identity setup

See [identity flow, defaults and constraints](docs/architecture/IDENTITY_AUTH.md) and [OpenAPI](contracts/openapi/identity.openapi.json). Apply explicit Goose migration 2 with the migration role using the existing migrate command; the API never migrates and requires identity schema artifacts. Supply GOOGLE_CLIENT_ID (backend web OAuth audience) and optional GOOGLE_AUTHORIZED_PARTY_IDS for trusted Android/iOS clients. Empty configuration makes Google exchange safely unavailable; it never installs a test verifier. Google transport always verifies TLS independently of DB/Redis testing exceptions.

Mobile clients obtain a server challenge, pass its nonce to Google Credential Manager and exchange the resulting fresh ID token with the challenge token. Login is the only signup path; password/identifiers purposes return short-lived single-use linked-identity proofs. Keep bearer secrets in OS secure storage, send installation binding, serialize refresh requests, and reauthenticate after ambiguous refresh outcomes. Production API transport must use HTTPS. Unverified phone aliases do not establish ownership or enable recovery.

Run default/race checks with `make check`. For guarded real PostgreSQL verification supply only TEST_DB_* and TEST_MIGRATION_DB_* from ignored local settings, set BIHO_INTEGRATION_ACK=biho_live_test, and run `go test -race -tags=integration -p=1 -count=1 ./internal/platform/postgres ./internal/modules/identity/postgres`. The foundation suite explicitly resets only the acknowledged test database, so do not run it concurrently with other integration consumers. Tests clean only tracked identity fixtures. Live Google end-to-end requires actual OAuth configuration and a fresh nonce-bound mobile token; signed local fixtures do not claim that external verification.
