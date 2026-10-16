# Contrato del emisor y del receptor

[English](../DELIVERY_CONTRACT.md) · Revisión documental: 2026-10-16.

[OpenAPI](openapi.yaml) define las operaciones HTTP de entrada. Esta guía explica el comportamiento que deben respetar emisores y receptores. Los campos e identificadores son datos del protocolo y no cambian entre idiomas.

## Publicar y reintentar con seguridad

`POST /api/v1/publish` requiere un token Bearer de cliente e `Idempotency-Key` de 1–128 bytes. El cuerpo es un objeto JSON con `endpoint_id`, `event_type` (1–128 bytes) y `payload` (cualquier valor JSON, incluido null). Los campos desconocidos se rechazan. La solicitud completa admite hasta 65536 bytes.

La huella idempotente cubre los bytes originales completos, no solo el payload ni JSON normalizado. Cambiar espacios puede producir 409. El ámbito es el cliente autenticado. La publicación inicial devuelve 202 con IDs estables y PENDING. Repetir los mismos bytes devuelve el recibo inicial, incluidos ciclo/estado originales; consultar GET para conocer el estado actual. Retención elimina la clave junto con el evento; después puede crear trabajo nuevo.

Tras un 503, una respuesta perdida u otro resultado incierto, conservar exactamente cuerpo y clave y reintentar con espera acotada. Un error de sincronización puede corresponder a transacción completa o ausente; cambiar la clave arriesga eventos duplicados. El 429 por cliente rechaza admisión de cola, sin prometer un intervalo concreto de reintento.

## Qué recibe el receptor

HookRelay envía POST con `Content-Type: application/json`. El cuerpo es el valor `payload` almacenado, no el sobre completo. `event_type` se guarda como metadato pero no se incluye en una cabecera saliente separada. Si el receptor lo necesita, incluirlo en el payload.

| Cabecera | Valor |
| --- | --- |
| `X-Webhook-Id` | ID estable de evento |
| `X-Webhook-Delivery-Id` | ID estable de entrega |
| `X-Webhook-Timestamp` | Segundos UNIX del envío |
| `X-Webhook-Signature` | HMAC-SHA256 hexadecimal en minúsculas |

La entrada de firma es el texto del timestamp, un punto literal y los bytes exactos del cuerpo:

```text
HMAC-SHA256(secret, timestamp + "." + body)
```

No parsear y volver a serializar JSON antes de verificar. Decodificar la firma hexadecimal, recalcular el MAC y comparar en tiempo constante. El receptor puede rechazar timestamps fuera de una tolerancia local (el receptor de prueba usa ±300 segundos). Los relojes deben estar sincronizados. Firma y timestamp válidos no impiden efectos repetidos dentro de la ventana: deduplicar IDs estables atómicamente con el procesamiento de negocio. El receptor incluido demuestra duplicados deliberadamente; no es un servicio productivo de deduplicación.

## Resultados y reintentos

| Observación | Estado/política |
| --- | --- |
| 2xx sin error de lectura de respuesta | SUCCESS |
| Error de transporte, 408, 429 o 5xx con intentos disponibles | RETRYING |
| Otro resultado HTTP o presupuesto agotado | DEAD |

El emisor lee/descarta hasta 4096 bytes de respuesta y cierra el cuerpo. No persiste respuestas. Un fallo al leer queda registrado como error sanitizado; la clasificación sigue considerando estado y reglas de error. No se siguen redirecciones. El timeout es de cinco segundos.

El envío inicial cuenta en `--max-attempts`. Los techos de backoff empiezan en 1, 2, 4 y 8 segundos y saturan a cinco minutos antes de aplicar jitter completo. Para 429/503, un único Retry-After válido (segundos enteros o fecha HTTP) puede elevar la espera hasta su tope separado. Valores ausentes, repetidos o inválidos usan backoff; fechas vencidas no añaden espera. Números enormes saturan. La demora efectiva es el máximo entre backoff y cabecera acotada. Se persiste la elegibilidad; el worker no duerme reteniendo la entrega.

## Consulta, historial y replay

El cliente consulta sus IDs conocidos, intentos y replays. Intentos/replays usan cursor numérico exclusivo `after`, orden ascendente y hasta 100 registros por página. Continuar desde el último número/ciclo hasta recibir un array vacío. Endpoints usa cursor exclusivo por ID léxico y hasta 1000 registros. Las páginas no representan un snapshot entre solicitudes.

Contadores e historial representan finalizaciones confirmadas, no cada envío iniciado. El proceso puede morir después de efectos en el receptor y antes de registrar el intento. Replay acepta solo DEAD, exige `expected_cycle` coincidente y respeta capacidad/ciclos. Conserva IDs e historial, registra al cliente autenticado como actor y abre otro ciclo. El actor identifica al cliente, no a la persona que comparte su token. El máximo de ciclos incluye el inicial.

## Interpretación de errores

La API de negocio usa JSON con `error`: 400 entrada inválida, 401 autenticación, 404 recurso no disponible/ajeno, 409 conflicto de estado/identidad, 413 publicación excesiva, 429 cuota de cliente, 503 almacenamiento/capacidad global. Los errores de decodificación de endpoint/replay, incluidos sus límites de tamaño, devuelven 400. Métricas usa token separado y puede devolver 500 en texto plano al fallar la recolección; las rutas de salud son públicas. Consultar respuestas por operación en OpenAPI.
