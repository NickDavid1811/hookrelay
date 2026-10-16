import {
  ArrowUpRight,
  PackageOpen,
  RotateCcw,
  CheckCircle2,
  AlertCircle,
} from "lucide-react";
import { scenarioDescriptions } from "../simulator/scenarios";
import type { Run } from "../../shared/api/client";
export const statusLabel: Record<Run["status"], string> = {
  PENDING: "En cola",
  PROCESSING: "Enviando",
  RETRYING: "Esperando reintento",
  SUCCESS: "Entregado",
  DEAD: "Intentos agotados",
};
export function Badge({ status }: { status: Run["status"] }) {
  return (
    <span className={`badge ${status.toLowerCase()}`}>
      <i />
      {statusLabel[status]}
    </span>
  );
}
export function Deliveries({
  runs,
  selected,
  onSelect,
}: {
  runs: Run[];
  selected?: string;
  onSelect: (id: string) => void;
}) {
  return (
    <section className="panel deliveries">
      <div className="panel-heading">

        <h2>Entregas</h2>
        <span className="count">{runs.length}</span>
      </div>
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th>PRUEBA / EVENTO</th>
              <th>ESTADO</th>
              <th>INTENTOS</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {runs.map((r) => (
              <tr key={r.id} className={r.id === selected ? "active" : ""}>
                <td>
                  <button className="row-pick" aria-pressed={r.id === selected} onClick={() => onSelect(r.id)}>
                    <span>{scenarioDescriptions[r.scenario].name}</span>
                    <small>{r.event_type} · {r.id.slice(0, 8)}…</small>
                  </button>
                </td>
                <td>
                  <Badge status={r.status} />
                </td>
                <td>
                  <span className="mono">
                    {r.attempts.toString().padStart(2, "0")}
                  </span>
                  <small className="cycle">ciclo {r.cycle}</small>
                </td>
                <td>
                  <button
                    className="icon-button"
                    aria-label={`Ver entrega ${r.id}`}
                    onClick={() => onSelect(r.id)}
                  >
                    <ArrowUpRight size={16} />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {!runs.length && (
        <div className="empty">
          <PackageOpen size={32} />
          <h3>El recorrido empieza aquí</h3>
          <p>Publica tu primer evento y observa qué sucede en cada etapa.</p>
        </div>
      )}
    </section>
  );
}
export function Detail({
  run,
  onReplay,
  pending,
  now,
}: {
  run?: Run;
  onReplay: () => void;
  pending: boolean;
  now: number;
}) {
  if (!run)
    return (
      <section className="panel detail empty-detail">
        <span className="section-code">INSPECTOR</span>
        <h2>Cada intento cuenta.</h2>
        <p>
          Selecciona una entrega para explorar sus respuestas, el receptor y la
          evidencia del recorrido.
        </p>
        <div className="mini-stages">
          <span>202</span>
          <span>HMAC</span>
          <span>204</span>
        </div>
      </section>
    );
  const effects = run.receipts.filter((r) => r.processed).length;
  const countdown = Math.max(0, ((run.next_estimate ?? 0) - now) / 1000);
  return (
    <section className="panel detail">
      <div className="panel-heading">
        <h2>Detalle del resultado</h2>
        <Badge status={run.status} />
      </div>
      <div className="detail-numbers">
        <div>
          <small>INTENTOS</small>
          <strong>{run.attempts}</strong>
        </div>
        <div>
          <small>CICLO</small>
          <strong>{run.cycle}</strong>
        </div>
        <div>
          <small>ÚLTIMO HTTP</small>
          <strong>{run.http_status || "—"}</strong>
        </div>
      </div>
      {run.status === "RETRYING" && (
        <div className="callout">
          {countdown > 0
            ? `El receptor pidió esperar: faltan al menos ${countdown.toFixed(1)} s.`
            : "La entrega espera para volver a enviarse. El siguiente intento aparecerá al realizarse."}
        </div>
      )}
      <div className="receiver-card">
        <div>
          <CheckCircle2 size={16} />
          <h3>Lo que vio el receptor</h3>
        </div>
        <p>
          <b>{run.receipts.length}</b> {run.receipts.length === 1 ? "recepción" : "recepciones"} · procesó el evento <b>{effects}</b> {effects === 1 ? "vez" : "veces"}
        </p>
        {effects > 1 && (
          <p className="duplicate">
            <AlertCircle size={13} /> Procesado más de una vez: el receptor necesita detectar duplicados.
          </p>
        )}
        <small>
          Procesar representa una acción, como registrar un pago. Este receptor de prueba no evita que esa acción se repita.
        </small>
      </div>
      {run.error && <p className="error-inline">{run.error}</p>}
      <h3 className="small-heading">Respuestas de cada intento</h3>
      {run.history.length ? (
        <div className="attempt-list">
          {run.history.map((a) => (
            <div key={a.number}>
              <span className="mono">
                #{a.number} <small>c{a.cycle}</small>
              </span>
              <span
                className={
                  a.http_status >= 200 && a.http_status < 300
                    ? "text-success"
                    : "text-warn"
                }
              >
                {a.http_status
                  ? `HTTP ${a.http_status}`
                  : "Sin respuesta HTTP"}
              </span>
            </div>
          ))}
        </div>
      ) : (
        <p className="muted small">Aún no hay intentos finalizados.</p>
      )}
      <details className="technical-ids"><summary>Identificadores técnicos</summary>      <dl className="ids">
        <dt>EVENT ID</dt>
        <dd title={run.event_id}>{run.event_id}</dd>
        <dt>DELIVERY ID</dt>
        <dd title={run.id}>{run.id}</dd>
      </dl>
      </details>
      {run.status === "DEAD" && (
        <button
          className="button secondary replay"
          disabled={pending}
          onClick={onReplay}
        >
          <RotateCcw size={14} />
          {pending ? "Solicitando…" : "Recuperar receptor y reenviar"}
        </button>
      )}
    </section>
  );
}
