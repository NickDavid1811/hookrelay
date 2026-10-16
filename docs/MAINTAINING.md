# Maintainer guide

[Español](es/MAINTAINING.md) · Documentation review: 2026-10-16. Code reference: `35c4373`.

Start with [architecture](ARCHITECTURE.md), [configuration](CONFIGURATION.md), [delivery contract](DELIVERY_CONTRACT.md) and [OpenAPI](openapi.yaml). This guide specifies how to change and check the implementation. Historical PASS results are in [history](history/SESSION.md); they are not fresh validation of a new change.

## Environment and checks

Use Go from `go.mod`, Python 3.10+, and Node 24/npm for the web. Tests bind loopback sockets, spawn processes and require writable temporary directories. Dependency installation and vulnerability scanning need network access. The Linux fault runners additionally need Docker Engine and kernel support.

```sh
make lint
make vulncheck
make ci
make coverage
make storage-full-test
make storage-io-test
```

| Command | Coverage |
| --- | --- |
| `make lint` | gofmt validation and go vet; no optional fallback |
| `make vulncheck` | Pinned govulncheck; download/analysis failures fail the target |
| `make ci` | Format, module verification, race tests, vet, builds, smoke/early-exit regression, real-process failures/replay, backup, tenants, capacity, web install/tests/build and lab scenarios |
| `make coverage` | Race-enabled Go coverage report; not an acceptance threshold or exhaustive verification |
| `make storage-full-test` | Actual ENOSPC in isolated 4 MiB Docker tmpfs |
| `make storage-io-test` | WAL write/sync EIO and interrupted transaction recovery |

Process suites run sequentially. Ordinary Go tests skip isolated filesystem fault cases unless the runner enables them; a skip is not evidence of that failure boundary. GitHub Actions runs lint, vulnerability scanning, coverage, `make ci` and both fault targets. Check `.github/workflows/ci.yml` for the exact current sequence.

Docker distribution has a separate smoke procedure: `docker compose up --build -d --wait`, publish/query using the getting-started example, then `docker compose down`. Use an isolated Compose project when needed and do not remove volumes holding required data. Do not claim a remote CI or release succeeded without observing that run. The tag release workflow is separate from CI; its existence does not establish an explicit validation dependency or runtime support on every compiled platform.

## Invariants to preserve

- Commit publication, receipt and delivery together. Idempotency is scoped to client and exact request bytes.
- Keep ownership/quota/state predicates in the same transaction as writes. Preserve lease fencing and expiry checks at finalization.
- Never hold SQLite transactions across network sends. Do not trade destination/DNS/proxy restrictions for a passing test.
- Confirm replay state and audit together; retention deletes complete eligible graphs and their idempotency keys.
- Dynamic endpoint creation must not mutate a runtime cache before commit. Static/dynamic secrets have no fallback chain.
- Join workers and maintenance before closing storage. Propagate operational errors to process status.
- Publish metrics for confirmed results after commit; distinguish HTTP observations from committed history.

## API and schema changes

Change handlers/application, both OpenAPI editions, examples and relevant HTTP tests together. `internal/relay/api_contract_test.go` is targeted regression coverage, not full schema conformance. Keep array/cursor compatibility unless explicitly versioning the contract. API version, SQLite schema version and distribution tag are separate concepts.

Never rewrite applied migrations. Add a versioned transaction and tests opening old databases, preserving rows, migrating and resuming delivery. Update `scripts/backup_schema.json` with canonical DDL and retain older entries. Backup tests must reject missing/altered required objects before publishing snapshots. Manual schema edits require reconciliation rather than silent approval.

Startup quick_check is structural and not a full index/content or payload integrity audit. Migrated storage may persist even when a later startup phase fails; use a pre-change offline backup for recovery. No automatic downgrade is provided. See [operations](OPERATIONS.md).

## Documentation maintenance

Update English and `docs/es/` in the same change. Keep protocol keys, code, identifiers and measurement files unchanged by translation. Link to one main explanation instead of copying it. Historical reports must identify baseline, environment, commands, results and limits; append corrections instead of presenting old results as current. All original benchmark JSON remains shared and unmodified.

For this documentation revision, existing long session notes were consolidated and obsolete planning removed. Earlier complete files are recoverable with `git show 35c4373:docs/SESSION.md` and the corresponding path for other documents. This preserves provenance without making the old transcript the current operating guide.
