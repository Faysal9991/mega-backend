# Typed Runtime Configuration

Scope: Step 1 configuration with the authorized Step 2 logging extension. No HTTP listener, database or business module. The owner approved Step 1 source-level architecture at reviewed baseline bf409ff; that approval does not establish production behavior.

## Architecture

`internal/platform/config.Load(lookup)` builds a typed `Config` with an `Environment` and an `HTTPConfig`. Durations are `time.Duration`, body bytes are `int64`, and port is an integer. Environment constants are development, test, staging and production.

`cmd/api/main.go` calls `Load` once using `os.LookupEnv`. Validation completes before logger construction. Invalid configuration produces a safe setting-specific error on stderr and exit status 1. Step 2 injects the validated snapshot into `logging.New` and emits a fixed INFO startup event; WARN/ERROR thresholds suppress it. It exits without starting a server. Future components will receive configuration explicitly through constructors.

No init function, mutable global, environment mutation, hot reload or dotenv parser is used. Tests inject map-backed lookups without touching process environment. Each recognized setting is read at most once per load; failure stops immediately and returns a zero Config rather than partially usable settings. Treat returned snapshots as immutable after wiring; Go does not enforce immutability of exported fields.

Missing optional values use defaults. Explicit empty values are always errors, including values that have defaults. Values are not whitespace-trimmed; accidental whitespace is invalid. Missing required settings report `is required`, while empty settings report `must not be empty`. Unknown environment variables are ignored: a process may inherit unrelated settings, but a misspelled optional key will not override its default.

## Environment reference

Bounds are inclusive. Defaults are conservative initial engineering choices, not measured workload recommendations. Review them when implementing the HTTP lifecycle; the values are validated but do not enforce server behavior yet.

| Variable | Typed field | Default | Validation |
| --- | --- | --- | --- |
| `APP_ENV` | `Environment` | `development` | Exactly development, test, staging or production |
| `APP_NAME` | `AppName` | `biho-live-api` in development/test | Required explicitly in staging/production; 1–64 ASCII letters/digits/dot/underscore/hyphen |
| `HTTP_HOST` | `HTTP.Host` | `127.0.0.1` in development/test | Required explicitly in staging/production; literal IPv4/IPv6, no bracket, zone identifier, DNS name or port |
| `HTTP_PORT` | `HTTP.Port` | `8080` | Base-10 integer, 1–65535; port zero is forbidden |
| `HTTP_READ_HEADER_TIMEOUT` | `HTTP.ReadHeaderTimeout` | `5s` | Go duration, 1ms–5m |
| `HTTP_READ_TIMEOUT` | `HTTP.ReadTimeout` | `15s` | Go duration, 1ms–5m; at least read-header timeout |
| `HTTP_WRITE_TIMEOUT` | `HTTP.WriteTimeout` | `30s` | Go duration, 1ms–5m |
| `HTTP_IDLE_TIMEOUT` | `HTTP.IdleTimeout` | `60s` | Go duration, 1ms–15m |
| `HTTP_SHUTDOWN_TIMEOUT` | `HTTP.ShutdownTimeout` | `10s` | Go duration, 1ms–5m |
| `HTTP_MAX_BODY_BYTES` | `HTTP.MaxBodyBytes` | `1048576` (1 MiB) | Base-10 bytes, 1024–16777216 (1 KiB–16 MiB) |
| `LOG_LEVEL` | `Logging.Level` (slog.Level) | `INFO` | Exactly DEBUG, INFO, WARN or ERROR |
| `LOG_FORMAT` | `Logging.Format` (LogFormat) | text in development/test, json in staging/production | text or json; staging/production reject text |
| `INSTANCE_ID` | `Logging.InstanceID` | local in development/test | Explicitly required in staging/production; 1–64 ASCII letters/digits/dot/underscore/hyphen |

Duration examples: `500ms`, `1.5s`, `2m`. Unitless numbers, overflow, zero, negative and excessive durations fail. Numeric parser errors are replaced with safe messages so input values cannot appear in error chains or formatted output. Hosts such as `::1`, `127.0.0.1` and `0.0.0.0` are accepted literals; hostname resolution and socket binding are not performed by the loader. Network availability, address ownership and port privileges are checked only when a future server binds.

There are no credentials or secret settings in this step. Application name is a non-secret machine label, not a free-form display string. Do not put sensitive information into any of these variables.

## Supplying environment variables

Go does not automatically read `.env` or `.env.example`. Supply variables through the process environment, a shell, an approved service supervisor or a future deployment secret/configuration mechanism. No infrastructure is added here.

Development, using defaults:

```sh
go run ./cmd/api
```

Explicit test environment:

```sh
APP_ENV=test HTTP_PORT=9090 go run ./cmd/api
```

Build a binary outside the repository and validate production settings:

```sh
go build -o /tmp/mega-api ./cmd/api
APP_ENV=production APP_NAME=biho-live-api HTTP_HOST=127.0.0.1 INSTANCE_ID=api-1 /tmp/mega-api
```

Set `APP_ENV=staging` or `APP_ENV=production` explicitly in the relevant environment. Both modes require explicit `APP_NAME`, `HTTP_HOST` and (from Step 2) `INSTANCE_ID`; unset mode deliberately selects local development. LOG_FORMAT defaults to json there, and any explicit text setting is rejected. Choose a bind address appropriate to the future network design. Do not treat mode selection as a security boundary. No secret is required to run these examples.

To use the provided development example in a POSIX-style shell, first inspect it, then export its simple assignments in a subshell:

```sh
(
  set -a
  . ./.env.example
  set +a
  go run ./cmd/api
)
```

Shell sourcing executes shell code; only source files you control and have reviewed. This command explicitly supplies variables; it is not application dotenv support. Avoid using `export $(cat ...)`, which mishandles parsing and can expose values. A copied local `.env` stays ignored and is never read automatically.

## Secret handling and extension

Real `.env`/`.env.*`, `secrets/`, PEM and key files remain ignored; only `.env.example` is allowed for tracking. Ignore patterns do not protect files already tracked, so verify staged changes as part of review. Never print entire configuration structs, raw environment values, credential URLs or parser errors containing user input.

Add future settings only when an authorized capability requires them: a typed field, documented unit/default/bound, validation and isolated tests. Required secret settings must have no sample/default credential and errors must identify only their key. Keep loading at startup with explicit injection. Do not add a configuration framework, global registry, dynamic reload, database/Redis settings or speculative provider knobs now.

## Verification and acceptance

Tests cover defaults, all overrides/modes, missing versus empty required values, all empty keys, invalid names/hosts, port boundaries/overflow, every timeout's syntax/range, read/header consistency, body limits, safe error formatting, independent snapshots and one-read-per-setting behavior. Startup tests verify success/failure output and exit codes through the same `run` function used by `main`.

See `STRUCTURED_LOGGING.md` for format precedence, instance identity, security policy and limitations. Loading now reads 13 recognized settings once each on success. Existing HTTP/application validation is preserved; production/staging now additionally require INSTANCE_ID as the explicitly authorized logging-related extension.

See the Step 1 and Step 2 verification sections in `FOUNDATION_VERIFICATION.md` for actual commands and coverage. Step 1 source-level architectural review passed per owner approval; Step 2 awaits review. Neither test coverage nor architectural review establishes measured production behavior.
