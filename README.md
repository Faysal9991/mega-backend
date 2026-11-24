# mega-backend

Biho Live 2.0 is a greenfield Go modular monolith. The current authorized scope is Step 5 Health & Readiness; no business endpoints exist.

Requires Go 1.26.3. The only external module is pinned Chi v5.5.0.

```sh
go run ./cmd/api
```

Development defaults bind to 127.0.0.1:8080. GET/HEAD `/health/live` and `/health/ready` return minimal common JSON (HEAD has no body), no-store and X-Request-ID. Liveness is independent of ordinary API admission; readiness follows successful initialization/binding at Serve entry and is cleared before shutdown. Other paths return safe 404 responses with structured completion events. SIGINT/SIGTERM initiate bounded graceful shutdown. Production/staging require explicit APP_ENV, APP_NAME, HTTP_HOST and INSTANCE_ID; JSON output is required. No credentials are needed or included.

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

Actual outcomes and limitations are in [foundation verification](docs/architecture/FOUNDATION_VERIFICATION.md). Step 4 scoped review is complete at c875d876; previous full-check and fix-specific review evidence is recorded separately. Step 5 is implemented for architectural review. See [response engine](docs/architecture/API_RESPONSE_ENGINE.md) and [common OpenAPI](contracts/openapi/common.openapi.json). See [health/readiness guarantees](docs/architecture/HEALTH_READINESS.md) and [operational OpenAPI](contracts/openapi/operational.openapi.json). Step 6 and all business/infrastructure work require separate authorization.
