# Current engineering backlog

[Español](es/BACKLOG.md) · Updated: 2026-10-16. Baseline: `b98ca1c`.

This is the current work list. Historical reports describe earlier experiments and do not establish validation of this repository's current revision. One dispatcher, local SQLite, finite retries and possible duplicate delivery remain the supported scope. This list does not authorize deployment or publication.

## Consolidation in this change

- Documentation: remove references to an unavailable previous-repository revision and instructions claiming that complete earlier transcripts can be restored here. Preserve historical measurement files and label the limits of provenance.
- Release gate: reuse the full CI workflow from the tag-triggered release workflow. Both publication jobs require successful validation of the same event commit. Keep validation read-only and grant write permissions only to publication jobs.
- Acceptance: no removed revision references remain; both language editions link here; workflow lint passes; publication jobs depend on validation without a bypass condition. Remote execution remains unverified until an actual run is observed.

## Prioritized pending work

| ID | Priority / type | Deliverable and acceptance criteria |
| --- | --- | --- |
| B1 | High / measurement | Extend the Linux benchmark with worker count, sustained duration, slow receivers and multiple clients. Record exact commit, parameters, environment, errors, queue age, publication/delivery latency, memory and storage. Compare repeated equivalent runs separately from CI; define an initial capacity envelope from evidence. |
| B2 | High / functionality | Add tenant-scoped delivery listing with state, endpoint and date filters, bounded stable cursor pagination and appropriate indexes. Prove isolation and traversal without duplicates on unchanged data; document concurrent changes and update both OpenAPI editions. |
| B3 | High / design then implementation | Define token/HMAC rotation and endpoint update semantics, including in-flight work, credential overlap, revocation and recovery. Define stored-secret protection for the intended deployment; implement with migration and regression coverage where needed. |
| B4 | High / deployment validation | Prepare one isolated pilot deployment with TLS, external HTTPS delivery, process supervision, protected secrets and actionable queue/disk/error alerts. Restore a backup with configuration and credentials into a separate instance; record recovery time and possible duplicate effects. |
| B5 | Medium / user validation | Observe a new user publishing, diagnosing failure and replaying DEAD work. Record misunderstandings; add browser interaction and keyboard/accessibility checks for critical Flow Lab journeys. |
| B6 | Conditional / scheduling | Use B1 and pilot evidence to decide whether per-client/endpoint rates, fair scheduling or circuit breakers are needed. If selected, define measurable behavior and prove that one slow/noisy tenant does not monopolize the configured budget. |
| B7 | Before production claim / independent review | Review security and recovery independently and retain evidence for the chosen revision and environment. Describe untested physical-storage/power-loss behavior explicitly; software fault tests do not certify hardware durability. |

## Boundaries and decisions

Broker migration, multiple active replicas, SSO, billing, provider connectors and a full operations portal remain outside this increment. Business adoption and commercial authorization under LICENSE are separate from technical validation. Do not treat possible extensions as commitments.

## Release validation contract

`release.yml` calls the local reusable `ci.yml`, including lint, vulnerability analysis, coverage, full CI, ENOSPC and WAL I/O/interruption checks. A failure, cancellation or skipped validation prevents both publication jobs. Checkout uses the event SHA, not a later branch head. CI triggered independently by push can also run; the release depends on its own reusable validation, not on an unrelated green status.

This gate does not make the image and binary publications atomic: either publication can fail after the other succeeds. A tag push is a publishing action; do not create a release tag merely to test this change. Local workflow lint verifies structure, not GitHub execution, registry permissions or cross-platform runtime behavior. Record those results only after observing them.

Local verification on 2026-10-16: actionlint v1.7.12 passed for both workflows; all relative Markdown links under docs resolved; the removed revision reference was absent; git diff whitespace checks passed. No application code changed, no application/fault suite was rerun, and no tag or release was published.
