# HTTP Server Foundation

Status: HTTP lifecycle with reviewed Step 4 response foundation and Step 5 operational health/readiness, awaiting Step 5 architectural review. No business endpoints exist.

## Composition and dependency

`cmd/api` loads configuration once, constructs the existing logging engine, injects one initially unready Lifecycle into the router and Server, and constructs `httpserver.Server`. Constructors receive explicit dependencies; no DefaultServeMux, mutable global server/router or init side effects. The listener factory is injected into the process runner for deterministic bind-failure/lifecycle tests; production supplies net.Listen.

Routing uses `github.com/go-chi/chi/v5 v5.5.0`, pinned in go.mod/go.sum. The [official stable release](https://github.com/go-chi/chi/releases/tag/v5.5.0) and downloaded tag module declare Go 1.24 minimum; the project's Go 1.26.3 satisfies that requirement. Local compilation, tests and race checks supply compatibility evidence for this project. Chi introduces no transitive module dependency. No framework or unrelated package was added.

Server construction, binding, Serve, Shutdown, Close and coordinated Run are separate responsibilities. The address uses net.JoinHostPort, supporting `127.0.0.1:8080` and `[::1]:8080`. Configuration still accepts only unscoped IP literals. The server is single-use; supply a validated snapshot, usable logger/router and listener. No TLS certificate or deployment infrastructure is configured.

## Limits

| Setting | Default | Inclusive bounds / semantics |
| --- | --- | --- |
| HTTP_MAX_HEADER_BYTES | 32768 | 1024–1048576 bytes; net/http MaxHeaderBytes |
| HTTP_REQUEST_TIMEOUT | 10s | 1ms–5m; cooperative request context deadline |
| HTTP_MAX_CONCURRENT_REQUESTS | 256 | 1–4096 admitted requests per router/process, nonblocking |

These are engineering defaults, not measured capacity. Existing host, port, read-header/read/write/idle/shutdown timeout and body-limit values remain unchanged and are now applied. There are 16 recognized configuration settings. Resource values parse strictly with setting-specific redacted errors; empty input never selects a default.

net/http ReadHeaderTimeout limits header receipt; ReadTimeout limits the whole request read, WriteTimeout bounds transport writes and IdleTimeout bounds keep-alive idle time. HTTP_REQUEST_TIMEOUT is independent: it cancels downstream context but neither kills handler code nor automatically interrupts blocked network reads or arbitrary dependencies. Use context-aware downstream operations. Configure transport limits consistently with expected uploads, response work and shutdown budgets; no speculative automatic cross-field business policy is added.

MaxHeaderBytes uses Go's parser accounting, which includes an implementation-specific read-buffer allowance rather than a precise application field-sum limit. Oversized parser input is rejected with 431 before routing. Malformed requests, transport timeouts and header rejections can occur before middleware: they cannot receive application request IDs or completion events. Connection/transport monitoring belongs to later observability work.

## Routing and response boundary

The centralized router accepts explicit registration functions for the `/api/v1` subrouter. No business registration is supplied in normal startup. Operational GET/HEAD /health/live and /health/ready are mounted outside /api/v1; all other resource requests return 404. Tests supply isolated test-only routes. A dedicated root infrastructure mount ensures middleware also runs for an empty business mux; Chi otherwise bypasses its middleware on an entirely empty mux.

Registered routes with unsupported methods return 405 and an Allow header derived with fresh, side-effect-free route probes for standard HTTP methods. Escaped paths follow the same RawPath preference as Chi. A path with no allowed method returns 404. Adding extension HTTP methods requires explicitly extending this policy; implicit business HEAD/OPTIONS endpoints are not created. Operational health routes explicitly register HEAD and advertise GET, HEAD. Request logging uses matched static templates; 404/405 use a fixed fallback.

Infrastructure errors now use the common Step 4 response engine and registry, preserving Allow, Retry-After and shared request IDs. Temporary plain-text InfrastructureError has been removed. Common contracts and failure/commitment semantics are documented in API_RESPONSE_ENGINE.md; endpoint DTO/authorization policies remain deferred.

## Lifecycle and signals

1. Validate configuration before logger creation or binding. Invalid settings exit 1 with a safe stderr message.
2. Emit startup.configuration_validated (http_server_implemented=true), then bind. Only after successful bind emit http.listener_bound; this states socket ownership, not readiness or availability of downstream services.
3. Enter Serve using the bound injected listener; after initialization, mark the shared lifecycle ready immediately before calling net/http.Serve. Clear readiness on Serve exit. Serve normalizes http.ErrServerClosed. Unexpected errors propagate internally, close active connections and result in a fixed safe failure event/message and exit 1; raw net errors and addresses are never logged.
4. signal.NotifyContext handles SIGINT/SIGTERM. On cancellation, first mark the lifecycle terminally unready, then log shutdown start; Shutdown closes listeners/idle connections and drains active requests using a separate background context bounded by HTTP_SHUTDOWN_TIMEOUT. The signal context is not used as request BaseContext, so normal drain does not immediately cancel in-flight work.
5. Direct Shutdown/Close also clear readiness before their net/http calls. On drain timeout, Close force-closes remaining connections, canceling their request contexts. The runner waits for its single Serve goroutine, logs a safe forced-shutdown event and exits 1. A completed drain exits 0. Lifecycle channels are bounded and there is no application waiting queue.

Close cannot terminate non-cooperative Go code, and Shutdown does not manage hijacked connections. Future WebSocket work must define its own connection registry/drain policy before use. Request bodies/streams are not buffered for panic recovery or shutdown.

## Security and operational limits

The peer-IP helper parses RemoteAddr and ignores Forwarded, X-Forwarded-For and X-Real-IP; no trust CIDRs are configured or inferred. Future Nginx/HAProxy/Traefik support requires an approved trusted-peer/forwarding-chain policy. No load balancer is implemented.

net/http raw ErrorLog is discarded because it can print stacks, request/TLS data and unreviewed error text. Explicit application events cover binding, serving, drain and handler panic failures. This trades raw low-level diagnostics for privacy; safe bounded transport metrics can be added in an authorized later phase. Ordinary slog sink errors are still not durable delivery evidence.

Admission bounds handler work, not all TCP connections, TLS handshakes, keep-alive sockets or net/http connection goroutines. Transport limits help but do not establish complete DoS protection or production capacity. Deployments need measured connection/resource budgets later. No throughput or latency improvement is claimed.

See MIDDLEWARE.md for ordering, error/status accounting, deadlines and future upgrade considerations; FOUNDATION_VERIFICATION.md records actual test outcomes and limitations.

See HEALTH_READINESS.md for probe admission bypass, snapshot guarantees, contracts and the absence of guaranteed HTTP responses after listener closure.

Step 6 startup first initializes the dedicated runtime pgxpool and checks database/role/app-schema access; only then does it construct/bind/serve HTTP. Database startup failure cannot make readiness true. Pool Close follows graceful/forced HTTP shutdown. Existing liveness/admission/abort semantics are unchanged. Readiness now also checks cached/coalesced pool availability while running (2s check timeout, 1s cache, 32-waiter cap); liveness never checks PostgreSQL. Temporary outages do not stop the lifecycle, and the final running gate prevents in-flight recovery during shutdown. No background polling is introduced.

Step 7 validates optional Redis settings before database/network initialization. After PostgreSQL startup, it injects one reusable Redis client: a bounded failed startup PING logs a safe warning but does not block serving or affect the existing PostgreSQL readiness checker/liveness. Close follows HTTP drain and precedes deferred PostgreSQL closure. Disabled mode constructs no Redis network client.
