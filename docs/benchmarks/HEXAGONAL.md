# Validation after hexagonal separation — 2026-10-13

[Español](../es/benchmarks/HEXAGONAL.md) · Documentation review: 2026-10-16.

Historical report. After restarting the session, loopback sockets were available and full make ci passed: format/modules/race/vet/build/HTTP smoke/failures/replay/SIGKILL/backup/tenants/capacity/retention. Tests were not changed to bypass network restrictions.

## Results

Two runs after CI, without concurrent CI: 200 events per publication concurrency 1/8, 1024-byte payload, one worker, local HMAC/204 receiver, WAL/FULL, 100 ms observation and one-second scrapes. There were 800 publications 202 and 800 finalized 204 attempts; integrity/references passed. Environment/binary hashes were recorded with uncommitted refactoring work.

| Run | Concurrency | Total drain (s) | Deliveries/s | Publication P99 (ms) | Delivery P99 (ms) | RSS dispatcher (MiB) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 1 | 1.86 | 107.6 | 22.67 | 431 | 27.98 |
| 1 | 8 | 2.68 | 74.5 | 137.59 | 1520 | 28.46 |
| 2 | 1 | 2.36 | 84.9 | 21.73 | 555 | 27.62 |
| 2 | 8 | 2.36 | 84.6 | 135.84 | 1202 | 27.61 |

The preceding [consecutive worker](WORKER_LOOP.md) runs drained in 1.10–2.09 seconds (~96–181/s); these took 1.86–2.68 seconds (~75–108/s). At concurrency 8, publication P99 was 135.84–137.59 ms versus 48.81–84.94 ms. This observed difference merits controlled comparison; neither performance equivalence nor architectural causation was established.

Machine sharing, uncontrolled caches/background load, short cases, two runs and an observer limit interpretation. Only one or two scrapes occurred under each load; do not compare operational scrape percentiles. This is not maximum capacity or an SLO.

## Interpretation today

The report's proposed next step was interleaved equivalent before/after comparisons and sustained/slow-receiver/fairness measurements before increasing concurrency. The current implementation subsequently gained configurable workers and a reader pool; that proposal is historical and these runs do not validate those later changes.

Use the [benchmark procedure](README.md) with a new output filename; the same command now measures the current checkout rather than reproducing historical binaries. Raw evidence: [first run](hexagonal-2026-10-13.json), [repeat](hexagonal-2026-10-13-repeat.json).
