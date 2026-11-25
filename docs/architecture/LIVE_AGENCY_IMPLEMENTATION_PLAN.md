# Authorized LiveKit / agency batch

Baseline: main HEAD 645c75c; initially clean worktree. Owner request on 2026-11-25 authorizes media backend integration, agency workflows and verified activity, superseding earlier deferral only for these capabilities. Wallet independent review remains pending. No legacy source is available or required for these owner-defined capabilities.

1. Add additive migration, explicit sqlc queries and durable provider operations. Preserve agency IDs as owner account IDs because Wallet references that identity; immutable owner, one active association per host.
2. Add bounded official SDK adapter, restrictive validated compatibility policy, application services, scoped authorization and audit, room/seat/moderation and agency workflows.
3. Verify signed webhooks, durable deduplication and immutable completed activity intervals. Connect source to existing Wallet eligibility without new financial writers.
4. Add mobile/admin/webhook OpenAPI contracts and deterministic/provider/PostgreSQL tests; run mandatory checks; consolidate actual results and limits.

Acceptance: no credentials printed, no production access, no deployment/commit/push, no invented financial policy; live E2E explicitly NOT TESTED without controlled server. Provider recovery must remain visible and retryable. Out-of-order/missing evidence must never fabricate duration. Unknown cross-classification compatibility denies by default. Review pending until independently supplied.

Owner clarification ADR 0013 is included: preserve historical records, clear active context on leave and start qualifying counts from zero at another agency/rejoin. The central writer/context journal makes the transition atomic with the approved association operation. Schema 1–8, default/race/build/vet/sqlc/contracts, context-reset/batched-reconciliation and actual unavailable-cache HTTP checks passed; full schema-8 Live PostgreSQL/race regression passed (482.48s). A provider-timeout boundary issue found during final runs was fixed and covered by desired-participant/deadline regression; initial failures remain in the consolidated evidence. Scope stops at independent review and real-provider E2E.
