# Implementation Roadmap

> Superseded on 2026-11-24 by the owner's greenfield direction and updated AGENTS.md. Historical note only. Follow `../IMPLEMENTATION_ROADMAP.md`; unavailable reference source does not block independent foundation work.

Date: 2026-11-24 (Asia/Dhaka)

## Current authorization

Phase 1: legacy source audit and architecture planning only, as required by `AGENTS.md`. No business module implementation is authorized. Phase 1 is blocked on legacy source availability and has not passed acceptance.

## Phase 1 work sequence

1. Obtain the legacy source location and resolve the audit commit. Inspect its applicable instructions, configuration references, migrations and tests without modifying legacy executable code.
2. Inventory all entry points, background work, schemas, integrations, permissions and recovery paths. Populate stable, evidence-backed parity IDs, recording unknowns and conflicting evidence.
3. Trace business and financial rules, exact v1 contracts and compatibility dependencies. Separate verified behavior from proposed security corrections; escalate ambiguous financial and permission rules.
4. Complete the required audit documents: `EXISTING_SYSTEM_AUDIT.md`, `FEATURE_PARITY_MATRIX.md`, `BUSINESS_RULES.md`, `PERMISSION_MATRIX.md`, `API_INVENTORY.md`, `DATABASE_ANALYSIS.md`, `DATA_MIGRATION_PLAN.md`, `SECURITY_ANALYSIS.md`, `PERFORMANCE_ANALYSIS.md`, `TESTING_STRATEGY.md`, `../architecture/GO_ARCHITECTURE.md` and `../api/API_STANDARDS.md`.
5. Refine this roadmap into evidence-based module phases, dependencies and acceptance gates. Record architecture decisions in `docs/decisions/`. Review the audit with the owner.

## Phase 1 acceptance gate

- Legacy commit and source references are recorded and all required investigation categories are accounted for.
- Contracts, financial rules, permissions and data preservation requirements are traceable; unresolved questions and release blockers remain explicit.
- Target architecture, compatibility strategy, testing, performance baseline collection, migration and rollback plans are reviewable.
- Each proposed implementation phase has a defined scope, dependencies, checks and acceptance gate.
- The owner reviews Phase 1 and explicitly authorizes Phase 2 before implementation begins.

## Subsequent implementation

Exact module ordering remains pending source analysis. The mandated target is a Go domain-driven modular monolith with PostgreSQL, pgx v5/pgxpool, sqlc and Redis, following the additional stack and engineering requirements in `AGENTS.md`. Backend instances must support stateless operation and explicit cross-instance recovery design. Actual infrastructure scaling, LiveKit, dynamic server registration, media clustering and Flutter integration are deferred.

Financial migration requires one authoritative wallet writer, atomic ledger/idempotency/outbox changes, reconciliation and an approved cutover/rollback process. No production access, deployment, commit or push is authorized by this roadmap.
