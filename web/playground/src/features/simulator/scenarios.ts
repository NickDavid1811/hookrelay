import type { ScenarioId } from "../../shared/api/client";
export const scenarioDescriptions: Record<ScenarioId, { name: string; description: string }> = {
    success: { name: "Éxito a la primera", description: "El receptor acepta el evento sin fallos." },
    slow: { name: "Respuesta lenta", description: "El receptor tarda 1,5 segundos en responder." },
    temporary: { name: "Fallo temporal", description: "Falla dos veces y acepta el tercer intento." },
    rate_limit: { name: "Pide esperar", description: "Pide al menos 3 segundos antes de reintentar." },
    drop: { name: "Respuesta perdida", description: "Procesa el evento, pero su respuesta no llega." },
    exhausted: { name: "Siempre falla", description: "Agota los cinco intentos y se detiene." },
  };
