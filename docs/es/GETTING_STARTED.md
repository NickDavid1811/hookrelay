# Primeros pasos

[English](../GETTING_STARTED.md) · Revisión documental: 2026-10-16. Referencia del código: `35c4373`.

Ejecutar los comandos desde la raíz del repositorio. Para una introducción visual, usar el [laboratorio](PLAYGROUND.md). Los ejemplos utilizan credenciales públicas solo para uso local y el receptor incluido en el repositorio.

## Demostración con Docker

Con Docker Engine y Compose disponibles:

```sh
docker compose up --build -d --wait
```

La API se publica en el loopback del host en `http://127.0.0.1:8080`. Compose inicia el receptor de prueba y conserva SQLite en un volumen con nombre. Para detener sin borrar datos:

```sh
docker compose down
```

Esta demostración no es una receta de producción. Un despliegue real necesita credenciales privadas, filesystem local persistente y protección TLS; consultar [operación](OPERATIONS.md).

## Ejecutar desde el código

Usar la versión Go indicada en `go.mod` y Python 3.10+ para los scripts de procesos. Node 24/npm solo son necesarios para compilar la web y ejecutar el CI completo. Compilar:

```sh
make build
```

Iniciar el receptor en una terminal:

```sh
export HOOKRELAY_SECRET='local-secret-change-me'
./bin/receiver
```

Iniciar el dispatcher en otra terminal:

```sh
export HOOKRELAY_TOKEN='local-token-change-me'
export HOOKRELAY_SECRET='local-secret-change-me'
./bin/dispatcher --allow-loopback-test
```

El destino local necesita el permiso explícito de loopback. Las direcciones predeterminadas son API `127.0.0.1:8080` y receptor `127.0.0.1:9090`; SQLite se guarda en `hookrelay.db`. Detener cada proceso con Ctrl+C. Conservar juntos la base y sus archivos WAL/SHM hasta un cierre limpio.

## Publicar y consultar

```sh
curl -i http://127.0.0.1:8080/api/v1/publish \
  -H 'Authorization: Bearer local-token-change-me' \
  -H 'Idempotency-Key: getting-started-1' \
  -H 'Content-Type: application/json' \
  --data-binary '{"endpoint_id":"test","event_type":"order.completed","payload":{"order_id":"ord_1"}}'
```

La respuesta 202 incluye `event_id`, `delivery_id` y el recibo inicial. Copiar `delivery_id` en las siguientes URLs, sustituyendo `DELIVERY_ID`:

```sh
curl -sS http://127.0.0.1:8080/api/v1/deliveries/DELIVERY_ID \
  -H 'Authorization: Bearer local-token-change-me'
curl -sS http://127.0.0.1:8080/api/v1/deliveries/DELIVERY_ID/attempts \
  -H 'Authorization: Bearer local-token-change-me'
```

Normalmente el receptor devuelve 204 y la entrega pasa a SUCCESS. Repetir la misma clave con exactamente los mismos bytes devuelve el recibo inicial; cambiar los bytes produce 409. Consultar el estado por separado. Un 202 no demuestra entrega.

## Si el ejemplo falla

Comprobar puertos libres, ambos procesos activos, secretos iguales y token de al menos 16 bytes. El secreto del receptor y el del endpoint deben coincidir. Un 401 corresponde a autenticación; un 404 puede indicar un endpoint no disponible o una entrega de otro cliente. Un 503 puede indicar almacenamiento o capacidad global. Conservar los bytes y la clave originales al reintentar una publicación incierta. Consultar [configuración](CONFIGURATION.md), [contrato](DELIVERY_CONTRACT.md) y [respuesta a fallos](STORAGE_FAILURES.md).
