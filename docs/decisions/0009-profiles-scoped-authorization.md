# ADR 0009: profile ownership and PostgreSQL scoped staff authority

Status: owner-authorized Step 9 implementation; independent review pending.

Use the concrete design in ../architecture/PROFILES_AUTHORIZATION.md. Profiles owns display_name/bio/avatar; migrate/backfill without dual writers. Identity retains authentication and composes profile reads. A narrow initializer bound to the same pgx transaction makes signup atomic.

Four fixed permissions, editable roles, independently protected owner authority and atomic audit replace implicit role-name privileges. A single transaction advisory security lock gives deterministic cross-instance revocation ordering for this bounded administration scope. This intentionally trades admin throughput for simple, auditable correctness; measure before finer locking. Redis never authorizes. No new dependencies or deployment changes.
