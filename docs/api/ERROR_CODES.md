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
