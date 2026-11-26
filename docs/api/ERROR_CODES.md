# Common error codes

Status: Step 4 implemented for review. Stable machine codes; clients branch on code, not message. HTTP mapping is centralized in `internal/platform/response`.

| Code | HTTP | Fixed public message |
| --- | --- | --- |
| `INVALID_REQUEST` | 400 | Invalid request |
| `INVALID_JSON` | 400 | Invalid JSON request |
| `UNSUPPORTED_MEDIA_TYPE` | 415 | Unsupported media type |
| `VALIDATION_FAILED` | 422 | Invalid request data |
| `UNAUTHENTICATED` | 401 | Authentication required |
| `FORBIDDEN` | 403 | Access forbidden |
| `NOT_FOUND` | 404 | Resource not found |
| `METHOD_NOT_ALLOWED` | 405 | Method not allowed |
| `CONFLICT` | 409 | Resource conflict |
| `PAYLOAD_TOO_LARGE` | 413 | Request body too large |
| `RATE_LIMITED` | 429 | Too many requests |
| `SERVICE_UNAVAILABLE` | 503 | Service temporarily unavailable |
| `REQUEST_TIMEOUT` | 504 | Request deadline exceeded |
| `INTERNAL_ERROR` | 500 | Internal server error |

INVALID_REQUEST includes nonobject/null JSON roots and nonoverflow body read failures. INVALID_JSON includes malformed/empty JSON, unknown struct fields, type mismatch, duplicate keys and trailing input. VALIDATION_FAILED covers explicit semantic field diagnostics. Unknown failures, invalid public metadata and serialization failure become INTERNAL_ERROR without raw causes or stacks. REQUEST_TIMEOUT is a cooperative context deadline before commitment; parent cancellation does not fabricate a timeout response.

405 preserves Allow; admission 503 includes Retry-After: 1. Callers returning 429 or other temporary failures must supply an appropriate Retry-After per operation policy. HEAD suppresses all bodies. Parser-level errors occur before this engine and may use different transport responses.

Auth/forbidden/conflict/rate-limit codes are common contracts only; no authentication, permission, business operation or rate-limit service is implemented. Field details use reviewed static metadata, lexical sorting/deduplication and at most 20 items. Financial success must only follow the authoritative commit.

Step 8 reuses existing registry entries rather than adding duplicate authentication errors: UNAUTHENTICATED for unknown identifier/wrong/no password, invalid/replayed Google proof or invalid/revoked bearer; FORBIDDEN for restrictions after valid proof/credentials; CONFLICT for alias uniqueness; VALIDATION_FAILED for malformed setup input; RATE_LIMITED with Retry-After:60; SERVICE_UNAVAILABLE for unavailable security state or missing Google audience. Raw provider/SQL/credential causes are never public. Success codes and projections are defined in identity.openapi.json.

## Step 9 reuse

No new error codes: strict JSON/unknown fields use INVALID_JSON (400), semantic/null/Unicode/pagination/catalog failures VALIDATION_FAILED (422), missing/expired/revoked session UNAUTHENTICATED (401), missing scoped authority/protected-owner changes FORBIDDEN (403), absent authorized target NOT_FOUND (404), uniqueness/reference/list-cap/bootstrap conflicts CONFLICT (409), per-account minute budget RATE_LIMITED (429, Retry-After: 60), unavailable security/audit storage SERVICE_UNAVAILABLE (503). Existing middleware cancellation mappings remain unchanged. Success codes: PROFILE_FETCHED, PROFILE_UPDATED, PERMISSIONS_FETCHED, USERS_FETCHED, USER_FETCHED, USER_PRIVATE_FETCHED, RESTRICTION_UPDATED, ROLES_FETCHED, ROLE_SAVED, ROLE_DELETED, ASSIGNMENTS_FETCHED, ASSIGNMENTS_UPDATED. See profiles.openapi.json.


## Financial batch

| Code | HTTP | Meaning |
| --- | --- | --- |
| FINANCIAL_CONFIGURATION_REQUIRED | 409 | No applicable enabled/supported explicit policy or review-only workflow configuration |
| INSUFFICIENT_DIAMONDS | 409 | Insufficient available diamonds for requested principal and fee |
| VERIFIED_ACTIVITY_UNAVAILABLE | 503 | Agency host cannot be authorized without trusted monthly activity |
| WITHDRAWAL_NOT_ELIGIBLE | 403 | Configured activity/balance/withdrawal eligibility requirement not satisfied |

Existing VALIDATION_FAILED covers invalid integer/range/min-max/transfer-period-limit inputs; CONFLICT covers fingerprint/version/state mismatches. Unavailable PostgreSQL/audit fails closed as SERVICE_UNAVAILABLE; no SQL/credential/provider details are returned. A successful review response reports settlement_status=not_configured, never paid or settled. Endpoint success/error DTOs and idempotency headers are in wallet.openapi.json.

Live/agency endpoints reuse registered VALIDATION_FAILED, UNAUTHENTICATED, FORBIDDEN, NOT_FOUND, CONFLICT, PAYLOAD_TOO_LARGE, RATE_LIMITED and SERVICE_UNAVAILABLE (plus the existing decoder/lifecycle error codes). No provider SDK/SQL error text is public. Successful stable codes: LIVE_COMMAND_ACCEPTED, LIVE_RESOURCE_FETCHED, LIVE_TOKEN_ISSUED, LIVE_EVENT_ACCEPTED, LIVE_RECONCILIATION_ACCEPTED. Accepted provider intent is not applied media state or financial settlement; live pending/retry semantics are in LIVE_AGENCY.md and live.openapi.json. Conflicting event IDs/command fingerprints/policy predecessors and occupied seats return CONFLICT; byte-bound webhook rejection returns PAYLOAD_TOO_LARGE; invalid signature returns UNAUTHENTICATED; provider recovery failures return SERVICE_UNAVAILABLE for explicit retry.

Delivery endpoints reuse existing stable VALIDATION_FAILED/INVALID_JSON/UNAUTHENTICATED/FORBIDDEN/NOT_FOUND/CONFLICT/RATE_LIMITED/SERVICE_UNAVAILABLE and Wallet financial codes; no provider error strings escape. Delivery outbox diagnostics (not HTTP authorization/financial codes) include PROVIDER_UNAVAILABLE, PROVIDER_REJECTED, RETRY_EXHAUSTED, INVALID_INTENT, INVALID_REGISTRATION, BINDING_OR_PREFERENCE_INACTIVE, ROOM_CLOSED, ROOM_SYNCHRONIZING and object/asset errors. Generic endpoint success is DELIVERY_REQUEST_COMPLETED; POST room gifts explicitly separates financial_status=committed from delivery_status=not_confirmed.

Consolidated discovery adds LIVE_LIST_RESTART_REQUIRED (409): the actor-bound list snapshot expired or a relevant revision/term boundary changed; restart with no cursor. Package purchase reuses FINANCIAL_CONFIGURATION_REQUIRED for missing/inactive authoritative pricing/accounting, INSUFFICIENT_DIAMONDS for available-fund failure, CONFLICT for version/fingerprint/term-quota conflicts, FORBIDDEN for protected powers/denials and SERVICE_UNAVAILABLE for unavailable authority. New generic success is CAPABILITY_RESOURCE_COMPLETED; a purchase reports financial_status=committed only after the atomic Wallet transaction commits, with immutable starts_at/expires_at (renewal may be scheduled). Migration diagnostics retain private inspection and print only safe operation/deadline/SQLSTATE categories.
