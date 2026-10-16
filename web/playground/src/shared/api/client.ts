export type ScenarioId =
  | "success"
  | "slow"
  | "temporary"
  | "rate_limit"
  | "drop"
  | "exhausted";
export interface Scenario {
  id: ScenarioId;
  name: string;
  description: string;
}
export interface TimelineEvent {
  at: number;
  kind: string;
  title: string;
  detail: string;
  source: "api" | "receiver" | "observation";
}
export interface Receipt {
  at: number;
  attempt: number;
  processed: boolean;
  phase: string;
  code: number;
}
export interface Attempt {
  number: number;
  cycle: number;
  http_status: number;
  error: string;
  completed_unix_ms: number;
}
export interface Run {
  id: string;
  event_id: string;
  scenario: ScenarioId;
  event_type: string;
  created: number;
  status: "PENDING" | "PROCESSING" | "RETRYING" | "SUCCESS" | "DEAD";
  cycle: number;
  attempts: number;
  http_status: number;
  error?: string;
  observed_at: number;
  next_estimate?: number;
  history: Attempt[];
  receipts: Receipt[];
  timeline: TimelineEvent[];
}
export interface Snapshot {
  runs: Run[];
  scenarios: Scenario[];
  stats: {
    queue: number;
    queue_limit: number;
    oldest: number;
    events: number;
    bytes: number;
    success: number;
    dead: number;
    worker: number;
  };
  observed_at: number;
  error?: string;
}
async function request<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch("/play/api/" + path, {
    method: body === undefined ? "GET" : "POST",
    headers:
      body === undefined
        ? {}
        : { "Content-Type": "application/json", "X-Playground-Request": "1" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const value = await response.json();
  if (!response.ok)
    throw new Error(value.error || "No se pudo completar la operación");
  return value;
}
export const api = {
  state: () => request<Snapshot>("state"),
  publish: (
    scenario: ScenarioId,
    event_type: string,
    payload: unknown,
    count: number,
  ) =>
    request<{ ids: string[] }>("publish", {
      scenario,
      event_type,
      payload,
      count,
    }),
  replay: (run: Run) =>
    request<{ status: string }>("replay/" + run.id, {
      expected_cycle: run.cycle,
    }),
};
