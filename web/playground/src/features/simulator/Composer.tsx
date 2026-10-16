import {
  Send,
  ChevronDown,
  Zap,
  Clock3,
  TriangleAlert,
  Gauge,
  Unplug,
  Skull,
} from "lucide-react";
import { scenarioDescriptions } from "./scenarios";
export { scenarioDescriptions };
import type { ScenarioId, Scenario } from "../../shared/api/client";
const icons = {
  success: Zap,
  slow: Clock3,
  temporary: TriangleAlert,
  rate_limit: Gauge,
  drop: Unplug,
  exhausted: Skull,
};
export function Composer({
  scenarios,
  scenario,
  setScenario,
  eventType,
  setEventType,
  payload,
  setPayload,
  count,
  setCount,
  onPublish,
  pending,
}: {
  scenarios: Scenario[];
  scenario: ScenarioId;
  setScenario: (v: ScenarioId) => void;
  eventType: string;
  setEventType: (v: string) => void;
  payload: string;
  setPayload: (v: string) => void;
  count: number;
  setCount: (v: number) => void;
  onPublish: () => void;
  pending: boolean;
}) {
  return (
    <section className="panel composer">
      <div className="panel-heading">
        <span className="step-token">1</span>
        <h2>Elige qué va a pasar</h2>
        <span className="subtle">Empieza con Éxito a la primera. Después prueba qué ocurre cuando el receptor falla.</span>
      </div>
      <div className="scenario-grid">
        {scenarios.map((s) => {
          const Icon = icons[s.id];
          return (
            <button
              key={s.id}
              className={`scenario ${scenario === s.id ? "selected" : ""}`}
              onClick={() => setScenario(s.id)}
              aria-pressed={scenario === s.id}
            >
              <Icon size={18} />
              <span>{scenarioDescriptions[s.id].name}</span>
              <small>{scenarioDescriptions[s.id].description}</small>
            </button>
          );
        })}
      </div>
      <div className="scenario-expectation"><strong>En esta prueba</strong><p>{({success: "El receptor acepta el evento a la primera. Espera ver una entrega correcta y un intento.", slow: "El receptor tarda 1,5 segundos en responder. Observa la espera mientras el envío está activo.", temporary: "El receptor falla dos veces con HTTP 503 y acepta el tercer intento. Observa cómo vuelve a la cola.", rate_limit: "El receptor pide esperar al menos 3 segundos con HTTP 429. El segundo intento debería funcionar.", drop: "El receptor procesa el evento, pero se pierde su respuesta. El reintento puede producir un efecto duplicado.", exhausted: "El receptor falla en los cinco intentos. La entrega se detiene; después podrás recuperar el receptor y reenviar."})[scenario]}</p></div>
      <details className="event-options"><summary>Personalizar el evento <span>Opcional · hay un ejemplo preparado</span></summary>
      <div className="composer-fields">
        <label>
          TIPO DE EVENTO
          <input
            value={eventType}
            maxLength={128}
            onChange={(e) => setEventType(e.target.value)}
            placeholder="invoice.paid"
          />
        </label>
        <label>
          EVENTOS
          <div className="select-wrap">
            <select
              value={count}
              onChange={(e) => setCount(Number(e.target.value))}
            >
              <option value="1">1 evento</option>
              <option value="5">5 eventos</option>
              <option value="10">10 eventos</option>
            </select>
            <ChevronDown size={14} />
          </div>
        </label>
      </div>
      <label className="payload-label">
        <span>
          PAYLOAD <small>JSON · máximo 4 KiB</small>
        </span>
        <textarea
          aria-label="Payload JSON"
          spellCheck={false}
          rows={5}
          value={payload}
          onChange={(e) => setPayload(e.target.value)}
        />
      </label>
      </details>
      <button
        className="button primary publish"
        onClick={onPublish}
        disabled={pending || !scenarios.length}
      >
        <Send size={16} />
        {pending
          ? "Publicando…"
          : `Publicar ${count === 1 ? "evento" : `${count} eventos`}`}

      </button>
      <p className="fine-print">
        Publicar inicia una entrega real en este laboratorio. Puedes usar el ejemplo sin editar nada.
      </p>
    </section>
  );
}
