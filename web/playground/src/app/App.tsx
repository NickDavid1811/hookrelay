import { useEffect, useReducer, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowUpRight,
  CheckCheck,
  Download,
  FlaskConical,
  GitBranch,
  Layers3,
  Pause,
  Play,
  Radio,
  RotateCcw,
  ShieldCheck,
  WifiOff,
} from "lucide-react";
import { api, type ScenarioId } from "../shared/api/client";
import { Composer } from "../features/simulator/Composer";
import { scenarioDescriptions } from "../features/simulator/scenarios";
import { Flow } from "../features/flow/Flow";
import {
  initialPlayback,
  playbackReducer,
  targetFor,
} from "../features/flow/model";
import { Deliveries, Detail, Badge } from "../features/deliveries/Deliveries";
import { Timeline } from "../features/timeline/Timeline";
export function App() {
  const query = useQuery({
    queryKey: ["lab"],
    queryFn: api.state,
    refetchInterval: 750,
    retry: 1,
  });
  const client = useQueryClient();
  const snapshot = query.data;
  const [scenario, setScenario] = useState<ScenarioId>("success"),
    [eventType, setEventType] = useState("invoice.paid"),
    [payload, setPayload] = useState(
      '{\n  "invoice_id": "inv_2026",\n  "amount": 150,\n  "currency": "USD"\n}',
    ),
    [count, setCount] = useState(1),
    [selected, setSelected] = useState<string>(),
    [error, setError] = useState("");
  const [now, setNow] = useState(Date.now()),
    [reduced, setReduced] = useState(
      window.matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
    [help, setHelp] = useState(false);
  const [playback, dispatch] = useReducer(playbackReducer, initialPlayback);
  const runs = snapshot?.runs ?? [],
    run = runs.find((r) => r.id === selected) ?? runs[0],
    target = targetFor(run);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    dispatch({ type: "reset" });
  }, [run?.id]);
  useEffect(() => {
    if ((run?.receipts.length ?? 0) > 1 || (run?.cycle ?? 1) > 1)
      dispatch({ type: "attempt" });
  }, [run?.receipts.length, run?.cycle]);
  useEffect(() => {
    let previous = performance.now();
    const timer = setInterval(() => {
      const tick = performance.now();
      dispatch({ type: "tick", delta: tick - previous, target });
      previous = tick;
    }, 32);
    return () => clearInterval(timer);
  }, [target]);
  const publish = useMutation({
    mutationFn: async () => {
      let value: unknown;
      try {
        value = JSON.parse(payload);
      } catch {
        throw new Error("El payload debe ser JSON válido.");
      }
      return api.publish(scenario, eventType, value, count);
    },
    onSuccess: (result) => {
      setError("");
      setSelected(result.ids[result.ids.length - 1]);
      client.invalidateQueries({ queryKey: ["lab"] });
      document
        .getElementById("flow")
        ?.scrollIntoView({
          behavior: reduced ? "instant" : "smooth",
          block: "start",
        });
    },
    onError: (e) => setError(e.message),
  });
  const replay = useMutation({
    mutationFn: () => {
      if (!run) throw new Error("Selecciona una entrega");
      return api.replay(run);
    },
    onSuccess: () => {
      setError("");
      dispatch({ type: "reset", position: 3.6 });
      client.invalidateQueries({ queryKey: ["lab"] });
    },
    onError: (e) => setError(e.message),
  });
  const online = !!snapshot && !query.isError && !snapshot.error;
  const stats = snapshot?.stats;
  const currentScenario = snapshot?.scenarios.find(
    (s) => s.id === run?.scenario,
  );
  return (
    <div className="shell">
      <header className="topbar">
        <a className="brand" href="/">
          <span className="brand-mark">
            <GitBranch size={20} />
          </span>
          hookrelay
          <span className="brand-separator" />
          <span className="brand-product">FLOW LAB</span>
        </a>
        <nav>
          <span className="environment">
            <span />
            Laboratorio local
          </span>
          <a className="export-link" href="/play/api/export" download>
            <Download size={14} />
            <span>Exportar sesión</span>
          </a>
          <span className="version">v0.1</span>
        </nav>
      </header>
      <main>
        <div className="breadcrumb">
          <span>HookRelay</span>
          <span>/</span>
          <strong>Aprende con una entrega real</strong>
          <span className="lab-tag">
            <FlaskConical size={11} /> LABORATORIO
          </span>
        </div>
        <section className="intro">
          <div>
            <p className="eyebrow">APRENDE PROBANDO</p>
            <h1>
              ¿Cómo llega tu evento
              <br className="mobile-break" /> a otro sistema?
            </h1>
            <p>
              HookRelay guarda tu evento, lo envía y vuelve a intentarlo si
              falla. Aquí puedes verlo y entenderlo paso a paso.
            </p>
          </div>
          <button className="button ghost" onClick={() => setHelp(!help)}>
            <GitBranch size={15} />
            Cómo funciona
            <ArrowUpRight size={14} />
          </button>
        </section>
        {help && (
          <aside className="help-box">
            <strong>Un webhook es un aviso entre sistemas.</strong>
            <p>
              Por ejemplo, tu aplicación avisa a otra que una factura se pagó.
              HookRelay guarda ese aviso y se encarga de entregarlo.
            </p>
            <p>
              Primero prueba Éxito a la primera. Después compara un fallo
              temporal o una respuesta perdida. Puedes leer las seis etapas a tu
              ritmo; repetir el dibujo no envía otro evento.
            </p>
            <p>
              El laboratorio usa datos aislados. Exporta la sesión antes de
              cerrar: la cronología visual no se recupera al reiniciar.
            </p>
          </aside>
        )}
        <ol className="journey-nav" aria-label="Cómo usar este laboratorio">
          <li>
            <span>1</span>
            <div>
              <strong>Elige y publica</strong>
              <p>Usa un ejemplo o simula un fallo.</p>
            </div>
          </li>
          <li>
            <span>2</span>
            <div>
              <strong>Explora el recorrido</strong>
              <p>Descubre qué hace cada etapa.</p>
            </div>
          </li>
          <li>
            <span>3</span>
            <div>
              <strong>Comprueba el resultado</strong>
              <p>Revisa la respuesta y los intentos.</p>
            </div>
          </li>
        </ol>
        {(query.isError || snapshot?.error) && (
          <div className="error-banner" role="alert">
            <WifiOff size={16} />
            {snapshot?.error ||
              "No se pudo conectar con el laboratorio. Comprueba que playground esté ejecutándose."}
          </div>
        )}
        {error && (
          <div className="error-banner" role="alert">
            {error}
            <button onClick={() => setError("")} aria-label="Cerrar error">
              ×
            </button>
          </div>
        )}
        <Composer
          scenarios={snapshot?.scenarios ?? []}
          {...{
            scenario,
            setScenario,
            eventType,
            setEventType,
            payload,
            setPayload,
            count,
            setCount,
          }}
          onPublish={() => publish.mutate()}
          pending={publish.isPending}
        />
        <section className="panel flow-panel" id="flow">
          <div className="flow-title">
            <div>
              <span className="step-token">2</span>
              <h2>Entiende el recorrido</h2>
              {run ? (
                <span className="real-status">
                  Estado real: <Badge status={run.status} />
                </span>
              ) : (
                <span className="waiting">Esperando tu primer evento</span>
              )}
            </div>
            <span className="flow-id">
              {run
                ? run.id.slice(0, 12) + "…"
                : "PUBLICAR → PERSISTIR → ENTREGAR"}
            </span>
          </div>
          <Flow
            run={run}
            position={reduced ? target : playback.position}
            playing={playback.playing}
            reduced={reduced}
          />
          <div className="playback-controls">
            <div>
              <button
                className="icon-button control"
                aria-label={
                  playback.playing ? "Pausar dibujo" : "Continuar dibujo"
                }
                disabled={!run}
                onClick={() => dispatch({ type: "toggle" })}
              >
                {playback.playing ? <Pause size={18} /> : <Play size={18} />}
                <span>
                  {playback.playing ? "Pausar dibujo" : "Continuar dibujo"}
                </span>
              </button>
              <button
                className="icon-button control"
                aria-label="Repetir dibujo sin reenviar el evento"
                disabled={!run}
                onClick={() => dispatch({ type: "replay" })}
              >
                <RotateCcw size={18} />
                <span>Repetir dibujo</span>
              </button>
              <span className="control-divider" />
              <select
                aria-label="Velocidad de animación"
                value={playback.speed}
                onChange={(e) =>
                  dispatch({ type: "speed", speed: Number(e.target.value) })
                }
              >
                <option value="0.5">0.5×</option>
                <option value="1">1×</option>
                <option value="2">2×</option>
              </select>
              <span className="playback-label">
                {!playback.playing
                  ? "Dibujo pausado · las entregas continúan"
                  : playback.mode === "replay"
                    ? "Reproducción visual · sin publicar"
                    : "Dibujo explicativo · resultado real debajo"}
              </span>
            </div>
            <label className="motion-toggle">
              <input
                type="checkbox"
                checked={reduced}
                onChange={(e) => setReduced(e.target.checked)}
              />
              Reducir movimiento
            </label>
          </div>
        </section>
        <section className="results-section" aria-labelledby="result-heading">
          <div className="result-heading">
            <span className="step-token">3</span>
            <div>
              <h2 id="result-heading">Comprueba qué ocurrió</h2>
              <p>Estos datos provienen de la API y del receptor de prueba.</p>
            </div>
          </div>
          <div className="result-summary" aria-live="polite">
            <strong>
              {!run
                ? "Tu resultado aparecerá aquí"
                : run.status === "SUCCESS"
                  ? "El receptor confirmó la entrega"
                  : run.status === "DEAD"
                    ? "La entrega se detuvo después de agotar los intentos"
                    : run.status === "RETRYING"
                      ? "Hubo un fallo. HookRelay volverá a intentarlo"
                      : run.status === "PROCESSING"
                        ? "HookRelay está enviando el evento"
                        : "Evento guardado. La entrega espera en la cola"}
            </strong>
            <p>
              {!run
                ? "Publica el ejemplo del paso 1 para empezar."
                : `${scenarioDescriptions[run.scenario].name} · ${run.attempts} intento(s) finalizado(s). `}
              {run?.status === "SUCCESS" &&
                "La respuesta de éxito quedó guardada en SQLite."}
              {run?.status === "DEAD" &&
                "Puedes recuperar el receptor y reenviar desde el detalle de abajo."}
            </p>
            {run && run.receipts.filter((r) => r.processed).length > 1 && (
              <p className="duplicate-summary">
                El receptor procesó el evento más de una vez. Una respuesta
                perdida puede causar duplicados, aunque el envío termine bien.
              </p>
            )}
          </div>
          {run && (
            <div className="workspace-grid">
              <Detail
                run={run}
                onReplay={() => replay.mutate()}
                pending={replay.isPending}
                now={now}
              />
              <Timeline run={run} />
            </div>
          )}
          <details className="session-history">
            <summary>
              Otras entregas de esta sesión{" "}
              <span>
                {runs.length} en total · selecciona una para explorarla
              </span>
            </summary>
            <Deliveries runs={runs} selected={run?.id} onSelect={setSelected} />
          </details>
        </section>
        <details className="system-details">
          <summary>
            Estado del laboratorio{" "}
            <span>
              {online ? "Conectado" : "Sin conexión"} · métricas técnicas
            </span>
          </summary>
          <section className="stats-strip" aria-label="Métricas actuales">
            <div>
              <Layers3 size={17} />
              <span>
                En cola
                <strong>
                  {stats?.queue ?? "—"}
                  <small>/ {stats?.queue_limit ?? 200}</small>
                </strong>
              </span>
            </div>
            <div>
              <CheckCheck size={17} />
              <span>
                Entregas correctas<strong>{stats?.success ?? "—"}</strong>
              </span>
            </div>
            <div>
              <RotateCcw size={17} />
              <span>
                Antigüedad de cola
                <strong>
                  {stats ? stats.oldest.toFixed(1) : "—"}
                  <small>s</small>
                </strong>
              </span>
            </div>
            <div>
              <Radio size={17} />
              <span>
                Worker
                <strong>
                  {stats?.worker === 1 ? "Activo" : "—"}
                  <small>×1</small>
                </strong>
              </span>
              <span className={`connection ${online ? "" : "offline"}`}>
                <i />
                {online ? "Conectado" : "Sin conexión"}
              </span>
            </div>
          </section>
        </details>
        <footer>
          <span>
            <ShieldCheck size={13} />
            Credenciales en el servidor. Datos del laboratorio aislados.
          </span>
          <span>
            {run && currentScenario
              ? scenarioDescriptions[currentScenario.id].name
              : "HookRelay · Cola persistente de entregas"}
            <span className="footer-dot">·</span>
            {snapshot?.observed_at
              ? "Última observación " +
                new Date(snapshot.observed_at).toLocaleTimeString("es", {
                  hour12: false,
                })
              : "Conectando…"}
          </span>
        </footer>
      </main>
    </div>
  );
}
