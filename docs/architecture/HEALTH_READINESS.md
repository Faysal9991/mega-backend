# Health and readiness

Status: Step 5 implemented for architectural review. Operational probes are outside the business `/api/v1` namespace. No database, Redis or other external service is checked.

## Contract

| Route | Methods | Successful response | Unready response |
| --- | --- | --- | --- |
| `/health/live` | GET, HEAD | 200, code HEALTH_LIVE, data {} | Lifecycle state does not affect liveness |
| `/health/ready` | GET, HEAD | 200, code HEALTH_READY, data {} | 503, code SERVICE_UNAVAILABLE, fixed safe message, Retry-After: 1 |

Responses use the common success/error envelopes with matching body/header request IDs, application/json, nosniff and Cache-Control: no-store. HEAD has the equivalent GET Content-Length and no body. Other methods return common 405 with Allow: GET, HEAD and no-store. No redirects, aliases or implicit OPTIONS endpoints are provided. Request bodies are unused but existing maximum-body protections apply. No configuration, dependency addresses or detailed diagnostics are returned. `contracts/openapi/operational.openapi.json` documents paths and references the unchanged common schema document, whose paths remain empty. Synthetic health fixtures are compared to actual router output.

## Lifecycle and injection

`cmd/api` creates one `httpserver.Lifecycle` and explicitly injects the same pointer into router and Server. Its zero value is unready. The atomic state is single-use: starting -> ready -> stopped, or starting -> stopped. Stopped is terminal; competing startup cannot restore readiness. Only Server lifecycle methods change the state; it must not be copied after use.

Initialization here means validated configuration, successful logging/router/server construction and successful listener binding. The runner does not mark readiness on constructor completion or binding alone. At Serve entry, the caller supplies the bound listener and Server marks ready immediately before delegating to net/http.Serve. Probe traffic is handled only once net/http accepts connections. An unexpected Serve exit clears readiness; Serve errors still propagate safely. A failed bind never enters Serve and stays unready. An already-canceled Run closes the listener without becoming ready.

Run marks unready before emitting shutdown_started and before initiating Shutdown. Direct Shutdown and Close also mark unready before calling net/http. Graceful drain still uses an independent bounded context, preserves active request contexts and joins the serving goroutine; forced Close still cancels connections. There is no propagation sleep or background checker. Tests gate already-accepted requests and observe readiness false before listener Close begins, then release the probes during drain.

## Admission and limitations

Exact operational probe paths bypass ordinary request-admission slots, for all methods (so unsupported methods still receive 405 under saturation). They retain request ID validation, safe completion logging, panic recovery, request body/context deadlines and net/http header/read/write/idle protections. Other requests retain bounded, nonwaiting admission. No external call, queue, loop or additional goroutine is introduced by probes. Existing completion outcome/abort semantics are unchanged.

Liveness means the process can execute and return the probe; readiness is a point-in-time lifecycle snapshot. Neither is a production capacity, financial correctness or dependency guarantee. Saturated ordinary admission does not itself make the lifecycle unready. Future dependency criteria require separate scope and tests. Synchronous logging, CPU/connection exhaustion and transport limits can still affect probes; bypassing API admission is not a separate listener or full DoS isolation.

Once shutdown closes the listener, new connections may fail rather than receive 503. Only requests that can still be served can observe draining responses. A probe that read the state just before shutdown may complete with its prior result; the atomic snapshot does not serialize the entire response against drain. Parser-level errors precede middleware and do not share its envelope/cache/request-ID guarantees. No production benchmark, media/deployment feature or probe consumer is implemented.
