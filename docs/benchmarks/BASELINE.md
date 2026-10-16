# Local baseline — 2026-10-13

[Español](../es/benchmarks/BASELINE.md) · Documentation review: 2026-10-16.

Historical measurement; one worker and SQLite WAL/FULL. Four cases completed 800 events: two runs of 200 at publication concurrency 1 and 8. All publications returned 202 and each event had one finalized 204 attempt; no DEAD delivery. Integrity/foreign keys passed after shutdown. This is not an exactly-once guarantee.

## Environment and results

AMD Ryzen 5 7520U, eight logical CPUs, Linux 7.2.9-arch1-1, Go 1.27.2-X:nodwarf5, Python 3.14.8, modernc.org/sqlite v1.60.1, Btrfs/NVMe project filesystem. Local Go receiver verified HMAC and returned 204 with logging. Payload 1024 bytes, publication body 1087 bytes, scrape interval one second and SQLite/RSS observation every 100 ms.

| Run | Concurrent publications | Burst acceptance (events/s) | Publication P50 / P99 (ms) | Total drain (s) | Deliveries/s until drained | Sampled dispatcher peak RSS (MiB) | Dispatcher mean CPU (% of one core) | Load scrape mean / max (ms) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| First run | 1 | 231.1 | 3.82 / 9.70 | 19.93 | 10.04 | 28.36 | 8.08 | 5.43 / 9.93 |
| First run | 8 | 369.0 | 18.12 / 55.69 | 20.01 | 10.00 | 26.06 | 7.95 | 5.69 / 11.82 |
| Second run | 1 | 169.2 | 4.48 / 16.85 | 19.94 | 10.03 | 28.22 | 8.38 | 5.66 / 11.14 |
| Second run | 8 | 201.3 | 34.82 / 112.91 | 19.96 | 10.02 | 28.20 | 8.57 | 6.28 / 25.55 |

Drainage near 10/s matched the then-current 100 ms tick and one send per step. It did not prove CPU/network/SQLite saturation: mean CPU was about 8–9% of one core. Consecutive dispatch was subsequently implemented; see [worker loop](WORKER_LOOP.md).

Concurrency 8 improved burst acceptance but raised individual latency. Between runs, publication P99 varied 9.70–16.85 ms at concurrency 1 and 55.69–112.91 ms at concurrency 8. No profiling established the cause, no sustained maximum was measured and no SLO followed.

Queue samples peaked at 188–194; publication-to-finalization P99 was about 18.6–19.2 seconds, dominated by drainage. Sampled dispatcher RSS was 26.06–28.36 MiB and receiver RSS 16.03–18.58 MiB, excluding the generator and not absolute peaks.

Nineteen scrapes per loaded case averaged 5.43–6.28 ms, maxima 9.93–25.55 ms. Twenty post-drain scrapes averaged 2.03–5.64 ms. They describe a 200-event retained dataset, not metrics CPU attribution or large-queue cost. There was no matching main run without scrapes, so no causal throughput effect was inferred.

DB/WAL/SHM apparent size before closing was about 4.47–4.49 MiB; after clean shutdown the database was 512000 bytes (0.49 MiB). Payloads totaled 204800 bytes. st_blocks does not capture all Btrfs compression/snapshot allocation or device writes.

## Reproduction and limits

Use the [benchmark procedure](README.md). Each case used a new database and cleaned binaries' processes, temporary data and logs. The report records hashes, commit, dirty flag and parameters. The second run added final build metadata; the first retained its earlier generator hash without changing the load procedure. Both used base `6f7266a374edb64e03b4827837ccb52bc9efb331` with uncommitted measurement work.

Shared machine, uncontrolled caches/load/temperature, two observations per concurrency, no separate warmup and 200 samples limit P99 evidence. Polling adds load; drain detection is sampled every 100 ms and timestamps use UTC milliseconds. No TLS/Internet, full disk, faults under load, long retention or thousands-of-events growth was measured in this experiment. No backend/concurrency change or CI performance threshold was introduced by the baseline itself.

Raw evidence: [first run](baseline-2026-10-13-initial.json), [second run](baseline-2026-10-13.json). Historical descriptions of the tick apply to the measured revision, not today's dispatcher.
