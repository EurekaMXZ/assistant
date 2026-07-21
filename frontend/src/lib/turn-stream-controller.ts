import type { TurnStreamFrame } from "./api-schemas";
import type { Turn, TurnStreamDone } from "./types";

export type TurnStreamConnectionState =
  | "connecting"
  | "streaming"
  | "reconnecting"
  | "settling"
  | "disconnected"
  | "completed"
  | "cancelled"
  | "failed";

export interface TurnStreamControllerOptions {
  turnId: string;
  signal: AbortSignal;
  openStream: (signal: AbortSignal) => AsyncIterable<TurnStreamFrame>;
  getTurn: (turnId: string) => Promise<Turn>;
  onEvent: (frame: TurnStreamFrame) => void;
  onStateChange?: (state: TurnStreamConnectionState) => void;
  wait?: (delayMs: number, signal: AbortSignal) => Promise<void>;
  maxReconnects?: number;
  baseDelayMs?: number;
  shouldReconnect?: (error: unknown) => boolean;
}

export type TurnStreamControllerResult =
  { kind: "terminal"; done: TurnStreamDone } | { kind: "active"; error: Error };

export function reconnectDelay(attempt: number, baseDelayMs = 500, maxDelayMs = 4_000) {
  return Math.min(maxDelayMs, baseDelayMs * 2 ** attempt);
}

export function waitForReconnect(delayMs: number, signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    const timer = window.setTimeout(resolve, delayMs);
    signal.addEventListener(
      "abort",
      () => {
        window.clearTimeout(timer);
        reject(new DOMException("Aborted", "AbortError"));
      },
      { once: true },
    );
  });
}

export async function runTurnStreamController({
  turnId,
  signal,
  openStream,
  getTurn,
  onEvent,
  onStateChange,
  wait = waitForReconnect,
  maxReconnects = 4,
  baseDelayMs = 500,
  shouldReconnect = () => true,
}: TurnStreamControllerOptions): Promise<TurnStreamControllerResult> {
  let lastError: Error | null = null;
  for (let attempt = 0; attempt <= maxReconnects && !signal.aborted; attempt += 1) {
    onStateChange?.(attempt === 0 ? "connecting" : "reconnecting");
    try {
      let terminal: TurnStreamDone | null = null;
      for await (const frame of openStream(signal)) {
        onStateChange?.("streaming");
        onEvent(frame);
        if (frame.event === "turn.done") terminal = frame.data;
      }
      if (terminal) {
        onStateChange?.(
          terminal.status === "completed"
            ? "completed"
            : terminal.status === "cancelled"
              ? "cancelled"
              : "failed",
        );
        return { kind: "terminal", done: terminal };
      }
      lastError = new Error("流式连接在任务完成前已关闭");
    } catch (error) {
      if ((error as Error).name === "AbortError") throw error;
      lastError = error instanceof Error ? error : new Error("流式连接失败");
      if (!shouldReconnect(error)) break;
    }
    if (attempt < maxReconnects) {
      await wait(reconnectDelay(attempt, baseDelayMs), signal);
    }
  }

  if (signal.aborted) throw new DOMException("Aborted", "AbortError");
  onStateChange?.("settling");
  const turn = await getTurn(turnId);
  if (turn.status === "completed" || turn.status === "failed" || turn.status === "cancelled") {
    const done: TurnStreamDone = {
      turn_id: turn.id,
      conversation_id: turn.conversation_id,
      status: turn.status,
      ...(turn.error_code ? { error_code: turn.error_code } : {}),
      ...(turn.error_message ? { error: turn.error_message } : {}),
    };
    onEvent({ event: "turn.done", data: done });
    onStateChange?.(
      turn.status === "completed"
        ? "completed"
        : turn.status === "cancelled"
          ? "cancelled"
          : "failed",
    );
    return { kind: "terminal", done };
  }
  const error = new Error(`${lastError?.message || "流式连接失败"}，任务仍在处理中，请重试连接`);
  onStateChange?.("disconnected");
  return { kind: "active", error };
}
