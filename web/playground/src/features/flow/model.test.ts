import { describe, it, expect } from "vitest";
import { initialPlayback, playbackReducer, pointFor, targetFor } from "./model";
import type { Run } from "../../shared/api/client";
describe("reproducción visual", () => {
  it("pausa sin avanzar y respeta la velocidad", () => {
    const paused = playbackReducer(initialPlayback, { type: "toggle" });
    expect(
      playbackReducer(paused, { type: "tick", delta: 420, target: 6 }).position,
    ).toBe(0);
    const fast = playbackReducer(initialPlayback, { type: "speed", speed: 2 });
    const normal = playbackReducer(initialPlayback, { type: "tick", delta: 420, target: 6 }).position;
    expect(normal).toBeGreaterThan(0);
    expect(
      playbackReducer(fast, { type: "tick", delta: 420, target: 6 }).position,
    ).toBe(normal * 2);
  });
  it("no pasa del estado observado", () => {
    expect(
      playbackReducer(initialPlayback, {
        type: "tick",
        delta: 9000,
        target: 3.7,
      }).position,
    ).toBe(3.7);
  });
  it("el retorno llega a la cola y no a una entrega exitosa inventada", () => {
    expect(pointFor(9)).toEqual({ x: 670, y: 83 });
    expect(
      targetFor({ status: "PENDING", receipts: [] } as unknown as Run),
    ).toBe(3.7);
  });
  it("un nuevo intento no despausa ni reinicia una reproducción", () => {
    const paused = { ...initialPlayback, playing: false, position: 4 };
    expect(playbackReducer(paused, { type: "attempt" })).toEqual(paused);
    const replay = { ...initialPlayback, mode: "replay" as const, position: 2 };
    expect(playbackReducer(replay, { type: "attempt" })).toEqual(replay);
  });
  it("reproducción solo cambia el modelo visual", () => {
    expect(
      playbackReducer({ ...initialPlayback, position: 6 }, { type: "replay" }),
    ).toEqual({ ...initialPlayback, mode: "replay" });
  });
});
