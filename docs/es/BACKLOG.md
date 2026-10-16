# Backlog de ingeniería vigente

[English](../BACKLOG.md) · Actualizado: 2026-10-16. Base: `b98ca1c`.

Esta es la lista vigente de trabajo. Los informes históricos describen experimentos anteriores y no validan la revisión actual de este repositorio. Se mantiene el alcance de un dispatcher, SQLite local, reintentos finitos y posibles duplicados. Esta lista no autoriza despliegues ni publicaciones.

## Consolidación de este cambio

- Documentación: eliminar referencias a una revisión no disponible del repositorio anterior e instrucciones que prometían recuperar aquí sus transcripciones completas. Conservar mediciones históricas y aclarar sus límites de procedencia.
- Bloqueo de releases: reutilizar el workflow completo de CI desde el workflow de tags. Ambos jobs de publicación requieren validar correctamente el mismo commit del evento. La validación tiene permisos de lectura; solo los jobs de publicación reciben permisos de escritura.
- Aceptación: ninguna referencia a la revisión retirada; enlaces desde ambas ediciones; lint de workflows correcto; publicaciones dependientes de la validación sin condiciones que permitan omitirla. La ejecución remota sigue sin verificarse hasta observar un run real.

## Trabajo pendiente priorizado

| ID | Prioridad / tipo | Entregable y criterios de aceptación |
| --- | --- | --- |
| B1 | Alta / medición | Ampliar el benchmark Linux con workers configurables, duración sostenida, receptores lentos y varios clientes. Registrar commit exacto, parámetros, entorno, errores, edad de cola, latencias, memoria y almacenamiento. Comparar ejecuciones equivalentes repetidas fuera de CI y definir capacidad inicial según evidencia. |
| B2 | Alta / funcionalidad | Añadir listado de entregas por cliente con filtros de estado, endpoint y fechas, cursor estable acotado e índices adecuados. Probar aislamiento y recorrido sin duplicados sobre datos sin cambios; documentar cambios concurrentes y actualizar ambas ediciones OpenAPI. |
| B3 | Alta / diseño e implementación | Definir rotación de tokens/HMAC y cambios de endpoints, incluyendo trabajo en curso, convivencia de credenciales, revocación y recuperación. Elegir protección de secretos según el despliegue; implementar con migración y regresiones cuando corresponda. |
| B4 | Alta / validación de despliegue | Preparar un piloto aislado con TLS, entrega HTTPS externa, supervisión, secretos protegidos y alertas de cola/disco/errores. Restaurar backup, configuración y credenciales en otra instancia; registrar tiempo de recuperación y riesgo de efectos duplicados. |
| B5 | Media / validación con usuarios | Observar a una persona nueva publicando, investigando fallos y reenviando DEAD. Registrar dudas; añadir pruebas de navegador, teclado y accesibilidad para los recorridos críticos del laboratorio. |
| B6 | Condicionada / planificación | Decidir con B1 y el piloto si hacen falta tasas por cliente/endpoint, reparto equitativo o circuit breakers. Si se incorporan, definir resultados medibles y probar que un cliente lento o ruidoso no monopoliza el presupuesto configurado. |
| B7 | Antes de afirmar producción / revisión independiente | Revisar seguridad y recuperación independientemente, conservando evidencia de revisión y entorno. Explicitar límites físicos/eléctricos no probados; las pruebas de software no certifican durabilidad del hardware. |

## Límites y decisiones

Broker, múltiples réplicas activas, SSO, facturación, conectores y portal operativo completo quedan fuera de este incremento. Adopción y autorización comercial según LICENSE son cuestiones separadas de la validación técnica. Las extensiones posibles no son compromisos.

## Contrato de validación de releases

`release.yml` llama al workflow reutilizable local `ci.yml`, incluyendo lint, vulnerabilidades, cobertura, CI completo, ENOSPC y E/S/interrupción del WAL. Fallo, cancelación o validación omitida impiden ambas publicaciones. Checkout usa el SHA del evento, no una cabecera de rama posterior. También puede correr el CI independiente del push; la release depende de su propia validación reutilizada, no de un estado verde ajeno.

El bloqueo no hace atómica la publicación de imagen y binarios: una puede fallar después de que la otra termine. Crear un tag inicia publicación; no usar tags de release solo para probar este cambio. El lint local comprueba estructura, no ejecución en GitHub, permisos del registro ni funcionamiento en otras plataformas. Registrar esos resultados únicamente después de observarlos.

Validación local del 2026-10-16: actionlint v1.7.12 pasó para ambos workflows; todos los enlaces Markdown relativos de docs resolvieron; no quedó la referencia a la revisión retirada; las comprobaciones de espacios de git diff pasaron. No cambió código de aplicación, no se repitieron sus suites ni las de fallos, y no se publicaron tags ni releases.
