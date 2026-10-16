# Engineering history — 2026-10-13 to 2026-10-15

[Español](../es/history/SESSION.md) · Documentation review: 2026-10-16.

This is a consolidated historical record, not the current specification or an active task list. Earlier constraints and next-step suggestions may have been superseded. This new repository does not include the earlier source history or complete original entries. Current behavior is described in the [overview](../CONTEXT_MASTER.md), [architecture](../ARCHITECTURE.md) and [maintainer guide](../MAINTAINING.md).

## 2026-10-13: delivery foundation

The initial vertical slice used Go, stdlib HTTP, a pure-Go SQLite driver, WAL/FULL and one serialized connection. It committed event/delivery/idempotency together, signed payload bytes, claimed work with a token/lease and recorded outcomes atomically. IDs were random 128-bit hexadecimal values, not UUIDv7. Initial operation was one fixed loopback endpoint and one client. Destination URLs were not supplied through the business API.

Recorded initial validation: Go tests, race tests, vet, build and real HTTP smoke passed. Reopen/persisted claim tests were not physical power-loss evidence. A temporary read-only workspace obstacle was resolved by relocating the repository to its present directory; that obstacle is historical.

## 2026-10-13: real process failures (base 2063ea5)

The receiver gained slow/503/429/drop modes and synchronized receipt logs. The process suite reproduced transient recovery, retry exhaustion, lost responses and SIGKILL during a send. It retained the real 30-second lease expiry rather than shortening production settings.

| Scenario | Final state | Finalized attempts | Receipts | Simulated business effects |
| --- | --- | ---: | ---: | ---: |
| First 503, then 204 | SUCCESS | 2 | 2 | 1 |
| First 429, then 204 | SUCCESS | 2 | 2 | 1 |
| Timeout after processing | SUCCESS | 2 | 2 | 2 |
| Lost response after processing | SUCCESS | 2 | 2 | 2 |
| Five 503 responses | DEAD | 5 | 5 | 0 |
| SIGKILL during send, restart | SUCCESS | 1 | 2 | 2 |

Stable IDs survived. A send interrupted before finalization existed in receiver observations but not attempt history. The recorded race/vet/build/smoke/failure checks passed; no exactly-once claim followed.

## 2026-10-13: replay and schema v2 (base bf39d12)

DEAD replay required matching expected_cycle, preserved IDs/history and committed new cycle/audit together. Actor was the authenticated client, not a person sharing a token. History used bounded exclusive cursors. Transactional v1→v2 migration preserved existing attempts and rejected future schema. Tests covered repeated cycles, racing requests, stale cycles, rollback and migration. Full local CI-style checks and real replay passed; this did not establish physical storage durability.

## 2026-10-13: backup/restore and CI

The offline utility used Python SQLite Backup API, integrity/foreign-key checks, staged atomic publication without overwrite, mode 0600 and file/directory synchronization. Tests included committed WAL, corrupt inputs, destination protection and restored replay/idempotency. It retained leases and required separate configuration/secrets. Later revisions strengthened schema validation.

CI introduced a shared `make ci` entry point with process suites in sequence and pinned actions. Initial workflow creation was local evidence only until a remote run was actually observed. Backup/restore and CI recorded passing local checks. The original entries distinguish their uncommitted work from remote execution.

## 2026-10-13: Retry-After (base a54ba8f)

429/503 handling accepted seconds or HTTP dates with a separate cap and used the larger of jittered backoff and bounded header delay. Invalid/repeated values fell back. Eligibility persisted through reopening; no worker sleep held the delivery. Huge digit strings saturated safely. Policy, HTTP/reopen and process timing tests passed along with full local CI.

## 2026-10-13: clients, endpoints, capacity and metrics

Schema v3 added clients, endpoints, client-scoped idempotency and ownership constraints. Configuration persisted identities and paused omitted work. Outgoing HTTPS/DNS/dial policy blocked internal targets and redirects/proxies; the process suite exercised isolation. Schema v4 added terminal timestamps/indexes for retention. Quotas rejected new work atomically, protected unfinished graphs and limited replay/retained payloads. Backup tests evolved with schema.

Metrics separated retained database gauges from process counters and HTTP observations from committed delivery results. Protected scrapes used a distinct token. Snapshots, paused work, expired leases, cleanup and concurrency had regression tests. Counters reset on process restart. Full local checks were reported passing for these increments; none was sustained-production evidence.

## 2026-10-13: measured baseline and consecutive work

The baseline completed 800 events in four cases, draining near 10/s because of one send per 100 ms tick. Removing the wait between available deliveries completed another 800 events at approximately 96–181/s in short local runs. CPU and publication latency changed; no causal or maximum-capacity conclusion was established. See [baseline](../benchmarks/BASELINE.md) and [worker loop](../benchmarks/WORKER_LOOP.md).

## 2026-10-13: hexagonal separation and validation

Responsibilities moved into core/application/adapters while preserving transactional operations and the relay facade. New tests checked dependency direction, ports, cancellation, retry and a real SQLite/HTTP-adapter vertical slice without sockets.

Initial full validation was blocked by inability to open local sockets; compilation and selected non-socket tests passed but did not substitute for full CI. After a new session allowed loopback sockets, complete local CI passed without skipping those tests. Two benchmark runs completed 800 events, with observed lower short-run drainage and higher concurrent-publication P99 than preceding runs. The cause was not established. See [hexagonal report](../benchmarks/HEXAGONAL.md).

## 2026-10-14: Flow Lab and UX

The lab composed a real isolated dispatcher and controlled receiver with six scenarios, real replay and evidence export. Tokens stayed in Go; session observations stayed in memory. The frontend used React/TypeScript, feature folders, remote query state and a pure playback reducer. CI gained web build/model tests and process smoke scenarios.

A later UX pass introduced three sequential steps, six selectable explanatory stages, manual reading, mobile layout, larger text and distinct drawing controls. Historical browser/model/process checks passed. This was not user-comprehension research or accessibility certification. See [UX review](PLAYGROUND_UX.md).

## 2026-10-15: concurrency and dynamic endpoints

The backend gained configurable 1–50 attempts/workers, explicit remote-listen permission, reader pooling and persisted dynamic secrets (schema v5). The API created destinations; later review removed unsafe runtime/SQLite double writes and secret fallback. Historical CI fixes addressed formatting and a TypeScript export/import error.

### Corrections to the original session narrative

The old entry incorrectly said v5 created `dynamic_endpoints`; the current migration adds `secret` to `endpoints`. The creation field is `id`, not `endpoint_id`. Generated secrets contain 256 random bits, not 128. A minimum length does not establish entropy. Claims of microsecond lookup or high addition rates lacked a specific supporting benchmark. The old Registry/cache/fallback descriptions are superseded, not current architectural contracts. Race detection is not exhaustive concurrency proof.

## 2026-10-15: backend correction (base f1d41bc)

Policy validation became free of runtime mutation; dynamic creation/quotas were transactional and resolution used committed storage. Startup validation became explicit. Loopback permission was controlled by the operator. Deletion raced safely with publication/claim; cleanup recovered quota after references expired. Backoff saturated before overflow, DSN pragmas applied per connection and shutdown joined workers/maintenance. Operational errors became failing process exits. Full local CI and targeted regressions were reported passing. No new dynamic-lookup performance equivalence was claimed.

The subsequent [backend review](AUDIT-2026-10-15.md), based on `38b6819`, records Compose, API, pagination, static deletion, backup schema, smoke isolation, tooling and documentation corrections.

## 2026-10-15: storage failures and corruption

Against `55d53f4`, nine deterministic FULL/read-only/contention cases and two real ENOSPC cases verified atomic rollback, sanitized errors, committed metrics and recovery. Against `c660ce1`, startup rejected a corrupt index before migration; tests added four exact WAL EIO cases, two interrupted-WAL cases and backup corruption recovery. Complete local CI, both Docker fault targets, lint, coverage and vulnerability analysis were reported passing on that date. These were software fault experiments, not electrical/device failure certification. Detailed injection points and limits are in [storage failures](../STORAGE_FAILURES.md).

## 2026-10-16: documentation consolidation

Current guides were rewritten in English with a corresponding Spanish edition, dated evidence separated from operating instructions, API wording reconciled and the original measurement files retained. This editorial change does not implement the previously discussed development plan, publish a release or add a new benchmark. Old proposals and session-specific permission/commit-status notes were removed from the current context; the complete earlier text is not included in this new repository.
