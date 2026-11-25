# Foundation Verification

Date: 2026-11-24 (Asia/Dhaka)

The original sections below record historical snapshots. Step 1 through Step 5 results and scoped reviews are recorded in appended sections. Earlier statements about missing capabilities are historical; current Step 5 evidence and limitations are at the end.

## Outcome

The initialized Go package builds cleanly. Architecture preparation for the current task is complete; production foundation services are not implemented. FND-001 remains specified pending owner architecture review, rather than claiming verified runtime capability. Stop here before the next implementation step.

Read current `AGENTS.md` and `CLAUDE.md`, inspected all workspace files and directories, checked Git state, and reviewed the existing entry point and module. Git has no commits, so there is no recorded source commit; checks apply to the current uncommitted workspace. Existing executable code, Go module and engineering instruction files were unchanged.

## Actual checks

| Command / check | Result | Limit |
| --- | --- | --- |
| `go version` | PASS: `go1.26.3 darwin/arm64` | Confirms local toolchain matches declared version; no dependency compatibility audit |
| `go build -o /tmp/biho-foundation-check ./cmd/api` | PASS, exit 0 | Compiles print-only entry point; artifact outside repository |
| `go test ./...` | PASS, exit 0; `[no test files]` | Compilation check; no behavior tested |
| `go vet ./...` | PASS, exit 0 | Static checks for existing package only |
| `go test -race ./...` | PASS, exit 0; `[no test files]` | No concurrent business behavior exercised |
| `gofmt -l cmd/api/main.go` | PASS, no output | Existing source already formatted |
| `git check-ignore .env .env.local secrets/probe.pem bin/probe data/probe` | All paths ignored | Pattern inspection only; no probe files created |
| `git check-ignore .env.example` | Exit 1: not ignored | Example configuration can be tracked |
| Documentation validation | PASS: 10 architecture/planning/historical documents checked before this report; 32 unique coverage IDs | Checked terminal newlines, trailing whitespace, balanced code fences and superseded notices; no Markdown renderer used |
| `git diff --check` | No output | All current files are untracked; this is not whitespace evidence for them. Separate documentation checks were used |

## Changes and boundaries

Created system architecture, database design principles, API standards, implementation roadmap, feature coverage matrix, testing strategy, ADR 0001 and this verification report. Marked the three previous migration documents superseded; retained them as historical context. No dependencies, schemas, business endpoints, modules or infrastructure were added. No commit, push, deployment or production connection occurred.

The deliberate scope correction is greenfield design with new `/api/v1/` contracts. No old-client compatibility or data import is implied. No detailed business or permission behavior was approved by these documents.

## Open risks and next step

HTTP/configuration/logging/health foundations, PostgreSQL/Redis, migrations, SQL generation, OpenAPI executable contracts and CI remain absent. Accordingly, integration, contract, dependency-security, SQL generation, multi-instance and load checks were not run. No configured lint/security checks exist. No performance measurement was taken.

Reference source is unavailable; feature-level financial, permission and other business policies need evidence and explicit approval before their implementation. Identifier/wire amount types, session/retention policies, provider requirements and workload budgets remain open. These do not block the proposed next independent capability: typed runtime configuration, after owner authorization. See the roadmap for its isolated acceptance gate.

## Step 1 — Typed runtime configuration

Date: 2026-11-24 (Asia/Dhaka). Baseline inspected: commit `55b4db29977d6c646bd41955bacb14ac94ce47a2`. Working tree initially contained only the owner's module-path correction. `github.com/Faysal9991/mega-backend` is preserved and used by all new imports; no additional go.mod change was needed.

### Scope delivered

- Standard-library typed configuration package, injectable lookup and startup-only loading. No mutable globals, init loader, dotenv dependency or hot reload.
- Explicit defaults for development/test and required `APP_NAME`/`HTTP_HOST` in staging/production. Invalid/empty input fails without falling back; errors name the setting without retaining raw input or parser errors.
- Bounded ports, duration values and request-body bytes, supported modes, validated application name and literal bind address, and read/header-timeout consistency.
- Startup exits 1 for invalid configuration and 0 after successful validation. Fixed successful output explicitly states that no HTTP server exists.
- Safe `.env.example`, environment supply documentation, isolated table-driven tests and updated roadmap/feature evidence. FND-002 is implemented with automated acceptance evidence; architectural review remains pending.

### Actual verification results

Final commands after Go changes, all exit 0:

| Command | Actual result |
| --- | --- |
| `gofmt -w internal/platform/config/config.go internal/platform/config/config_test.go cmd/api/main.go cmd/api/main_test.go` | Formatted all changed Go files |
| `go build ./...` | PASS |
| `go test ./...` | PASS: `cmd/api`, `internal/platform/config` |
| `go test -race ./...` | PASS: both packages; no race reports |
| `go vet ./...` | PASS; no findings |
| `go test -cover ./internal/platform/config/...` | PASS; **100.0% statement coverage** |
| `git diff --check` | PASS; no whitespace errors in tracked diff |

Additional process checks built a temporary binary outside the repository and invoked it with isolated child environments: defaults and explicit production settings exited 0; missing production APP_NAME, empty port and malformed port exited 1. Invalid startup had no stdout and never echoed the sentinel input. Explicitly sourcing the checked-in `.env.example` in a child shell succeeded. Temporary artifacts were removed. These process checks preceded a final host-validation tightening (reject IPv6 zones); the final unit/race/build checks above include that change and its regression case.

`git check-ignore` confirmed `.env`, `.env.production`, `.env.local`, `secrets/probe.pem`, `probe.pem` and `probe.key` are ignored. No probe files were created. `.env.example` is not ignored. `git ls-files` found no tracked `.env`/`.env.*`, secret-directory, PEM or key files at inspection. Separate checks covered terminal newline, trailing whitespace and balanced documentation fences for newly untracked files, which `git diff --check` does not inspect.

### Changed files

- Added `internal/platform/config/config.go` and `config_test.go`.
- Updated `cmd/api/main.go`; added `cmd/api/main_test.go`.
- Added `.env.example` and `docs/architecture/RUNTIME_CONFIGURATION.md`.
- Updated `docs/IMPLEMENTATION_ROADMAP.md`, `docs/business/FEATURE_COVERAGE_MATRIX.md` and this report.
- Existing owner change to `go.mod` remains intact. No external dependency, database schema or business/compatibility rule was added or changed.

### Acceptance and limits

All Step 1 automated checks pass. Tests exercise defaults, all settings/modes, missing versus empty values, syntax/overflow/range failures, safe formatting, snapshot independence and lookup counts without global environment changes. Coverage is statement coverage, not a proof of every deployment condition.

Configuration values are not yet enforced by a running HTTP server. Bind availability, port privileges, workload suitability, integration, load and production behavior remain untested. Hostnames and scoped IPv6 addresses are deliberately unsupported; only unscoped IP literals are accepted. Unknown inherited environment keys are ignored; optional-key typos can retain a default. Production/staging must explicitly set APP_ENV to select their stricter requirements. No secrets are required at this step, and no sensitive configuration is printed.

No SQL generation, lint/security tool, database or Redis checks apply because those tools/capabilities are not introduced. No performance measurements were taken. Structured logging, HTTP, response helpers, health endpoints and all excluded capabilities remain unimplemented. No commit, push or deployment occurred. Stop for architectural review; Step 2 requires separate authorization.

## Step 1 architectural approval

The owner explicitly confirmed that Step 1 passed source-level architectural review for its authorized scope at reviewed baseline `bf409ff738a3e61bb535b06e99cab9102005f594`. This approval is recorded as owner-provided review evidence, separate from the earlier reported command results and from measured production behavior, which remains unavailable. FND-002 is verified for the reviewed foundation scope. Step 2's necessary logging extension is covered by the separate review gate below.

## Step 2 — Structured logging final verification

Date: 2026-11-24 (Asia/Dhaka). Baseline: `bf409ff738a3e61bb535b06e99cab9102005f594`. Final verification applies to the uncommitted Step 2 working tree. Interrupted earlier work was retained and completed; no owner edits were overwritten. FND-003 is implemented with local automated evidence; architectural review remains pending.

### Scope and security review

- Standard-library slog constructor with injected writer and validated configuration. No mutable global logger, SetDefault, init side effects, external dependency, network logging, asynchronous queue or rotation.
- Configuration adds only LOG_LEVEL, LOG_FORMAT and INSTANCE_ID. Existing application/HTTP validation and all regression scenarios remain; expected snapshots/read counts and staging/production fixtures were extended for the three settings. Staging/production require explicit instance identity and JSON output. Development/test default to INFO, text and local identity.
- With and WithGroup are intercepted before built-in handler resolution/serialization. Sensitive parent groups propagate redaction to nested/attached values. Raw errors, arbitrary objects and LogValuer are replaced without calling Error, String, MarshalJSON or LogValue; panic-on-evaluation test fixtures verify this.
- Final review found ordinary duplicate attributes could still create duplicate JSON keys. The handler now uses immutable attached key sets and record-local deduplication, with first accepted occurrence winning in each object, including inline/nested groups. A colliding WithGroup suppresses its subsequent attributes. JSON tests inspect tokens before map decoding to detect duplicates.
- Final review also retained sensitive treatment when invalid/reserved group names become invalid_group; descendants cannot escape the policy during rename. Collisions after rename are suppressed.
- Reserved metadata cannot be overridden through attached or record attributes. Key/event syntax is bounded ASCII; invalid events are replaced without echoing input. Text and JSON handlers escape control characters; tests verify newline-bearing values do not add log records.
- UTC timestamps, production JSON, independent logger configuration/default preservation and concurrent derived loggers are covered. Eight concurrent workers write 200 independently parsed records to one handler family without corruption or race findings.
- Startup loads configuration exactly once, injects its logger, emits an INFO startup.configuration_validated event with http_server_implemented=false, and starts no HTTP server. DEBUG/INFO emit it; WARN/ERROR suppress it without failure. Invalid configuration still fails before logger creation at every threshold; startup sink errors produce a fixed safe stderr message and exit 1.

### Actual command results

All commands below exited 0 on the local Go 1.26.3 darwin/arm64 toolchain:

| Command | Actual result |
| --- | --- |
| `go build ./...` | PASS |
| `go test -count=1 ./...` | PASS: cmd/api, internal/platform/config, internal/platform/logging; uncached |
| `go test -race -count=1 ./...` | PASS: all three packages; uncached, no race reports |
| `go vet ./...` | PASS; no findings |
| `go test -cover ./...` | PASS: cmd/api 94.4%, config 100.0%, logging 96.2% statement coverage; config coverage reused an unchanged cached result |
| `git diff --check` | PASS; no tracked whitespace errors |
| `gofmt -l` on all seven changed/new Go files | PASS; no output; files were formatted with gofmt -w |
| `go list -m all` | Only github.com/Faysal9991/mega-backend; no external modules |

Process-level smoke checks built a temporary binary outside the repository and verified production DEBUG/INFO JSON events and UTC time; WARN/ERROR produced no startup event and exited 0. Malformed LOG_LEVEL, empty LOG_FORMAT and missing required production INSTANCE_ID exited 1 with safe stderr and no success output. The explicitly shell-supplied development .env.example succeeded. Temporary artifacts were removed.

Ignore-rule inspection confirmed real .env, .env.production, secret-directory PEM and key paths remain ignored; .env.example remains trackable. No probe secrets were created. Separate whitespace checks cover untracked Go/documentation files because git diff --check omits them.

### Changed files for Step 2

- Updated `cmd/api/main.go` and `cmd/api/main_test.go`.
- Updated `internal/platform/config/config.go` and `config_test.go`; added `logging_test.go`.
- Added `internal/platform/logging/logging.go` and `logging_test.go`.
- Updated `.env.example`, `docs/architecture/RUNTIME_CONFIGURATION.md`, `docs/IMPLEMENTATION_ROADMAP.md`, `docs/business/FEATURE_COVERAGE_MATRIX.md` and this report.
- Added `docs/architecture/STRUCTURED_LOGGING.md` and `docs/decisions/0002-logging-attribute-policy.md`.
- `go.mod`, business rules and unrelated architecture decisions are unchanged.

### Limitations and remaining risks

No unresolved failure was found in the exercised Step 2 checks. Test/coverage results establish local evidence only; they do not establish production reliability or comprehensive confidentiality. Key-based redaction cannot recognize secrets under innocent keys or in syntactically valid dynamic event/group names. Callers must select safe primitive attributes and static names; never log raw request bodies/headers, environment/configuration snapshots or private user content.

Redaction is intentionally conservative and may hide useful diagnostic fields. Raw errors and LogValuer are intentionally unsupported; callers must supply reviewed failure codes. Duplicate attributes/groups are dropped rather than merged. Normalization/deduplication allocate per record, and synchronous output can block. Standard slog convenience methods discard sink errors; logging is not financial settlement or durable audit evidence. Independent logger families sharing an unsafe writer require caller-provided synchronization. The constructor requires a validated configuration snapshot and a usable writer; do not bypass startup validation or mutate attached object/group inputs concurrently.

Instance-ID uniqueness is an operator responsibility; APP_ENV must explicitly select staging/production. There is no distributed collector, request correlation or fleet/load verification. A small synthetic benchmark is provided but was not run during final verification; no throughput, allocation improvement or production performance claim is made.

No SQL generation/database/Redis checks apply. No extra lint/security tool is configured. HTTP server/middleware, response engine and all Step 3+ capabilities remain absent. No commit, push or deployment occurred. Step 2 stops here for source-level architectural review; Step 3 requires explicit authorization.

## Step 2 architectural approval

The owner explicitly confirmed that Step 2 passed source-level architectural review for its authorized scope. That approval is recorded against the inspected Step 2 commit `7dc9d97e9c21584c05a6b10263beab905bba12dc` (feat(logging): implement secure structured logging foundation), the baseline for Step 3. This is owner-provided review approval, distinct from earlier reported test results and unmeasured production behavior. FND-003 is verified for that foundation scope. The logging implementation itself is unchanged in Step 3.

## Step 3 — HTTP server and middleware final verification

Date: 2026-11-24 (Asia/Dhaka). Baseline: `7dc9d97e9c21584c05a6b10263beab905bba12dc`; initial working tree was clean. Evidence below applies to the uncommitted Step 3 working tree. FND-004 is implemented with local automated evidence and awaits architectural review. No later roadmap phase is authorized.

### Delivered scope and decisions

- Constructor-injected net/http lifecycle and centralized Chi router. Listener binding is separate from construction/Serve/Shutdown. Bind addresses use net.JoinHostPort; IPv4/IPv6 address construction is tested. No DefaultServeMux, mutable global state or implicit startup.
- Existing timeout/body settings applied, with only three new validated settings: maximum header bytes (32768, range 1024–1048576), cooperative request timeout (10s, range 1ms–5m), and nonwaiting concurrent admission (256, range 1–4096). Defaults are engineering proposals, not measured production capacity. Application/logging validation and regression scenarios remain intact; successful lookup counts now cover 16 settings.
- Strict single-value request IDs, crypto/rand replacement and private context propagation. Request completion logs only approved correlation/method/template/status/duration/byte fields through unchanged Step 2 logging. Unknown methods/paths use fixed safe values; payloads, raw URLs/queries, credentials, cookies and private content are not logged.
- Safe pre-commit panic 500; committed partial responses are aborted through ErrAbortHandler with original status/bytes retained. No response buffering. Capacity releases during unwinding. Chunked consumers explicitly classify MaxBytesError before responding; infrastructure never reads whole request bodies.
- Peer-IP validation ignores all forwarding headers. No trusted proxy CIDR feature, load balancer or deployment work. ResponseController Unwrap and protocol-appropriate writer capabilities are tested.
- SIGINT/SIGTERM trigger separately bounded drain. Active request contexts are not canceled just because shutdown begins. Timeout force-close cancels connections, returns an error/exit 1, and the single Serve goroutine is joined. Unexpected Serve errors propagate safely. Raw net/http diagnostics are suppressed to avoid unreviewed stack/request leakage.
- Temporary infrastructure error responses are centralized plain status text. No final JSON response/error engine, health endpoint or fake business route exists. Main supplies no module registrations; all actual paths currently return a protected 404.

### Defect found and resolved during implementation

An initial successful-lifecycle test failed because Chi skips middleware when its mux is completely empty. A dedicated infrastructure root mount now guarantees middleware runs before the empty business mux; a regression test checks request ID and completion output on empty-router 404. Method tests also cover escaped-path Allow handling and avoid a 405 with no allowed method on unknown paths. The final runs below passed after these changes; the initial failure was not hidden by weakening tests.

### Dependency and compatibility evidence

Pinned `github.com/go-chi/chi/v5 v5.5.0`, mandated by the approved stack. Verified the official stable release and downloaded exact tag module: its Go minimum is 1.24, satisfied by installed/project Go 1.26.3. go.mod/go.sum record the version and checksums. go list -m all reports only the project and Chi; no transitive module was introduced. Tests compile and run locally on darwin/arm64. See ADR 0003 and HTTP_SERVER.md for the official release link and decision.

### Actual final commands

All commands exited 0:

| Command | Actual result |
| --- | --- |
| gofmt -w on changed/new Go files | Applied; final gofmt -l produced no output |
| `go mod tidy` | PASS; Chi is a direct pinned dependency |
| `go mod verify` | PASS: all modules verified |
| `go build ./...` | PASS |
| `go test -count=1 ./...` | PASS: cmd/api, config, httpserver, logging; uncached |
| `go test -race -count=1 ./...` | PASS: all four packages; uncached, no race reports |
| `go vet ./...` | PASS; no findings |
| `go test -cover ./...` | PASS: cmd/api 80.5%, config 100.0%, httpserver 98.7%, logging 96.2% statement coverage |
| `git diff --check` | PASS; no tracked whitespace errors |

Tests cover ID ambiguity/invalid/oversized values and 128 concurrent generated IDs, route/template privacy, 404/405/Allow, status/bytes, pre/post-commit panic and slot recovery, known and chunked oversized bodies, real TCP oversized-header 431, nonwaiting overload, cooperative deadlines and parent cancellation, spoofed forwarding headers, writer capabilities/ResponseController, bind failure, unexpected Serve error, active-request graceful drain and forced connection cancellation. Channel synchronization and explicit timeout bounds replace arbitrary sleeps. Existing configuration and logging regression tests remain active; only necessary expected fields/counts and the obsolete startup state were updated. Existing startup fixtures use a canceled lifecycle context to validate configuration/logging without binding; separate lifecycle tests exercise real listeners.

Additional binary smoke tests built outside the repository, used isolated loopback ports and explicit non-secret production settings, and verified safe JSON/UTC startup, a correlated 404, exactly one completion event, no raw sentinel path/query/header values in logs, and graceful exit 0 for both SIGTERM and SIGINT. A genuinely occupied local port produced a safe listener-failure event/message and exit 1. Temporary artifacts/processes were cleaned up. These process checks exercise signal handling outside the instrumented package-coverage run; they do not increase its reported percentage.

### Changed files

- Added `internal/platform/httpserver/server.go`, `router.go`, `middleware.go`, `server_test.go`, `middleware_test.go`.
- Updated configuration and existing tests; added `internal/platform/config/http_test.go`.
- Updated `cmd/api/main.go` and its regression tests; added `cmd/api/lifecycle_test.go`.
- Updated go.mod and added go.sum for Chi only.
- Updated `.env.example`, README.md, roadmap, runtime/logging documentation, feature matrix, ADR 0002 approval status and this report.
- Added HTTP_SERVER.md, MIDDLEWARE.md and ADR 0003. Unrelated business/schema/media architecture is preserved.

### Acceptance limits and open risks

All current Step 3 automated checks pass. This is local transport/security/regression evidence, not production-readiness or performance certification. No load benchmark, p95/p99 measurement, production connection or deployment occurred.

Context cancellation cannot stop non-cooperative handler code or arbitrary blocked I/O. Synchronous logging can block. Shutdown/Close cannot drain hijacked streams; WebSocket and upgraded-stream byte accounting remain future work. Standard protocol-aware writer capabilities and ResponseController are covered; exotic custom writer combinations and legacy CloseNotifier assertions are not guaranteed. Normal completed response byte counts are application writes, not client delivery proof.

Admission does not bound every TCP connection or net/http connection goroutine. Parser-level header/malformed-request failures precede request-ID/logging middleware, and Go's header parser includes an extra read-buffer allowance. Low-level raw diagnostics are intentionally suppressed. Exact fleet/connection budgets, safe transport metrics, TLS/HTTP2 setup and trusted-proxy chain handling require later explicit scope. Instance uniqueness is operator-managed; static template registration and safe log field selection remain code-review obligations.

Temporary text errors must be replaced/reviewed in Step 4. Extension HTTP methods require an explicit Allow-policy update; no automatic business HEAD/OPTIONS semantics are invented. No additional security/lint tool is configured, and SQL generation/database/Redis checks do not apply. No commit, push or deploy occurred. Stop for Step 3 architectural review; recommended next phase is Step 4 only after owner authorization.

## Step 4 — API response, error and validation foundation

Date: 2026-11-24 (Asia/Dhaka). Inspected HEAD: `0caa8e2c752e8f1bb0502ffe76c00ac882b98aba`; initial `git status --short` was empty. Results apply to the uncommitted Step 4 working tree. The owner explicitly authorized Step 4 only. No additional Step 3 review approval is inferred. FND-005 is **implemented**, with local automated evidence, awaiting architectural review.

### Delivered scope and decisions

Reused the previously present but unintegrated/untested `apperror`, `response`, `validation` and `requestmeta` helpers. Formatted these existing sources and completed integration rather than introducing competing packages. HTTP mapping remains at the response transport boundary; errors preserve private wrapped causes with errors.Is/As and static public messages. Field diagnostics are copied, lexically ordered/deduplicated and bounded to 20. Shared requestmeta correlation replaces the HTTP-only duplicate key; the existing HTTP accessor remains available.

Common response encoding supports 200/201/202 and bodyless 204, typed nil-as-empty collections, optional metadata, consistent headers/IDs and HEAD suppression. Compact output is staged up to 1 MiB before final headers. Invalid metadata/serialization returns INTERNAL_ERROR, while short/failed writes return safe errors without another response. JSON decoding reuses body limits, parses media with mime.ParseMediaType, preserves json.Number precision and enforces object/unknown-field/document/duplicate policies. Struct matching remains case insensitive; Unicode-fold-equivalent duplicate keys are rejected even inside dynamic objects. Pagination checks consistency with null terminal cursors, without a cursor engine.

Infrastructure 404/405/413/500/503/504 now uses the engine. Allow, Retry-After, admission release, cooperative deadlines/cancellation and completion logging are retained. Chi's existing wrapper/capabilities are unchanged. Informational responses remain nonfinal, Flush/final/implicit writes prevent replacement, explicit ErrAbortHandler passes through, and partial-panic/failed-write recovery aborts without appending bytes. Existing config, logging, startup and lifecycle tests remain active. No runtime route, health endpoint, business validator, database schema, legacy adapter or deployment is added.

### Actual final checks

Installed toolchain: `go1.26.4 darwin/arm64`; go.mod still declares Go 1.26.3. No module version or runtime dependency changed.

| Command | Actual result |
| --- | --- |
| `gofmt -w` on changed/new platform Go sources | PASS; final `gofmt -l internal cmd` returned no paths |
| `go mod tidy` | PASS; no go.mod/go.sum change |
| `go mod verify` | PASS: all modules verified |
| `go build ./...` | PASS, exit 0 |
| `go test -count=1 -cover ./...` | PASS, all eight packages; coverage below |
| `go test -race -count=1 ./...` | PASS, all eight packages, no race reports |
| `go vet ./...` | PASS, no findings |
| `git diff --check` | PASS; supplementary newline/trailing-whitespace checks also cover untracked text files |
| OpenAPI/JSON Schema validation | PASS: OpenAPI 3.1 document, 19 positive fixtures, 8 negative cases; reproduction in contracts/openapi/README.md |

Statement coverage: cmd/api **80.5%**, apperror **100.0%**, config **100.0%**, httpserver **98.6%**, logging **96.2%**, requestmeta **100.0%**, response **98.9%**, validation **97.3%**. Coverage is local statement execution evidence, not proof of every runtime condition. No test failure occurred during Step 4 implementation/checks. An inspection command encountered the absent tests directory; no check was skipped as a result (Go tests live alongside packages).

OpenAPI tooling was not preinstalled. Created an isolated `/tmp/biho-step4-openapi-validation` development venv with openapi-spec-validator **0.7.2** and jsonschema **4.26.0**. The documented validation command checks document validity and the same fixtures compared to actual Go writer output, plus invalid success/error codes, messages, extra properties and pagination cases. This adds no repository Python runtime or Go dependency. No endpoint request/response middleware validator or generated client was run; there are no operation paths. No additional lint/security tool is configured; SQL generation/database/Redis/provider checks do not apply. No production or load checks were run and no performance improvement is claimed.

### Changed files

- Reused/updated: `internal/platform/apperror/error.go`, `response/response.go`, `validation/json.go`, `validation/result.go`, `requestmeta/request_id.go`.
- Integrated: `internal/platform/httpserver/middleware.go`, `router.go`, `server.go`; adjusted the existing middleware test's temporary response expectation and consumer calls.
- Added tests: `apperror/error_test.go`, `response/response_test.go`, `response/contract_test.go`, `validation/json_test.go`, `requestmeta/request_id_test.go`, `httpserver/response_test.go` under internal/platform.
- Added common contract/fixtures/reproduction: `contracts/openapi/common.openapi.json`, `fixtures/envelopes.json`, `README.md`.
- Added documentation: `docs/api/ERROR_CODES.md`, `docs/architecture/API_RESPONSE_ENGINE.md`, `docs/decisions/0004-api-response-error-validation.md`.
- Updated README, API standards, HTTP server/middleware architecture, ADR 0003 follow-up, implementation roadmap, feature coverage, testing strategy and this evidence report.

### Acceptance limits and review status

All requested applicable checks passed. FND-005 remains implemented pending owner architectural review; no business feature is verified by common error fixtures. No business/financial/permission or old-client compatibility rule was changed. The intentional transport change is replacing temporary text failures with the new JSON contract.

Output staging bounds do not cap encoding/json/custom-marshaler peak memory; DTOs must be independently bounded. Use typed List fields for collections; arbitrary nil slices are not rewritten. Static message/field selection remains a privacy obligation. Unknown fields are rejected for ordinary struct DTOs, but custom nested unmarshaling and map/interface fields need later operation-specific review. Go byte limits can be stricter than JSON Schema character limits. Parser-level failures occur before middleware. Non-cooperative handlers, synchronous log sinks, upgraded streams and unmeasured capacity retain the previously documented limitations.

No commit, push, deploy or production connection occurred. Stop after Step 4 for review. Step 5 health endpoints are the recommended next capability only after separate explicit authorization.

## Step 4 review follow-up — completion logging on abort

Review baseline and inspected HEAD: `5598a2b4ec2e8f89af2f2dd35c7c2fbeddfdef35`; initial working tree clean. Scope: only the reported completion logging defect. FND-005 remains implemented pending architectural review; no approval or later-phase authorization is inferred.

Previously the deferred logger converted every zero tracked status into 200, even during pre-commit abort unwinding. It now records whether the inner handler chain returned normally, without recovering or changing the panic. One completion event contains outcome=completed for normal return (including a safely recovered 500), or outcome=aborted for unwinding. Normal no-write completion retains implicit 200. Pre-commit abort uses status=0 to represent no final HTTP status committed; post-commit abort retains committed status and successful byte counts. Logging writes no replacement response and includes no panic/error details. Existing recovery, ErrAbortHandler propagation and admission behavior remain unchanged.

Regression evidence includes real loopback requests: normal no-write completion returns empty HTTP 200; pre-commit ErrAbortHandler returns EOF with no response; a flushed partial 202 returns exactly partial bytes followed by unexpected EOF; a recovered panic returns the sanitized 500 envelope. Tests assert matching outcome/status/bytes, exactly one completion event per request, no sensitive sentinel output or extra events, and successful admission of the next request with a one-slot limit. Existing direct Flush/204/abort tests still assert sentinel propagation and no appended response, now also checking aborted completion fields. The exact completion-field regression expectation includes the new outcome field.

Actual checks, all exit 0:

- `gofmt -w internal/platform/httpserver/middleware.go internal/platform/httpserver/middleware_test.go internal/platform/httpserver/response_test.go`; final formatting inspection reports no paths.
- `go test -count=1 ./internal/platform/httpserver -run 'Test(CompletionOutcomesOverTCP|RouterAndLogging|FlushAndAbortNeverAppend|PanicsAndCapacityRelease|ResponseControllerAndPartialPanicOverTCP)$'`: PASS.
- `go test -count=1 ./...`: PASS, all eight packages.
- `go test -race -count=1 ./...`: PASS, all eight packages, no race reports.
- `go vet ./...`: PASS, no findings.
- `git diff --check`: PASS.

Changed only the completion logger, two existing test files, middleware documentation, this review evidence and the roadmap/feature traceability notes. No dependency, API response contract, business behavior or production performance claim changes. No Step 5 implementation, commit, push or deployment. Stop for review of this finding fix.

## Step 4 scoped review approval

The owner confirms Step 4 review is complete at `c875d876e3782b9cd672bf092c6ce57febe645b5`. Scope is recorded precisely: the previous Step 4 baseline passed full checks; the completion-logging fix passed source review, focused TCP regression tests, HTTP/startup race tests, vet and formatting/diff checks. These owner-provided review results are distinct from the earlier agent-reported local runs. No production-performance approval, business capability or broader Step 3 review approval is inferred. FND-005 is verified for the reviewed Step 4 foundation scope.

## Step 5 — Health & Readiness

Inspected HEAD: `c875d876e3782b9cd672bf092c6ce57febe645b5`. Initial Git status contained only edits in docs/IMPLEMENTATION_ROADMAP.md and docs/business/FEATURE_COVERAGE_MATRIX.md; their preexisting review notes were preserved and extended in place. Step 5 only was authorized. Results below apply to the uncommitted working tree. FND-006 is implemented with local acceptance evidence, awaiting architectural review.

### Delivered scope

- Added `internal/platform/httpserver/health.go` and `health_test.go`: zero-value unready atomic Lifecycle, terminal stop state, and operational GET/HEAD /health/live and /health/ready. Success uses HEALTH_LIVE/HEALTH_READY with empty data; not ready uses common SERVICE_UNAVAILABLE/503 and Retry-After: 1.
- Updated `cmd/api/main.go`, `httpserver/router.go`, `server.go` and middleware admission to inject the same lifecycle, mark ready at Serve entry after initialization and binding, clear before shutdown/close or Serve exit, and bypass ordinary admission only for exact probe paths. Retained other shared protections, request IDs, safe logs/recovery and the completed/aborted logging fix. Every application-level probe response has no-store; HEAD/405/Allow use the existing response/method semantics.
- Updated startup and existing HTTP/lifecycle test call sites for explicit state injection. Added state assertions to graceful/forced-shutdown regressions and startup probes to the executable lifecycle test.
- Added operational.openapi.json and fixtures/health.json; updated contract reproduction instructions. Common OpenAPI schemas/fixtures and go.mod/go.sum are unchanged. No runtime dependency or external check is added.
- Updated README, API standards, HTTP/middleware architecture, roadmap, feature coverage and testing strategy. Added HEALTH_READINESS.md and ADR 0005. Recorded scoped Step 4 approval in ADR 0004 and this report.

### Actual verification

Toolchain: `go1.26.4 darwin/arm64`; module declaration remains Go 1.26.3. All final requested commands exited 0:

| Check | Result |
| --- | --- |
| `gofmt -w` on changed/new Go sources; `gofmt -l cmd internal` | PASS; final inspection returned no paths |
| `go build ./...` | PASS |
| `go test -count=1 ./...` | PASS, all eight packages |
| `go test -race -count=1 ./...` | PASS, all eight packages; no race reports |
| `go vet ./...` | PASS, no findings |
| `git diff --check` | PASS; supplementary newline/whitespace checks include untracked files |

Focused runs also passed: `go test -count=1 ./internal/platform/httpserver -run 'TestHealth|TestLifecycleConcurrentAndTerminal'` and `go test -count=1 ./internal/platform/httpserver ./cmd/api`. Common and operational OpenAPI 3.1 document validation passed using the existing isolated development venv (openapi-spec-validator 0.7.2, jsonschema 4.26.0). Six health fixtures matched operational response schemas and six invalid diagnostic-bearing payloads were rejected. Fixtures are independently compared against real GET/HEAD router output; documented validation also verifies operation paths, schema references and bodyless HEAD declarations. No endpoint code generator or deployment tooling was run.

Tests exercise startup/ready/draining fixtures; empty success data and sanitized failures; matching IDs, JSON/nosniff/no-store/length/retry headers; HEAD and unsupported methods/Allow; exact routing and inherited body limits; minimal safe completion logs; saturated one-slot API admission while probes still respond; concurrent lifecycle mutation/read and terminal stop; constructor/nil-listener failure, failed Accept, canceled startup, Shutdown/Close before Serve; executable startup GET/HEAD; real loopback already-accepted probes returning live=200 and ready=503 during graceful drain. A channel-signaled listener verifies readiness was cleared before Close begins; no arbitrary sleep is used. Existing graceful/forced shutdown, partial response, abort/EOF and no-write completion regressions remain enabled.

### Guarantees, limitations and handoff

Initialization here is configuration, logging/router/server construction and successful binding. Server marks ready immediately before net/http.Serve; new probes can execute only after transport acceptance. Readiness is a point-in-time lifecycle snapshot, not a future PostgreSQL/Redis or capacity check. Stop is terminal and competing startup cannot restore readiness. Run marks unready before shutdown logging and transport shutdown; direct Shutdown/Close clear it first as well. There is no drain sleep or background loop.

An already-accepted request can observe draining state; a new request after listener closure may receive a connection error rather than 503. A probe racing the state transition can return its earlier snapshot. Liveness is independent of lifecycle/admission but still shares CPU, connection limits, transport, logger and response protections. Parser-level failures precede application envelopes/cache headers. These are local acceptance tests, not production availability/performance approval; no load benchmark or fresh coverage percentage was measured in this step. No database/Redis/sqlc/provider checks apply; additional lint/security tooling is not configured.

No business/financial/compatibility behavior changed. No database, Redis, authentication, business module, WebSocket, LiveKit, deployment, commit or push was implemented. Stop after Step 5 for review. Step 6 PostgreSQL foundation remains a proposed next step requiring separate authorization.


## Step 6 — secure PostgreSQL connectivity blocker

Inspected HEAD: e99d4fed2176ae3899eedd90e81e76823d98be86. Initial Git working tree was clean. Read applicable instructions, the current roadmap and database-design constraints. The owner explicitly authorized Step 6 against an existing Coolify testing server, including new biho_live_dev and biho_live_test databases and dedicated roles, contingent on verified TLS. This does not establish Step 5 review approval.

Actual preflight: connected to the owner-specified public TCP endpoint with an eight-second socket timeout and sent only PostgreSQL's eight-byte SSLRequest (length 8, request code 80877103). The server replied with the single byte N: PostgreSQL TLS negotiation explicitly refused. No PostgreSQL StartupMessage, username, database name, password or other authentication data was sent. A TLS handshake and certificate/identity verification could not begin. This is evidence of reachable TCP and refused PostgreSQL TLS, not an authenticated PostgreSQL session or proof of server version/configuration.

`git check-ignore .env` confirmed .env is ignored. Its contents/password were not read because the transport-security gate failed first. No credentials were printed or committed. No fallback to plaintext, sslmode=require without verification, trust bypass or infrastructure modification was attempted.

Outcome: FND-007 blocked. No remote SQL, database/role creation, privilege changes, migrations or destructive tests occurred; postgres and development data were untouched. No Go dependency, application source, migration or sqlc configuration was introduced. Build/unit/race/vet/database checks were not run for this documentation-only blocker report; Step 6 integration acceptance remains unexecuted. Documentation whitespace/fence checks and git diff --check passed.

Required next condition: the owner-designated server must support PostgreSQL TLS and present a certificate verifiable for the intended IP or DNS identity using a trusted CA. Supply the intended DNS name/CA certificate if system trust and the provided IP are insufficient. Then repeat verification before reading the bootstrap secret and performing authorized isolated database/role setup and Step 6 implementation. No TLS weakening, commit, push or deployment occurred.


## Step 6 resumption — TLS established, credential file pending

The owner explicitly permits sslmode=require without CA/identity verification for this temporary development/testing server. This supersedes the prior identity-verification gate for this testing connection only; sslmode=disable remains forbidden and the planned production configuration must enforce verify-full.

Actual credential-free preflight now succeeds: PostgreSQL SSLRequest returns S and an encrypted TLS 1.3 handshake completes, with hostname/certificate verification intentionally disabled under the owner's exception. Server identity is not verified. No authentication data was sent, so this does not prove successful database authentication or server version.

At resumed inspection, no .env or other scoped environment-file candidate existed; only .env.example was present. Requested the local credential-file path. Following the owner's acknowledgment, rechecked and still found no credential file. Created an owner-only (0600), Git-ignored .env template containing the supplied bootstrap connection settings, PGSSLMODE=require and an empty PGPASSWORD; no existing file was overwritten. Requested that the owner fill the password locally rather than send it through chat. No password was read, printed or transmitted; authentication remains pending.

No SQL, role/database creation, destructive tests, schema or runtime implementation has occurred. The three existing blocker-document edits were retained and extended. FND-007 remains blocked on the missing credential, not on TLS negotiation. Documentation-only checks apply; no Go/database test result or production TLS enforcement is claimed for unfinished code.

## Step 6 — completed PostgreSQL foundation (resumption)

Baseline remains e99d4fed2176ae3899eedd90e81e76823d98be86. Initial modified files were the three prior blocker documents (roadmap, coverage matrix, this report); their history was preserved and superseded with current evidence. The owner supplied the password locally and explicitly authorized require without identity verification for testing, direct pgxpool now, future PgBouncer transaction-pool compatibility without deploying it, and direct Goose migrations. This does not establish Step 5 architectural review approval.

### Authentication and remote changes

Authenticated the existing testing endpoint first with postgres over encrypted TLS 1.3; PostgreSQL version 18.7 was returned. Server identity/CA trust was intentionally not verified under the testing exception. No sslmode=disable or plaintext fallback was used. Target database/role names were absent; administrator permissions were checked read-only. Observed server max_connections=100 and 9 pg_stat_activity rows are a snapshot, not a capacity guarantee.

Created biho_live_dev and biho_live_test from template0, each owned by its separate migration role. Four new nonsuperuser login roles: biho_live_dev_app, biho_live_dev_migrator, biho_live_test_app, biho_live_test_migrator. Runtime aggregate role connection limits are 12; migration limits 2. Revoked PUBLIC access on the two databases/public schemas, granted own-database runtime CONNECT, prepared private migration schemas and schema-USAGE default privileges. No unrelated database ACL, administrator password or existing data was modified. Runtime cannot create database/schema/table/temp objects or read migration history; test credentials cannot enter dev. Existing unrelated databases' PUBLIC privileges were not changed or claimed as cluster-wide isolation.

Generated separate random passwords, persisted them before provisioning for partial-failure recovery, then passed them in process environment to parameterized/quoted psql variables. Passwords were never printed, placed in command arguments or committed. Ignored .env retains mode 0600 and now holds separate runtime/direct-migration/test credentials; no existing secret file was overwritten. Application/test processes used only their relevant credentials; bootstrap secrets were not supplied to API or integration subprocesses.

### Scope implemented

- New separate typed database configuration loader, redacted secret/config formatting, explicit environment-target separation and require-only-in-dev/test versus verify-full-in-staging/production enforcement, including constructor revalidation. No automatic dotenv loading or global environment mutation.
- Direct pgxpool with 8 default/max-configurable 32 connections, min 0, bounded connect/startup context, idle/lifetime/jitter limits and generated authenticated database/role/app-schema checks. Runtime role must match the database app role. No session SET/search_path hooks; extended unnamed execution with statement/description caches disabled. Safe initialization errors, cleanup on failure and cancellation; initialization precedes HTTP bind/readiness; pool Close follows HTTP shutdown. No ongoing dependency probe loop.
- Explicit direct MIGRATION_DB_* settings, separate migration role, embedded Goose schema migration and private qualified version table. One direct database/sql bridge connection (required by Goose), bounded session advisory locking, no global registry/setters or raw SQL/error logging. API never auto-migrates. DownTest refuses default/dev/prod targets and verifies authenticated test identity before writes; rollback is non-CASCADE.
- Pinned pgx v5.13.0 and Goose v3.30.0 with recorded module checksums; sqlc v1.31.1 is a pinned external build tool. Generated SQL bindings, generation/diff Makefile targets and fresh-install testing bootstrap SQL are reviewable. Exact module Go minimums 1.25/1.26/1.26 compile with project Go 1.26.3 and installed go1.26.4 darwin/arm64.
- No business table, API route/contract, legacy adapter, database import, Redis or other business capability. Existing HTTP/logging/config/lifecycle/abort tests are preserved; lifecycle tests now inject their database starter.

### Actual verification

Final applicable commands exited 0:

| Command | Actual result |
| --- | --- |
| gofmt on all changed/new Go files; final gofmt -l | PASS; no unformatted paths |
| `go mod tidy` | PASS |
| `go mod verify` | PASS: all modules verified |
| `go build ./...` | PASS |
| `go test -count=1 ./...` | PASS |
| `go test -count=1 -cover ./...` | PASS; default isolated suites |
| `go test -race -count=1 ./...` | PASS; no race reports |
| `go vet ./...` | PASS |
| `make check-generated` | PASS: pinned sqlc v1.31.1 diff, no generated drift |
| `go test -race -tags=integration -count=1 -cover ./...` with explicitly supplied TEST credentials and acknowledgment | PASS; real testing PostgreSQL, no race reports |
| `git diff --check` plus supplementary new-file whitespace/fence checks | PASS |
| Ignored/untracked-secret check and secret-value scan of changed/new repository text | PASS; .env ignored/untracked, no local secret values found in reviewable changes |

Default suite statement coverage: API command 79.7%, config 99.3%, HTTP 97.8%, logging 96.2%, app errors/request metadata 100%, responses 98.9%, validation 97.3%, postgres 59.4%. With real integration/race enabled, postgres coverage is 81.2%. Standalone migration command, embedded source wrapper and generated bindings have no own unit tests and report 0% under per-package coverage; their production code is exercised by migration CLI/integration/startup runs outside those packages' own counters. No full-coverage claim is made. No load/performance benchmark was run.

Real integration exercised migration fresh/up/down/up, duplicate Up, two concurrent direct migrations, transactional upgrade failure with no surviving table/version effect, runtime DDL/history denials, pool-size/acquisition backpressure and timeout, server-observed running-query cancellation and usable pool afterward, explicit transaction rollback, check constraints, parameterized int64 precision, and read-only rejected dev login with test credentials. All destructive tests and synthetic fixture DDL were confined to biho_live_test; cleanup left both app schemas empty of tables and migration version 1. Development received only initial upgrade and read-only startup/inspection; postgres received no destructive test.

Real development smoke built an API binary outside the repository, supplied only runtime development credentials, initialized before binding a loopback listener, verified both health probes/status/request IDs, and exited 0 after SIGTERM. Migration CLI successfully upgraded development using its dedicated direct role. Final read-only inspection confirmed both targets at version 1 with zero app tables and TLS 1.3.

Two fixture issues were caught after tightening environment isolation: a configuration lookup-count test used dev values with test environment, and the deliberate cross-database ACL test was rejected by client target validation before reaching PostgreSQL. Corrected fixture environments without weakening validation or permissions; the latter now intentionally uses a development connection configuration with test credentials and still receives database-side SQLSTATE 42501. Focused and final complete integration/race runs passed after fixes. Earlier successful runs remain distinct from final evidence.

### Files and limits

Added config/database.go and its tests; platform/postgres pool/migration/unit/integration code plus generated dbsql; database bootstrap/migration/query artifacts; sqlc.yaml, Makefile and cmd/migrate. Updated API startup/lifecycle tests, .env.example, module pins, README, runtime/database/HTTP/health/testing docs, roadmap and coverage matrix. Added POSTGRESQL_FOUNDATION.md and ADR 0006. The local .env is excluded from source changes and remains secret.

FND-007 is implemented with local/real-test acceptance evidence, pending architectural review. No business/financial/compatibility rule changed. No PgBouncer was installed, configured or deployed. Future proxy requirements include cache/protocol/cancellation testing, schema-qualified/session-independent runtime operations, explicit transactions, independent frontend/backend/fleet connection budgets, TLS/auth on both legs and a permanently direct migration route for session locks. Present tests use direct PostgreSQL only and do not prove proxy behavior. Production verify-full enforcement is tested at configuration/constructor level, not deployed against a production certificate. Ongoing DB-outage readiness and populated production schema rollback/backup/restore remain outside this foundation. Pool Close requires consumers to release leases; no claim is made that arbitrary uncooperative code can be terminated.

No commit, push or application/proxy deployment occurred. Stop after Step 6 for review. Later Redis/business/infrastructure steps require separate authorization.

## Step 6 two-gap follow-up — transactions and ongoing readiness

Date: 2026-11-25 (Asia/Dhaka). Inspected HEAD cf296a9ff0f6d27042a4f961178a6ea4e5cfc9a8; working tree was clean. Scope is only the two owner-requested missing Step 6 requirements. FND-007 remains implemented pending architectural review; no prior approval or later-phase authorization is invented.

### Implemented behavior

Added `postgres.Transactions` with an injected BeginTx consumer interface satisfied by the existing pool, explicit TxOptions and callback context propagation. Commit runs once only on callback success with an uncanceled caller. Every unfinished transaction rolls back in a defer, including error, failed commit, cancellation and panic. Cleanup preserves context values but detaches caller cancellation/deadline and imposes its own required 1ms–5s bound. ErrTxClosed is benign; other rollback errors join the original cause. Fixed safe wrapper messages/Go formatting preserve errors.Is/As without printing raw causes. Original panic propagates after cleanup. No transaction/commit is automatically retried, and rollback after a commit error does not establish whether a commit was settled.

Added one on-demand `Availability` checker over the already-initialized runtime pool, injected at API composition into Lifecycle. Production wiring uses a 2s check deadline, 1s success/failure cache and at most 32 coalesced waiting request handlers. Excess waiters fail closed; individual waiters honor cancellation/deadline. One synchronous check is active at a time, with an independent timeout so a disconnected first caller does not poison peers. No custom background loop/goroutine, new pool or unbounded waiting queue. Panic releases the flight and caches failure before existing recovery handles it. The HTTP running gate is checked before and after dependency checking. Database unavailability returns common 503 without changing terminal lifecycle state, so recovery is possible while running; shutdown cannot be reversed by cached/in-flight success. Liveness and unsupported-method routing do not check PostgreSQL. Existing JSON/HEAD/no-store/retry/header behavior is retained; operational OpenAPI descriptions reflect the new readiness condition without changing schemas.

API lifecycle regression tests now inject a close+availability dependency pair; they verify actual startup route wiring through loss/live independence/recovery and resource closure. Existing configuration, TLS, pgx pool, direct Goose migration and generation code remain unchanged. No module/version, role, credential, permanent migration or infrastructure change was introduced.

### Actual verification

All final applicable checks exited 0:

| Command/check | Result |
| --- | --- |
| gofmt on changed/new Go source; `gofmt -l cmd internal database` | PASS, no remaining paths |
| `go build ./...` | PASS |
| `go test -count=1 ./...` | PASS, full default suite |
| `go test -race -count=1 ./...` | PASS, no race reports |
| `go vet ./...` | PASS |
| `go test -race -tags=integration -count=1 -v ./internal/platform/postgres` | PASS, explicitly executed against existing dedicated test app/migrator credentials; new real transaction/readiness test 15.59s and existing foundation integration 22.77s in this run |
| `go test -count=1 ./cmd/api ./internal/platform/postgres ./internal/platform/httpserver` and focused race run | PASS |
| Operational OpenAPI validation with existing isolated validator | PASS; common schema references intact |
| `git diff --check` and supplementary new-file text/secret checks | PASS |

The integration suite was available and executed, not skipped. The launcher read the existing ignored local file without printing credentials, supplied only TEST_DB_*/TEST_MIGRATION_DB_* and explicit biho_live_test acknowledgment to the subprocess, and retained approved testing TLS settings. It did not provision/rotate roles or connect with administrator credentials. Existing TLS policy tests also pass: temporary dev/test require with unverified identity, staging/production verify-full, no plaintext fallback. No new coverage percentage, production-performance or proxy-behavior claim is made.

Unit cases cover context/options, commit once/no retry, begin/callback/commit/rollback error classification including SQLSTATE/ambiguous transport error, canceled callback cleanup, panic identity and bounded cleanup completion, cleanup error joining/timeouts, safe error formatting, success/failure cache and recovery, canceled owner/waiter behavior, 32 genuine coalesced waiters plus fail-closed overflow, and pinger panic recovery without a leaked flight. HTTP cases cover 200 -> 503 -> 200 while running, HEAD failures, independent liveness, no checking before startup/after shutdown and a successful check finishing after Shutdown still returning 503.

Real PostgreSQL follow-up created only app.step6_followup_fixture inside the authenticated biho_live_test target, granted its dedicated test app role the needed fixture privileges, and removed the fixture afterward. It verified one committed row, rolled-back callback/panic/cancellation rows, SQLSTATE 23505 classification, ErrTxCommitRollback classification without callback replay, no leaked pool leases and subsequent successful ping. Real HTTP probes over loopback used that same test pool: reserving its two connections induced bounded availability failure and 16 concurrent readiness 503s from one coalesced check; liveness stayed 200; releasing leases and expiring the controlled cache restored readiness 200. A gated successful real-pool check completed during real HTTP Shutdown and still returned 503; later handler checks remained unready without querying PostgreSQL. This simulates temporary pool unavailability, not a server restart/network outage. No infrastructure changes were made. Existing integration guards/cases also ran and cleaned up all named fixtures; no destructive test targeted development or postgres.

During the initial starter-signature edit, a broad replacement accidentally changed two unrelated test listener/Accept nil returns to the new database dependency struct, producing a compilation failure. Restored those two original listener returns and reran focused/full/race checks successfully. No test was deleted or weakened to pass. The final source and behavior are reflected in the passing results above.

### Files, limitations and stop gate

Added transaction.go/transaction_test.go, availability.go/availability_test.go and followup_integration_test.go under platform/postgres. Updated HTTP lifecycle/health tests and API composition/lifecycle tests. Updated PostgreSQL/health/HTTP docs, README, ADR 0006, operational OpenAPI descriptions, roadmap, coverage matrix, testing strategy and this evidence report. No unrelated initial edits existed.

Readiness caches snapshots: outage/recovery detection can lag by the remaining 1s TTL plus a next check of up to 2s, subject to scheduling/transport behavior. Overflow intentionally returns 503 even if PostgreSQL might be healthy. A detached shared check can outlive its disconnected owner but remains driver-timeout bounded; arbitrary injected code that ignores context cannot be forcibly terminated. A response already past its final running gate may overlap shutdown; no new-connection 503 is promised after listener closure. Cleanup on panic preserves the original panic even if rollback reports failure, and an ambiguous commit remains an operation-specific reconciliation concern. These limitations are documented, not represented as production availability or financial settlement guarantees.

No Redis, business module, infrastructure/PgBouncer setup, commit, push or deployment. Stop after the two Step 6 fixes for review.

## Step 7 — original implementation and historical real-test gate

The focused acceptance audit appended below supersedes this section’s missing-credential blocker. Original execution evidence is retained as history.

Date: 2026-11-25 (Asia/Dhaka). Initial HEAD 63992e40ae1ccf28dddc7dea1948ac1c65635a2d, working tree clean. The owner authorized Step 7 only and identified this as reviewed Step 6 behavior to preserve. Existing scoped approvals and pending review statuses were retained; no broader production/performance approval is inferred. FND-008 is implemented for review, with shared-endpoint acceptance explicitly blocked below.

### Delivered scope and decisions

- Added config.RedisConfig/LoadRedis, explicit disabled/no-URL/no-network path, redacted URL/config values, strict booleans and positive bounded timeouts. Enabled URLs require rediss, explicit port, password, database 0 and no query/fragment override. Empty ACL username normalizes to default. TLS verifies chain and host/IP by default, with explicit bypass allowed only in dev/test and rejected even while disabled in staging/production. Constructors revalidate the snapshot.
- Pinned stable go-redis v9.23.0 after inspecting official release/pinned source and exact Go 1.26.0 module minimum (project 1.26.3; installed go1.26.4 darwin/arm64). Added only this client and required transitives/checksums. SDK semantics were verified: input MaxRetries=-1 disables command retries (zero would default to three); DialerRetries=1 is one attempt per sequence; zero/negative would default to five.
- One private reusable standalone client per process, PoolSize/MaxActiveConns default 8, min idle 0, max concurrent dials 1, disabled dedicated pipeline pool/maintenance/identity features and RESP2. A shared TLS dial gate caps all physical connections, including auxiliary pools/probes; no parallel IPv4/IPv6 fast fallback. No Pub/Sub/pipeline/CSC/transaction/routing API is exposed. SDK built-in dial circuit probes may later recover under the same time/socket bounds; no custom reconnect framework.
- Get/Set/Delete only, with versioned environment prefixes, ASCII suffix limit 128 bytes, Set values at most 64 KiB, whole-ms TTLs from 1ms through 24h, distinct misses/empty hits/disabled/unavailable/other safe errors, context classification and no raw server/transport causes in messages. Receive values are checked after go-redis allocation; this is not a hard wire/heap allocation cap. Server memory/eviction/ACL and fleet budgets are documented, not changed.
- Each call has a bounded context and at most one of MaxConns operation workers, with immediate fail-closed overflow. This makes caller cancellation prompt even though pinned socket I/O honors deadlines rather than cancellation alone; residual work retains a slot until bounded completion. Close rejects new work, closes the driver once and joins workers. No unbounded waiting queue or application background loop. Timed-out writes may have executed, so this remains disposable/idempotent cache only.
- Startup PING has its own bound, emits a static safe warning on connection/auth/TLS failure and retains the optional client. Invalid enabled config fails before PostgreSQL/network startup. API serving and PostgreSQL readiness/liveness continue after optional connection failure. Cancellation during cache startup closes dependencies and exits without binding. Client Close follows HTTP drain and precedes PostgreSQL closure. Step 6 pool/transaction/readiness/migration/TLS source is unchanged.
- The SDK exposes only a process-scoped logger; installed a constant immutable discard logger via its atomic API before constructing clients, preventing raw SDK diagnostics. Application warnings use the existing injected safe logger/static codes. This SDK-global policy/tradeoff is explicit in ADR 0007; no global application client/sink was introduced.

### Actual checks

Final local applicable checks passed (exit 0):

| Command/check | Actual result |
| --- | --- |
| gofmt on changed/new sources; final `gofmt -l cmd internal database` | PASS; no paths |
| `go mod tidy` | PASS |
| `go mod verify` | PASS: all modules verified |
| `go build ./...` | PASS |
| `go test -count=1 ./...` | PASS; full default suite |
| `go test -race -count=1 ./...` | PASS; no race reports |
| `go vet ./...` | PASS |
| Focused cache/config/API race tests, including final in-flight Close and startup cancellation tests | PASS |
| `go test -count=1 -cover ./...` | PASS snapshot: rediscache 91.7%, config 98.6%, API 71.6%; remaining baseline coverage reported in tool output, no coverage proof of real endpoint |
| `git diff --check` and supplemental new-file text/secret scans | PASS |

Tests use controlled local TLS RESP servers for cache hit/miss/empty values, TTL expiry via controlled clock, environment/version namespace, size/key/TTL policy, safe command-error classification, default self-signed-certificate rejection versus explicit test bypass, auth failure/recovery and dropped-connection recovery. A retryable LOADING reply was received exactly once, not replayed. Hard-dial gate tests reject extra connections and release the slot once; normalized client options verify retry/pool/auxiliary semantics. Concurrent blocked commands demonstrate bounded worker/connection use and immediate cancellation with context classification; client Close interrupts in-flight socket work and is idempotent. Earlier deadlines are not extended. API tests show optional Redis outage warning while live/ready remain 200, exactly one Close after HTTP stop, preserved PostgreSQL cleanup, no bind on invalid configuration and graceful cleanup on startup cancellation. No disruptive test touched the shared server.

An initial test detected that startup checked only the outer execution error, overlooking a returned PING command error; this prevented the expected safe startup diagnostic. Corrected Open to inspect both error levels; the TLS/auth diagnostic and recovery cases then passed. No tests were weakened or removed.

### Real endpoint verification — BLOCKED, not passed

The saved Git-ignored .env contained no REDIS_URL or other REDIS_* setting at inspection and repeated checks. Requested that the owner save the supplied URL locally or identify its ignored file path, without sending credentials through chat. No Redis secret was printed or added to source, and no empty/guessed password was used.

Credential-free preflight: TCP 169.58.127.58:7200 was reachable, but standard TLS certificate/IP identity verification failed. No Redis authentication/commands were sent during preflight and no verification bypass was attempted. Actual chain/IP diagnostics were suppressed. The testing exception requires an explicit REDIS_TLS_INSECURE_SKIP_VERIFY=true setting from the owner in development/test, or suitable trust/identity; it is not enabled automatically.

Explicit real suite execution: `go test -race -tags=redis_integration -run TestRedisIntegrationDisposableCache -count=1 -v ./internal/platform/rediscache`, with only local REDIS_* values supplied and BIHO_REDIS_INTEGRATION_ACK=169.58.127.58:7200/0 plus REDIS_ENABLED=true. Result: nonzero/FAIL with `BLOCKED: enabled Redis integration configuration unavailable or invalid; credentials suppressed`, before any client or key was created. This is missing-credential acceptance evidence, not a successful connectivity/auth/Get/Set/Delete/expiry test. An earlier invocation without acknowledgment also correctly refused execution; it is not counted as endpoint verification.

The real-test implementation enforces the exact endpoint/default ACL/database, creates crypto-random per-run suffixes within the test/version namespace, tracks all planned keys before writes, uses 30s-or-shorter TTLs, and cleans up only tracked keys with an independent 10s context while the client remains open. It tests Get/Set/Delete, empty/missing values, short TTL expiry, concurrent bounds and cancellation. These endpoint cases remain UNEXECUTED until credentials/TLS policy are supplied. Missing/failed cleanup will be reported, not hidden. No real Redis key was written during this blocked run; no global enumeration or server control command was issued.

Required next condition: save REDIS_URL in the existing ignored file (or identify its actual ignored path), and provide verified trust/IP identity or explicitly set the allowed dev/test bypass flag. Then rerun the existing real integration suite under the same acknowledgment. Do not paste the URL/password into chat or downgrade to plaintext. Step 7 acceptance remains blocked pending that real run.

### Files and limits

Added internal/platform/config/redis.go and tests, internal/platform/rediscache implementation/unit/opt-in real tests, cmd/api/cache_test.go, REDIS_CACHE.md and ADR 0007. Updated API optional-service wiring, .env.example placeholders, module pins/checksums, README, runtime/HTTP/health/testing docs, roadmap, feature matrix and this report. Corrected HEALTH_READINESS.md's stale “No external call” sentence to distinguish independent liveness from existing bounded PostgreSQL readiness. No health/API wire contract or Step 6 runtime source was changed.

No performance/load/production availability claim. The process-scoped SDK logger suppresses other SDK clients' diagnostics; all clients must use this ownership boundary. Hard client limits do not replace a server maxmemory/eviction policy or fleet budget. Receive allocation is only bounded after decoding. Canceled Set/Delete may have executed; credentials/config are static and changed local credentials require appropriate restart/reconfiguration, not a new hot-reload mechanism.

No authentication/session/rate-limit/wallet/job/PubSub/LiveKit business implementation, infrastructure/ACL/server mutation, FLUSHDB/FLUSHALL/KEYS/CONFIG SET, shared-server stop, commit, push or deployment. Stop at Step 7 review/real-test gate.

## Step 7 focused acceptance audit — 2026-11-25 (Asia/Dhaka)

Baseline: 8f32219, initially clean. Scope: original Step 7 requirements recorded in REDIS_CACHE.md, ADR 0007, roadmap and source contracts; no business/Step 8 implementation. Inspected actual config, adapter, API composition/lifecycle and installed go-redis v9.23.0 options.go, redis.go and internal/pool/pool.go. This audit supersedes the historical missing-credential blocker above. Credentials were supplied to child-process environments from ignored .env, parsing values without shell evaluation or printing them. Only REDIS_* and (for API tests) TEST_DB_* were supplied. No credentials entered argv, logs or tracked files.

### Requirement verdicts after the fix

| Acceptance requirement | Result | Evidence / limit |
| --- | --- | --- |
| Optional/default-disabled configuration; disabled means no credential lookup/client/network | PASS | Loader call tracking, nil-client/disabled operations, actual disabled API startup with invalid unread URL |
| Strict typed configuration and constructor revalidation | PASS | Config invalid-input/boundary tests and constructor production-bypass rejection; invalid configuration precedes DB/client/listener |
| One reusable private standalone client; explicit injection | PASS | Actual API run -> runWithDatabase -> runWithServices -> rediscache.Open; private client, no command escape API |
| Hard physical connection cap, including auxiliary activity | PASS | PoolSize + MaxActiveConns, pipeline disabled, no idle prefill, shared timed TLS dial semaphore, FallbackDelay=-1; direct third-dial rejection/release tests at cap 2; simultaneous blocked commands remain capped; real concurrent pool 4 <= cap 8 |
| Disabled command retries | PASS | Input MaxRetries=-1 normalizes to 0; pinned command loop inspected; LOADING delivered once; RESP2/no CSC/HIMPORT features avoid special replay paths |
| Disabled immediate dial retries | PASS | DialerRetries=1; pinned loop uses attempt < DialerRetries. Later caller reconnects and bounded built-in circuit probe remain possible, as documented |
| Bounded dial/pool/read/write/startup/operation timeouts | PASS | Every duration validated 1ms–10s; custom timed TLS dial and context-aware driver; blocked-operation timeout/earlier-deadline tests; optional startup bound |
| Prompt cancellation with bounded residual work | PASS | Established socket cancellation returns safely while bounded worker retains admission; canceled real GET rejected; caller deadline never extended |
| TLS-only, minimum TLS 1.2, chain/hostname verification default | PASS | Actual custom TLS dialer and ServerName inspected; redis:// and URL overrides rejected; untrusted local certificate rejected without insecure fallback |
| Explicit dev/test bypass only; staging/production enforce verification | PASS | Config and constructor tests, including disabled production; real endpoint used existing explicit test bypass (encrypted, identity unverified) |
| Credential/config/error/SDK log redaction | PASS | Config/Secret formatting + JSON tests; raw local server sentinel suppressed; SDK immutable discard logger; real API logs checked for supplied URL/DB password, no raw transport errors emitted |
| Optional startup outage and later recovery | PASS | Controlled local TLS auth/network failure then recovery through the same client; real API local refused-port startup warning, serving continues; no shared-server disruption |
| Health/readiness independent of Redis; preserve PostgreSQL gate | PASS | Actual PostgreSQL startup/readiness + live/ready HTTP 200 in real enabled, disabled and locally unavailable modes; existing PostgreSQL tests unchanged |
| Graceful drain then cache Close once then PostgreSQL Close | PASS | Source defer order and local close-order assertion; actual composition-root context cancellation succeeds in all 3 modes; blocked adapter Close interrupts I/O and joins workers |
| Versioned environment namespaces and bounded safe keys | PASS | Explicit prefix assertion, allowed ASCII/length validation; random integration/benchmark suffixes |
| SET with bounded values and mandatory bounded whole-ms TTL | PASS | Source and limits tests, real SET; owned copy protects canceled caller reuse; no persistent TTL accepted |
| GET hit, empty-value hit and MISS distinction | PASS | Controlled and real tests; no full models/business data cached |
| Single-key DELETE and absence semantics | PASS | Local idempotent delete and real delete followed by MISS |
| Actual TTL expiry | PASS | Real 100ms expiry tested after 200ms; deterministic local clock test |
| Isolated real keys and cleanup | PASS | Crypto-random tracked namespace, TTL <=30s; independent cleanup context deletes exact keys and verifies MISS; benchmark cleanup also verified; no enumeration/global deletion |
| Concurrent operations, fail-fast admission and race safety | PASS | Real 16-way GET: 4 hits/12 safe bounded rejections, no invalid results; blocked cap-2 test, mixed concurrent GET/SET/DELETE/Close, full race suite and tagged real race suite |
| Sequential operations release admission before completion is visible | PASS | Confirmed FAIL before fix: 100,000 sequential execute calls at cap 1 spuriously returned CACHE_UNAVAILABLE. Fix releases slot before result publication; normal/panic paths now pass |
| Bounded cache benchmark with latency/allocations separated from network | PASS | Measurements below; local dispatch versus warmed remote driver/adapter, no speedup inferred |
| Build/uncached tests/race/vet/module/generation/format/diff checks | PASS | Actual commands below, all exit 0 |
| No authoritative cache/business/Step 8 expansion | PASS | Source/diff review; Get/Set/Delete only, no auth/session/financial/jobs/PubSub/media implementation or contract changes |
| Trusted identity handshake against the shared endpoint | NOT TESTED | Existing explicit test bypass was required; remote chain/hostname trust is not established. Default verified rejection tested locally |
| Shared Redis maxmemory/eviction/least-privilege ACL and fleet limits | NOT TESTED | Documented operational prerequisites; no CONFIG/ACL inspection or infrastructure mutation authorized/performed |
| Production/multi-instance capacity, reliability or performance | NOT TESTED | Small single-process synthetic tests only; no production SLO claim |

No remaining confirmed Step 7 source defect from this audit. FND-008 remains implemented pending owner architectural acceptance; technical evidence does not imply owner approval.

### Executed checks and measurements

All final commands passed: `go build ./...`; `go test -count=1 ./...`; `go test -race -count=1 ./...`; `go vet ./...`; `go vet -tags=redis_integration ./cmd/api ./internal/platform/rediscache`; `go mod verify` (all modules verified); `make check-generated` (sqlc v1.31.1 diff); `gofmt -l cmd internal database` (empty); `git diff --check`. No additional lint/security tool is configured. No dependency or generated SQL changed; no tidy necessary. PostgreSQL destructive/migration integration suites were not rerun: this change does not touch them; actual API integration exercised authenticated test database/schema startup and readiness without migrations.

With ignored local settings and `BIHO_REDIS_INTEGRATION_ACK=169.58.127.58:7200/0`: `go test -race -tags=redis_integration -run TestRedisIntegration -count=1 -v ./internal/platform/rediscache ./cmd/api` passed. Cache test: 7.09s; actual API test: 8.47s (enabled 3.66s, disabled 2.68s, unavailable 2.12s). Both suites include runtime/race instrumentation; these durations are not request-latency measurements. The API integration calls the actual run composition root, including real PostgreSQL startup and TCP HTTP; shutdown is driven by context cancellation rather than an OS signal. Signal delivery itself was not separately exercised.

Synthetic benchmark environment: Go 1.26.3, darwin/arm64, Apple M4, default benchmark parallelism suffix 10, one serial caller, warmed connection, one 21-byte synthetic value, no race instrumentation. Remote sample size 20 per mode; nearest-rank quantiles, insufficient for reliable tail/SLO estimation. Remote TLS uses the explicit local testing bypass. No business dataset or production workload is represented.

| Measured path | Samples | Mean/op | p50 / p95 / p99 | B/op | allocs/op |
| --- | ---: | ---: | --- | ---: | ---: |
| Local context + admitted worker/channel dispatch, no driver/network | 100,000 | 991.2 ns | Not collected | 576 | 8 |
| Remote public adapter GET | 20 | 222.048 ms | 211.0 / 253.0 / 344.4 ms | 1,412 | 21 |
| Same private driver GET, remote TLS/server/network included | 20 | 235.411 ms | 211.4 / 305.6 / 312.0 ms | 620 | 10 |

Commands: `go test -run '^$' -bench BenchmarkAdapterDispatch -benchtime=100000x -benchmem -count=1 ./internal/platform/rediscache`; with the explicit Redis supplier, `go test -tags=redis_integration -run '^$' -bench BenchmarkRedisRemoteGet -benchtime=20x -benchmem -count=1 ./internal/platform/rediscache`. Dispatch excludes key/value conversion, driver encoding/decoding and socket work. Remote driver/adapter harnesses include their caller timeout context. Similar remote times show dominant remote I/O; independent samples cannot isolate network transit from Redis/TLS or justify subtraction/speedup claims. Client allocations alone do not measure server RSS/CPU/bytes.

An initial 100-sample benchmark failed when its single shared 30-second fixture expired during the second mode. Cleanup completed without errors. Corrected the benchmark fixture lifecycle (refresh the owned key outside timing per mode), capped samples at 50, ran 20 per mode, and retained the short TTL safety constraint. Final measurements above use nearest-rank percentiles. No production adapter behavior was changed to accommodate the benchmark.

Remaining limitations: receive-value size is checked after driver decoding/allocation; cancellation does not prove SET/DELETE did not execute; SDK logging policy is process-wide; per-process caps do not impose fleet/server resource limits. These preexisting constraints remain documented in REDIS_CACHE.md/ADR 0007. No shared-server stop, FLUSH/KEYS/CONFIG/ACL command, schema mutation, credential change, infrastructure change, commit, push or deployment occurred. Stop at Step 7 owner review.

## Step 7 deadline classification follow-up — 2026-11-25 (Asia/Dhaka)

Baseline 44a2f5e; initially clean. Scope is only the confirmed deadline-error classification defect. The previous audit's passing deadline checks did not establish race-free classification: independent review reported two failures in twenty repeated runs when a socket deadline elapsed before the effective context published Err/Done.

Added a shared contextStatus helper at execute admission and completion boundaries. It preserves a nonnil ctx.Err() first, otherwise treats an already-reached absolute Deadline as context.DeadlineExceeded. The existing safe CACHE_UNAVAILABLE wrapper retains that cause for errors.Is. No network-timeout-wide conversion was added: a socket timeout before the context deadline remains plain CACHE_UNAVAILABLE. Admission slot release-before-result ordering, socket/worker/pool limits, TLS, optional-cache behavior and dependencies are unchanged.

Deterministic regression uses a synthetic context with unpublished Err/Done and a test-controlled absolute deadline, moving from future to past inside the command callback to model the completion race without timer scheduling. Admission starts with an expired unpublished deadline and must not execute the command. Both subtests failed against the original execute implementation before the fix. The final timeout fixture is a net.OpError wrapping os.ErrDeadlineExceeded. The earlier-socket-timeout control uses the same fixture with a future context deadline and verifies errors.Is(context.DeadlineExceeded) remains false and the error text stays CACHE_UNAVAILABLE. A separate check preserves published context.Canceled even when the absolute deadline has passed. Existing real TLS test files and all existing deadline assertions remain intact.

Actual final checks (all exit 0):

| Command | Result |
| --- | --- |
| `go test -race -run '^(TestDisabledAndEarlierDeadline\|TestDelayedContextDeadlinePublication\|TestEarlierSocketTimeoutClassification\|TestContextStatusPreservesPublishedError)$' -count=100 ./internal/platform/rediscache` | PASS, 3.785s; each selected test repeated 100 times, no races |
| `go test -count=1 ./...` | PASS, full uncached default suite |
| `go test -race -count=1 ./...` | PASS, full uncached default race suite |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `gofmt -l cmd internal database` | PASS, empty output |
| `git diff --check` | PASS |

Changed only cache.go, cache_test.go and scoped verification/roadmap/coverage documentation. Controlled local TLS tests ran in the full suites. Opt-in shared Redis/API integration tests and benchmarks were not rerun for this classification-only change; prior endpoint/measurement evidence remains historical, not a new claim. No new performance measurements or dependency/sqlc checks were needed; dependencies and SQL are unchanged. FND-008 remains implemented pending owner architectural acceptance. No business/API contract, infrastructure, Step 8, commit, push or deployment changes. Stop at the scoped Step 7 review gate.
