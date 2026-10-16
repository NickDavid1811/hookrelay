# Getting started

[Español](es/GETTING_STARTED.md) · Documentation review: 2026-10-16. Code reference: `35c4373`.

Run commands from the repository root. For a visual introduction, use the [Flow Lab](PLAYGROUND.md). These examples use public local-only credentials and a receiver included in the repository.

## Docker demonstration

With Docker Engine and Compose available:

```sh
docker compose up --build -d --wait
```

The API is published on host loopback at `http://127.0.0.1:8080`. Compose starts the test receiver and retains SQLite in a named volume. To stop without deleting data:

```sh
docker compose down
```

This demonstration is not a production deployment recipe. Real deployment needs private credentials, a persistent local filesystem and TLS protection; see [operations](OPERATIONS.md).

## Run from source

Use the Go version in `go.mod` and Python 3.10+ for the process scripts. Node 24/npm are needed only for the web build and full CI. Build:

```sh
make build
```

Start the receiver in one terminal:

```sh
export HOOKRELAY_SECRET='local-secret-change-me'
./bin/receiver
```

Start the dispatcher in another terminal:

```sh
export HOOKRELAY_TOKEN='local-token-change-me'
export HOOKRELAY_SECRET='local-secret-change-me'
./bin/dispatcher --allow-loopback-test
```

The explicit loopback permission is required for the local destination. Default addresses are API `127.0.0.1:8080` and receiver `127.0.0.1:9090`; SQLite is `hookrelay.db`. Stop each process with Ctrl+C. Keep database and WAL/SHM files together until clean shutdown.

## Publish and inspect

```sh
curl -i http://127.0.0.1:8080/api/v1/publish \
  -H 'Authorization: Bearer local-token-change-me' \
  -H 'Idempotency-Key: getting-started-1' \
  -H 'Content-Type: application/json' \
  --data-binary '{"endpoint_id":"test","event_type":"order.completed","payload":{"order_id":"ord_1"}}'
```

A 202 response contains `event_id`, `delivery_id` and the initial receipt. Copy `delivery_id` into the following URLs, replacing `DELIVERY_ID`:

```sh
curl -sS http://127.0.0.1:8080/api/v1/deliveries/DELIVERY_ID \
  -H 'Authorization: Bearer local-token-change-me'
curl -sS http://127.0.0.1:8080/api/v1/deliveries/DELIVERY_ID/attempts \
  -H 'Authorization: Bearer local-token-change-me'
```

The receiver normally returns 204 and the delivery becomes SUCCESS. Reusing the same key with exactly the same request bytes returns the initial receipt; changing the bytes returns 409. Query delivery state separately. A 202 does not prove delivery.

## If the example fails

Check that ports are free, both processes are still running, secrets match and the token is at least 16 bytes. The receiver secret and endpoint secret must be the same. A 401 concerns authentication; a 404 may indicate an unavailable endpoint or a delivery owned by another client. A 503 may indicate storage or global capacity failure. Preserve the original bytes and key when retrying an uncertain publication. See [configuration](CONFIGURATION.md), [delivery contract](DELIVERY_CONTRACT.md) and [storage response](STORAGE_FAILURES.md).
