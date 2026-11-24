# Structured Logging Foundation

Status: Step 2 implementation with local verification; source-level architectural review is pending. No HTTP server, request middleware or business service is implemented.

## Architecture and configuration

`logging.New(cfg, writer)` constructs an independent `*slog.Logger` from a validated startup configuration and injected `io.Writer`. Call `config.Load` once in the composition root and inject the logger into consumers. The constructor does not read environment variables, change `slog.Default`, start goroutines or create files. Only the Go standard library is used.

| Setting | Default | Validation and precedence |
| --- | --- | --- |
| `LOG_LEVEL` | `INFO` | Exactly `DEBUG`, `INFO`, `WARN`, `ERROR`; empty/unknown values fail |
| `LOG_FORMAT` | `text` in development/test; `json` in staging/production | Exactly `text` or `json`; explicit JSON is allowed in development/test; explicit text is rejected in staging/production |
| `INSTANCE_ID` | `local` in development/test | Required explicitly in staging/production; 1–64 ASCII letters, digits, dots, underscores or hyphens; empty/invalid values fail |

Missing optional settings use defaults; explicit empty values never do. Errors identify the setting without including its input value. Instance ID must be a non-secret process/replica label supplied by the operator, unique among concurrently running replicas. Validation cannot guarantee fleet-wide uniqueness. Do not use user/device identifiers or credentials. `local` is only a development/test convenience.

Set APP_ENV explicitly for staging/production. If adapting the development `.env.example`, change LOG_FORMAT to json and INSTANCE_ID to an appropriate replica label as well. The application does not automatically load dotenv files.

## Output and levels

Each record has `time`, `level`, `event`, `service`, `environment` and `instance_id`, plus approved structured attributes. Built-in `msg` is renamed to `event`. JSON output is one object per line; development text output is quoted key/value text. Record timestamps and time-valued attributes are converted to UTC. Ordinary slog calls supply timestamps; a manually constructed record with zero time follows standard handler behavior and omits time.

```json
{"time":"2026-11-24T00:00:00Z","level":"INFO","event":"startup.configuration_validated","service":"biho-live-api","environment":"production","instance_id":"api-1","http_server_implemented":false}
```

Use DEBUG for bounded diagnostic facts, INFO for lifecycle/normal operational events, WARN for recoverable degradation and ERROR for failed operations. All levels use the same security policy; DEBUG never grants permission to log private data.

Startup validates configuration once, constructs the logger and injects it into `validateStartup`. Its fixed INFO event states `http_server_implemented=false`. DEBUG/INFO thresholds emit it; WARN/ERROR thresholds suppress it and still exit 0 after successful validation. Invalid configuration exits 1 with a safe pre-logger stderr message at every threshold. Startup uses Handler.Handle after Enabled to observe write errors, reporting a fixed safe failure and exit 1. Ordinary slog convenience methods discard handler errors; logging is not a durable audit ledger.

## Defensive attribute policy

The policy handler intercepts both record attributes and WithAttrs before the built-in handler can resolve or serialize values. WithGroup propagates sensitive context. Only selected primitive slog values, UTC times and recursively reviewed groups pass through. No user code is invoked to format arbitrary values.

- Case-insensitive key matching removes `_`, `-` and `.` before matching sensitive fragments: password/passwd/pwd, authorization, token, apikey, secret, otp, cookie and credential. It also blocks body, header, config, environ, content and error fragments. Matching is deliberately conservative and can redact harmless names.
- Sensitive keys and entire sensitive groups become `[REDACTED]`. Attributes under a sensitive WithGroup are redacted, including attributes attached by subsequent With calls and nested groups.
- Any values, raw errors, structs, maps, pointers, byte slices, Stringer/JSON-marshaler objects and LogValuer values become `[UNREVIEWED_VALUE]`. Error, String, MarshalJSON and LogValue methods are never called by the logging policy. Project objects explicitly into approved primitive fields instead.
- Recursive attribute groups stop at depth 8 with `[GROUP_DEPTH_LIMIT]`. Code must use bounded, static group structures; this is not a general limit on the number of application-created child loggers or attributes.
- Metadata keys time, level, msg, event, service, environment and instance_id are reserved case-insensitively. Caller attributes with those names are dropped even within groups.
- Attribute keys are 1–64 ASCII letters/digits/dot/underscore/hyphen. Invalid named attributes are dropped; empty group names inline their children; an empty primitive key becomes `value`.
- Duplicate keys use the first accepted occurrence per object, including With attributes, inline groups and record attributes. A WithGroup whose key already exists suppresses subsequent branch attributes rather than producing another JSON key. Avoid reusing a field as both a scalar and a group. Duplicate group attributes are not merged.
- Invalid/reserved WithGroup names become `invalid_group`, with all descendants treated as sensitive. If that replacement collides, the branch is suppressed. Previously attached safe fields remain intact.

Tests inspect JSON tokens to detect duplicate keys before decoding into maps; map decoding alone would silently overwrite duplicates. Derived handlers share no mutable deduplication maps: With clones state and Handle uses record-local state.

## Safe usage and injection limits

Event names must be static machine-readable literals, at most 64 bytes, starting with a lowercase ASCII letter and containing only lowercase letters, digits, underscores and dots. Invalid event names become `logging.invalid_event` without echoing the input. Attribute/group names must also be static. Standard handlers escape control characters in primitive values so they cannot create extra log records.

```go
logger.Info("worker.completed", slog.Int("attempt", 1), slog.Bool("retryable", false))
logger.With("component", "worker").WithGroup("operation").Info("worker.failed",
    slog.String("failure_code", "DEPENDENCY_UNAVAILABLE"))
if logger.Enabled(ctx, slog.LevelDebug) {
    logger.DebugContext(ctx, "worker.queue_observed", slog.Int("pending", calculateSafeCount()))
}
```

These snippets assume an injected logger and a reviewed bounded count operation. Use stable failure codes rather than raw errors or error text. Do not log request bodies/headers, environment dumps, configuration snapshots, tokens or private user content at any level.

Key matching and event syntax checks cannot sanitize arbitrary strings. A credential under an innocent key, or a dynamically constructed syntactically valid event/group/key, can still leak. Selecting safe fields and static names during code review is the primary control. Never call Error/String/MarshalJSON yourself and pass the resulting private string under an innocent key. No redaction can undo such application-level disclosure.

## Concurrency, performance and future work

Standard synchronous handlers serialize writes across one logger and its With/WithGroup derivatives. Independent New calls do not share configuration or handler state; if independent loggers share a non-thread-safe writer, the caller must synchronize that writer. Do not concurrently mutate supplied arbitrary objects or attribute group slices during logging.

There are no network calls, queues, background goroutines, retries, file rotation or custom asynchronous buffers. Synchronous output can block on a slow sink. Sanitization and duplicate-key tracking allocate per accepted record; With preformats safe attached attributes. Filtered levels avoid handler processing, but application argument construction still runs unless guarded with Enabled. Arbitrary LogValuer execution is intentionally unsupported, including for expensive construction.

`BenchmarkJSONLogging` is a reproducible synthetic benchmark using io.Discard and one integer attribute. Run `go test -run '^$' -bench BenchmarkJSONLogging -benchmem ./internal/platform/logging`. It measures no collector, filesystem or production workload; no speedup or throughput guarantee is claimed.

Future request correlation will use explicit request-scoped logger derivation with a validated request ID when the HTTP/middleware phase is authorized. No request IDs, tracing middleware, telemetry exporter or deployment collector is added here. See FOUNDATION_VERIFICATION.md for actual checks and remaining risks.
