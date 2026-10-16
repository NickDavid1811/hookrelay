# Glossary

[Español](es/GLOSSARY.md) · Documentation review: 2026-10-16. Code reference: `35c4373`.

Code names, states and keys remain unchanged; this table explains their meaning.

| Term | Meaning in HookRelay |
| --- | --- |
| Event | A published event type and JSON payload retained with a stable ID. |
| Delivery | The work of sending one event to one endpoint; retries preserve its ID. |
| Endpoint | A configured URL, owner and signing-secret policy. |
| Client / tenant | The authenticated ownership boundary. A shared client token does not identify individual people. |
| Worker | A bounded background loop that claims, sends and finalizes deliveries. |
| Claim / lease | Temporary ownership of a delivery until its stored deadline. Expiry permits recovery. |
| Fencing token | A random claim token checked at finalization to reject a stale worker. |
| Commit / rollback | Commit makes a transaction effective; rollback discards its uncommitted changes. |
| Uncertain commit | An error does not establish whether the complete transaction persisted; recovery must check. |
| Idempotency | Repeated identical publication bytes/key return the initial receipt while its record is retained. |
| Deduplication | Receiver-side prevention of repeated business effects using stable identifiers. |
| Attempt | A network send in ordinary language; stored attempt history specifically records committed finalizations. |
| Cycle / replay | A cycle has a finite attempt budget. Replay opens another cycle for DEAD work and preserves history. |
| Backoff / jitter | Increasing retry delay and its randomized variation; eligibility is persisted. |
| Retention | Time-based eligibility to remove terminal event graphs, history and idempotency keys. |
| Admission quota | A bound on accepted/retained work; not a requests-per-second rate limit. |
| HMAC | A shared-secret message authentication code over the timestamp and exact body bytes. |
| SSRF | Server-side request forgery: using a server to reach unintended destinations. |
| WAL / SHM | SQLite write-ahead log and shared-memory sidecar; do not discard them from active databases. |
| FULL / ENOSPC / EIO | SQLite synchronization setting; OS no-space error; OS input/output error. SQLITE_FULL is a different SQLite error, not the FULL setting. |
| Gauge / counter | A current-state value versus a process-lifetime accumulating metric. |
| P99 / RSS / SLO | 99th percentile; resident memory; service-level objective. Historical measurements do not imply an SLO. |
| Outbox | A publisher-side pattern linking business changes to later publication. HookRelay does not make that external transaction atomic. |
