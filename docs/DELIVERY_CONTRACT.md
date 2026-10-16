# Publisher and receiver contract

[Español](es/DELIVERY_CONTRACT.md) · Documentation review: 2026-10-16. Code reference: `35c4373`.

[OpenAPI](openapi.yaml) defines the inbound HTTP operations. This guide explains the behavior a publisher and receiver must preserve. Field names and identifiers are protocol data and are identical in both language editions.

## Publish and retry safely

`POST /api/v1/publish` requires a client Bearer token and an `Idempotency-Key` of 1–128 bytes. The body is one JSON object with `endpoint_id`, `event_type` (1–128 bytes) and `payload` (any JSON value, including null). Unknown envelope fields are rejected. The entire request body is limited to 65536 bytes.

The idempotency fingerprint covers complete original request bytes, not just the payload or normalized JSON. Whitespace changes can therefore produce a 409. Scope is the authenticated client. A successful initial publication returns 202 with stable event/delivery IDs and PENDING. A byte-identical retry returns the original acceptance receipt, including its initial cycle/state; GET the delivery to learn current state. Retention deletes the key with its event, after which the key can create new work.

After 503, a lost response or another uncertain outcome, retain the exact original body and key and retry with bounded backoff. A storage synchronization error can mean a complete commit or no commit; choosing a new key risks duplicate events. Client 429 is a queue-admission rejection, not a promise of a particular retry interval.

## What the receiver gets

HookRelay sends POST with `Content-Type: application/json`. The body is the stored `payload` value, not the full publication envelope. `event_type` is stored as event metadata but is not included in a separate outgoing header. If the receiver needs that type, include it in the payload.

| Header | Value |
| --- | --- |
| `X-Webhook-Id` | Stable event ID |
| `X-Webhook-Delivery-Id` | Stable delivery ID |
| `X-Webhook-Timestamp` | UNIX seconds for the send |
| `X-Webhook-Signature` | Lowercase hexadecimal HMAC-SHA256 |

The signature input is the timestamp text, a literal period and the exact body bytes:

```text
HMAC-SHA256(secret, timestamp + "." + body)
```

Do not parse and reserialize JSON before verifying. Decode the hex signature, recompute the MAC and use constant-time comparison. A receiver can reject timestamps outside a local tolerance window (the test receiver uses ±300 seconds). Clock synchronization matters. A valid timestamp/signature does not prevent repeated effects inside that window: deduplicate stable IDs atomically with business processing. The included receiver deliberately demonstrates duplicates and is not a production deduplication service.

## Outcomes and retries

| Observation | State/policy |
| --- | --- |
| 2xx with no response-read error | SUCCESS |
| Transport error, 408, 429 or 5xx with attempts remaining | RETRYING |
| Other HTTP result, or exhausted retry budget | DEAD |

The sender reads/discards at most 4096 response bytes and closes the body. Response bodies are not persisted. A response-read failure is recorded as a sanitized error; classification still uses the received status and error rules. Redirects are not followed. Request timeout is five seconds.

The initial send counts toward `--max-attempts`. Backoff ceilings begin at 1, 2, 4 and 8 seconds and saturate at five minutes before full jitter. For 429/503, one valid Retry-After value (integer seconds or HTTP date) can raise the delay up to its separate configured ceiling. Missing, repeated or invalid values fall back to backoff; expired dates do not delay. Huge digit-only values saturate. Effective delay is the larger of backoff and the bounded header delay. Eligibility is persisted; the worker does not sleep holding the delivery.

## Query, history and replay

Clients can query their own known delivery IDs, attempts and replays. Attempts/replays use exclusive numeric `after` cursors, ascending order and at most 100 records per page. Continue with the last number/cycle until an empty array. Endpoints use an exclusive lexical ID cursor and at most 1000 records. Pages are not a snapshot across requests.

Attempt counts/history represent committed finalizations, not every network send started. A process may die after receiver effects but before recording an attempt. Replay accepts DEAD only, requires matching `expected_cycle` and respects capacity/cycle limits. It preserves event/delivery IDs and prior history, records the authenticated client as actor, and opens a new cycle. The actor identifies a client, not an individual sharing its token. Maximum cycles includes the initial cycle.

## Error interpretation

Business API errors use JSON with an `error` string: 400 invalid input, 401 authentication, 404 unavailable/not owned, 409 state/identity conflict, 413 oversized publication, 429 client quota, 503 storage/global capacity. Endpoint/replay body decoding errors, including their size ceilings, return 400. Metrics has a separate token and may return a plain-text 500 on collection failure; health routes are public. See OpenAPI for operation-specific responses.
