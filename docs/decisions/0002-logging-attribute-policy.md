# ADR 0002: Defensive synchronous logging policy

Date: 2026-11-24 (Asia/Dhaka)

Status: Implemented for authorized Step 2; awaiting source-level architectural review.

## Context

Structured attributes can expose credentials through attached attributes, nested groups, lazy LogValuer resolution, error formatting or arbitrary serialization. Duplicate JSON keys can disguise metadata or produce inconsistent collector interpretation. The owner requires a standard-library-only synchronous foundation, not a collector or asynchronous logging system.

## Decision

Construct explicitly injected slog loggers with built-in text/JSON handlers behind a defensive policy handler. Use JSON in staging/production and UTC timestamps everywhere. Attach validated service/environment/instance metadata beneath the policy so callers cannot replace it. No default/global logger mutation.

Sanitize before WithAttrs and Handle reach built-in resolution/formatting. Redact sensitive keys/groups, suppress arbitrary values and LogValuer without executing application methods, and permit explicitly selected primitive fields. Reserve metadata names and deduplicate every object using first accepted occurrence. Suppress colliding WithGroup branches and treat renamed invalid groups as sensitive. Preserve immutable handler state for concurrent children.

Require static bounded event names/keys; replace invalid events and rely on standard handler escaping for control characters in approved primitive values. Safe field selection remains the primary privacy control: no key policy can sanitize arbitrary strings.

## Consequences

This deliberately sacrifices automatic object/error formatting and duplicate-group merging. Operators provide non-secret unique instance labels. Synchronous sinks can block and ordinary slog calls discard sink errors; startup explicitly checks its write. Logging is not a durable financial audit mechanism. Per-record sanitization/deduplication has allocation cost; no performance gain is claimed. Request correlation, collectors, rotation and asynchronous delivery remain deferred.

See `docs/architecture/STRUCTURED_LOGGING.md` for usage and `docs/architecture/FOUNDATION_VERIFICATION.md` for actual checks, coverage and limitations.
