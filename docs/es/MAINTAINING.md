# Guía de mantenimiento

[English](../MAINTAINING.md) · Revisión documental: 2026-10-16.

Comenzar por [arquitectura](ARCHITECTURE.md), [configuración](CONFIGURATION.md), [contrato](DELIVERY_CONTRACT.md) y [OpenAPI](openapi.yaml). Esta guía explica cómo modificar y verificar la implementación. Los PASS históricos están en el [historial](history/SESSION.md); no validan cambios nuevos.

## Entorno y controles

Usar Go según `go.mod`, Python 3.10+ y Node 24/npm para la web. Los tests abren sockets loopback, crean procesos y necesitan temporales escribibles. Dependencias y vulnerabilidades requieren red. Los runners Linux también requieren Docker Engine y soporte del kernel.

```sh
make lint
make vulncheck
make ci
make coverage
make storage-full-test
make storage-io-test
```

| Comando | Cobertura |
| --- | --- |
| `make lint` | Validación gofmt y go vet, sin fallback opcional |
| `make vulncheck` | govulncheck fijado; fallos de descarga/análisis hacen fallar el target |
| `make ci` | Formato, módulos, carreras, vet, compilación, smoke/salida temprana, fallos/replay de procesos reales, backup, clientes, capacidad, instalación/tests/build web y escenarios del laboratorio |
| `make coverage` | Cobertura Go con carreras; no es umbral de aceptación ni verificación exhaustiva |
| `make storage-full-test` | ENOSPC real en tmpfs Docker aislado de 4 MiB |
| `make storage-io-test` | EIO de escritura/sincronización WAL y recuperación de transacción interrumpida |

Las suites de procesos son secuenciales. Go omite los casos aislados de filesystem sin habilitación del runner; omitirlos no demuestra ese fallo. GitHub Actions ejecuta lint, vulnerabilidades, cobertura, `make ci` y ambos targets de fallos. Consultar la secuencia vigente en `.github/workflows/ci.yml`.

La distribución Docker se verifica por separado: `docker compose up --build -d --wait`, publicar/consultar el ejemplo y terminar con `docker compose down`. Usar proyecto aislado cuando corresponda; no eliminar volúmenes con datos necesarios. No afirmar éxito de CI/release remoto sin observarlo. El workflow de tags es independiente del CI; existir no demuestra dependencia explícita de validación ni soporte operativo de cada plataforma compilada.

## Invariantes que deben conservarse

- Confirmar publicación, recibo y entrega juntos. La idempotencia se limita al cliente y bytes exactos.
- Comprobar propietario/cuota/estado dentro de la transacción de escritura. Preservar token y vigencia de reserva al finalizar.
- No mantener transacciones durante envíos de red. No debilitar restricciones de destino/DNS/proxy para aprobar tests.
- Confirmar estado y auditoría de replay juntos; retención elimina grafos elegibles completos y claves idempotentes.
- Crear un endpoint dinámico no debe mutar una caché antes del commit. No hay fallback de secretos estáticos/dinámicos.
- Esperar workers y mantenimiento antes de cerrar la base. Propagar fallos operativos al estado de salida.
- Emitir métricas de resultados confirmados después del commit; distinguir observaciones HTTP de historial persistido.

## Cambios de API y esquema

Actualizar handlers/aplicación, ambas ediciones OpenAPI, ejemplos y tests HTTP relevantes juntos. `internal/relay/api_contract_test.go` cubre regresiones concretas, no conformidad completa. Mantener arrays/cursores salvo versionado explícito. Versión API, esquema SQLite y tag de distribución son conceptos diferentes.

No reescribir migraciones aplicadas. Añadir transacción versionada y tests de apertura antigua, conservación de filas, migración y reanudación. Actualizar DDL canónico en `scripts/backup_schema.json` sin cambiar entradas anteriores. Los tests de backup deben rechazar objetos requeridos ausentes/alterados antes de publicar. Esquemas editados manualmente requieren reconciliación explícita.

quick_check de arranque es estructural; no audita completamente índices/contenido ni payload. La migración puede persistir aunque falle otra fase; recuperar con backup offline anterior. No existe downgrade automático. Consultar [operación](OPERATIONS.md).

## Mantenimiento documental

Actualizar inglés y `docs/es/` en el mismo cambio. No traducir claves del protocolo, código, identificadores ni archivos de medición. Enlazar una explicación principal en lugar de copiarla. Los informes históricos deben identificar base, entorno, comandos, resultados y límites; añadir correcciones sin presentar resultados antiguos como actuales. Los JSON originales son compartidos e inalterados.

Esta revisión consolidó las notas extensas de sesiones y retiró planificación obsoleta. Este repositorio nuevo contiene registros consolidados, no el historial fuente anterior ni las transcripciones originales completas. Los identificadores históricos describen trabajo anterior y no son revisiones disponibles aquí. Registrar las nuevas validaciones contra commits disponibles en este repositorio.
