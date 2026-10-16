# Historial técnico — 2026-10-13 a 2026-10-15

[English](../../history/SESSION.md) · Revisión documental: 2026-10-16. Referencia del código: `35c4373`.

Este registro histórico consolidado no es especificación vigente ni lista activa de tareas. Restricciones y sugerencias anteriores pueden estar superadas. Las entradas originales completas se recuperan con `git show 35c4373:docs/SESSION.md`. El comportamiento actual está en [panorama](../CONTEXT_MASTER.md), [arquitectura](../ARCHITECTURE.md) y [mantenimiento](../MAINTAINING.md).

## 2026-10-13: base de entrega

El flujo inicial usaba Go, HTTP estándar, driver SQLite puro Go, WAL/FULL y una conexión serializada. Confirmaba evento/entrega/idempotencia juntos, firmaba bytes, reservaba con token/plazo y registraba resultados atómicamente. Los IDs eran aleatorios de 128 bits en hexadecimal, no UUIDv7. Había un destino loopback fijo y un cliente; la API no recibía URLs.

Validación inicial registrada: tests Go, carreras, vet, compilación y smoke HTTP real aprobados. Reabrir/reservar no demostraba pérdida eléctrica. Un obstáculo temporal de workspace de solo lectura se resolvió trasladando el repositorio a su ubicación actual; es una restricción histórica.

## 2026-10-13: fallos de procesos reales (base 2063ea5)

El receptor añadió modos slow/503/429/drop y logs sincronizados. La suite reprodujo recuperación transitoria, agotamiento, respuesta perdida y SIGKILL durante envío. Conservó el vencimiento real de 30 segundos.

| Escenario | Estado final | Intentos finalizados | Recepciones | Efectos simulados |
| --- | --- | ---: | ---: | ---: |
| Primer 503 y después 204 | SUCCESS | 2 | 2 | 1 |
| Primer 429 y después 204 | SUCCESS | 2 | 2 | 1 |
| Timeout tras procesar | SUCCESS | 2 | 2 | 2 |
| Respuesta perdida tras procesar | SUCCESS | 2 | 2 | 2 |
| Cinco 503 | DEAD | 5 | 5 | 0 |
| SIGKILL durante envío y reinicio | SUCCESS | 1 | 2 | 2 |

Los IDs se conservaron. El envío interrumpido existía en el receptor pero no en historial de intentos. Pasaron carreras/vet/build/smoke/fallos; no se afirmó exactamente una vez.

## 2026-10-13: replay y esquema v2 (base bf39d12)

Replay de DEAD exigía expected_cycle coincidente, conservaba IDs/historial y confirmaba ciclo/auditoría juntos. El actor era cliente autenticado, no persona. Historial usaba cursores exclusivos acotados. Migración v1→v2 conservaba intentos y rechazaba versiones futuras. Se probaron ciclos, concurrencia, ciclo atrasado, rollback y migración. Pasaron controles locales y replay real; no demostraban durabilidad física.

## 2026-10-13: backup/restauración y CI

La herramienta offline usó Backup API Python, integridad/referencias, publicación temporal atómica sin reemplazo, modo 0600 y sincronización de archivo/directorio. Probó WAL, corrupción, protección de destino y replay/idempotencia restaurados. Conservaba reservas y exigía configuración/secretos separados. Revisiones posteriores reforzaron validación de esquema.

CI introdujo `make ci`, procesos secuenciales y acciones fijadas. Crear workflow local no demostraba ejecución remota hasta observarla. Backup y CI registraron controles locales aprobados; las notas originales distinguían cambios sin commit de ejecución remota.

## 2026-10-13: Retry-After (base a54ba8f)

429/503 admitía segundos o fechas HTTP con tope separado y máximo entre cabecera y backoff con jitter. Inválidos/repetidos usaban backoff. Elegibilidad persistía al reabrir; no se retenía entrega durmiendo. Números enormes saturaban. Pasaron tests de política, HTTP/reapertura, tiempos de proceso y CI local completo.

## 2026-10-13: clientes, endpoints, capacidad y métricas

V3 añadió clientes, endpoints, idempotencia por cliente y restricciones de propiedad. Configuración persistía identidades y pausaba trabajo omitido. HTTPS/DNS/conexión bloqueaba destinos internos, redirecciones y proxies; procesos verificaban aislamiento. V4 añadió fechas/índices terminales para retención. Cuotas rechazaban admisión atómicamente, protegían grafos pendientes y limitaban replay/payload. Backup evolucionó con esquema.

Métricas separó gauges persistidos, contadores del proceso, observaciones HTTP y resultados confirmados. Scrape usaba token separado. Se probaron snapshots, pausas, reservas vencidas, limpieza y concurrencia. Los contadores se reiniciaban con el proceso. Se informaron controles locales completos aprobados; no era evidencia sostenida de producción.

## 2026-10-13: línea base y procesamiento consecutivo

La línea base completó 800 eventos y drenaba unos 10/s por enviar solo uno por tick de 100 ms. Eliminar espera con trabajo disponible completó otros 800 a unos 96–181/s en pruebas locales cortas. Cambiaron CPU y latencia; no se estableció causalidad ni capacidad máxima. Consultar [base](../benchmarks/BASELINE.md) y [loop](../benchmarks/WORKER_LOOP.md).

## 2026-10-13: separación hexagonal y validación

Se separaron core/aplicación/adaptadores conservando transacciones y fachada relay. Tests comprobaron dependencias, puertos, cancelación, reintentos y un flujo SQLite/adaptadores HTTP sin sockets.

Al principio se bloquearon sockets locales: compilación/tests seleccionados aprobaron, pero no sustituían CI completo. Tras nueva sesión con loopback permitido, pasó CI sin omitir tests. Dos benchmarks completaron 800 eventos con menor drenaje y mayor P99 concurrente observados que antes. No se determinó causa. Consultar [informe hexagonal](../benchmarks/HEXAGONAL.md).

## 2026-10-14: laboratorio y UX

El laboratorio compuso dispatcher real aislado y receptor con seis escenarios, replay y exportación. Tokens permanecían en Go; observaciones, en memoria. React/TypeScript usaba funcionalidades, consultas remotas y reducer puro visual. CI añadió build/tests de modelo y smoke de procesos.

La revisión UX añadió tres pasos, seis etapas explicativas seleccionables, lectura manual, móvil, texto mayor y controles de dibujo separados. Pasaron comprobaciones históricas de navegador/modelo/procesos. No fue investigación de comprensión ni certificación de accesibilidad. Consultar [revisión UX](PLAYGROUND_UX.md).

## 2026-10-15: concurrencia y endpoints dinámicos

Se añadieron 1–50 intentos/workers, permiso de escucha remota, pool lector y secretos persistidos v5. La API creó destinos; después se retiraron doble escritura runtime/SQLite y fallback de secretos. Se corrigieron formato e importación/exportación TypeScript de CI.

### Correcciones al relato original

La entrada antigua decía incorrectamente que v5 creaba `dynamic_endpoints`; añade `secret` a `endpoints`. La creación usa `id`, no `endpoint_id`. Generar secreto produce 256 bits aleatorios, no 128. Longitud mínima no garantiza entropía. Microsegundos y altas tasas de adición no tenían benchmark específico. Registry/caché/fallback antiguos están superados, no son contratos actuales. El detector de carreras no demuestra exhaustivamente la concurrencia.

## 2026-10-15: corrección del backend (base f1d41bc)

Validación de política dejó de mutar runtime; creación/cuotas fueron transaccionales y resolución consultó almacenamiento confirmado. Arranque validaba explícitamente. El operador controlaba loopback. Borrado se coordinaba con publicación/reserva; limpieza recuperaba cuota al vencer referencias. Backoff evitaba overflow; DSN aplicaba pragmas por conexión y apagado esperaba workers/mantenimiento. Errores operativos terminaban con fallo. Se informaron CI completo y regresiones aprobados; no equivalencia de rendimiento del lookup nuevo.

La [revisión posterior](AUDIT-2026-10-15.md), base `38b6819`, detalla Compose, API, paginación, borrado estático, esquema backup, smoke aislado, herramientas y documentación.

## 2026-10-15: almacenamiento y corrupción

Sobre `55d53f4`, nueve casos FULL/lectura/contención y dos ENOSPC verificaron rollback, errores sanitizados, métricas confirmadas y recuperación. Sobre `c660ce1`, se rechazó índice corrupto antes de migrar; se añadieron cuatro EIO WAL exactos, dos WAL interrumpidos y backup de corrupción. Se informaron CI completo, ambos targets Docker, lint, cobertura y vulnerabilidades aprobados esa fecha. Son experimentos de software, no certificación eléctrica/física. Consultar [fallos](../STORAGE_FAILURES.md).

## 2026-10-16: consolidación documental

Se redactaron guías vigentes en inglés y español, separando evidencia histórica de instrucciones, reconciliando API y preservando archivos de medición. Este cambio editorial no implementa el plan de desarrollo anterior, publica release ni añade benchmark. Se retiraron propuestas obsoletas y notas de permisos/commits del contexto vigente; su texto completo permanece en Git.
