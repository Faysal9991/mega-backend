# Redis optional disposable cache — Step 7

Status: implemented for review; real-endpoint acceptance audit results and limitations are recorded separately in FOUNDATION_VERIFICATION.md. Step 6 PostgreSQL transaction/readiness behavior at 63992e4 is preserved; no Redis dependency is added to liveness or PostgreSQL readiness.

## Configuration and explicit environment supply

`config.LoadRedis` is a separate typed loader, following the database-loader convention. REDIS_ENABLED defaults false. Disabled configuration never reads REDIS_URL/resource settings, constructs a go-redis client or performs network I/O. TLS bypass is still parsed/validated, even while disabled. Invalid enabled configuration fails before PostgreSQL/client/listener initialization; errors contain only setting names and safe reasons. Config and Secret URL formatting/JSON redact the credential URL. API code does not automatically read .env; supply only relevant process variables through a trusted explicit environment supplier. Never shell-expand/print a credential URL or place it in command arguments. Local .env remains ignored.

| Setting | Default/policy |
| --- | --- |
| REDIS_ENABLED | false; only true/false accepted |
| REDIS_URL | Required when enabled; rediss only, nonempty password, ACL username (default if omitted), explicit port, database 0 only |
| REDIS_TLS_INSECURE_SKIP_VERIFY | false; explicit true allowed only in development/test, rejected in staging/production |
| REDIS_MAX_CONNS | 8; range 1–32; actual hard socket ceiling includes auxiliary pools |
| REDIS_DIAL_TIMEOUT | 2s |
| REDIS_POOL_TIMEOUT | 500ms |
| REDIS_READ_TIMEOUT / REDIS_WRITE_TIMEOUT | 1s each |
| REDIS_STARTUP_TIMEOUT | 3s |
| REDIS_OPERATION_TIMEOUT | 2s |

All durations must be 1ms–10s, never unlimited/negative. Earlier caller deadlines win. URL length is at most 8192 UTF-8 bytes; password at most 4096 bytes, username 128 (an omitted username normalizes to default). Query options, fragments, encoded database paths and nonzero databases are rejected; URL parameters cannot override TLS, pool/retry/timeout policy. Constructors revalidate exported configuration. Only one standalone client is supported, not Sentinel/Cluster/custom routing.

TLS is always encrypted, minimum TLS 1.2, with explicit host/IP identity verification and system CA trust by default. Production/staging cannot disable verification. A temporary testing bypass still uses TLS but **does not verify server identity or certificate trust**; it must be explicitly present in local settings, never inferred from a failed handshake. Configure a valid certificate/host identity and trusted system CA for production/private CA requirements; no insecure fallback is attempted.

## Client lifetime and optional failure

One injected reusable cache/client per API process. A bounded startup PING is performed; authentication/network/TLS failure emits only cache.startup_unavailable plus static CACHE_UNAVAILABLE and retains the client for later operations to recover. It does not make PostgreSQL readiness fail or prevent HTTP serving. Invalid configuration remains fatal. Shutdown waits for the existing HTTP drain/forced-close flow, then closes cache once before closing PostgreSQL. Close rejects new work, closes the driver and joins bounded operation workers. There is no custom reconnect loop or background cache framework.

The public adapter exposes Get, Set and single-key Delete only; the underlying go-redis client is private. Nothing caches business data in this step. Do not use it for authentication, sessions, authorization, rate limiting, wallet balances, idempotency/locks, jobs, messaging, Pub/Sub or any durable/authoritative coordination. A cache miss/unavailable result must never authorize or settle an operation.

## Verified go-redis option semantics and resource limits

Pin stable github.com/redis/go-redis/v9 v9.23.0 (Go 1.26 minimum). Exact pinned source was inspected rather than assuming option names imply hard limits:

- MaxRetries input **-1** normalizes to zero retries; input zero would enable three retries. Normal Get/Set/Delete/Ping commands are not automatically replayed. Protocol 2 and no CSC/HIMPORT use avoid the pinned RESP3 desync/preparation forced-replay paths.
- DialerRetries=1 gives one attempt per dial sequence; zero/negative would use five. A custom TLS Dialer also imposes a timeout and a shared fail-fast semaphore across every physical connection, so auxiliary activity cannot bypass the hard socket cap. Parallel IPv4/IPv6 fast fallback is disabled on the net.Dialer; DNS-address attempts are serial within the same bounded dial budget, avoiding transient socket fan-out.
- PoolSize and MaxActiveConns both equal REDIS_MAX_CONNS, MinIdleConns=0, MaxIdleConns bounded, MaxConcurrentDials=1. PoolSize alone is not a hard cap. PipelinePoolSize=-1 disables the dedicated pipeline pool, which otherwise adds connections beyond the main MaxActiveConns. Pub/Sub APIs are not exposed/used; their unused lazy pool has no connections. RESP2, disabled maintenance notifications, disabled identity reporting and no CSC/autopipelining keep auxiliary features off. The common physical dial gate still covers all pools/probes.
- The pinned driver has bounded internal stale-connection selection and a built-in dial circuit-breaker probe after enough failed dial sequences. That library probe uses the same timed/capped Dialer and stops when the client closes; no application reconnect worker is added. No claim is made that all future attempts cease after one failure: later caller operations and that driver probe can recover availability.
- Socket read/write buffers are 4096 bytes; these are **not maximum reply sizes**. Idle lifetime is 5m, connection lifetime 30m. No minimum idle prefill.

ContextTimeoutEnabled=true honors context deadlines on driver I/O. Pinned established socket reads do not promptly wake for cancellation alone. The adapter therefore admits at most REDIS_MAX_CONNS operation workers, fails excess requests immediately without a waiting queue, and uses a single buffered result channel per admitted operation. Caller cancellation/deadline returns promptly with a safe error; a worker may remain until its bounded driver I/O finishes. It retains its admission slot until completion, so canceled callers cannot accumulate unlimited workers. Workers recover internal driver panics into safe command failures. No worker can issue a second command retry. A canceled/timed-out Set/Delete may still have reached Redis; TTLs and disposable/idempotent semantics are required. Do not treat failure as proof that Redis did not execute the command.

Driver diagnostics: go-redis v9.23 only offers a process-scoped logger, and its default pool messages can include raw server/transport errors. Before client construction the adapter installs the same immutable discard logger through the SDK's atomic SetLogger API. This is a deliberate third-party process logging policy, not a mutable global application client/sink. All application diagnostics use the existing injected safe slog logger and static codes. It suppresses diagnostics from any other go-redis client in the process; create Redis clients only through this owner. Never replace it with a raw-error formatting logger. This dependency limitation/tradeoff is recorded in ADR 0007.

## Adapter contract, keys, TTLs and allocation

Get returns (value, hit, error): nil/false/nil means miss; an empty existing value is still hit=true. Disabled operations return CACHE_DISABLED; closed/unreachable/auth/ACL/timeout/cap failures return CACHE_UNAVAILABLE. Context cancellation/deadline remain discoverable with errors.Is, without raw network/server causes in formatting. Invalid key/TTL, oversized values and nonavailability Redis command errors have separate stable local codes. Delete is single-key and idempotent when absent. None of these errors is a new HTTP/business error contract.

Keys use `biho_live:<environment>:cache:v1:` plus a caller suffix of 1–128 ASCII bytes restricted to letters/digits/colon/underscore/hyphen. No caller-controlled environment/version or arbitrary wire command. Keep keys nonsecret and bounded; tenancy/business cache policy is not invented. Set accepts at most 65536 value bytes and a TTL of 1ms–24h in whole milliseconds. No zero/persistent TTL is permitted. Set takes its own bounded copy before asynchronous cancellation can let callers reuse their input buffer. Get rejects values larger than 65536 **after decoding** and does not return them.

Receive-allocation limitation: the Redis bulk reply/string is allocated by go-redis before the adapter can check its length; converting accepted data to []byte adds a copy. The adapter/read-buffer limit does not cap malicious or oversized wire allocations (Redis may permit much larger strings). Restrict write access/ACL namespaces, use trusted cache data, enforce upstream value bounds and server/client resource budgets; this is not a streaming/hardened untrusted-message framework.

## Server and fleet requirements

Budget maximum sockets as process replicas × configured hard cap, plus other clients/admin consumers; zero minimum idle is not a promise of zero established sockets. Include connection-churn/TLS/worker memory costs and bounded in-flight responses. Client workers and sockets are capped per instance, not automatically across deployments. A bounded synthetic dispatch/remote GET benchmark is recorded in FOUNDATION_VERIFICATION.md; it establishes no production capacity or improvement claim.

Use a disposable-cache service with finite maxmemory and an appropriate eviction policy (for example allkeys-lru/lfu), memory headroom and monitoring; size from measured datasets/workload. TTL is not a replacement for a memory limit, and maxmemory can exclude some buffers/overheads. Server OOM/eviction/outage must be safe cache loss. Do not share eviction policy with durable coordination in a future phase. A future least-privilege ACL should allow only required handshake/PING/Get/Set/Delete and the intended environment/version namespace; the supplied default ACL was not changed or assumed least privilege. This step neither inspects nor changes shared memory/eviction/ACL/server configuration. These memory/eviction caveats follow the [official Redis eviction documentation](https://redis.io/docs/latest/develop/reference/eviction/).

## Test operations and safety

Unit/race tests use controlled local TLS RESP doubles for untrusted-certificate rejection, explicit testing bypass, auth/network outage/recovery, earlier deadlines and prompt cancellation, socket cap, no command replay, startup diagnostic redaction, cache semantics, controlled-clock expiry, value limits and shutdown. API tests prove an unavailable optional Redis still permits live/ready 200 and closes once after HTTP stops. PostgreSQL readiness still depends on the existing PostgreSQL checker only.

Real tests require `redis_integration` build tag, REDIS_ENABLED=true, supplied REDIS_URL/TLS policy and `BIHO_REDIS_INTEGRATION_ACK=169.58.127.58:7200/0`. They enforce exact authorized endpoint/default ACL/database 0, generate crypto-random per-run suffixes, track every planned key before writes, use TTLs at most 30s, and delete only those tracked keys with an independent bounded cleanup context while the client is still open. They never enumerate keys or use global/server-control commands. Cleanup failure is reported; short TTLs remain a fallback, not a false cleanup success. Actual availability/results are in FOUNDATION_VERIFICATION.md; missing credentials/TLS/auth/ACL are blockers, never passes.

```sh
# Supply only REDIS_* from a trusted explicit environment supplier; no URL argv.
BIHO_REDIS_INTEGRATION_ACK=169.58.127.58:7200/0 \
  go test -race -tags=redis_integration -run TestRedisIntegrationDisposableCache -count=1 -v ./internal/platform/rediscache
```

No FLUSHDB, FLUSHALL, KEYS, CONFIG SET, ACL mutation, shared-server restart or deployment is performed. Disruptive tests run only against local controlled doubles.


## Focused acceptance follow-up

The 2026-11-25 audit supersedes the historical missing-credential blocker. Real operations/expiry and tracked-key deletion followed by MISS verification pass, including opt-in API tests with actual PostgreSQL startup and Redis enabled/disabled/unavailable. Unavailability/recovery tests disturb only controlled local endpoints. The adapter now releases an operation slot before publishing completion; previously a sequential single-slot caller could spuriously receive CACHE_UNAVAILABLE. A 100,000-operation regression reproduced the defect before the fix. Panic completion releases admission in the same path.

Opt-in API lifecycle test: `go test -race -tags=redis_integration -run TestRedisIntegration -count=1 -v ./internal/platform/rediscache ./cmd/api`. Supply only REDIS_* and TEST_DB_* settings from the existing ignored .env via an explicit trusted supplier, plus the same endpoint acknowledgment. The API test maps TEST_DB_* to DB_* in APP_ENV=test; it performs startup/readiness reads only, with no migration or schema changes. The disabled case supplies an invalid URL to confirm it is not read; the unavailable case uses a closed local port.

Benchmarks: `go test -run '^$' -bench BenchmarkAdapterDispatch -benchtime=100000x -benchmem -count=1 ./internal/platform/rediscache`; under the same explicit Redis environment supplier, `go test -tags=redis_integration -run '^$' -bench BenchmarkRedisRemoteGet -benchtime=20x -benchmem -count=1 ./internal/platform/rediscache`. The remote benchmark enforces at most 50 samples per mode, owns one random 30-second-TTL key, refreshes it outside timing before each mode and verifies deletion/MISS in cleanup. Remote adapter/driver measurements include TLS/network/server time; dispatch measures only local context/worker/channel overhead. Do not subtract separate remote samples to estimate local adapter cost.
