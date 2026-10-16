# Revisión de UX/UI del laboratorio — 2026-10-14

[English](../../history/PLAYGROUND_UX.md) · Revisión documental: 2026-10-16. Referencia del código: `35c4373`.

Objetivo: una persona debe poder publicar un ejemplo, explicar qué hace HookRelay y distinguir aceptación de entrega, sin conocer SQLite, HMAC, backoff o los códigos de estado internos.

## Problemas encontrados y cambios

| Problema anterior | Consecuencia | Corrección aplicada |
| --- | --- | --- |
| Base de 13 px y numerosos textos de 7–11 px | Descripciones, controles e historial difíciles de leer | Base de 16 px; explicación principal 18 px; texto auxiliar de 14–16 px; títulos de sección 22–24 px |
| Métricas primero y tres columnas de paneles simultáneos | No había un inicio evidente ni una secuencia de acciones | Tres pasos ordenados: elegir/publicar, explorar el recorrido, comprobar el resultado |
| Diagrama con siglas y un punto en movimiento | Indicaba una ruta pero no enseñaba qué ocurre o por qué | Seis etapas numeradas y seleccionables con explicación, ejemplo y una idea clave |
| Explicación cambiaba al ritmo del movimiento | No permitía leer con calma | Lectura manual por defecto, Anterior/Siguiente y seguimiento automático opcional |
| Publicación y reenvío mezclados con reproducción visual | Riesgo de confundir un dibujo repetido con otra entrega | Botones con texto: Pausar dibujo, Repetir dibujo; estado real identificado por separado |
| SVG móvil con desplazamiento horizontal | Parte del flujo permanecía fuera de pantalla | Secuencia vertical con flechas en pantallas estrechas; seleccionar una etapa lleva a su explicación |
| JSON, IDs y métricas siempre presentes | Sobrecarga técnica antes de entender el concepto | Ejemplo listo para publicar; personalización, IDs, otras entregas y métricas desplegables |
| Escenarios descritos mediante HMAC/503/backoff/DEAD | Era necesario conocer la implementación para elegir | Nombres y descripciones en lenguaje directo; cada escenario anticipa el resultado esperado |
| Resultado centrado en un badge y contadores | No quedaba claro si el receptor recibió o procesó el evento | Resumen explícito de éxito, espera o agotamiento, y advertencia de procesamiento duplicado |
| Controles pequeños y solo iconos | Acciones poco descubribles y difíciles de pulsar | Etiquetas visibles, objetivos de al menos 44 px, foco visible y navegación con botones nativos |

## Criterios de interacción

- Publicar inicia trabajo real y lleva al recorrido. El ejemplo inicial funciona sin editar JSON.
- Seleccionar una etapa o Anterior/Siguiente conserva la explicación elegida mientras el backend sigue trabajando.
- El movimiento es una explicación temporal, nunca una traza exacta de las transacciones. La etiqueta de estado real y el resultado proceden de la API.
- El paso Guardar explica expresamente que 202 confirma aceptación durable, no entrega al receptor.
- El caso de fallo explica el retorno a Esperar → Enviar → Confirmar y el agotamiento tras cinco intentos.
- El detalle muestra las respuestas de cada intento; los IDs quedan disponibles sin dominar la pantalla.
- En pantallas pequeñas, únicamente la tabla histórica puede desplazarse dentro de su propio contenedor. El flujo no requiere desplazamiento lateral.
- Se respeta movimiento reducido; las explicaciones y controles siguen disponibles sin depender de animaciones o solo del color.

## Validación y límites

Compilación TypeScript/Vite y cinco pruebas del modelo de reproducción: PASS. Se ajustó la prueba de velocidad para comprobar la relación 2× sin depender de una duración fija; el recorrido se ralentizó de aproximadamente 2,5 a 8,4 segundos a velocidad 1×.

Prueba integral del laboratorio: PASS en los seis escenarios, recuperación/replay y exportación. No hubo cambios al backend en este ajuste de UX.

Revisión en navegador: selección de etapa y Siguiente, publicación con éxito en viewport móvil de 390×844, resultado persistido y ausencia de desbordamiento horizontal de la página; diagrama horizontal a 1280×900. Los estilos de textos visibles inspeccionados parten de 14 px. Se revisó el caso de respuesta perdida con el dibujo pausado para separar resultado y reproducción visual.

Esta revisión es una inspección y validación funcional, no una prueba de comprensión con usuarios ni una certificación WCAG. Siguiente validación recomendada: pedir a una persona nueva que publique el ejemplo y explique la diferencia entre 202 y 204, el motivo de un reintento y el riesgo de duplicados, sin instrucciones externas. Registrar dónde duda y ajustar esa parte del recorrido.

## Nota editorial — 2026-10-16

Estas observaciones fechadas no son una auditoría nueva del navegador para revisiones posteriores. La guía vigente está en [laboratorio](../PLAYGROUND.md).
