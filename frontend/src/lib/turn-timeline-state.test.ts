import { describe, expect, it } from "vitest";
import { createTurnTimelineState, transitionTurnTimelineState } from "./turn-timeline-state";
import type { Turn } from "./types";

const turn = {
  id: "turn-1",
  conversation_id: "conversation-1",
  status: "processing",
  metadata: {},
} as Turn;

describe("turn timeline state transitions", () => {
  it("treats snapshots as authoritative and filters assistant output from the timeline", () => {
    const initialized = transitionTurnTimelineState(createTurnTimelineState(), {
      type: "initialize-turns",
      turns: [turn],
    }).state;
    const result = transitionTurnTimelineState(initialized, {
      type: "snapshot",
      turnId: turn.id,
      snapshot: {
        turn_id: turn.id,
        conversation_id: turn.conversation_id,
        status: "completed",
        started_at: "2026-01-01T00:00:00Z",
        completed_at: "2026-01-01T00:00:12Z",
        items: [
          {
            id: "reasoning-1",
            type: "reasoning",
            content_text: "**Inspecting**\n\nFull reasoning body.",
            metadata: { sequence_number: 4 },
            created_at: "2026-01-01T00:00:00Z",
          },
          {
            id: "output-1",
            type: "output_text",
            content_text: "Answer",
            metadata: { sequence_number: 5 },
            created_at: "2026-01-01T00:00:01Z",
          },
        ],
      },
    });

    expect(result.state.timelines[turn.id].items.map((item) => item.id)).toEqual(["reasoning-1"]);
    expect(result.state.turnsById[turn.id].status).toBe("completed");
    expect(result.state.turnsById[turn.id]).toMatchObject({
      started_at: "2026-01-01T00:00:00Z",
      completed_at: "2026-01-01T00:00:12Z",
    });
    expect(result.state.timelines[turn.id].items[0].content_text).toBe(
      "**Inspecting**\n\nFull reasoning body.",
    );
    expect(result.state.itemSequences["turn-1:output-1"]).toBe(5);
  });

  it("rejects duplicate and out-of-order item sequences", () => {
    const first = transitionTurnTimelineState(createTurnTimelineState(), {
      type: "item-delta",
      turnId: turn.id,
      conversationId: turn.conversation_id,
      delta: {
        item_id: "reasoning-1",
        item_type: "reasoning",
        delta: "first",
        sequence_number: 2,
        created_at: "2026-01-01T00:00:00Z",
      },
    });
    const duplicate = transitionTurnTimelineState(first.state, {
      type: "item-delta",
      turnId: turn.id,
      conversationId: turn.conversation_id,
      delta: {
        item_id: "reasoning-1",
        item_type: "reasoning",
        delta: "duplicate",
        sequence_number: 2,
        created_at: "2026-01-01T00:00:01Z",
      },
    });

    expect(duplicate.accepted).toBe(false);
    expect(duplicate.state).toBe(first.state);
    expect(first.state.timelines[turn.id].items[0].content_text).toBe("first");
  });

  it("accepts the first output delta after its item declaration", () => {
    const declaration = transitionTurnTimelineState(createTurnTimelineState(), {
      type: "item-upsert",
      turnId: turn.id,
      conversationId: turn.conversation_id,
      item: {
        id: "output-1",
        type: "output_text",
        status: "streaming",
        content_text: "",
        created_at: "2026-01-01T00:00:00Z",
      },
    });
    const first = transitionTurnTimelineState(declaration.state, {
      type: "item-delta",
      turnId: turn.id,
      conversationId: turn.conversation_id,
      delta: {
        item_id: "output-1",
        item_type: "output_text",
        delta: "#",
        sequence_number: 7,
        created_at: "2026-01-01T00:00:01Z",
      },
    });
    const duplicate = transitionTurnTimelineState(first.state, {
      type: "item-delta",
      turnId: turn.id,
      conversationId: turn.conversation_id,
      delta: {
        item_id: "output-1",
        item_type: "output_text",
        delta: "#",
        sequence_number: 7,
        created_at: "2026-01-01T00:00:02Z",
      },
    });

    expect(declaration.accepted).toBe(true);
    expect(first.accepted).toBe(true);
    expect(first.state.itemSequences["turn-1:output-1"]).toBe(7);
    expect(duplicate.accepted).toBe(false);
  });

  it("keeps the turn record as the single terminal status owner", () => {
    const initialized = transitionTurnTimelineState(createTurnTimelineState(), {
      type: "register-turn",
      turn,
    }).state;
    const completed = transitionTurnTimelineState(initialized, {
      type: "turn-done",
      turnId: turn.id,
      done: {
        turn_id: turn.id,
        status: "failed",
        error_code: "provider_error",
        error: "upstream failed",
      },
    }).state;

    expect(completed.turnsById[turn.id]).toMatchObject({
      status: "failed",
      error_code: "provider_error",
      error_message: "upstream failed",
    });
  });
});
