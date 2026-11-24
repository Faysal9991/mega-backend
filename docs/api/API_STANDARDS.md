# API Standards

Status: Greenfield baseline derived from `AGENTS.md`. There are no implemented endpoints or executable OpenAPI contracts yet. Examples illustrate shapes, not approved profile functionality.

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

Use 200/201/202/204 appropriately; 204 has no body. A pending financial intent must not be returned as settled. Define 400/401/403/404/409/422/429/500/503 semantics per operation; document 404 concealment policy where needed. Set Retry-After where appropriate. Finalize the common error registry alongside the response engine before implementation.

Central response/error/validation helpers must avoid SQL, stack traces and secrets. DTOs are separate from persistence models. Enforce bounded body size, types, ranges, unknown-field policy and pagination; for new JSON DTOs default to rejecting unknown fields unless the reviewed contract allows them. Preserve required provider payloads. Derive actor identity from authenticated context; authorize every resource/action.

Use `X-Request-ID` for the bounded, validated correlation ID and echo it in body-bearing responses; generate one when absent/invalid. Define header validation and trust-proxy policy during HTTP foundation. Never use the ID as an authorization or idempotency key.

Financial endpoints require actor/operation-scoped idempotency keys and canonical request fingerprints. Exact header/key limits and retry response semantics require endpoint review. Same key with a different payload is a conflict; retrying a committed request returns its recorded outcome. Cancellation and timeout are not evidence that settlement did not commit.

OpenAPI schemas and fixtures must cover success, validation, unauthenticated, forbidden, not-found, conflict, throttling and infrastructure failures as applicable. Endpoint implementation and contract verification arrive in later authorized steps. Do not add placeholder business routes now.
