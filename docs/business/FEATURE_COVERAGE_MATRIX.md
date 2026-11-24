# Feature Coverage Matrix

Status: Business requirements inventory with incremental foundation evidence. Step 1 source-level architectural review is approved by the owner at bf409ff. Step 2 source-level review is approved by the owner against inspected baseline 7dc9d97; Step 3 HTTP implementation and Step 4 response/error/validation foundation await architectural review. No business module is authorized or verified.

Business rows have these unresolved fields until individually specified: reference path/symbol/commit, approved detailed requirements, roles, inputs/outputs, side effects, data dependencies, edge cases, new API/code/tests and intentional changes. Foundation evidence is recorded separately per row. No business API route or physical table is assumed. Source availability does not block the independent foundation task.

Permitted statuses: discovered, specified, implemented, verified, deferred, blocked, excluded (owner-approved only). `discovered` means a requested capability needs specification, not that reference behavior has been validated.

| Stable ID | Capability | Requirement / evidence | Status | Gate / acceptance |
| --- | --- | --- | --- | --- |
| FND-001 | Foundation inspection and system architecture | Current owner task; AGENTS.md; local Go bootstrap | specified | Inspection/check results and architecture review; no runtime feature claim |
| FND-002 | Typed runtime configuration | Owner Step 1 requirements; `internal/platform/config/config.go`, `config_test.go`; `cmd/api/main.go`, `main_test.go`; `docs/architecture/RUNTIME_CONFIGURATION.md` | verified | Owner confirms source-level architectural review passed at bf409ff; actual local test results recorded separately in FOUNDATION_VERIFICATION.md; no production performance or runtime-service claim |
| FND-003 | Structured logging foundation | Owner Step 2 requirements; `internal/platform/logging/`, `internal/platform/config/logging_test.go`, startup tests; `docs/architecture/STRUCTURED_LOGGING.md` | verified | Owner source-level approval recorded against 7dc9d97; earlier local results remain distinct from production evidence; logging behavior/security policy preserved in Step 3 |
| FND-004 | HTTP lifecycle/router/middleware foundation | Owner Step 3 requirements; `internal/platform/httpserver/`, `internal/platform/config/http_test.go`, `cmd/api/lifecycle_test.go`; HTTP_SERVER.md and MIDDLEWARE.md | implemented | Actual local checks in FOUNDATION_VERIFICATION.md; architectural review pending; no business/health endpoints; Step 4 response integration is tracked by FND-005 |
| FND-005 | API response, error and validation foundation | Owner Step 4 request; `internal/platform/{apperror,response,validation,requestmeta}/`; HTTP middleware integration and tests; `contracts/openapi/common.openapi.json` and fixtures; API_STANDARDS.md, ERROR_CODES.md, API_RESPONSE_ENGINE.md | implemented | Local command/contract/security/concurrency evidence in FOUNDATION_VERIFICATION.md; architectural review pending. Roles: transport callers; input: typed DTOs/errors/bounded JSON; output: safe envelopes/diagnostics; effects: HTTP/logging only; dependencies: existing Chi/config/logger; edge cases: HEAD/204/Flush/commitment, panic, body/encoding/write failure. Intentional change: temporary text errors become JSON; shared request IDs. No business approval inferred |
| IDN-001 | Identity, sessions and recovery | Requested coverage; no reference evidence | discovered | Verify providers, challenge/recovery policy and session requirements |
| PRO-001 | Profiles | Requested coverage; no reference evidence | discovered | Approve fields, privacy and editing authority |
| SOC-001 | Follows and blocks | Requested coverage; no reference evidence | discovered | Approve relationship and visibility semantics |
| LVL-001 | Levels | Requested coverage; no reference evidence | discovered | Approve thresholds, counters and calculation rules |
| AGY-001 | Agency/host classification and live time | Requested coverage; no reference evidence | discovered | Approve role transitions, accounting periods and time eligibility |
| RSL-001 | Reseller stock and recharge | Requested coverage; no reference evidence | discovered | Approve stock units, ownership and Wallet settlement |
| WAL-001 | Wallet, ledger and reservations | Mandatory financial invariants in AGENTS.md; detailed policies unknown | discovered | Approved arithmetic/eligibility; concurrency, recovery and reconciliation evidence |
| GFT-001 | Gifts and commissions | Requested coverage; no reference evidence | discovered | Approve price/recipient rules, rates, rounding and settlement |
| TRF-001 | Transfers | Requested coverage; no reference evidence | discovered | Approve limits, eligibility, fees and rounding |
| WDR-001 | Withdrawals and refunds | Requested coverage; no reference evidence | discovered | Approve state machine, provider recovery and refund policy |
| ENT-001 | VIP/VVIP/King/Queen memberships | Requested coverage; no reference evidence | discovered | Approve entitlement grants, expiry, renewals and pricing |
| COS-001 | Themes and entry effects | Requested coverage; no reference evidence | discovered | Approve ownership, activation and eligibility |
| ROM-001 | Room permissions, seats, lock, handover and reconnect | Requested coverage; no reference evidence | discovered | Approve room/live-session state machine and recovery; media excluded |
| LDR-001 | Leaderboards | Requested coverage; no reference evidence | discovered | Approve ranking windows, ties and authoritative counters |
| ENG-001 | Gift Chain | Requested coverage; no reference evidence | discovered | Approve trigger, duration, state and monetary effects |
| ENG-002 | Fireworks | Requested coverage; no reference evidence | discovered | Approve trigger, state and monetary effects |
| ENG-003 | Rocket | Requested coverage; no reference evidence | discovered | Approve trigger, state and monetary effects |
| GAM-001 | Games | Requested coverage; no reference evidence | discovered | Approve each game's rules, fairness and Wallet settlement |
| PKB-001 | PK battle scope assessment | Explicit requirement to verify scope; no evidence of implementation | discovered | Owner decision after evidence; no assumed game mechanics |
| MSG-001 | Messenger/chat audit | Requested coverage; no reference evidence | discovered | Approve access, delivery, audit/retention and privacy |
| FED-001 | Feed | Requested coverage; no reference evidence | discovered | Approve content, visibility, moderation and pagination |
| MOD-001 | Moderation and device bans | Requested coverage; no reference evidence | discovered | Approve enforcement scopes and appeals; device ID is not identity proof |
| NTF-001 | Notifications | Requested coverage; no reference evidence | discovered | Approve channels, preferences, retry and deduplication |
| ANL-001 | Analytics | Requested coverage; no reference evidence | discovered | Approve event definitions, privacy and retention |
| SUP-001 | Support | Requested coverage; no reference evidence | discovered | Approve ticket/access and audit requirements |
| ADM-001 | Backend admin operations | Requested coverage; no reference evidence | discovered | Approve scoped permissions; use owner services for sensitive writes |
| MED-001 | LiveKit/media integration and clustering | Explicit future scope | deferred | Separate authorization; architecture seams only |
| INF-001 | Dynamic registration, provisioning and autoscaling | Explicit future scope | deferred | Separate infrastructure and security design/authorization |
| MOB-001 | Flutter integration | Explicit future scope | deferred | Separate client authorization |
| AUI-001 | Admin UI | AGENTS.md defers implementation | deferred | Separate UI authorization |
| HIS-001 | Historical data import / client compatibility / cutover | AGENTS.md requires separate future decision | deferred | Explicit owner decision and dedicated plan |

Additional discovered capabilities receive new stable IDs. Do not mark requirements verified merely because documentation or a placeholder exists. Implementation rows must link approved requirements, source/API/code/tests and actual acceptance evidence.
