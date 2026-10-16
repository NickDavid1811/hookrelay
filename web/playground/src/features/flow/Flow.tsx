import { useEffect, useState } from "react";
import { Send, ShieldCheck, Database, Layers3, Cpu, Radio, ArrowRight, CheckCircle2 } from "lucide-react";
import type { Run } from "../../shared/api/client";
const stages = [
  { title: "Tu aplicación publica", short: "Publicar", icon: Send, description: "Tu aplicación avisa que ocurrió algo, por ejemplo: una factura fue pagada. Envía ese evento a HookRelay.", evidence: "El evento contiene un tipo y datos en JSON." },
  { title: "La API lo recibe", short: "Recibir", icon: ShieldCheck, description: "HookRelay comprueba quién envía el evento y valida sus datos antes de aceptarlo.", evidence: "Una petición inválida se rechaza antes de crear la entrega." },
  { title: "Se guarda en SQLite", short: "Guardar", icon: Database, description: "HookRelay guarda el evento y su entrega pendiente juntos en la base de datos. Así el trabajo no depende de que tu aplicación siga esperando.", evidence: "HTTP 202 significa guardado y aceptado. Todavía no significa entregado." },
  { title: "Espera en la cola", short: "Esperar", icon: Layers3, description: "La entrega queda pendiente hasta que el trabajador puede enviarla. Si hubo un fallo temporal, aquí espera antes de volver a intentarlo.", evidence: "La cola está persistida en SQLite; no es otro servicio separado." },
  { title: "El trabajador lo envía", short: "Enviar", icon: Cpu, description: "El worker toma una entrega de la cola, firma la petición y la envía al receptor. La firma permite comprobar su origen.", evidence: "El envío HTTP ocurre después de guardar el evento." },
  { title: "El receptor responde", short: "Confirmar", icon: Radio, description: "El sistema receptor procesa el evento y devuelve una respuesta. HookRelay guarda el resultado del intento: éxito, espera para reintentar o intentos agotados.", evidence: "HTTP 204 confirma éxito en este laboratorio. Si se pierde la respuesta, puede repetirse un evento ya procesado." },
];
export function Flow({ run, position, playing, reduced }: { run?: Run; position: number; playing: boolean; reduced: boolean }) {
  const [chosen, setChosen] = useState<number | null>(0);
  useEffect(() => setChosen(0), [run?.id]);
  const active = position > 6 ? (run?.status === "DEAD" ? 5 : 3) : Math.min(5, Math.floor(position / 6 * 5));
  const selected = chosen ?? active;
  const stage = stages[selected];
  return <div className={`flow-explainer ${reduced ? "reduced" : ""}`}>
    <p className="flow-instruction">Sigue los números. Selecciona cualquier etapa para entender qué hace.</p>
    <div className="animation-progress" aria-hidden="true"><span style={{ width: `${Math.min(100, position / 6 * 100)}%` }} /></div>
    <p className="animation-status">{run ? `Animación explicativa: etapa ${active + 1} de 6 · ${stages[active].short.toLowerCase()}` : "Explora las etapas antes de publicar tu primer evento."}</p>
    <ol className="stage-list" aria-label="Las seis etapas de una entrega">
      {stages.map((s, i) => { const Icon = s.icon; return <li key={s.short}>
        <button type="button" className={`stage-button ${selected === i ? "selected" : ""} ${run && active === i && playing ? "traversing" : ""}`} aria-pressed={selected === i} aria-controls="stage-explanation" onClick={() => { setChosen(i); if (window.innerWidth <= 1100) document.getElementById("stage-explanation")?.scrollIntoView({ behavior: reduced ? "instant" : "smooth", block: "start" }); }}>
          <span className="stage-top"><span className="stage-number">{i + 1}</span><Icon size={24} aria-hidden="true" /></span>
          <strong>{s.short}</strong><span>{s.title}</span>

        </button><ArrowRight className="stage-arrow" size={20} aria-hidden="true" />
      </li>; })}
    </ol>
    <div className="stage-explanation" id="stage-explanation" aria-live="polite">
      <div className="explanation-number">{selected + 1}</div>
      <div><p className="eyebrow">ETAPA {selected + 1} DE 6 · EXPLICACIÓN</p><h3>{stage.title}</h3><p>{stage.description}</p><p className="learning-note"><CheckCircle2 size={20} aria-hidden="true" />{stage.evidence}</p></div>
      <div className="lesson-controls"><button className="button secondary" disabled={selected === 0} onClick={() => setChosen(Math.max(0, selected - 1))}>Anterior</button><button className="button secondary" disabled={selected === 5} onClick={() => setChosen(Math.min(5, selected + 1))}>Siguiente etapa</button><button className="follow-animation" onClick={() => setChosen(chosen === null ? selected : null)}>{chosen === null ? "Leer a mi ritmo" : "Seguir animación"}</button></div>
    </div>
    <div className={`route-outcome ${run?.status === "DEAD" ? "warning" : ""}`}>
      <strong>¿Y si falla?</strong><span>Un fallo temporal vuelve a <b>4. Esperar</b> → <b>5. Enviar</b> → <b>6. Confirmar</b>. Si agota cinco intentos, la entrega se detiene y puedes reenviarla.</span>
    </div>
    <p className="animation-disclaimer">El movimiento explica el recorrido; no mide el tiempo de cada operación. El resultado real aparece debajo y sigue actualizándose aunque pauses la animación.</p>
  </div>;
}
