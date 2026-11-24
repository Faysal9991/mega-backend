# API Standards

Status: Step 4 common foundation scoped review complete at c875d876; Step 5 operational probes implemented for review. Common OpenAPI schemas exist in `contracts/openapi/common.openapi.json`; paths remain empty. No business endpoint exists. Operational health routes are defined separately in operational.openapi.json. Examples illustrate shapes, not approved profile functionality.

New APIs use `/api/v1/` independently of old routes. Resource names, methods, trailing-slash behavior, authentication, authorization and request/response schemas must be reviewed in OpenAPI before each endpoint is implemented. No legacy adapters or dual versions are planned. Provider callbacks retain their externally required protocol and raw-body signature semantics.

Success shape:

```json
{"success":true,"code":"PROFILE_FETCHED","data":{},"request_id":"..."}
```

Error shape:

```json
{"success":false,"code":"VALIDATION_FAILED","message":"Invalid request data","error":{"fields":[{"field":"amount","code":"INVALID_AMOUNT","message":"Must be positive"}]},"request_id":"..."}
```

Clients branch on stable machine-readable codes; optional messages are human-readable. Use snake_case fields and UTC RFC3339 timestamps. Empty collections are `[]`. Define optional versus null deliberately. List responses add `meta.pagination` containing `limit`, `next_cursor` and `has_more`; specify cursor type, terminal cursor representation, stable ordering and maximum limit per endpoint.

IDs and amount wire types remain open decisions. Whole-diamond arithmetic is int64 internally, but JavaScript numeric precision requires explicit wire consideration. Do not silently choose a JSON number or string for identifiers or amounts. Document exact units, ranges and fractional representation before financial contracts.

Use 200/201/202/204 appropriately; 204 has no body. A pending financial intent must not be returned as settled. Define 400/401/403/404/409/422/429/500/503 semantics per operation; document 404 concealment policy where needed. Set Retry-After where appropriate. The common registry is defined in [ERROR_CODES.md](ERROR_CODES.md), including 405, 413, 415 and cooperative 504 semantics.

Central response/error/validation helpers must avoid SQL, stack traces and secrets. DTOs are separate from persistence models. Enforce bounded body size, types, ranges, unknown-field policy and pagination; for new JSON DTOs default to rejecting unknown fields unless the reviewed contract allows them. Preserve required provider payloads. Derive actor identity from authenticated context; authorize every resource/action.

Use `X-Request-ID` for the bounded, validated correlation ID and echo it in body-bearing responses; generate one when absent/invalid. Define header validation and trust-proxy policy during HTTP foundation. Never use the ID as an authorization or idempotency key.

Financial endpoints require actor/operation-scoped idempotency keys and canonical request fingerprints. Exact header/key limits and retry response semantics require endpoint review. Same key with a different payload is a conflict; retrying a committed request returns its recorded outcome. Cancellation and timeout are not evidence that settlement did not commit.

OpenAPI schemas and fixtures must cover success, validation, unauthenticated, forbidden, not-found, conflict, throttling and infrastructure failures as applicable. Common fixtures are tested against writer output and validated separately against common schemas. Endpoint-specific implementation and contract verification require later authorization. Do not add placeholder business routes now.

## Step 4 executable policies

Use `response.Writer`, `apperror.Error`, `validation.Decoder`/`Result` and shared `requestmeta` correlation. HTTP mapping belongs only to `response`; unknown failures use fixed INTERNAL_ERROR. Never serialize or log wrapped causes. Field errors are static, bounded to 20, sorted by field/code/message and deduplicated. A syntax guard cannot detect secrets in arbitrary strings; no input-derived messages are allowed.

Use `response.List[T]` on collection DTO fields to encode nil as []; optional/null scalar fields require explicit endpoint decisions. 200/201/202 envelopes are compact pre-commit JSON capped at 1 MiB including newline; handlers must also bound DTOs because encoding/json may allocate internally. 204 has no body or content type/length. HEAD has the equivalent content length and no bytes. Writing after commitment is rejected; write failure is terminal without a second response. Informational status, Flush, partial-panic abort, Allow, Retry-After and cancellation behavior are preserved.

Decode using the validated body limit (or a tighter endpoint budget). Require application/json with optional charset=utf-8, parsed with mime.ParseMediaType; unsupported media/parameters/encodings are 415. The root must be an object struct DTO. Reject empty/malformed/unknown-field/multiple-document/trailing-garbage JSON; body overflow, including chunked reads, is 413. Numeric interface values use json.Number. Exact-case enforcement is not claimed: encoding/json matches struct fields case insensitively. Reject duplicates at every object level, including escaped and Unicode simple-fold/case equivalents. Plain DTOs must not use custom nested decoders to bypass strict validation. Null/coercion semantics otherwise follow encoding/json; semantic checks remain explicit. See [engine architecture](../architecture/API_RESPONSE_ENGINE.md) for full bounds and caveats.

Pagination metadata uses limit 1–1000 (operation contracts must choose an appropriate maximum), next_cursor as opaque string/null, and has_more as boolean. Continuing pages require a nonempty cursor, at most 1024 UTF-8 bytes, without controls or whitespace; terminal pages require next_cursor=null and has_more=false. No total counts or cursor engine are introduced. Identifier/amount wire types remain unresolved for future operation contracts.

## Operational probe exception (Step 5)

GET/HEAD /health/live and /health/ready sit outside the business /api/v1 namespace and use the same envelope/header/error helpers. Success codes are HEALTH_LIVE and HEALTH_READY with data {}; unready is SERVICE_UNAVAILABLE/503 with Retry-After: 1. Every application-level probe response sets Cache-Control: no-store; HEAD has no body and the equivalent GET length. Only GET/HEAD are registered; unsupported methods return 405 with Allow: GET, HEAD. Parser-level failures precede middleware. No credentials or dependency diagnostics are exposed. See HEALTH_READINESS.md and operational.openapi.json for lifecycle guarantees and limits.
