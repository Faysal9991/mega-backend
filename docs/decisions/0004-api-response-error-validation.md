# ADR 0004: Common API response, error and validation boundary

Date: 2026-11-24 (Asia/Dhaka). Status: Implemented under explicit Step 4 authorization; architectural review pending.

## Decision

Complete and reuse existing helper packages rather than create a parallel engine. Application errors remain independent of HTTP; transport owns status mapping and immutable public envelopes. Reuse requestmeta as the single correlation context while preserving the public httpserver.RequestID accessor. Inject the existing slog logger and response writer; no runtime dependency is added.

Stage compact JSON before final commitment with a 1 MiB output cap. Preserve Chi's existing protocol-aware writer capabilities, informational/final distinction, completion logging and cooperative timeout behavior. Refuse writes after commitment. Preserve explicit ErrAbortHandler; abort partial-response panics and failed middleware writes without appending an error.

Use the existing request body limiter plus bounded decoder staging. Require object DTOs, application/json and explicit numeric types. Retain encoding/json's case-insensitive struct matching but reject every object's duplicate/Unicode-fold-equivalent keys in a token pass. Collect at most 20 lexically ordered diagnostics. Pagination validates metadata only, with null terminal cursors. Common OpenAPI paths remain empty.

## Consequences

Temporary text errors become the new JSON contract, with no legacy adapter. Static safe metadata is a caller obligation; character checks do not detect secrets. encoding/json can allocate internally beyond staged output; DTOs require independent bounds. Custom nested decoders/maps need reviewed endpoint semantics. JSON Schema character lengths do not replace runtime UTF-8 byte limits. Schema validation tooling is isolated development tooling, not a Python backend dependency. No business behavior, identifier/amount representation, health endpoint, database or production performance is approved by this step. See API_RESPONSE_ENGINE.md and FOUNDATION_VERIFICATION.md.
