# ADR 0008: PostgreSQL-authoritative identity and opaque sessions

Status: Step 8 owner-authorized implementation; architectural review pending.

Use the concrete schema, routes, defaults and transaction policy in ../architecture/IDENTITY_AUTH.md. Google verified subject is the sole account-creation path. Explicit server nonce challenges bind login/reauthentication and single-use proofs bind sensitive mutations to the already-linked subject. No email/phone account linking. Opaque random access/refresh tokens avoid application signing-key management and make revocation/account/installation checks authoritative on every protected request. Rotated refresh history detects reuse; concurrent/ambiguous refresh requires reauthentication.

Add pinned maintained coreos/go-oidc/v3 v3.21.0 for reviewed RS256/JWKS validation/caching and golang.org/x/crypto v0.59.0 for Argon2id rather than handwritten cryptographic verification or hashing. The default Google TLS transport is independent of testing database/Redis exceptions. Hash concurrency and durable rate limits bound expensive work. Identity owns generated SQL separately from platform connectivity. No new service/infrastructure, Redis authority, universal user_type or deferred business workflows.

Migration 2 is explicit, additive and reversibly drops only identity-owned tables; production data rollback requires separate review. Existing isolated migration/version/failure tests must include version 2 and fail injected version 3. No runtime automatic migration.
