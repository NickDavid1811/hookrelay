# Laboratorio visual

[English](../PLAYGROUND.md) · Revisión documental: 2026-10-16. Referencia del código: `35c4373`.

El laboratorio muestra publicaciones y entregas reales. `cmd/playground` inicia un dispatcher aislado, un receptor controlado y un servidor web. No consulta SQL directamente ni duplica reglas de entrega.

## Ejecutar

Usar Go según `go.mod`, Node 24/npm y Python 3 para verificar procesos. Desde la raíz:

```sh
make web-install
make playground
```

Abrir `http://127.0.0.1:8090` y detener con Ctrl+C. Se compilan binarios y recursos web. Los datos están en `work/playground/lab.db`, excluidos de Git y separados de la base habitual. La UI escucha en `127.0.0.1:8090`, el receptor en `127.0.0.1:9091` y la API privada en puerto loopback efímero.

```sh
./bin/playground -h
./bin/playground -listen 127.0.0.1:8092 -receiver-listen 127.0.0.1:9092 -data-dir work/playground-alternative
```

Conservar puerto de receptor al reutilizar base: los destinos no cambian automáticamente. Excluir de Git el directorio alternativo antes de usarlo. Mantener listeners en loopback; no existe contrato de despliegue público/LAN. Ante fallos, revisar puertos, permisos y compilación de `web/playground/dist`. Usar directorio nuevo para una demostración aislada en lugar de editar la base manualmente.

## Recorrido y escenarios

Elegir escenario, editar opcionalmente tipo/payload y publicar uno, cinco o diez eventos. Seleccionar entrega para ver historial, recepciones y cronología. Hay tres pasos: publicar, explorar y comprobar. Seis etapas permiten lectura Anterior/Siguiente; animar es opcional. El texto de la interfaz está actualmente en español, independientemente del idioma documental.

| Escenario | Receptor | Resultado esperado |
| --- | --- | --- |
| Éxito | 204 | SUCCESS, un intento |
| Lento | Espera 1,5 segundos | SUCCESS; envío activo visible |
| Fallo temporal | Dos 503 y después 204 | SUCCESS, tres intentos |
| Límite de tasa | 429 con Retry-After de 3 segundos y después 204 | SUCCESS, dos intentos |
| Respuesta perdida | Procesa el primero y cierra sin respuesta | Reintento y dos efectos observados |
| Agotamiento | Siempre 503 | DEAD tras cinco intentos |

Para DEAD, restablecer/reenviar cambia ese receptor a éxito y solicita replay real auditado. Pausa/repetición/velocidad solo afectan al dibujo, no al worker ni crean publicación. En móvil el flujo es vertical; JSON, IDs, otras entregas y métricas son desplegables. Se respeta movimiento reducido.

La animación ilustra. API/historial son evidencia persistida; las recepciones son observaciones; el sondeo marca detección, no tiempos internos exactos. Puede omitir estados breves. La cuenta de 429 refleja un mínimo conocido, no elegibilidad exacta con jitter. Exportar descarga el snapshot sin payloads, credenciales ni URLs privadas; conserva metadatos de tipo de evento introducidos por el usuario.

## Persistencia y límites

SQLite retiene trabajo del dispatcher. Listas de sesión, recepciones y cronología visual son estructuras acotadas en memoria y no se reconstruyen al reiniciar; exportar antes de cerrar. La sesión admite 200 publicaciones. Cola global/cliente 200, eventos retenidos 1000, payload 4 MiB y cinco ciclos incluido el inicial. El resto de retención usa valores de core. Son límites de demostración, no capacidad garantizada.

Reiniciar puede recuperar trabajo persistido sin reconstruir su sesión visual. Una pantalla vacía no implica base vacía. El caso de respuesta perdida demuestra efectos que un receptor productivo debe deduplicar.

## Fachada local y desarrollo

| Método/ruta | Propósito |
| --- | --- |
| `GET /play/api/state` | Snapshot de sesión y métricas seleccionadas |
| `GET /play/api/export` | Mismo snapshot como JSON descargable |
| `POST /play/api/publish` | Scenario, event_type, payload y count |
| `POST /play/api/replay/{id}` | Recuperación controlada con expected_cycle |

Los cuerpos de mutación admiten 12 KiB, un valor JSON y ningún campo desconocido. La fachada limita payload a 4096 bytes, event_type a 128 bytes y count a 1–12 (la UI ofrece 1/5/10). El lote no es atómico: una respuesta fallida puede incluir accepted_ids de trabajo ya aceptado; no repetir todo el lote a ciegas. Requieren `X-Playground-Request: 1`. Host/Origin, restricciones de acceso cruzado y cabeceras protegen la fachada; tokens/secretos quedan en Go. No es la API de negocio versionada.

Para desarrollo visual, iniciar backend compilado y luego `npm --prefix web/playground run dev`. Vite usa loopback y proxy de `/play/api` al puerto 8090, ajustando Origin. No publicar ese servidor.

```sh
make web-test
make playground-test
make ci
```

Instalar dependencias primero; `make ci` las instala desde lockfile. Los cinco tests de modelo y smoke de procesos no cubren toda interacción ni accesibilidad. Reconstrucción de sesiones, trazas internas correlacionadas, tests de componentes/accesibilidad y comprensión con usuarios siguen como extensiones sin implementar o validar. Consultar [revisión UX fechada](history/PLAYGROUND_UX.md).
