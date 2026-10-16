# Operación y recuperación

[English](../OPERATIONS.md) · Revisión documental: 2026-10-16.

Esta guía describe procedimientos soportados y significado de señales. No certifica un despliegue productivo. Ejecutar comandos desde la raíz y adaptar rutas a la instalación elegida.

## Despliegue y arranque

Usar una instancia del dispatcher con SQLite en filesystem local persistente. La API escucha por defecto en loopback literal; escuchar fuera requiere `--insecure-allow-remote-listen` y no incluye TLS. Proteger el tráfico mediante reverse proxy TLS configurado adecuadamente o un límite de despliegue privado. Compose es una demostración con credenciales públicas, no esa configuración productiva.

Mantener privados tokens de cliente, secretos y credencial de métricas. La autenticación guarda hashes de tokens; SQLite contiene payloads y secretos HMAC dinámicos, estos últimos en texto plano. Proteger directorio y backups además del archivo principal. Conservar configuración, referencias de secretos y flags aparte del backup de base.

El arranque comprueba estructura, migra, aplica configuración y valida destinos persistidos antes de servir. Una fase posterior puede fallar cuando la base ya fue migrada/configurada; un arranque fallido no implica archivos intactos. El listener se abre antes de iniciar workers. Investigar con logs y estado del proceso. No hay downgrade automático: conservar backup offline anterior y binario/configuración compatibles. Evitar abrir un esquema nuevo con un binario antiguo.

SIGINT/SIGTERM cancela trabajo, cierra HTTP y espera workers/mantenimiento antes de cerrar la base. Un envío cancelado puede dejar reserva recuperable. Los fallos de almacenamiento en envío o mantenimiento detienen el proceso con error; el supervisor debe hacer visibles los reinicios repetidos.

## Salud y monitorización

| Señal | Interpretación |
| --- | --- |
| `/healthz` | HTTP responde; no demuestra progreso de workers |
| `/readyz` | Ping SQLite funciona; no demuestra escritura, espacio ni acceso al receptor |
| `hookrelay_queue_depth` | Todo el trabajo sin terminar, incluido el pausado |
| `hookrelay_queue_oldest_age_seconds` | Edad desde publicación del trabajo pendiente más antiguo |
| `hookrelay_queue_paused` | Trabajo pendiente de clientes/endpoints inactivos |
| `hookrelay_expired_leases` | Reservas PROCESSING vencidas actuales, no recuperaciones acumuladas |
| `hookrelay_worker_running` | Bucle Run activo (0/1), no número de workers ni prueba de progreso |
| `hookrelay_delivery_http_requests_total` | Solicitudes salientes observadas, incluidas las no finalizadas |
| `hookrelay_delivery_results_total` | Resultados confirmados por este proceso |
| `hookrelay_publications_total` | Publicaciones nuevas confirmadas y recibos idempotentes aceptados |
| `hookrelay_cleanup_last_success_timestamp_seconds` | Último lote correcto del proceso; cero antes del primero |

Habilitar `/metrics` con `HOOKRELAY_METRICS_TOKEN` separado. El scrape usa `Authorization: Bearer TOKEN`; sin token configurado devuelve 404, credencial inválida devuelve 401 y un fallo de recolección puede producir 500 en texto plano. El ejemplo Prometheus está en `config/prometheus.example.yml`.

Contadores e histogramas se reinician con el proceso. Los gauges reflejan datos retenidos; retención reduce esos conteos pero no decrementa contadores. El snapshot usa una transacción que no comparte con los contadores en memoria. El histograma HTTP incluye lectura/cierre acotados, excluyendo finalización SQLite. Los contadores API cubren publish/replay, no todas las rutas. No se emiten etiquetas de alta cardinalidad por cliente/URL/ID por defecto.

Vigilar el espacio físico por separado. Las cuotas de payload excluyen índices, historiales, WAL, SHM, backups y sobrecoste del filesystem. Limpiar requiere escrituras y no rescata necesariamente un disco ya lleno. Los umbrales dependen de la carga medida; el repositorio aporta métricas, no un despliegue completo de alertas.

## Backup offline y restauración

1. Pausar emisores, detener el dispatcher y confirmar su salida. Guardar configuración, secretos y flags por separado.
2. Elegir nombres nuevos con directorios padre existentes. No copiar solo la base principal activa en WAL ni descartar archivos auxiliares.
3. Crear y validar la copia con la herramienta:

```sh
mkdir -p ./backups ./restored
python3 scripts/backup.py backup ./hookrelay.db ./backups/snapshot.db
python3 scripts/backup.py restore ./backups/snapshot.db ./restored/hookrelay.db
```

La Backup API incluye datos WAL confirmados. La herramienta valida integridad, referencias y objetos requeridos de esquemas 1–5 mediante `scripts/backup_schema.json`. Publica mediante hard link desde un archivo temporal sin reemplazar destinos/auxiliares, sincroniza archivo/directorio y produce modo 0600. Requiere soporte de hard links. No detiene procesos, cifra, autentica el origen ni certifica objetos extra arbitrarios. Una base estructuralmente válida no demuestra corrección de datos de negocio.

4. Arrancar solo la copia elegida con identidades, configuración y credenciales originales. Cambiar `--db` a la ruta restaurada. No ejecutar ambas copias como par activo.
5. Verificar salud, historiales, métricas y trabajo reanudado antes de reactivar emisores. Conservar el original hasta terminar. Las reservas restauradas mantienen sus plazos; dejar que venzan naturalmente. Una copia antigua puede reenviar trabajo ya procesado.

Aunque la Backup API lee WAL activo coherentemente, los escritores ocupados pueden demorarla. El procedimiento soportado es offline. Antes de migrar, hacer backup; recuperar/downgradear requiere snapshot y configuración compatibles, no editar el esquema manualmente.

## Incidentes y límites

Ante 503 o publicación incierta, conservar bytes/clave y reintentar con espera acotada. Ante fallos de almacenamiento, detener emisores/proceso, revisar logs/filesystem y seguir [recuperación](STORAGE_FAILURES.md). No borrar/truncar auxiliares para liberar espacio. Conservar la entrada corrupta para investigar; restaurar una copia sana validada a ruta nueva.

DEAD requiere replay auditado; reiniciar no restablece intentos. El trabajo pausado necesita restaurar configuración. Los receptores deben deduplicar IDs tras reintentos/restauración. Pérdida eléctrica, fallo físico, corrupción arbitraria y políticas TLS/backup/secretos del despliegue necesitan evidencia separada; los tests históricos no las demuestran.
