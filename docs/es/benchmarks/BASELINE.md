# HookRelay — línea base local, 2026-10-13

[English](../../benchmarks/BASELINE.md) · Revisión documental: 2026-10-16. Referencia del código: `35c4373`.

Informe histórico: las referencias a comportamiento actual y recomendaciones corresponden a la revisión medida, no al código actual. Consultar [índice y límites](README.md).

El flujo completó 800 eventos en cuatro casos: dos pasadas de 200 eventos con concurrencias de publicación 1 y 8. Todas las publicaciones respondieron 202; hubo un intento finalizado HTTP 204 por evento y ninguna entrega DEAD. Integridad SQLite y referencias válidas después del cierre. Esto describe la ejecución observada, no una garantía de entrega exactamente una vez.

Entorno: AMD Ryzen 5 7520U, 8 CPU lógicas, Linux 7.2.9-arch1-1, go version go1.27.2-X:nodwarf5 linux/amd64, Python 3.14.8, modernc.org/sqlite v1.60.1. Datos en /dev/nvme0n1p5[/@home/archlinux/Documents/proyectos/hookrelay] btrfs. Receptor local Go con HMAC, 204 y logging; WAL/FULL; un worker. Payload de 1024 bytes, cuerpo de publicación 1087 bytes. Scrape cada segundo; observación SQLite/RSS cada 100 ms.

| Pasada | Publicaciones concurrentes | Aceptación en ráfaga (eventos/s) | Publicación P50 / P99 (ms) | Drenaje total (s) | Entregas/s hasta drenar | RSS máximo muestreado dispatcher (MiB) | CPU media dispatcher (% de un núcleo) | Scrape bajo carga media / máximo (ms) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Primera pasada | 1 | 231.1 | 3.82 / 9.70 | 19.93 | 10.04 | 28.36 | 8.08 | 5.43 / 9.93 |
| Primera pasada | 8 | 369.0 | 18.12 / 55.69 | 20.01 | 10.00 | 26.06 | 7.95 | 5.69 / 11.82 |
| Segunda pasada | 1 | 169.2 | 4.48 / 16.85 | 19.94 | 10.03 | 28.22 | 8.38 | 5.66 / 11.14 |
| Segunda pasada | 8 | 201.3 | 34.82 / 112.91 | 19.96 | 10.02 | 28.20 | 8.57 | 6.28 / 25.55 |

Los cuatro casos drenan aproximadamente diez eventos por segundo. Es consistente con el loop actual: ticker de 100 ms y un único envío por step. No demuestra saturación de CPU, red o SQLite; el dispatcher promedió alrededor de 8–9% de un núcleo durante carga y drenaje. Antes de añadir trabajadores, conviene permitir que un worker tome otro trabajo disponible inmediatamente al completar el anterior, manteniendo espera cuando no hay trabajo. Esa optimización todavía no está implementada.

Con ocho publicaciones concurrentes aumentó la tasa de aceptación en cada pasada, pero también la latencia individual. La variación entre pasadas es relevante: P99 de publicación fue 9.70–16.85 ms con concurrencia 1 y 55.69–112.91 ms con concurrencia 8. No es una carga sostenida ni capacidad máxima, y estas cifras no constituyen un SLO. No se identificó por profiling la causa de la variación.

La cola alcanzó entre 188 y 194 entregas en las muestras; publicación-a-finalización P99 fue aproximadamente 18.6–19.2 segundos, dominado por la espera de drenaje de la ráfaga. La RSS muestreada del dispatcher quedó entre 26.06 y 28.36 MiB y la del receptor entre 16.03 y 18.58 MiB. No incluye memoria del generador ni equivale al máximo absoluto.

Scrapes durante carga: medias 5.43–6.28 ms, máximos 9.93–25.55 ms, con 19 muestras por caso. Después del drenaje, medias 2.03–5.64 ms con 20 muestras por caso. Esto mide latencia con 200 eventos retenidos, no CPU atribuible a métricas ni coste con grandes colas. No se comparó una pasada principal sin scrapes periódicos; no inferir un impacto causal en throughput.

Disco: DB/WAL/SHM sumaron aproximadamente 4.47–4.49 MiB de tamaño aparente antes del cierre; la DB quedó en 512000 bytes (0.49 MiB) después de apagar limpiamente el dispatcher. Los payloads suman 204800 bytes. st_blocks se registra en JSON, pero no mide toda la asignación física/compresión/snapshots de Btrfs ni escrituras del dispositivo.

Reproducir desde el repositorio:

```sh
make benchmark BENCH_ARGS='--events 200 --concurrency 1 8 --payload-bytes 1024 --scrape-interval 1 --output docs/benchmarks/new-run.json'
```

El generador está limitado a 10000 eventos, 64 solicitudes concurrentes y payload de 60000 bytes. Cada caso usa DB nueva; ambos binarios se cierran y los datos/logs temporales se eliminan. El informe conserva hashes de binarios y del generador, commit, estado modificado y parámetros. La segunda pasada usa el generador final con build info; la primera conserva su hash previo a mejoras de metadata/validación sin cambios en el procedimiento de carga. Base Git: 6f7266a374edb64e03b4827837ccb52bc9efb331; ambos informes registran árbol modificado por los artefactos de medición.

Limitaciones: equipo compartido por todos los procesos, cachés/carga/temperatura no controladas, dos observaciones por concurrencia, sin warmup separado, percentiles con 200 muestras y P99 con evidencia limitada. El sondeo de SQLite y RSS añade carga; el drenaje se detecta cada 100 ms y sus latencias usan reloj UTC en milisegundos. No se probaron TLS/Internet, disco lleno, fallos bajo carga, retención larga ni crecimiento a miles de eventos. No se cambiaron el backend o su concurrencia ni se añadieron umbrales de CI.

Informes: [primera pasada](../../benchmarks/baseline-2026-10-13-initial.json) y [segunda pasada](../../benchmarks/baseline-2026-10-13.json).

Actualización posterior: el procesamiento consecutivo está implementado y medido. Consultar [comparación del loop](WORKER_LOOP.md); los informes originales permanecen como evidencia histórica.
