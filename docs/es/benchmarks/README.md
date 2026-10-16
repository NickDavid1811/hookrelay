# Benchmarks históricos

[English](../../benchmarks/README.md) · Revisión documental: 2026-10-16. Referencia del código: `35c4373`.

Los informes describen experimentos locales breves del 2026-10-13, no capacidad productiva actual ni objetivo de nivel de servicio (SLO). Los seis JSON originales se conservan byte por byte y se comparten entre idiomas.

| Etapa | Base del código con árbol modificado | Diseño medido | Informe |
| --- | --- | --- | --- |
| Línea base | `6f7266a3` | Un worker, una conexión, un envío por tick de 100 ms | [Base](BASELINE.md) |
| Envío consecutivo | `cbda68cf` | Un worker; espera solo sin trabajo elegible | [Loop](WORKER_LOOP.md) |
| Separación hexagonal | `316ae530` | Un worker tras separar paquetes | [Hexagonal](HEXAGONAL.md) |

Cada etapa tiene dos pasadas de 200 eventos con concurrencia de publicación 1 y 8: 800 por etapa, 2400 totales. Las publicaciones registradas respondieron 202 y los intentos finales 204. Es evidencia de esos casos, no exactamente una vez. Las tablas distinguen latencia de publicación y publicación-a-finalización, y drenaje breve de throughput sostenido.

## Interpretar la evidencia

JSON conserva entorno, parámetros, base, estado modificado, hashes de binarios/generador e información de compilación cuando estaba disponible. Todas las pasadas tenían árbol modificado: commit/hashes no reconstruyen por sí solos el parche fuente exacto. La documentación original completa está en Git en `35c4373`.

Comparten equipo, no controlan cachés/temperatura/carga ajena y usan cargas breves, picos muestreados y un observador que añade trabajo. Los informes delimitan muestras de scrape y contabilidad física de disco. No inferir causalidad ni tasa máxima de diferencias antes/después.

El backend actual tiene workers configurables y pool lector separado; estos resultados no validan esos cambios posteriores. Ejecutar hoy la receta mide el checkout actual, no binarios históricos. Elegir salida nueva, registrar revisión exacta y ejecutar separado del CI:

```sh
make benchmark BENCH_ARGS='--events 200 --concurrency 1 8 --payload-bytes 1024 --scrape-interval 1 --output docs/benchmarks/new-run.json'
```

El generador es específico de Linux (`/proc`) y admite hasta 10000 eventos, 64 publicaciones concurrentes y payload de 60000 bytes. Usa bases temporales y limpia procesos/logs. No ofrece validación completa de carga sostenida, TLS externo, receptores lentos o reparto entre clientes. Las claves JSON permanecen en inglés como identificadores de medición también en esta edición.
