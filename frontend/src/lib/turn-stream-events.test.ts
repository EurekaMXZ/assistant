import { describe, expect, it, vi } from "vitest";
import {
  TurnStreamEventChain,
  createTurnStreamEventHandler,
  dispatchTurnStreamEvent,
  type TurnStreamDispatchContext,
} from "./turn-stream-events";

function dispatchContext(
  overrides: Partial<TurnStreamDispatchContext> = {},
): TurnStreamDispatchContext {
  return {
    onConversationUpdated: () => undefined,
    onItemDelta: () => undefined,
    onItemDone: () => undefined,
    onItemUpsert: () => undefined,
    onSnapshot: () => undefined,
    onTurnDone: () => undefined,
    ...overrides,
  };
}

describe("turn stream event chain", () => {
  it("dispatches every delta through the chain", () => {
    let output = "";
    const context = dispatchContext({
      onItemDelta: (delta) => {
        output += delta.delta;
      },
    });

    dispatchTurnStreamEvent(context, {
      event: "item.delta",
      data: {
        item_id: "output-1",
        item_type: "output_text",
        delta: "Hel",
        sequence_number: 1,
        created_at: "2026-01-01T00:00:00Z",
      },
    });
    dispatchTurnStreamEvent(context, {
      event: "item.delta",
      data: {
        item_id: "output-1",
        item_type: "output_text",
        delta: "lo",
        sequence_number: 2,
        created_at: "2026-01-01T00:00:01Z",
      },
    });

    expect(output).toBe("Hello");
  });

  it("invokes only the handler registered for the current event", () => {
    const delta = vi.fn();
    const done = vi.fn();
    const chain = new TurnStreamEventChain([
      createTurnStreamEventHandler("item.delta", delta),
      createTurnStreamEventHandler("turn.done", done),
    ]);

    const handled = chain.dispatch(dispatchContext(), {
      event: "item.delta",
      data: {
        item_id: "output-1",
        item_type: "output_text",
        delta: "text",
        created_at: "2026-01-01T00:00:00Z",
      },
    });

    expect(handled).toBe(true);
    expect(delta).toHaveBeenCalledOnce();
    expect(done).not.toHaveBeenCalled();
  });

  it("rejects duplicate event registrations", () => {
    expect(
      () =>
        new TurnStreamEventChain([
          createTurnStreamEventHandler("item.delta", () => undefined),
          createTurnStreamEventHandler("item.delta", () => undefined),
        ]),
    ).toThrow("Duplicate turn stream event handler: item.delta");
  });
});
