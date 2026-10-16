# Glosario

[English](../GLOSSARY.md) · Revisión documental: 2026-10-16. Referencia del código: `35c4373`.

Los nombres de código, estados y claves no se traducen; esta tabla explica su significado.

| Término | Significado en HookRelay |
| --- | --- |
| Evento | Tipo de evento y payload JSON publicados, retenidos con ID estable. |
| Entrega | Trabajo de enviar un evento a un endpoint; reintentar conserva su ID. |
| Endpoint o destino | URL configurada con propietario y política de secreto de firma. |
| Cliente / tenant | Límite de propiedad autenticado. Un token compartido no identifica personas individuales. |
| Worker o trabajador | Bucle acotado en segundo plano que reserva, envía y finaliza entregas. |
| Reserva / lease | Propiedad temporal hasta un plazo persistido. Vencer permite recuperación. |
| Token de reserva | Token aleatorio comprobado al finalizar para rechazar workers con reserva sustituida. |
| Confirmación / reversión | Commit hace efectiva una transacción; rollback descarta cambios sin confirmar. |
| Commit incierto | El error no determina si persistió la transacción completa; recuperación debe comprobarlo. |
| Idempotencia | Repetir bytes/clave de publicación devuelve recibo inicial mientras se conserva el registro. |
| Deduplicación | Prevención de efectos de negocio repetidos en el receptor usando IDs estables. |
| Intento | En lenguaje común, un envío; el historial almacenado registra específicamente finalizaciones confirmadas. |
| Ciclo / replay | Un ciclo tiene presupuesto finito. Replay abre otro para DEAD y conserva historial. |
| Backoff / jitter | Espera creciente y variación aleatoria para reintentar; se persiste la elegibilidad. |
| Retención | Elegibilidad por tiempo para borrar grafos terminales, historial y claves idempotentes. |
| Cuota de admisión | Límite de trabajo aceptado/retenido; no es tasa de solicitudes por segundo. |
| HMAC | Código de autenticación con secreto compartido sobre timestamp y bytes exactos del cuerpo. |
| SSRF | Falsificación de solicitudes desde servidor para acceder a destinos no previstos. |
| WAL / SHM | Log anticipado y archivo auxiliar de memoria compartida de SQLite; no descartarlos de bases activas. |
| FULL / ENOSPC / EIO | Configuración de sincronización SQLite; error del SO sin espacio; error de E/S. SQLITE_FULL es otro error SQLite, no el ajuste FULL. |
| Gauge / contador | Valor de estado actual frente a métrica acumulada durante el proceso. |
| P99 / RSS / SLO | Percentil 99; memoria residente; objetivo de nivel de servicio. Medir históricamente no implica un SLO. |
| Outbox | Patrón del emisor que vincula cambios de negocio con publicación posterior. HookRelay no hace atómica esa transacción externa. |
