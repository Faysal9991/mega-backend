# mega-backend

Biho Live 2.0 is a greenfield Go modular monolith. The current authorized scope is Step 3 HTTP transport and middleware; there are no business or health endpoints yet.

Requires Go 1.26.3. The only external module is pinned Chi v5.5.0.

```sh
go run ./cmd/api
```

Development defaults bind to 127.0.0.1:8080. Requests currently return a safe 404 with X-Request-ID and a structured completion event. SIGINT/SIGTERM initiate bounded graceful shutdown. Production/staging require explicit APP_ENV, APP_NAME, HTTP_HOST and INSTANCE_ID; JSON output is required. No credentials are needed or included.

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

Actual outcomes and limitations are in [foundation verification](docs/architecture/FOUNDATION_VERIFICATION.md). Stop for Step 3 architectural review; Step 4 response engine and all business/infrastructure work require separate authorization.
