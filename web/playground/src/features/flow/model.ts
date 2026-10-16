import type { Run } from "../../shared/api/client";
export interface Playback {
  position: number;
  playing: boolean;
  speed: number;
  mode: "live" | "replay";
}
export const initialPlayback: Playback = {
  position: 0,
  playing: true,
  speed: 1,
  mode: "live",
};
export type Action =
  | { type: "tick"; delta: number; target: number }
  | { type: "toggle" }
  | { type: "speed"; speed: number }
  | { type: "replay" }
  | { type: "reset"; position?: number }
  | { type: "attempt" };
export function playbackReducer(state: Playback, action: Action): Playback {
  switch (action.type) {
    case "tick": {
      if (!state.playing) return state;
      const position = Math.min(
        action.target,
        state.position + (action.delta * state.speed) / 1400,
      );
      return position === state.position ? state : { ...state, position };
    }
    case "toggle":
      return { ...state, playing: !state.playing };
    case "speed":
      return { ...state, speed: action.speed };
    case "replay":
      return { ...state, position: 0, playing: true, mode: "replay" };
    case "attempt":
      return state.playing && state.mode === "live"
        ? { ...state, position: 3.6 }
        : state;
    case "reset":
      return {
        ...initialPlayback,
        position: action.position ?? 0,
        speed: state.speed,
      };
  }
}
export function targetFor(run?: Run) {
  if (!run) return 0;
  if (run.status === "RETRYING" || run.status === "DEAD") return 9;
  if (run.status === "SUCCESS") return 6;
  if (run.receipts.length) return 5.65;
  if (run.status === "PROCESSING") return 4.8;
  return 3.7;
}
export function pointFor(position: number) {
  const x = [85, 280, 475, 670, 865, 1060];
  if (position <= 6) {
    const progress = Math.min(5, Math.max(0, (position / 6) * 5));
    const i = Math.min(4, Math.floor(progress));
    const t = progress - i;
    return { x: x[i] + (x[i + 1] - x[i]) * t, y: 83 };
  }
  const t = Math.min(1, (position - 6) / 3),
    u = 1 - t;
  return {
    x:
      u * u * u * 1060 +
      3 * u * u * t * 1160 +
      3 * u * t * t * 575 +
      t * t * t * 670,
    y:
      u * u * u * 83 +
      3 * u * u * t * 235 +
      3 * u * t * t * 235 +
      t * t * t * 83,
  };
}
