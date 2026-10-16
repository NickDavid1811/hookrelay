# HookRelay — procesamiento consecutivo, 2026-10-13

[English](../../benchmarks/WORKER_LOOP.md) · Revisión documental: 2026-10-16.

Informe histórico: las referencias a comportamiento actual y recomendaciones corresponden a la revisión medida, no al código actual. Consultar [índice y límites](README.md).

Se eliminó la espera de 100 ms entre entregas disponibles, manteniendo una instancia y un trabajador. La espera queda solo cuando no hay trabajo elegible. Retención se comprueba entre envíos y durante idle; cancelación interrumpe el HTTP activo y conserva reservas recuperables si la finalización no se confirmó. No cambia esquema v4, persistencia FULL, seguridad de destinos, cuotas ni política de reintentos.

Misma receta que la línea base: 200 eventos por caso, payload 1024 bytes, publicación concurrency 1 y 8, receptor Go local HMAC/204 con logging, SQLite WAL/FULL en Btrfs/NVMe del proyecto, observador cada 100 ms y scrapes cada segundo. Las dos nuevas pasadas completaron 800 eventos, todos con publicación 202 y un intento finalizado 204 por evento; integridad/FK válidas. Los binarios y generador se identifican por hash en JSON.

| Pasada | Concurrencia publicación | Tiempo total hasta drenar (s) | Entregas/s | Publicación P50 / P99 (ms) | Entrega P99 (ms) | RSS dispatcher muestreada (MiB) | CPU media dispatcher (% de un núcleo) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Antes: segunda pasada | 1 | 19.94 | 10.0 | 4.48 / 16.85 | 18558 | 28.22 | 8.4 |
| Antes: segunda pasada | 8 | 19.96 | 10.0 | 34.82 / 112.91 | 18754 | 28.20 | 8.6 |
| Después: primera pasada | 1 | 2.09 | 95.6 | 6.21 / 24.19 | 510 | 28.60 | 65.9 |
| Después: primera pasada | 8 | 1.76 | 114.0 | 29.93 / 84.94 | 820 | 28.26 | 74.1 |
| Después: repetición | 1 | 1.10 | 181.2 | 4.77 / 8.56 | 96 | 27.99 | 84.3 |
| Después: repetición | 8 | 1.80 | 110.9 | 17.05 / 48.81 | 1234 | 27.79 | 74.3 |

El límite artificial de alrededor de diez envíos/s desaparece. Con esta carga local, el drenaje pasa de ~20 s a 1.10–2.09 s (~96–181/s). Publicación-a-finalización P99 nuevo: 96–1234 ms, frente a ~18.6–19.2 segundos anteriores. Estas tasas incluyen aceptación y detección de drenaje y no son throughput máximo sostenido.

CPU media del dispatcher durante la ventana más corta: 66–84% de un núcleo, frente a ~8% cuando había largas esperas entre envíos. No representa todos los ocho núcleos ni implica por sí solo saturación del equipo. RSS nueva muestreada: ~27.8–28.6 MiB. Los tamaños DB/WAL/SHM permanecen en el mismo orden (~4.5 MiB antes de cerrar y ~0.49 MiB después), con pequeñas diferencias de páginas.

La latencia de publicación no mejora uniformemente: P99 nuevo de 8.56–24.19 ms con concurrencia 1 y 48.81–84.94 ms con concurrencia 8. Worker y API comparten una conexión SQLite; la actividad más intensa puede competir por esa conexión. Sin profiling ni carga controlada no se atribuye causalmente cada variación a ese recurso. No se garantiza un SLO de publicación.

La duración de los casos nuevos solo permite uno o dos scrapes periódicos bajo carga, frente a 19 en la línea base; esos percentiles no permiten una comparación fiable del coste de métricas. Se conservan 20 scrapes después de drenar en cada caso. Ningún cambio se propone a métricas basándose en estas pocas muestras.

Validación: make ci completo PASS (race/vet/formato/módulos/build/smoke/fallos/replay/SIGKILL/backup/tenants/capacity). Pruebas nuevas: elegibilidad, futuro/idle/contexto precancelado, cancelación durante HTTP con reserva recuperable, un HTTP activo, mantenimiento con backlog. La primera ejecución se interrumpió por un fixture bloqueado al cerrar su servidor; el fixture ahora lee el cuerpo y garantiza liberación incluso al fallar. Todas las pruebas y la ejecución completa posterior pasaron.

Reproducir:

```sh
make benchmark BENCH_ARGS='--events 200 --concurrency 1 8 --payload-bytes 1024 --scrape-interval 1 --output docs/benchmarks/new-loop-run.json'
```

Limitaciones: mismo equipo compartido, solo dos nuevas pasadas por concurrencia, sin control de cachés/temperatura/carga; benchmark local, sin TLS/Internet, sin latencia de receptor lento ni carga sostenida. Picos/fin de drenaje se muestrean cada 100 ms. No es garantía de producción. Más trabajadores no necesariamente mejorarán este resultado: medir fairness, costes SQLite y escenarios lentos antes de decidir.

Informes: [antes](../../benchmarks/baseline-2026-10-13.json), [después](../../benchmarks/worker-loop-2026-10-13.json), [repetición](../../benchmarks/worker-loop-2026-10-13-repeat.json).
