# Consolidated implementation and fixes

Owner request: 2026-11-25, baseline e94b765, clean worktree. Existing delivery code is committed in this baseline; preserve it. Wallet/Live independent reviews remain pending.

1. Diagnose migration ordering, locks, startup and fixture boundaries using only guarded biho_live_test. Correct demonstrated defects; retain runtime deadlines and data guards. Record an external blocker with evidence rather than indefinite retries.
2. Extend Access as the single current-state capability resolver: bounded catalog, immutable policy, direct grants/denials, room roles, protected owner and safe active-package powers.
3. Integrate global staff moderation, hidden entry, distinct global live bans, atomic self-seats and dynamic room policies into existing Live operations/provider reconciliation.
4. Add versioned package catalog, direct entitlements, immutable purchases and Wallet-owned atomic posting. Financial distribution requires explicit approved policy; no free/missing-policy activation.
5. Integrate indexed host-package priority into the actual live discovery endpoint with versioned/expiry-aware cursors and availability filters.
6. Add scoped APIs/contracts, security/concurrency/regression fixtures and traceability. Run one final full local verification set and one serialized integration chain after source freeze. No deploy, infrastructure changes, commit or push.

Acceptance: policy revocation across instances, deny precedence, protected-power rejection, hidden projections/provider grants, distinct live restrictions, seat races, purchase reconciliation/idempotency, ranking traversal/stale cursors, durable audit rollback, and existing module regressions. Actual external SDK/client behavior remains NOT TESTED without controlled resources. Owner approved 100% platform diamond profit without VAT/commission and appended renewal periods after the existing same-package term. No business-policy clarification remains pending.

Execution complete locally (2026-11-26): all six planned areas implemented, full local set and all eight serialized PostgreSQL/race packages PASS on final attempts. Two test-fixture corrections and actual outcomes are retained in CONSOLIDATED_BATCH_VERIFICATION.md. Stop at this backend acceptance boundary; independent reviews and actual external E2E/load/operations gates remain open. No commit/push/deploy.
