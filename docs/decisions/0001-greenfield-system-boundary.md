# ADR 0001: Greenfield modular monolith boundary

Date: 2026-11-24 (Asia/Dhaka)

Status: Accepted constraints from owner direction and AGENTS.md; detailed module partitioning remains proposed.

## Context

Earlier workspace documentation treated this project as a Python-to-Go migration. The owner explicitly corrected the mission to new software development. Current engineering rules match that direction. Existing code is only a Go module and print-only entry point; no historical source is available in the workspace.

## Decision

Build a new Go modular monolith with the mandated stack and freshly designed schema, `/api/v1/` contracts and services. Use Django only as read-only evidence for feature discovery and approved rules. No Python runtime, legacy schema, compatibility adapter or historical import is assumed. Old documents under `docs/migration/` are superseded.

PostgreSQL owns durable state; Wallet is the single owner of monetary writes. Design stateless HTTP replicas and explicit recovery instead of relying on sticky sessions. Keep media and infrastructure-management implementation deferred. Introduce code/packages/dependencies one authorized capability at a time.

## Consequences

Independent foundation design can proceed without a complete reference audit. Business policies still require approval and traceability; no legacy rounding or permission rule becomes mandatory by inference. Historical import, client compatibility and cutover require separate owner decisions. No claim of feature parity, production readiness or measured performance follows from this architecture.
