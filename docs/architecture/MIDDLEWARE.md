# HTTP Middleware Foundation

Status: Step 3 transport with authorized Step 4 response integration. Constructors capture the validated HTTP settings and logger once. No middleware reads environment variables, performs authentication or implements business logic.

## Order and request flow

1. Request ID: select one strictly valid untrusted correlation value or generate 128 random bits using crypto/rand, hex-encoded as 32 characters. Store it under a private typed context key and set X-Request-ID.
2. Completion logging: install Chi's protocol-aware WrapResponseWriter and defer one completion event through the existing security-filtered slog logger.
3. Recovery: handle handler/middleware panic without disclosing its value or stack. Being inside completion logging ensures final status/bytes are observed even on panic; being outside admission ensures slots release during unwinding.
4. Admission: acquire a per-process buffered-channel slot without waiting. If full, return 503 with Retry-After: 1. Deferred release covers normal return, errors, cancellations and panics.
5. Body limit: reject known excessive Content-Length before handler execution and wrap the body in http.MaxBytesReader. Do not read/buffer it.
6. Request deadline: derive context.WithTimeout from the incoming context, preserving earlier cancellation/deadlines; cancel it on return. Execute the handler synchronously without a timeout goroutine.
7. Route dispatch: business routes register explicitly under /api/v1 when authorized. The current executable has no business registrations.

## Request IDs and IP boundaries

Incoming X-Request-ID must be exactly one value across all casing variants: 1–64 ASCII letters/digits/hyphen/underscore, with no spaces, commas or control characters. Empty, oversized, ambiguous or invalid input is replaced without echoing it. Entropy failure fails closed by aborting before application dispatch; correlation cannot be fabricated. Go 1.26 crypto/rand.Read itself fails irrecoverably on entropy failure. Normally all middleware responses, including 404/405/413/500/503/504, retain the ID header.

RequestID(ctx) delegates to requestmeta.ID(ctx), the shared typed context for response encoding and logging. It returns the value for explicitly injected consumers. It is never an authentication credential, uniqueness guarantee for caller values or financial idempotency key. A caller can reuse it; do not put credentials/private content in it. Protocol/parser failures before middleware have no request ID.

ClientIP reads only the TCP peer in RemoteAddr, parses IPv4/IPv6 and normalizes IPv4-mapped addresses. Invalid/scoped peer addresses are not accepted. Forwarding headers remain untrusted and ignored. There is no implicit RemoteAddr rewrite or proxy discovery.

## Logging, status and writer capabilities

One INFO http.request_completed event is attempted for every request entering completion middleware, including early rejection and recovered panic. Level filtering/sink failure can suppress delivery; this is not an audit ledger. It has only request_id, method, route, status, duration_ms and response_bytes in addition to required service metadata. Failure events use static names and safe selected correlation fields.

Method is a standard uppercase HTTP method or OTHER. Route is the registered template, never a URL, path parameter or query. Empty/404/405 routes use unmatched. The code never logs authorization, cookies, body, raw URL/query, peer address, private profile data, panic text or raw error. Concurrent handlers use the same safe logger family.

The Chi writer records the first final status (informational 1xx does not finalize), implicit 200, flushes and successful Write/ReadFrom bytes. A handler returning without writes is counted as implicit 200 with zero body bytes. Counts are application writes, not proof of client receipt, wire overhead or settlement.

The protocol-aware wrapper preserves standard HTTP/1 Flusher/Hijacker/ReaderFrom and HTTP/2 Flusher/Pusher combinations and exposes Unwrap. Use http.ResponseController for supported underlying deadline/full-duplex capabilities. Tests verify capability forwarding and actual TCP flush/deadline handling; no response-body buffering is introduced. Exotic custom writer method combinations or legacy CloseNotifier assertions are not an interface guarantee; prefer ResponseController/Unwrap. The executable uses ordinary net/http HTTP/1 transport; HTTP/2/TLS configuration is not implemented.

Before final commitment a panic yields the common safe INTERNAL_ERROR JSON envelope with 500; error and completion events share request_id. After commitment recovery emits a safe failure event then panics http.ErrAbortHandler so net/http aborts the connection/stream. It never changes the committed status or appends a fake 500. Completion records the already-written status/bytes. Any bytes committed before failure are irreversible. Hijacked/upgraded stream lifecycle and byte accounting require a separate future design; no WebSocket is implemented now.

## Body size and deadlines

Known Content-Length above HTTP_MAX_BODY_BYTES returns 413 before handler execution. For chunked/unknown lengths, consumers must check read errors before writing a response:

Use validation.Decoder with the configured maximum (or tighter endpoint budget); it maps wrapped MaxBytesError to PAYLOAD_TOO_LARGE/413. Other body consumers can still classify IsBodyTooLarge and pass an apperror to an injected response.Writer before commitment. Check every read/write error. Middleware itself never reads or buffers the body; the JSON decoder performs bounded staging.

When a cooperative handler returns after its deadline without committing, middleware sends 504. A committed response is not replaced. Parent cancellation propagates; no application-level response is guaranteed to a disconnected client. A handler ignoring its context may continue running or even write after the deadline. Neither this middleware nor net/http transport timeout forcibly stops non-cooperative computation. Use context-aware I/O and explicit bounds in every future module. See HTTP_SERVER.md for transport timeouts and shutdown distinctions.

## Common error contract and future work

404/405/413/500/503/504 now use the common JSON engine. Explicit http.ErrAbortHandler passes through without a replacement envelope or panic event; completion logging still runs. Write failure aborts without a second response. HEAD suppresses bytes, 204 remains bodyless and informational responses do not finalize. See API_RESPONSE_ENGINE.md for decoder/encoding bounds and the error registry. Header/parser errors from net/http happen outside this boundary and may lack correlation/logging. No health endpoints, rate-limit service, trusted proxy configuration, response compression, JWT, database, realtime or deployment infrastructure is added. Future modules must not bypass body-error checks, inject raw paths into route templates, log dynamic event names or treat request IDs as trusted identities.
