# ADR 0005: Operational health and lifecycle readiness

Status: Implemented under explicit Step 5 authorization; architectural review pending.

Inject a single atomic, initially unready Lifecycle into router and Server. Serve with an already-bound listener marks ready after initialization; shutdown/close and Serve exit mark terminally unready. CAS prevents startup from reversing a concurrent stop. Keep the existing single-use server lifecycle and drain behavior.

Add exact GET/HEAD /health/live and /health/ready. Use the existing response engine with empty data objects and safe codes; readiness failure uses SERVICE_UNAVAILABLE. Set no-store and explicit GET/HEAD method semantics. Probe paths bypass ordinary admission slots while retaining the remaining HTTP protections, request IDs, logging and recovery. Do not add dependency checks or background work. Operational OpenAPI paths reference the separate, unchanged common schemas.

Consequences: liveness reflects ability to serve the probe; readiness is a lifecycle snapshot, independent of ordinary admission saturation. Listener closure may prevent any HTTP response during shutdown; no drain sleep or guaranteed post-close 503 is introduced. Logging/CPU/connection exhaustion can still affect probes. See HEALTH_READINESS.md for exact boundaries and FOUNDATION_VERIFICATION.md for actual checks and review status.
