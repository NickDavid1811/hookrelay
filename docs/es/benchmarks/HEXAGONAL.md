# Validación tras la separación hexagonal — 2026-10-13

[English](../../benchmarks/HEXAGONAL.md) · Revisión documental: 2026-10-16. Referencia del código: `35c4373`.

Informe histórico: las referencias a comportamiento actual y recomendaciones corresponden a la revisión medida, no al código actual. Consultar [índice y límites](README.md).

Tras reiniciar Codex, se verificó la apertura de sockets de loopback y pasó `make ci` completo: formato, módulos, pruebas Go con `-race`, vet, build, smoke HTTP, fallos/reintentos/replay/SIGKILL, backup/restauración, clientes y capacidad/retención. No se modificaron las pruebas para omitir restricciones de red.

Dos pasadas posteriores al CI, sin CI concurrente: 200 eventos por concurrencia de publicación 1/8, payload 1024 bytes, un worker, receptor local HMAC/204, WAL/FULL, sondeo 100 ms, scrapes cada segundo. 800 publicaciones 202 y 800 intentos finalizados 204; integridad y referencias válidas. Los informes incluyen entorno y hashes de binarios; árbol con refactorización sin commit.

| Pasada | Concurrencia | Drenaje total (s) | Entregas/s | Publicación P99 (ms) | Entrega P99 (ms) | RSS dispatcher (MiB) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 1 | 1.86 | 107.6 | 22.67 | 431 | 27.98 |
| 1 | 8 | 2.68 | 74.5 | 137.59 | 1520 | 28.46 |
| 2 | 1 | 2.36 | 84.9 | 21.73 | 555 | 27.62 |
| 2 | 8 | 2.36 | 84.6 | 135.84 | 1202 | 27.61 |

Las pasadas anteriores de [worker consecutivo](WORKER_LOOP.md) drenaron en 1.10–2.09 s (~96–181/s); estas drenaron en 1.86–2.68 s (~75–108/s). Con concurrencia 8, publicación P99 actual 135.84–137.59 ms frente a 48.81–84.94 ms anteriores. Es una diferencia observada que merece comparación controlada; no se declara equivalencia de rendimiento ni se atribuye causalmente la variación a la arquitectura. Equipo compartido, cachés/carga sin aislar, cargas breves, dos pasadas por caso y observador que añade carga. Solo 1–2 scrapes durante cada carga; no comparar percentiles operativos de scrape con estas muestras. No es prueba de capacidad máxima ni un SLO.

Pendiente: comparar versiones antes/después en condiciones equivalentes e intercaladas antes de decidir una optimización. Mantener un worker y medir carga sostenida, receptores lentos y fairness antes de ampliar concurrencia.

Reproducir con un archivo de salida que aún no exista:

```sh
make benchmark BENCH_ARGS='--events 200 --concurrency 1 8 --payload-bytes 1024 --scrape-interval 1 --output docs/benchmarks/new-hexagonal-run.json'
```

Informes: [primera pasada](../../benchmarks/hexagonal-2026-10-13.json), [repetición](../../benchmarks/hexagonal-2026-10-13-repeat.json).
