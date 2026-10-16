# Panorama y alcance del proyecto

[English](../CONTEXT_MASTER.md) · Revisión documental: 2026-10-16.

HookRelay es un servicio autohospedado de envío de webhooks y un proyecto de ingeniería backend. Recibe eventos, persiste el trabajo de entrega, envía solicitudes HTTP y registra resultados para investigar fallos y reenviar entregas agotadas.

## Alcance implementado

El backend es un monolito modular en Go con puertos y adaptadores y una base SQLite local de esquema v5. Incluye clientes autenticados, aislamiento por propietario, publicación atómica, idempotencia por cliente, endpoints estáticos y dinámicos, solicitudes salientes firmadas, workers e intentos configurables, replay auditado, retención, cuotas de admisión, métricas protegidas y herramientas de backup/restauración. El laboratorio local demuestra solicitudes reales y escenarios de fallo.

Actualmente un evento crea una entrega a un endpoint. No existe una API general de listado/búsqueda de entregas: se consultan IDs conocidos. Los clientes se aprovisionan mediante configuración de arranque; la API de negocio permite crear endpoints dinámicos.

## Garantías y límites

| Comportamiento | Significado |
| --- | --- |
| HTTP 202 | Se aceptó la transacción de publicación; no demuestra procesamiento por el receptor. |
| Publicación atómica | Evento, entrega y recibo idempotente se confirman juntos. |
| Reintentos y recuperación | El trabajo elegible se reintenta con presupuestos finitos; las reservas abandonadas se recuperan al vencer. |
| Identificadores estables | Los IDs de evento y entrega se conservan entre reintentos y replay; permiten deduplicar efectos en el receptor. |
| HTTP 2xx | El receptor aceptó la solicitud HTTP; HookRelay desconoce si terminó el proceso de negocio. |
| Persistencia | SQLite WAL con sincronización FULL, sujeto a las garantías del filesystem y del hardware. |

Puede haber duplicados tras perder una respuesta, interrumpir el proceso, no poder confirmar una finalización o restaurar una copia antigua. No se garantiza exactamente una vez, orden estricto, alta disponibilidad ni despliegue activo con varias instancias. Los intentos finitos no garantizan recepción eventual. HookRelay no hace atómica la transacción de negocio del emisor con la publicación; la aplicación podría necesitar su propia outbox.

Los IDs contienen 128 bits aleatorios criptográficos codificados en 32 caracteres hexadecimales. UUIDv7 fue una propuesta inicial, no el formato implementado. Los valores de reintentos, cuotas y retención se describen en [configuración](CONFIGURATION.md); el comportamiento del receptor, en el [contrato de entrega](DELIVERY_CONTRACT.md).

## Uso previsto y madurez

El objetivo técnico es demostrar Go, APIs HTTP, transacciones, concurrencia, seguridad, recuperación y operación mediante evidencia reproducible. Un público comercial posible son equipos pequeños que mantienen integraciones SaaS o de sistemas de negocio. Son hipótesis: el repositorio no demuestra clientes, ingresos ni disposición a pagar; tampoco documenta una comprobación de marca o dominio del nombre.

Existen pruebas de interrupción de procesos, efectos duplicados, agotamiento del almacenamiento, corrupción seleccionada y errores de E/S del WAL. Hay benchmarks locales históricos; no son mediciones de capacidad sostenida de la implementación actual de workers y pool lector. Consultar [mediciones](benchmarks/README.md) y [validación de almacenamiento](STORAGE_FAILURES.md).

La aptitud para producción requiere revisión independiente y evidencia del despliegue concreto: carga sostenida, HTTPS externo, TLS, secretos, alertas y restauración. Las pruebas de software no demuestran garantías ante pérdida eléctrica ni fallos físicos de dispositivo/controlador.

## Decisiones de alcance

El diseño actual mantiene un despliegue del dispatcher y SQLite local. Brokers, réplicas activas, transformaciones, conectores por proveedor, facturación, SSO, roles avanzados y portal completo de consumidores quedan fuera del alcance inicial. Control de tasa, reparto entre clientes, circuit breakers, rotación de secretos y búsqueda de entregas son extensiones posibles; no son garantías actuales ni compromisos de esta revisión documental.

Este documento sustituye el contexto inicial que mezclaba propuestas y estado actual. Las decisiones y correcciones fechadas permanecen en el [historial técnico](history/SESSION.md). Por sí solo no autoriza desarrollo, despliegue o publicación.

Las prioridades y criterios de aceptación vigentes están en el [backlog de ingeniería](BACKLOG.md).
