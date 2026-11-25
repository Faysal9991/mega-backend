# ADR 0011: current-month agency-host withdrawal activity

Status: final owner-confirmed business decision, 2026-11-25. Implementation remains independent review pending; no production approval is implied.

The withdrawal activity period is the **current calendar month**, from business month-start through eligibility check time. This supersedes the earlier previous-completed-month proposal in ADR 0010 and the initial financial batch. Default eligibility requires at least 15 distinct qualifying business dates, each with at least 120 verified live minutes. Once met, the activity requirement is satisfied immediately, without waiting for month-end. Apply the configured audio-host/video-host policy or explicit shared agency scope and preserve its immutable version.

A required configurable IANA business timezone determines day/month boundaries, independently of server/caller timezone; no timezone is silently selected. Count only completed authoritative live-duration segments, merge overlaps and deduplicate identities before splitting into business dates. No client duration or missing activity is trusted/fabricated. Runtime collection remains unavailable until a trusted adapter is implemented, so the existing gate stays closed.

Every new withdrawal command obtains fresh authoritative activity independently of display. Recheck eligibility atomically with current association/policy, account restrictions, funds and other withdrawal requirements inside the guarded financial transaction. Collect outside financial locks, retain the actual source cutoff in the snapshot, and reject month/policy/association changes before accepting. Do not carry prior-month eligibility into a new month. The monthly reset is a new calculation window, never deletion or rewriting of history.

Publish a new immutable current_calendar_month policy version to activate the corrected selector. Existing previous-period configurations fail closed; do not silently reinterpret them. Historical activity and accepted withdrawal/policy snapshots remain unchanged. All other approved wallet, fees, caps, review/reservation and provider-settlement gates remain unchanged. No migration, live collector, payout integration or active commercial policy is introduced by this change.

Implementation: internal/modules/agency/activity.go and the existing Wallet transaction eligibility boundary. Contracts: wallet.openapi.json. Checks and remaining limitations: FINANCIAL_VERIFICATION.md.

Collector follow-up: the owner-authorized LiveKit/agency batch at baseline 645c75c supplies a PostgreSQL trusted source when provider configuration is enabled (ADR 0012). This does not change this decision's period, policy/association/month recheck or financial approval gates. Disabled/unverified collection remains fail closed; real LiveKit E2E and independent review remain pending.
