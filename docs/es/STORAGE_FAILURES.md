# Validación y recuperación ante fallos de almacenamiento

[English](../STORAGE_FAILURES.md) · Revisión documental: 2026-10-16. Referencia del código: `35c4373`.

La validación inicial utilizó la base `55d53f4`. La ampliación sobre `c660ce1` añade errores E/S e interrupción del WAL y corrige detección de corrupción al arrancar: ahora se comprueba estructura antes de migrar. No cambia el esquema v5 ni el contrato HTTP.

## Reproducir

```sh
# Incluido en make test / make ci; SQLite real, sin Docker.
go test -race -count=1 -timeout=60s ./internal/relay -run '^TestStorage' -v

# ENOSPC real; necesita Docker Engine local y Go.
make storage-full-test

# EIO de escritura/sincronización WAL y recuperación de transacción interrumpida.
make storage-io-test
```

GitHub Actions ejecuta regresiones deterministas/corrupción mediante `make ci`, seguidas de ambos targets obligatorios separados. Docker no disponible, fallo de compilación, error inesperado o test fallido hacen fallar `storage-full-test`. Go normal omite tests Linux de filesystem sin habilitación del runner; una omisión no demuestra ENOSPC.

El runner compila un ejecutable Linux de pruebas y monta solo ese archivo en modo lectura en un contenedor desechable sin privilegios. La raíz es de solo lectura, no hay red y un tmpfs de 4 MiB contiene bases temporales y un archivo de presión. Se comprueban tipo/tamaño del filesystem antes de llenarlo. Debe observarse ENOSPC del sistema operativo; permisos insuficientes no califican. No llena disco del host, monta datos productivos ni exige contenedores privilegiados.

## Fronteras ejercitadas

| Fallo | Punto de inyección | Resultado exigido |
| --- | --- | --- |
| Publicación `SQLITE_FULL` | Antes de insertar clave, después de evento y entrega | 503 sanitizado; grafo original intacto; sin métrica de aceptación; misma clave funciona al recuperarse y repite recibo. |
| Alta de endpoint `SQLITE_FULL` | Después de insertarlo | 503; sin endpoint retenido ni cambio parcial de propietario; reintento correcto. |
| Reserva `SQLITE_FULL` | Después de pasar a PROCESSING | Error propagado; sigue PENDING y no envía; reserva/entrega funciona al liberar presión. |
| Replay `SQLITE_FULL` | Antes de auditoría, después de actualizar ciclo | 503; ciclo/estado/auditoría intactos; reintento genera una auditoría. |
| Retención `SQLITE_FULL` | Antes de borrar evento, después de historial dependiente | Grafo completo conservado; cero eliminados; limpieza posterior elimina todo. |
| Configuración `SQLITE_FULL` | Al insertar endpoint tras desactivar configuración previa en transacción | Propietarios, autenticación y destinos previos intactos. |
| Escritor de solo lectura | `query_only` produce `SQLITE_READONLY` | 503 sin publicación; reserva fallida no envía; recuperación al quitar restricción. Modela escritor restringido, no montaje físico de solo lectura. |
| Escritor competidor | Otra conexión mantiene transacción | `SQLITE_BUSY`, 503 y sin publicación parcial; reintento funciona tras rollback. Busy timeout no garantiza esperar ese plazo en cada conflicto. |
| Finalización `SQLITE_FULL` | Inserción de historial tras actualizar entrega | Run falla y se detiene; reserva PROCESSING persiste sin intento ni métrica de éxito. Reabrir y vencer reserva permite reenviar mismo ID. |
| ENOSPC durante publicación | WAL truncado y tmpfs lleno | Error de almacenamiento, 503 y grafo intacto. Liberar espacio permite reintento; trabajo aceptado sobrevive reapertura y se entrega. |
| ENOSPC durante finalización | Llenar tmpfs tras reserva confirmada y antes de devolver 204 del transporte | Falla finalización, se detiene worker y persiste reserva sin intento/éxito. Tras liberar espacio/reabrir, recuperar reserva reenvía mismo ID y confirma SUCCESS. |

Los FULL deterministas usan el límite real `max_page_count` y triggers exclusivos de test que asignan un BLOB grande. Verifican rollback tras sentencias anteriores, no solo rechazo al inicio. No equivalen a disco físico lleno. Los tests tmpfs cubren esa frontera del SO, incluido append/commit del WAL.

Las respuestas salientes de estos tests usan transporte HTTP en proceso. Sockets, firmas y muerte de procesos reales están en smoke/failures. Los fixtures vencen reservas para evitar esperar 30 segundos; producción espera vencimiento real, probado aparte con SIGKILL. La recuperación verifica integridad, referencias, IDs e historial. Dos envíos tras finalización fallida pueden corresponder legítimamente a un intento persistido.

## Corrupción, EIO y WAL interrumpido

### Detección de corrupción al arrancar

Antes se abría una base con índice `eligible` corrupto: revisar claves foráneas no inspeccionaba esa estructura. `storage.Open` ejecuta `PRAGMA quick_check(1)` antes de journal/migraciones. La regresión corrompe offline una página raíz de índice en fixtures v4/v5 y exige rechazo sin modificar el archivo principal. V4 demuestra rechazo antes de migrar. El diagnóstico no se copia a readiness correcto.

Es comprobación estructural inicial, no reparación ni revisión continua. Su coste depende del tamaño. [quick_check omite comprobaciones UNIQUE y coherencia índice/contenido](https://www.sqlite.org/pragma.html#pragma_quick_check); backup offline sigue usando integridad completa y referencias. Un bit alterado en payload con estructura válida puede no detectarse. No se añadió checksum criptográfico ni migración.

La regresión de backup corrompe una página real de índice, exige fallo de backup/restauración desde fuente corrupta sin publicar ni modificar origen, y restaura una copia sana intacta. Los rechazos no dejan temporales.

### Errores de E/S del kernel sobre WAL

`make storage-io-test` ejecuta cuatro casos hijos: escritura y sincronización durante publicación y después de aceptación del receptor. Un filtro seccomp restrictivo devuelve EIO para `pwrite64` o `fsync`/`fdatasync` en descriptores WAL de prueba. Se exigen códigos exactos `SQLITE_IOERR_WRITE` (778) y `SQLITE_IOERR_FSYNC` (1034); otro fallo o inyección ausente no aprueba.

El filtro cubre todos los hilos y descriptores WAL de lectura/escritura; SQLite puede abrir uno incluso desde el pool SQL de lectura. Solo existe en cada hijo desechable. Se conservan restricciones normales del contenedor, sin privilegios extra ni bases del host. Falta de soporte del kernel hace fallar el target, no omitirlo.

La publicación devuelve 503 sanitizado y no incrementa aceptación. Fallar finalización propaga error sin éxito confirmado. El padre reabre sin filtro y verifica integridad, propiedad, trabajo aceptado e idempotencia.

Una sincronización fallida es un **commit incierto**. Recuperación debe permitir transacción completa o ausente, nunca historial parcial. Si ya existe SUCCESS con intento no hace falta replay; si persiste PROCESSING sin intento, vencer reserva permite reenviar mismo ID. Por eso se repiten bytes/clave después de publicación incierta, no se cambia la clave.

### Escrituras interrumpidas y cola incompleta

Dos casos matan un hijo en barrera explícita después de volcar páginas sin commit al WAL. Antes se acepta otra publicación con FULL. Se exige prefijo WAL confirmado seguido de frames no confirmados. Un caso deja la cola intacta; otro corta 13 bytes del último frame sin commit, solo sobre fixture offline.

Reabrir conserva ambas publicaciones aceptadas y sus recibos, descarta el grafo no confirmado, supera integridad/referencias y permite entrega. Complementa la [frontera de commit del WAL](https://www.sqlite.org/wal.html#how_wal_works) y recuperación real de reservas. El fixture SQL grande omite deliberadamente el límite público de payload solo para forzar volcado de caché.

Simula interrupción y escrituras incompletas seleccionadas. No corta energía, descarta caché del kernel, emula caché de disco deshonesta ni certifica durabilidad eléctrica.

## Respuesta operativa

1. Tratar 503, especialmente tras E/S/sync, como publicación fallida/incierta; conservar bytes/clave y reintentar con backoff acotado. Perder respuesta también puede seguir a commit correcto; misma clave resuelve ambigüedad mientras se conserve.
2. Revisar logs, proceso y espacio. Fallos de envío/mantenimiento se propagan desde Run y detienen con error. Revisar reinicios repetidos del supervisor. Readiness solo hace Ping: no demuestra escritura ni espacio.
3. Pausar emisores y detener dispatcher antes de mantener filesystem. Liberar archivos ajenos desechables, ampliar espacio o corregir permisos/montaje. **No borrar ni truncar DB, WAL o SHM.** Retención necesita escritura y no rescata necesariamente disco lleno.
4. Recuperado espacio/escritura, seguir [backup offline](OPERATIONS.md#backup-offline-y-restauración) a ruta nueva con capacidad. Valida integridad, referencias y esquema requerido. Si falla, preservar original e investigar; no reescribir filas.
5. Reiniciar con base, identidades, configuración, credenciales y flags originales. Verificar salud, métricas y entregas. Reservas pendientes vuelven a ser elegibles al vencer su plazo persistido (normalmente 30 segundos desde reserva). No poner plazos a cero manualmente en producción.
6. Receptores deduplican IDs: el efecto puede preceder al fallo de persistir resultado. DEAD requiere replay explícito auditado; reiniciar no renueva presupuestos.

## Límites de evidencia

Pasaron nueve casos deterministas y dos ENOSPC con pools separados. La ampliación añade cuatro EIO exactos, dos WAL interrumpidos, corrupción v4/v5 y backup/restauración de corrupción. El registro del 2026-10-15 informa PASS de `make ci`, `make storage-full-test`, `make storage-io-test`, `make lint`, `make coverage` y `git diff --check`. `make vulncheck` terminó sin vulnerabilidades alcanzables conocidas. Son resultados históricos, no ejecuciones de esta revisión documental.

Pérdida eléctrica literal, dispositivo/controlador defectuoso, confirmación falsa de flush, corrupción arbitraria de sectores y todos los subtipos SQLITE_IOERR no están cubiertos. Requieren datos desechables y hardware/laboratorio que modele el fallo previsto. No hacer cortes abruptos sobre estación de trabajo o despliegue vivo. Carga sostenida y políticas TLS/secretos/alertas/backup siguen siendo validaciones separadas.
