# Configuración

[English](../CONFIGURATION.md) · Revisión documental: 2026-10-16.

Ejecutar comandos desde la raíz. `./bin/dispatcher --help` es la referencia del ejecutable; los flags se declaran en `cmd/dispatcher/main.go` y sus valores y validaciones en `internal/core/limits.go`.

## Clientes y endpoints estáticos

Sin `--config`, el dispatcher crea el cliente local `local-client` y el endpoint `test`, leyendo `HOOKRELAY_TOKEN` y `HOOKRELAY_SECRET`. El destino predeterminado es local y requiere `--allow-loopback-test`.

Para configuración explícita:

```sh
./bin/dispatcher --config config/clients.example.json
```

El ejemplo referencia variables de entorno; definir valores privados antes de iniciar. Usa dominios de ejemplo, no destinos reales. Un documento tiene esta forma:

```json
{"clients":[{"id":"billing","token_env":"BILLING_TOKEN","endpoints":[{"id":"invoice-events","url":"https://receiver.example.com/webhook","secret_env":"BILLING_WEBHOOK_SECRET","allow_loopback_test":false}]}]}
```

El archivo admite hasta 1 MiB y un documento JSON; rechaza campos desconocidos. Requiere de 1 a 100 clientes. Los IDs de clientes y endpoints estáticos usan de 1 a 64 letras ASCII, dígitos, `_` o `-`. Los IDs de endpoint son globalmente únicos. Los nombres de variables usan el formato de identificador validado; tokens y secretos HMAC requieren de 16 a 4096 bytes. Los tokens deben ser distintos entre clientes y del token de métricas. La longitud por sí sola no garantiza entropía.

El arranque reconcilia la configuración atómicamente. Los clientes y endpoints estáticos omitidos quedan inactivos. Su trabajo pendiente se pausa, no se borra. Restaurar la misma identidad/configuración permite reanudarlo. URL, propietario, referencia de entorno del secreto y política loopback existentes son inmutables; usar un nuevo ID para cambiarlos. Cambiar el valor de la misma variable de secreto/token tiene efecto al reiniciar, sin protocolo de rotación con claves superpuestas. No hay recarga en caliente.

## Endpoints dinámicos

`POST /api/v1/endpoints` acepta `id`, `url`, `secret` opcional y los flags loopback descritos en [OpenAPI](openapi.yaml). Un ID vacío/omitido genera `ep_` y 16 caracteres hexadecimales. Un secreto vacío/omitido genera `hrsec_` y 256 bits aleatorios en hexadecimal. La respuesta de creación incluye el secreto asignado; los GET posteriores no lo devuelven. SQLite y sus backups guardan secretos dinámicos en texto plano.

La solicitud loopback del cliente también requiere `--allow-loopback-test` del operador. Los destinos externos normales requieren HTTPS en puerto 443 y direcciones públicas. La configuración estática puede autorizar explícitamente loopback literal. No se siguen redirecciones ni proxies de entorno; DNS se comprueba al conectar. No existe soporte general para destinos de redes privadas.

Los endpoints dinámicos son inmutables por API. DELETE solo los desactiva sin entregas PENDING, RETRYING o PROCESSING. Borrar por API un endpoint de configuración devuelve 409. Retención elimina después los dinámicos inactivos sin referencias; conserva identidades estáticas. Las cuotas cuentan registros retenidos, incluidos inactivos, no solo los activos visibles.

## Parámetros de ejecución

| Flag | Valor inicial | Significado |
| --- | --- | --- |
| `--db` | `hookrelay.db` | Ruta local SQLite |
| `--listen` | `127.0.0.1:8080` | IP literal y puerto; loopback por defecto |
| `--endpoint` | `http://127.0.0.1:9090/webhook` | Destino en modo local heredado |
| `--config` | vacío | Configuración JSON explícita de arranque |
| `--allow-loopback-test` | false | Permiso del operador para destinos loopback de prueba |
| `--insecure-allow-remote-listen` | false | Permite escuchar fuera de loopback sin TLS integrado |
| `--workers` | 1 | Workers concurrentes, 1–50 |
| `--max-attempts` | 5 | Intentos por ciclo, 1–50; incluye el envío inicial |
| `--retry-after-max` | 5m | Tope de Retry-After; positivo y hasta 24h |
| `--max-queue` | 10000 | Máximo global de entregas sin terminar |
| `--max-client-queue` | 1000 | Máximo de entregas sin terminar por cliente |
| `--max-events` | 20000 | Máximo de eventos retenidos |
| `--max-payload-bytes` | 268435456 | Máximo de bytes de payload retenidos; excluye sobrecoste físico de la base |
| `--max-endpoints` | 1000 | Máximo global de endpoints retenidos |
| `--max-client-endpoints` | 100 | Máximo de endpoints retenidos por cliente |
| `--max-cycles` | 10 | Ciclos totales incluido el inicial; 1–1000 |
| `--success-retention` | 168h | Retención desde SUCCESS |
| `--dead-retention` | 720h | Retención desde DEAD |
| `--cleanup-interval` | 1m | Intervalo entre lotes de limpieza |
| `--cleanup-batch` | 100 | Grafos de eventos por transacción de limpieza; 1–1000 |

La cola incluye PENDING, RETRYING y PROCESSING. Superar capacidad por cliente devuelve 429; superar capacidad global/almacenamiento devuelve 503. Son cuotas de admisión, no controles de solicitudes por segundo. Los límites de cola/eventos/endpoints/payload deben ser positivos; retención e intervalo de limpieza, de al menos un segundo. Bajar una cuota no elimina datos existentes.

Definir `HOOKRELAY_METRICS_TOKEN` con una credencial de operador separada de 16–4096 bytes para habilitar `/metrics`; sin ella devuelve 404. Esta documentación no configura credenciales. Consultar [operación](OPERATIONS.md) para monitorización, almacenamiento y despliegue.
