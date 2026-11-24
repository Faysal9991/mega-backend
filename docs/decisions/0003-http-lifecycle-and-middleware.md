# ADR 0003: HTTP lifecycle, middleware and trust boundary

Date: 2026-11-24 (Asia/Dhaka)

Status: Implemented for authorized Step 3; awaiting architectural review.

## Context and decision

Step 2 source-level review is approved by the owner against inspected logging baseline 7dc9d97e9c21584c05a6b10263beab905bba12dc. Preserve its logger/configuration behavior while introducing only HTTP lifecycle and middleware. Production performance remains unmeasured.

Use net/http with pinned Chi v5.5.0, whose tag module requires Go 1.24 and compiles on project Go 1.26.3. Chi is explicitly mandated by AGENTS.md; no other module is added. Explicit constructor injection separates router, binding, serving and bounded shutdown.

Order middleware as request ID, completion logging, recovery, nonwaiting admission, body limit and cooperative deadline. Use Chi's protocol-aware response writer; no entire-response buffering. Use an infrastructure root mount so even an empty business mux runs middleware. Keep 405 Allow semantics through isolated standard-method probes, including escaped paths. No fake business route is registered.

Use crypto/rand for replacement IDs; accept only one strict bounded incoming ID as untrusted correlation. Ignore all forwarded-IP headers, use only the validated peer and defer trusted-chain configuration. Log only safe route templates and bounded methods, never raw paths or payloads.

Recover pre-commit panic to safe 500; abort post-commit streams through ErrAbortHandler. Release admission during unwinding. A context deadline cooperatively cancels synchronous work without creating per-request timeout goroutines. Signals initiate separate bounded drain without prematurely canceling active request contexts. Force-close on drain expiry and propagate unexpected lifecycle errors.

## Consequences

Infrastructure errors are temporary plain status text pending Step 4. Parser-level failures cannot receive application correlation IDs. Raw net/http diagnostics are disabled to avoid unreviewed stack/request leakage. Synchronous logging and non-cooperative handlers remain operational risks; admission is not a complete connection/DoS budget. Hijacked/WebSocket drain and upgraded-stream accounting are future work. See HTTP_SERVER.md, MIDDLEWARE.md and FOUNDATION_VERIFICATION.md for evidence and limits.

Step 4 update: temporary plain-text errors are replaced by the common engine under ADR 0004. The original Step 3 review status is unchanged; no owner approval is inferred.
