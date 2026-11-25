# Agency-host activity eligibility boundary

Status: implemented for independent review as part of the owner-authorized Wallet / economy batch at inspected HEAD `442a8d8ad6b3410bda2aed08c91561e56d364b0f`. This module evaluates only the activity requirement, not final withdrawal eligibility or settlement. No verified live-activity collector, LiveKit integration, client duration submission, or provider settlement is implemented here.

## Ownership and configuration

`internal/modules/agency` defines `Association`, `Policy`, `Session`, `ActivitySource`, `Result`, and the pure calculator. Agency classification is exactly `audio_host` or `video_host`; it is separate from account status, staff permissions, premium entitlements and wallet ownership. An association is effective on `[effective_from, effective_until)`; changing classification closes the prior relationship and creates a new historical relationship. The batch integration owner supplies the minimal persistence and financial references; this pure package performs no SQL, Redis, HTTP or account writes.

A policy has a version, effective timestamp, scope (`agency`, `audio_host`, or `video_host`), explicit business timezone, supported period selector, and editable thresholds. `Policy.AppliesTo` handles exact classification or shared agency scope. `Policy.Validate` rejects missing version/effective timestamp, invalid scope/timezone, host-dependent `Local`, unsupported period selectors, days outside 1–31 or daily minutes outside 1–1440. These numeric bounds are supported-configuration limits; a configuration outside them remains unsupported rather than being silently coerced.

The owner's initial activity thresholds are constants `InitialQualifyingDays=15` and `InitialDailyLiveMinutes=120`. They are not an automatically active financial policy. The proposed previous completed calendar month is the only implemented selector (`previous_completed_calendar_month`), and the timezone is mandatory. No default timezone or financially binding period is installed; a policy must explicitly configure them. Unsupported periods/configurations fail closed. Current requests use the immutable policy selected by the financial service; completed request snapshots retain their prior policy version and calculation. Historical association-transition proration is not invented by this calculator.

## Authoritative source and results

`ActivitySource.Sessions(ctx, account_id, period_start, period_end)` must provide a complete, bounded, authoritative snapshot of verified session intervals overlapping the requested half-open interval. Crossing sessions must be included. Each `Session` carries a server-owned immutable live-session/interval ID, account ID, start and end; a room/channel ID alone is insufficient identity. Only closed intervals ending no later than the injected calculation time are accepted. No caller-submitted watch/live durations, installation IDs, Redis presence or client assertions are accepted as evidence.

The application composition must inject `UnavailableSource{}` until a trusted collector exists. A nil source or source error yields `eligible=false` with `VERIFIED_ACTIVITY_UNAVAILABLE`. Cancellation is propagated; a source that returns successful data after cancellation cannot publish an eligible result. Invalid/cross-account/reversed/future intervals and conflicting duplicate IDs fail closed with `INVALID_VERIFIED_ACTIVITY`; they are not partially counted. An authoritative successful empty snapshot is different: it returns zero qualifying days and `INSUFFICIENT_QUALIFYING_DAYS`.

`Result.Eligible` means only `ACTIVITY_REQUIREMENT_MET`; financial orchestration must still check account restrictions, available funds, amount/fee/period policies, reservations and provider configuration. It must not infer payout approval or settlement from activity qualification.

## Deterministic calculation and bounds

1. Resolve the previous completed month in the explicitly configured business timezone; expose the exact period boundaries as UTC timestamps.
2. Validate account/session identities, the entire bounded source snapshot and exact duplicate consistency. Reject snapshots over 10,000 records; the source adapter must also limit its query and allocation. No network call occurs while ledger or financial transaction locks are held.
3. Clip intervals to the selected month, deduplicate identical session IDs and reject contradictory duplicates. Sort a private interval copy and take their union across sessions, so neither overlapping sessions nor duplicate records multiply duration. Caller input is not mutated.
4. Split union intervals by business calendar days and timezone transitions. Use elapsed duration rather than wall-clock hour subtraction: daylight-saving days may have 23 or 25 hours. Skipped/repeated midnight boundaries are handled explicitly; a skipped civil day receives no fabricated activity.
5. Compare the exact accumulated daily duration against `minimum_daily_live_minutes * time.Minute`. Fractional seconds are retained until qualification; 119 minutes 59.999 seconds does not become 120 minutes. `verified_seconds` is a whole-second diagnostic projection, not the value used to round qualification upward.
6. Count distinct qualifying date keys and compare with the minimum-day threshold. Fourteen qualifying dates never become fifteen because of extra same-day hours; time that genuinely crosses into another business date belongs to that date. Output dates are sorted and empty day collections are `[]`.

Memory and sorting are bounded by the 10,000-record snapshot; the result is bounded by the month. There are no remote calls per session/day and no global mutable calculator state. No benchmark, production accuracy claim or VPS capacity claim follows from unit-test durations.

## Verification

`go test -race -count=1 ./internal/modules/agency` passed. Tests cover audio/video/shared policy scopes; 14 vs 15 qualifying days, 119 vs 120 minutes, extra same-day hours; duplicate/overlapping/disjoint sessions; fractional-second boundaries; source input immutability; month clipping, cross-midnight sessions and timezone-independent caller clocks; New York 23/25-hour DST days; Santiago's skipped midnight and Havana's repeated midnight; Apia's skipped civil day boundary; unavailable/malformed/cross-account activity, cancellation and post-source cancellation; policy validation/future policies/history snapshots; resource bounds; effective association history; and parallel deterministic calls under the race detector.

Initial tests caught a fixture mistake: its eight-hour sessions began at 19:00 and correctly crossed into a fifteenth business day. The fixture now starts at 10:00 so the intended fourteen-distinct-day assertion is valid; no duration or threshold assertion was weakened. No real activity, external service, database mutation or settlement was used. Full repository / financial integration verification is owned by the consolidated batch report.
