# Flow Lab

[Español](es/PLAYGROUND.md) · Documentation review: 2026-10-16.

The Flow Lab is a local visual demonstration backed by real publications and deliveries. `cmd/playground` starts an isolated dispatcher, a controlled receiver and a web server. It does not read SQL directly or duplicate delivery rules.

## Run

Use Go from `go.mod`, Node 24/npm and Python 3 for full process verification. From the repository root:

```sh
make web-install
make playground
```

Open `http://127.0.0.1:8090` and stop with Ctrl+C. The build prepares binaries and web assets. Data lives in `work/playground/lab.db`, excluded from Git, separate from the ordinary dispatcher database. UI binds `127.0.0.1:8090`, receiver `127.0.0.1:9091`, private dispatcher API an ephemeral loopback port.

```sh
./bin/playground -h
./bin/playground -listen 127.0.0.1:8092 -receiver-listen 127.0.0.1:9092 -data-dir work/playground-alternative
```

Retain the receiver port when reusing a database because persisted destinations do not change automatically. Ignore an alternative data directory in Git before use. Keep all listeners on loopback; this lab has no public/LAN deployment contract. If startup fails, inspect port conflicts, data-directory access and whether `web/playground/dist` was built. Use a new data directory for an isolated demonstration instead of changing an existing database manually.

## Walkthrough and scenarios

Choose a scenario, optionally edit event type/payload, and publish one, five or ten events. Select a delivery to inspect history, receipts and timeline. The UI follows three steps: publish, explore the route and check the result. Six stages support manual Previous/Next reading; animation is optional. The interface text currently uses Spanish, regardless of documentation language.

| Scenario | Receiver | Expected result |
| --- | --- | --- |
| Success | 204 | SUCCESS, one attempt |
| Slow | Waits 1.5 seconds | SUCCESS; visible active send |
| Temporary failure | Two 503 responses, then 204 | SUCCESS, three attempts |
| Rate limit | 429 with Retry-After 3 seconds, then 204 | SUCCESS, two attempts |
| Lost response | Processes first request, closes connection without response | Retry and two observed effects |
| Exhausted | Always 503 | DEAD after five attempts |

For DEAD, the reset-and-resend control changes that controlled receiver to success and requests a real audited replay. Pause/repeat/speed controls affect only the drawing, never the worker or a new publication. The mobile route is vertical; advanced JSON, IDs, other deliveries and metrics are collapsible. Reduced motion remains supported.

The animation is illustrative. API/history is persisted evidence; receiver receipts are observations; polling timestamps show detection, not exact internal transaction times. Polling may miss brief states. The 429 countdown represents a known minimum, not exact eligibility with jitter. Export downloads the snapshot without payloads, credentials or private destination URLs; user-provided event-type metadata remains part of the snapshot.

## Persistence and limits

SQLite retains dispatcher work. Session lists, receiver observations and visual timeline are bounded in-memory structures and are not reconstructed on restart; export before stopping. A session allows 200 publications. Queue limits are 200 global/client, retained-event cap 1000, payload budget 4 MiB and maximum cycles five including the initial cycle. Other retention defaults come from core limits. These are demonstration limits, not capacity claims.

A restart may recover pending database work without reconstructing its old visual session. Do not infer that an empty screen means an empty database. The lost-response case deliberately demonstrates effects that a production receiver must deduplicate.

## Local facade and development

| Method/path | Purpose |
| --- | --- |
| `GET /play/api/state` | Session snapshot and selected metrics |
| `GET /play/api/export` | Same snapshot as downloadable JSON |
| `POST /play/api/publish` | Scenario, event_type, payload and count |
| `POST /play/api/replay/{id}` | Controlled recovery with expected_cycle |

Mutation bodies allow 12 KiB, one JSON value and no unknown fields. Publication payload is limited to 4096 bytes, event_type to 128 bytes and count to 1–12 at the facade (the UI offers 1/5/10). A batch is not atomic: a failed publish response may include accepted_ids for work already accepted; do not blindly repeat the whole batch. Mutations require `X-Playground-Request: 1`. Host/Origin checks, cross-site restrictions and security headers protect the local facade; tokens/secrets remain in Go. This facade is not the versioned business API.

For visual development, start the built backend, then `npm --prefix web/playground run dev`. Vite listens on loopback and proxies `/play/api` to port 8090, adjusting Origin. Do not deploy the development server publicly.

```sh
make web-test
make playground-test
make ci
```

Install web dependencies first; `make ci` installs from the lockfile. The five model tests and process smoke scenarios do not cover all browser interactions or accessibility. Session reconstruction, explicit correlated internal tracing, component/accessibility tests and user-comprehension evaluation remain unimplemented or unvalidated extensions. See the dated [UX review](history/PLAYGROUND_UX.md).
