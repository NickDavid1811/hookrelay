# Operations and recovery

[Español](es/OPERATIONS.md) · Documentation review: 2026-10-16.

This guide describes supported procedures and monitoring semantics. It is not a production deployment certification. Run commands from the repository root and adapt paths to the chosen installation.

## Deployment and startup

Use one dispatcher instance with a persistent local SQLite filesystem. The default API listener is literal loopback; remote binding requires `--insecure-allow-remote-listen` and provides no built-in TLS. Protect traffic with an appropriately configured TLS reverse proxy or private deployment boundary. Compose is a local demonstration with public credentials, not that production configuration.

Keep client tokens, endpoint secrets and the metrics credential private. Tokens are hashed for client authentication; dynamic HMAC secrets and event payloads are stored in SQLite, and dynamic secrets are plaintext. Protect the containing directory and backups as well as the main file. Preserve configuration, environment-secret references and startup flags independently of database backups.

Startup checks database structure, migrates, applies configuration and validates persisted destinations before serving. A database can already be migrated/configured when a later startup step fails; do not assume failed startup means unchanged files. Listener startup precedes workers. Use logs/process status to investigate failure. There is no automatic downgrade; preserve a pre-upgrade offline backup and the matching binary/configuration. Avoid restarting an older binary against a newer schema.

SIGINT/SIGTERM cancels work, shuts down HTTP and waits for worker/maintenance completion before closing storage. A cancelled in-flight send may leave a recoverable lease. Dispatch or maintenance storage failures stop the process with failure status; a supervisor should expose repeated restarts rather than hide them.

## Health and monitoring

| Signal | Interpretation |
| --- | --- |
| `/healthz` | The HTTP process responds; not proof of worker progress |
| `/readyz` | SQLite Ping succeeds; not proof of write capacity, free disk or receiver reachability |
| `hookrelay_queue_depth` | All unfinished work, including paused clients/endpoints |
| `hookrelay_queue_oldest_age_seconds` | Age since publication of oldest unfinished work |
| `hookrelay_queue_paused` | Unfinished work tied to inactive clients/endpoints |
| `hookrelay_expired_leases` | Current expired PROCESSING claims, not cumulative recoveries |
| `hookrelay_worker_running` | Run loop active (0/1), not worker count or proof of forward progress |
| `hookrelay_delivery_http_requests_total` | Observed outgoing requests, including unfinalized results |
| `hookrelay_delivery_results_total` | Results committed by this process |
| `hookrelay_publications_total` | Committed new publications and accepted idempotent receipts |
| `hookrelay_cleanup_last_success_timestamp_seconds` | Last successful batch in this process, zero before its first success |

Enable `/metrics` with a separate `HOOKRELAY_METRICS_TOKEN`. The scrape uses `Authorization: Bearer TOKEN`; an absent configured token disables the route (404), invalid credentials return 401, and collection failure can return plain-text 500. The example Prometheus configuration is `config/prometheus.example.yml`.

Counters/histograms reset when the process restarts. Storage gauges describe retained database state; retention lowers those counts but does not decrement process counters. Snapshot queries use a transaction, but do not share one with memory counters. The HTTP duration histogram includes bounded response reading/closing and excludes SQLite finalization. API request counters cover publish/replay, not every route. No default high-cardinality per-client/URL/ID labels are emitted.

Monitor filesystem capacity separately. Payload quotas exclude indexes, histories, WAL, SHM, backups and filesystem overhead. Cleanup requires successful writes and cannot be relied on to rescue an already full disk. Alert thresholds depend on measured workload; this repository provides metrics, not a complete alerting deployment.

## Offline backup and restoration

1. Pause publishers, stop the dispatcher and confirm exit. Preserve configuration, secrets and flags separately.
2. Choose new destination names with existing parent directories. Do not copy only the main file of an active WAL database or discard its sidecars.
3. Create and validate the snapshot with the supplied utility:

```sh
mkdir -p ./backups ./restored
python3 scripts/backup.py backup ./hookrelay.db ./backups/snapshot.db
python3 scripts/backup.py restore ./backups/snapshot.db ./restored/hookrelay.db
```

The SQLite Backup API includes committed WAL data. The utility validates integrity, foreign keys and required schema objects for versions 1–5 using `scripts/backup_schema.json`. It publishes through a staged hard link without replacing existing destinations or sidecars, synchronizes file/directory and produces mode 0600. It requires a filesystem supporting hard links. It does not stop processes, encrypt snapshots, authenticate their origin or certify arbitrary extra schema objects. A structurally valid database is not proof of correct business data.

4. Start only the chosen restored database with the original client identities, configuration and credentials. Change `--db` to the restored path. Do not run original and restored copies as an active pair.
5. Check health, histories, metrics and resumed deliveries before resuming publishers. Keep the original until verification completes. Restored leases retain their original deadlines; allow expiry naturally. An old snapshot can resend already processed work.

Although the Backup API can read a live WAL database coherently, busy writers can delay completion. The supported operating procedure above is offline. Migrations require a pre-change backup; downgrade/recovery uses a compatible snapshot and configuration, not manual schema edits.

## Incidents and limits

For 503 or uncertain publication outcomes, retain original bytes/key and retry with bounded backoff. For storage faults, stop publishers and the affected process, inspect logs/filesystem and follow [storage recovery](STORAGE_FAILURES.md). Never delete/truncate database sidecars to make space. Preserve corrupt input for investigation; restore a healthy validated backup to a new path.

DEAD work requires explicit audited replay; restart does not reset its budget. Paused work requires restoring the relevant configuration. Receivers must deduplicate stable IDs after retries and restoration. Literal electrical loss, physical device failure, arbitrary corruption and deployment-specific TLS/backup/secret policies require separate evidence; historical software tests do not establish them.
