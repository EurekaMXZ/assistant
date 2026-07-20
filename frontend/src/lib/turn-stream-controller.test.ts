import { describe, expect, it, vi } from "vitest";
import type { TurnStreamFrame } from "./api-schemas";
import { runTurnStreamController } from "./turn-stream-controller";
import type { Turn } from "./types";

async function* frames(values: TurnStreamFrame[]) {
  for (const value of values) yield value;
}

const processingTurn = {
  id: "turn-1",
  conversation_id: "conversation-1",
  status: "processing",
} as Turn;

describe("turn stream reconnect", () => {
  it("uses bounded exponential reconnect and succeeds after a clean disconnect", async () => {
    let attempt = 0;
    const wait = vi.fn(async () => undefined);
    const result = await runTurnStreamController({
      turnId: "turn-1",
      signal: new AbortController().signal,
      openStream: () => {
        attempt += 1;
        return frames(
          attempt === 1
            ? []
            : [{ event: "turn.done", data: { turn_id: "turn-1", status: "completed" } }],
        );
      },
      getTurn: async () => processingTurn,
      onEvent: () => undefined,
      wait,
    });
    expect(result.kind).toBe("terminal");
    expect(wait).toHaveBeenCalledWith(500, expect.any(AbortSignal));
  });

  it("reconciles terminal turn state after reconnect exhaustion", async () => {
    const events: TurnStreamFrame[] = [];
    const result = await runTurnStreamController({
      turnId: "turn-1",
      signal: new AbortController().signal,
      openStream: () => frames([]),
      getTurn: async () => ({
        ...processingTurn,
        status: "failed",
        error_message: "upstream failed",
      }),
      onEvent: (event) => events.push(event),
      wait: async () => undefined,
      maxReconnects: 1,
    });
    expect(result).toMatchObject({ kind: "terminal", done: { status: "failed" } });
    expect(events.at(-1)?.event).toBe("turn.done");
  });

  it("reconciles a cancelled turn after the stream disconnects", async () => {
    const events: TurnStreamFrame[] = [];
    const result = await runTurnStreamController({
      turnId: "turn-1",
      signal: new AbortController().signal,
      openStream: () => frames([]),
      getTurn: async () => ({
        ...processingTurn,
        status: "cancelled",
      }),
      onEvent: (event) => events.push(event),
      wait: async () => undefined,
      maxReconnects: 0,
    });
    expect(result).toMatchObject({ kind: "terminal", done: { status: "cancelled" } });
    expect(events.at(-1)?.event).toBe("turn.done");
  });

  it("returns a retryable result when the turn remains nonterminal", async () => {
    const result = await runTurnStreamController({
      turnId: "turn-1",
      signal: new AbortController().signal,
      openStream: () => frames([]),
      getTurn: async () => processingTurn,
      onEvent: () => undefined,
      wait: async () => undefined,
      maxReconnects: 0,
    });
    expect(result.kind).toBe("retryable");
  });
});
