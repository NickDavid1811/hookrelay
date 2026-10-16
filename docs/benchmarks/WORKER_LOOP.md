# Consecutive worker processing — 2026-10-13

[Español](../es/benchmarks/WORKER_LOOP.md) · Documentation review: 2026-10-16.

Historical comparison. The dispatcher removed the 100 ms wait between available deliveries, retaining one instance/worker. It waited only without eligible work. At that revision retention was checked between sends/while idle; cancellation interrupted active HTTP and preserved recoverable claims if finalization was not confirmed. Schema v4, FULL, destination security, quotas and retry policy were unchanged.

## Procedure and results

Same recipe as baseline: 200 events/case, 1024-byte payload, publication concurrency 1/8, local Go HMAC/204 logging receiver, WAL/FULL on project Btrfs/NVMe, 100 ms observer and one-second scrapes. Two new runs completed 800 events, all 202 with one finalized 204/event; integrity/foreign keys passed. JSON identifies binary/generator hashes.

| Run | Publication concurrency | Total time until drained (s) | Deliveries/s | Publication P50 / P99 (ms) | Delivery P99 (ms) | Sampled dispatcher RSS (MiB) | Mean dispatcher CPU (% of one core) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Before: second run | 1 | 19.94 | 10.0 | 4.48 / 16.85 | 18558 | 28.22 | 8.4 |
| Before: second run | 8 | 19.96 | 10.0 | 34.82 / 112.91 | 18754 | 28.20 | 8.6 |
| After: first run | 1 | 2.09 | 95.6 | 6.21 / 24.19 | 510 | 28.60 | 65.9 |
| After: first run | 8 | 1.76 | 114.0 | 29.93 / 84.94 | 820 | 28.26 | 74.1 |
| After: repeat | 1 | 1.10 | 181.2 | 4.77 / 8.56 | 96 | 27.99 | 84.3 |
| After: repeat | 8 | 1.80 | 110.9 | 17.05 / 48.81 | 1234 | 27.79 | 74.3 |

The artificial roughly 10 sends/s cap disappeared. Drainage moved from about 20 seconds to 1.10–2.09 seconds (~96–181/s). Publication-to-finalization P99 was 96–1234 ms versus roughly 18.6–19.2 seconds before. Rates include acceptance/drain detection and are not maximum sustained throughput.

CPU averaged 66–84% of one core in the shorter window, versus ~8% with waits; this is not all eight cores or proof of machine saturation. Sampled RSS was ~27.8–28.6 MiB. DB/WAL/SHM stayed around ~4.5 MiB before shutdown/~0.49 MiB after, with small page differences.

Publication latency did not uniformly improve: P99 8.56–24.19 ms at concurrency 1 and 48.81–84.94 ms at 8. At that revision API/worker shared one SQLite connection and could contend. Without profiling/controlled load, variations were not attributed causally and no publication SLO was promised.

Only one or two periodic scrapes fit under new load, versus 19 at baseline. Those samples cannot reliably compare metrics cost. Twenty post-drain scrapes remained per case; no metrics optimization followed from them.

## Validation and limits

Recorded complete make ci passed, including race/vet/format/modules/build/smoke/failures/replay/SIGKILL/backup/tenants/capacity. New tests covered eligibility, idle/future/pre-cancelled contexts, active HTTP cancellation/recoverable claim, one active send and maintenance with backlog. An initial fixture hung closing its server; it was fixed to read/release reliably, and later tests/full checks passed.

Shared machine, only two new runs per concurrency, uncontrolled caches/temperature/load, no TLS/Internet, slow receiver or sustained load. Peaks/drain detection were sampled at 100 ms. These are not production guarantees; more workers do not necessarily improve the measured workload. Use the [benchmark procedure](README.md) with a new output filename; running today measures current code.

Raw evidence: [before](baseline-2026-10-13.json), [after](worker-loop-2026-10-13.json), [repeat](worker-loop-2026-10-13-repeat.json).
