# Historical benchmarks

[Español](../es/benchmarks/README.md) · Documentation review: 2026-10-16. Code reference: `35c4373`.

These reports describe short local experiments on 2026-10-13, not current production capacity or a service-level objective (SLO). All six original JSON files are retained byte-for-byte and shared by both language editions.

| Stage | Source baseline, dirty working tree | Design measured | Report |
| --- | --- | --- | --- |
| Initial baseline | `6f7266a3` | One worker, one connection, one send per 100 ms tick | [Baseline](BASELINE.md) |
| Consecutive dispatch | `cbda68cf` | One worker, idle wait only when no eligible work | [Worker loop](WORKER_LOOP.md) |
| Hexagonal separation | `316ae530` | One worker after package separation | [Hexagonal](HEXAGONAL.md) |

Each stage has two runs with 200 events at publication concurrency 1 and 8: 800 events per stage, 2400 overall. All recorded publications returned 202 and final attempts 204. This is evidence for the observed cases, not exactly-once delivery. Tables distinguish publication latency from publication-to-finalization latency and short-burst drainage from sustained throughput.

## How to read the evidence

JSON preserves environment, parameters, source baseline, dirty flag, binary/generator hashes and build information where available. All runs used a dirty working tree: the commit/hash metadata does not reconstruct an exact source patch by itself. Original complete documentation is available in Git at `35c4373`.

Runs share one machine, lack controlled caches/temperature/background load, and use brief loads, sampled peaks and a polling observer that adds load. The reports explain scrape-sample limits and physical-disk accounting. Do not infer causality or a maximum rate from before/after differences.

The current backend supports configurable workers and a separate reader pool; these historical results do not validate those later changes. Running the command today measures today's checkout, not the historical binaries. Use a new output name, record the exact revision and run separately from CI:

```sh
make benchmark BENCH_ARGS='--events 200 --concurrency 1 8 --payload-bytes 1024 --scrape-interval 1 --output docs/benchmarks/new-run.json'
```

The generator is Linux-specific (`/proc`) and limited to 10000 events, 64 concurrent publishers and 60000-byte payloads. It uses temporary databases and cleans processes/logs. It does not provide the complete sustained-load, external TLS, slow-receiver or multi-tenant fairness validation of a deployment. Original JSON field names remain English protocol/measurement keys in the Spanish edition too.
