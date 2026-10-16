# Configuration

[Español](es/CONFIGURATION.md) · Documentation review: 2026-10-16.

Commands run from the repository root. `./bin/dispatcher --help` is the executable reference; flag declarations are in `cmd/dispatcher/main.go` and defaults/validation in `internal/core/limits.go`.

## Clients and static endpoints

Without `--config`, the dispatcher creates the legacy local client `local-client` and endpoint `test`, reading `HOOKRELAY_TOKEN` and `HOOKRELAY_SECRET`. The default destination is local and requires `--allow-loopback-test`.

For explicit configuration:

```sh
./bin/dispatcher --config config/clients.example.json
```

The example references environment variables; define private values before starting. It uses placeholder domains and is not a live delivery target. A configuration document has this shape:

```json
{"clients":[{"id":"billing","token_env":"BILLING_TOKEN","endpoints":[{"id":"invoice-events","url":"https://receiver.example.com/webhook","secret_env":"BILLING_WEBHOOK_SECRET","allow_loopback_test":false}]}]}
```

The file is limited to 1 MiB and one JSON document; unknown fields are rejected. Configuration requires 1–100 clients. Client and static endpoint IDs use 1–64 ASCII letters, digits, `_` or `-`. Endpoint IDs are globally unique. Environment variable names use the validated identifier format; tokens and HMAC secrets require 16–4096 bytes. Tokens must be unique across clients and differ from the metrics token. Length alone does not guarantee secret entropy.

Startup reconciles configuration atomically. Omitted clients become inactive; omitted static endpoints become inactive. Their unfinished work is paused rather than removed. Restoring the same identity/configuration can resume it. Existing destination URL, owner, secret environment reference and loopback policy are immutable; use a new endpoint ID for those changes. Changing the value of the same secret/token environment variable takes effect on restart, without an overlapping-key rotation protocol. There is no hot reload.

## Dynamic endpoints

`POST /api/v1/endpoints` accepts `id`, `url`, optional `secret` and the loopback flags described in [OpenAPI](openapi.yaml). An omitted/empty ID generates `ep_` plus 16 hexadecimal characters. An omitted/empty secret generates `hrsec_` plus 256 random bits encoded as hexadecimal. The creation response includes the assigned secret; subsequent GETs do not return it. SQLite and backups contain dynamic secrets as plaintext.

A client's loopback request also requires the operator's `--allow-loopback-test`. Normal external destinations require HTTPS on port 443 and public resolved addresses. Static configuration can explicitly authorize a literal loopback destination. Redirects and environment proxies are not followed; DNS results are checked at dial time. Private-network destinations are not generally supported.

Dynamic endpoints are immutable through the API. DELETE deactivates them only when no PENDING, RETRYING or PROCESSING deliveries remain. Configuration-owned endpoints return 409 on API deletion. Retention eventually removes unreferenced inactive dynamic endpoints; static identities are retained. Quotas count retained endpoints, including inactive records, not only visible active endpoints.

## Runtime settings

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `hookrelay.db` | Local SQLite path |
| `--listen` | `127.0.0.1:8080` | Literal IP and port; loopback by default |
| `--endpoint` | `http://127.0.0.1:9090/webhook` | Destination in legacy local mode |
| `--config` | empty | Explicit startup JSON configuration |
| `--allow-loopback-test` | false | Operator permission for test loopback destinations |
| `--insecure-allow-remote-listen` | false | Permit non-loopback API binding without built-in TLS |
| `--workers` | 1 | Concurrent workers, 1–50 |
| `--max-attempts` | 5 | Attempts per cycle, 1–50; initial send counts |
| `--retry-after-max` | 5m | Ceiling for Retry-After, positive and at most 24h |
| `--max-queue` | 10000 | Global unfinished delivery cap |
| `--max-client-queue` | 1000 | Unfinished delivery cap per client |
| `--max-events` | 20000 | Retained event cap |
| `--max-payload-bytes` | 268435456 | Retained payload byte cap; excludes physical database overhead |
| `--max-endpoints` | 1000 | Retained endpoint cap globally |
| `--max-client-endpoints` | 100 | Retained endpoint cap per client |
| `--max-cycles` | 10 | Total cycles including initial cycle; 1–1000 |
| `--success-retention` | 168h | Retention after SUCCESS |
| `--dead-retention` | 720h | Retention after DEAD |
| `--cleanup-interval` | 1m | Interval between cleanup batches |
| `--cleanup-batch` | 100 | Event graphs per cleanup transaction; 1–1000 |

Queue counts include PENDING, RETRYING and PROCESSING. Client admission overflow returns 429; global/storage overflow returns 503. These are admission quotas, not requests-per-second controls. Queue/event/endpoint/payload limits must be positive; retention and cleanup intervals must be at least one second. Lowering a quota does not delete existing data.

Set `HOOKRELAY_METRICS_TOKEN` to a separate 16–4096-byte operator credential to enable `/metrics`; otherwise it returns 404. No credentials are configured through this documentation. See [operations](OPERATIONS.md) for monitoring, storage and deployment precautions.
