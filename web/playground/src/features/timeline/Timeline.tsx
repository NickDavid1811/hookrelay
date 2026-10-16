import { Activity } from "lucide-react";
import type { Run } from "../../shared/api/client";
function explain(text: string) {
  return text.replaceAll("HTTP 0", "Sin respuesta HTTP").replaceAll("procesamiento simulado: true", "el receptor procesó el evento").replaceAll("procesamiento simulado: false", "el receptor no procesó el evento").replaceAll("HMAC verificado", "Firma verificada").replaceAll("persistidos atómicamente", "guardados juntos en SQLite");
}
export function Timeline({ run }: { run?: Run }) {
  const events = run ? [...run.timeline].sort((a, b) => a.at - b.at) : [];
  return (
    <section className="panel timeline">
      <div className="panel-heading">
        <Activity size={16} />
        <h2>Qué pasó, en orden</h2>
        <span className="subtle">Hechos registrados · del más antiguo al más reciente</span>
      </div>
      {events.length ? (
        <ol>
          {events.map((event, i) => (
            <li key={`${i}-${event.at}`}>
              <span className={`timeline-dot ${event.source}`} />
              <time>
                {new Date(event.at).toLocaleTimeString("es", { hour12: false })}
              </time>
              <div>
                <strong>{({ PENDING: "Entrega en cola", PROCESSING: "Enviando al receptor", RETRYING: "En espera para reintentar", SUCCESS: "Entrega confirmada", DEAD: "Se agotaron los intentos" } as Record<string, string>)[event.title] ?? explain(event.title)}</strong>
                <p>{explain(event.detail)}</p>
              </div>
              <span className="source">
                {event.source === "api"
                  ? "Fuente: API"
                  : event.source === "receiver"
                    ? "Fuente: receptor"
                    : "Fuente: consulta de estado"}
              </span>
            </li>
          ))}
        </ol>
      ) : (
        <p className="muted empty-timeline">
          Aquí aparecerán los hechos observados. No se inventan pasos internos
          que la API no expone.
        </p>
      )}
    </section>
  );
}
