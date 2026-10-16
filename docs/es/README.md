# Documentación de HookRelay

[English](../README.md) · Revisión documental: 2026-10-16.

HookRelay recibe eventos mediante una API HTTP autenticada, guarda el trabajo de entrega en SQLite y envía webhooks firmados en segundo plano. Esta documentación explica la implementación, sus procedimientos operativos y la evidencia que delimita sus garantías.

## Por dónde empezar

| Tu objetivo | Lectura |
| --- | --- |
| Consultar tareas vigentes | [Backlog de ingeniería](BACKLOG.md) |
| Consultar vocabulario técnico | [Glosario](GLOSSARY.md) |
| Entender alcance y garantías | [Panorama del proyecto](CONTEXT_MASTER.md) |
| Ejecutar un ejemplo local | [Primeros pasos](GETTING_STARTED.md) |
| Entender paquetes, transacciones y datos | [Arquitectura](ARCHITECTURE.md) |
| Configurar clientes, destinos y límites | [Configuración](CONFIGURATION.md) |
| Integrar emisores y receptores | [Contrato de entrega](DELIVERY_CONTRACT.md), [OpenAPI](openapi.yaml) |
| Operar, respaldar y restaurar | [Operación](OPERATIONS.md) |
| Investigar fallos de almacenamiento | [Validación y recuperación del almacenamiento](STORAGE_FAILURES.md) |
| Modificar código y reproducir controles | [Guía de mantenimiento](MAINTAINING.md) |
| Usar la demostración visual local | [Laboratorio visual](PLAYGROUND.md) |
| Revisar cambios y mediciones anteriores | [Historial técnico](history/SESSION.md), [revisión del backend](history/AUDIT-2026-10-15.md), [revisión UX](history/PLAYGROUND_UX.md), [benchmarks](benchmarks/README.md) |

Un desarrollador nuevo debería leer panorama, primeros pasos, arquitectura y mantenimiento, en ese orden. Para operar el servicio, leer configuración, operación y fallos de almacenamiento. Para auditarlo, comenzar por las garantías, el contrato de entrega y la evidencia fechada: un PASS corresponde únicamente a la ejecución registrada.

## Organización y autoridad

Las guías vigentes permanecen en este nivel. `history/` contiene registros fechados; `benchmarks/` contiene informes históricos y JSON originales de medición. Esta estructura pequeña separa instrucciones de evidencia sin dispersar las guías relacionadas.

El inglés es la versión editorial principal. Esta edición española incluye las mismas guías, informes históricos y descripciones OpenAPI. Los comandos, rutas, claves JSON, campos HTTP, estados e identificadores se mantienen iguales. Los JSON originales de benchmarks son compartidos por ambas ediciones para conservar un único registro; cada informe español enlaza directamente al original y explica las mediciones en español.

Las fuentes de implementación resuelven dudas sobre hechos: los handlers y la aplicación definen el comportamiento; las migraciones, el esquema; `go.mod` y el lockfile web, las dependencias; el Makefile y los workflows, los controles. OpenAPI documenta el contrato HTTP. Una discrepancia es un defecto documental o de implementación que debe reconciliarse, no una autorización para reinterpretar el comportamiento silenciosamente.

## Evidencia y mantenimiento

Esta revisión reorganiza la documentación; no repite los experimentos históricos de carga, fallos, Docker o CI. Los resultados históricos conservan fechas, referencias de base y limitaciones. No implica certificación de producción ni garantías nuevas de rendimiento.

Actualizar ambos idiomas al cambiar el comportamiento. Mantener una explicación principal por tema y enlazarla desde los demás documentos. Conservar las mediciones originales. Registrar comandos, revisión exacta, entorno y límites de cada evidencia nueva. El historial está consolidado y no es una transcripción literal. Este repositorio nuevo no incluye el historial fuente anterior.
