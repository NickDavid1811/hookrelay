# Storage failure validation and recovery

[Español](es/STORAGE_FAILURES.md) · Documentation review: 2026-10-16. Code reference: `35c4373`.

Initial validation used baseline `55d53f4`. The follow-up against `c660ce1` adds WAL I/O/interruption tests and fixes startup corruption detection: storage now runs a structural check before migrations. Schema v5 and HTTP contracts remain unchanged.

## Reproduce

```sh
# Included in make test / make ci; real SQLite, no Docker required.
go test -race -count=1 -timeout=60s ./internal/relay -run '^TestStorage' -v

# Actual filesystem ENOSPC; requires a local Docker Engine and Go.
make storage-full-test

# Isolated WAL write/sync EIO and interrupted transaction recovery.
make storage-io-test
```

GitHub Actions runs deterministic/corruption regressions through `make ci`, followed by separate mandatory `storage-full-test` and `storage-io-test` steps. An unavailable Docker daemon, compilation failure, unexpected error or test failure makes `storage-full-test` fail. Ordinary `go test` skips the Linux filesystem tests unless explicitly enabled by the isolated runner; a skip is not ENOSPC evidence.

The runner compiles a Linux test executable and mounts only that file read-only in a disposable, unprivileged container. Its root filesystem is read-only, network access is disabled, and a dedicated 4 MiB tmpfs contains the temporary databases and an allocated pressure file. The test checks filesystem type and size before filling it. It must observe the OS error `ENOSPC`; a permission failure does not qualify. It does not fill the host disk, mount production data or require privileged containers.

## Exercised boundaries

| Failure | Injection point | Required result |
| --- | --- | --- |
| Publication `SQLITE_FULL` | Before inserting the idempotency key, after the event and delivery inserts | HTTP 503 with a sanitized error; original graph unchanged; no accepted-publication metric; same key succeeds after recovery and returns the same receipt when repeated. |
| Endpoint creation `SQLITE_FULL` | After inserting the endpoint | HTTP 503; no retained endpoint or partial ownership change; retry succeeds. |
| Claim `SQLITE_FULL` | After updating the delivery to PROCESSING | Error propagated, delivery remains PENDING and nothing is sent; claim/delivery succeeds after releasing pressure. |
| Replay `SQLITE_FULL` | Before inserting its audit record, after the cycle update | HTTP 503; cycle, status and audit unchanged; retry creates exactly one replay record. |
| Retention `SQLITE_FULL` | Before deleting the event, after deleting dependent history | Whole graph preserved; removed count is zero; subsequent cleanup removes the complete graph. |
| Configuration `SQLITE_FULL` | Endpoint insertion after old configuration is deactivated in the transaction | Previous owners, client authentication and runtime destinations remain intact. |
| Read-only writer | SQLite `query_only` rejects writes with `SQLITE_READONLY` | HTTP 503 and no new publication; failed claim sends nothing; writes recover once the restriction is removed. This models a writer restriction, not a physical read-only mount. |
| Competing writer | Another SQLite connection holds a write transaction | `SQLITE_BUSY`, HTTP 503 and no partial publication; retry succeeds after rollback of the competing writer. A busy timeout does not guarantee every lock conflict waits that long. |
| Finalization `SQLITE_FULL` | Attempt-history insertion after the delivery state update | Worker run returns an error and stops; PROCESSING lease persists, with no committed attempt or success metric. Reopening and expiring the lease allows resend with the same delivery ID. |
| Filesystem ENOSPC during publication | Truncated WAL followed by filling the isolated tmpfs | SQLite reports a storage failure, HTTP returns 503 and the graph is unchanged. Freeing space allows retry; previously accepted work survives reopening and is delivered. |
| Filesystem ENOSPC during finalization | Fill tmpfs after a committed claim, immediately before an outbound transport returns 204 | Finalization fails, worker stops, lease persists without an attempt or success result. After space is freed and the database reopened, lease recovery resends the same delivery and commits SUCCESS. |

The deterministic FULL tests use SQLite's real `max_page_count` ceiling and test-only triggers that allocate a large BLOB. They verify rollback after preceding SQL statements, not just rejection before a transaction starts. They do not represent physical disk exhaustion. The separate tmpfs tests cover that OS boundary, including WAL append/commit failure.

Outbound responses in these new tests use an in-process HTTP transport. Real receiver sockets, signatures and process death are covered separately by the existing smoke/failure suites. The recovery fixtures explicitly expire leases to avoid a 30-second sleep; production must wait for expiry. The SIGKILL suite independently covers real-time expiry. Recovery checks database integrity, foreign keys, stable IDs and committed history. Two sends after a failed finalization can legitimately correspond to one persisted attempt.

## Corruption, EIO and interrupted WAL follow-up

### Startup corruption detection

The previous implementation opened a database with a corrupt `eligible` index because its foreign-key scan did not inspect that structure. `storage.Open` now runs `PRAGMA quick_check(1)` before journal-mode writes and migrations. The regression corrupts an index root page offline in schema v4 and v5 fixtures, then requires rejection without changing the main file. The v4 case also proves the failure happens before migration to v5. Check diagnostics are not copied into a successful readiness response.

This is a structural startup check, not a repair or continuous scan. It adds a database-size-dependent startup cost. SQLite documents that [quick_check omits UNIQUE and index/table-content consistency checks](https://www.sqlite.org/pragma.html#pragma_quick_check); the offline backup utility still runs full integrity and foreign-key checks. A payload bit flip that leaves valid SQLite structure is not guaranteed to be detected. No cryptographic payload checksum or schema migration was added.

The backup regression corrupts a real index page, requires backup and restore-from-corrupt-source to fail without publishing output or modifying the source, then restores and validates an untouched healthy snapshot. Rejected inputs leave no temporary backup files.

### Kernel-returned WAL I/O errors

`make storage-io-test` runs four child-process cases: WAL write and synchronization errors during publication and after a receiver acknowledgement. A restrictive Linux seccomp filter returns `EIO` for `pwrite64` or `fsync`/`fdatasync` on the test WAL descriptors. The exact expected driver codes are checked: `SQLITE_IOERR_WRITE` (778) and `SQLITE_IOERR_FSYNC` (1034). A different failure or missing injection cannot pass.

The filter covers all threads and all read/write descriptors for the fixture WAL; SQLite may open such a descriptor even through a read-only SQL pool. Each filter exists only in its disposable child process. The container retains its normal security restrictions, no privileges are added, and no host database is mounted. Missing kernel support fails the target; it is not silently skipped.

Publication returns a sanitized 503 and increments no accepted metric on the injected failure. A finalization failure propagates from the worker and increments no committed-success metric. The parent reopens the database without the filter and verifies integrity, ownership, accepted work and idempotent retry.

A failed synchronization is an **uncertain commit**. Recovery must allow either the complete transaction or its absence, never partial history. If finalization is already SUCCESS with its attempt record, no replay is needed; if a persisted claim remains PROCESSING with no attempt, lease expiry permits resend with the same delivery ID. This distinction is why clients retry identical request bytes with the same key after an uncertain publication rather than choosing a new key.

### Interrupted writes and incomplete tail

Two cases kill a child at an explicit barrier after uncommitted pages have spilled into WAL. Before the interrupted transaction, the child accepts another publication with FULL synchronization. The tests verify that the WAL contains a committed prefix followed by uncommitted frames. One case leaves the tail intact; the other cuts 13 bytes from its final uncommitted frame, on an offline fixture only.

After reopening, both accepted publications and their original receipts remain usable, the uncommitted event/delivery/key graph is absent, integrity and references pass, and accepted work can be delivered. This complements [SQLite's WAL commit boundary](https://www.sqlite.org/wal.html#how_wal_works) and the existing real-time lease recovery suite. The large uncommitted SQL fixture deliberately bypasses the public payload ceiling solely to force cache spill.

These cases simulate a process interruption and selected incomplete writes. They do not power off a physical device, discard the host kernel's cache, emulate a dishonest drive cache or certify electrical-loss durability.

## Operational response

1. Treat HTTP 503, especially after an I/O/sync error, as a failed/uncertain publication, retain its original body bytes and idempotency key, and retry with bounded backoff. A lost HTTP response can also follow a successful commit; the same key resolves that ambiguity while retained.
2. Inspect dispatcher logs, process status and filesystem usage. A dispatch or maintenance storage failure propagates from `Run`; the executable stops with a failure status. Check a supervisor for repeated restart failures. `/readyz` only pings SQLite and cannot prove write availability or free disk.
3. Pause publishers and stop the affected dispatcher before filesystem maintenance. Reclaim unrelated disposable files, expand the filesystem or correct its mount/permissions. **Do not delete or truncate the SQLite database, WAL or SHM to make room.** Retention needs working write transactions and cannot be relied on to rescue an already full filesystem.
4. Once space/write access is restored, follow the [offline backup procedure](OPERATIONS.md#offline-backup-and-restoration) to a new path with sufficient capacity. It checks integrity, foreign keys and required schema objects. On a validation failure, preserve the original files and investigate rather than rewriting delivery rows.
5. Restart with the original database, client identities, configuration, credentials and flags. Verify liveness, protected metrics and delivery results. Unfinished claims become eligible after the persisted lease expires (normally 30 seconds from claim). Never manually zero leases in production to imitate the test fixture.
6. Receivers must deduplicate stable IDs: a webhook may have produced effects before its result failed to persist. Deliveries already DEAD require an explicit, audited replay if retry is desired; restarting does not reset their budgets.

## Evidence limits and continuation

The initial nine deterministic cases and two ENOSPC cases passed with separate writer/read pools. The follow-up adds four exact WAL I/O error cases, two interrupted-WAL cases, two startup-corruption fixtures (v4/v5), and a backup corruption/restoration regression. Validation on 2026-10-15: complete `make ci`, `make storage-full-test`, `make storage-io-test`, `make lint`, `make coverage` and `git diff --check` passed. `make vulncheck` completed without known reachable vulnerability findings. Commands and CI configuration keep deterministic, filesystem-full and EIO/interruption validation explicit.

Literal electrical loss, drive/controller failure, false flush acknowledgements, arbitrary sector corruption and every `SQLITE_IOERR` subtype remain outside this test environment. Validating those requires disposable data and dedicated hardware or a documented storage fault laboratory that models the intended failure. Do not use the workstation or a live deployment for abrupt power-cut experiments. Sustained load and deployment-specific TLS, secrets, alerts and backup policies also remain separate validation tasks.
