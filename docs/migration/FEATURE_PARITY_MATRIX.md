# Feature Parity Matrix

> Superseded on 2026-11-24 by greenfield requirements. Historical note only. Current coverage is tracked in `../business/FEATURE_COVERAGE_MATRIX.md`; old-system parity is not the implementation objective.

Status: Source inventory blocked. No legacy feature has been discovered or verified from executable evidence.

Feature IDs will be assigned to source-backed behaviors and remain stable. Permitted statuses: discovered, specified, implemented, verified, deferred, blocked. Required project coverage is an investigation checklist, not proof that a legacy feature exists.

| Feature ID | Behavior | Legacy source / symbol / commit | Routes / contracts | Roles | Inputs / outputs | Side effects | Data dependencies | Edge cases | Target code / tests | Status | Intentional differences / acceptance evidence |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |

No feature rows are populated until the legacy source is available. The initial Go startup message establishes no migrated feature.

## Required investigation coverage

- Identity, profiles, follows/blocks, levels, agency/host classification and live time.
- Reseller stock, gifts/commissions, wallet, transfers, withdrawals/refunds and financial recovery.
- VIP/VVIP/King/Queen memberships, themes and entry effects.
- Room permissions, seats, locks, host handover, reconnect and leaderboards.
- Gift Chain, Fireworks, Rocket, games and evidence of any PK battle implementation.
- Messenger/chat audit, feed, moderation/device bans, notifications, analytics, support and admin.
- Every router, view, serializer, model, migration, constraint, signal, admin action, permission, WebSocket event, scheduled task, integration, feature flag, disabled path and compatibility path.
- Additional behavior discovered during the audit.

For each behavior, distinguish observed code from documentation claims and deployed behavior. Future LiveKit, dynamic server registration, media clustering, horizontal scaling implementation and Flutter integration remain outside the current implementation scope; inventory existing dependencies during the audit.
