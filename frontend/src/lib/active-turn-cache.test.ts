import { beforeEach, describe, expect, it } from "vitest";
import {
  clearActiveTurnSnapshot,
  readActiveTurnSnapshot,
  writeActiveTurnSnapshot,
} from "./active-turn-cache";
import type { TurnStreamSnapshot } from "./types";

const snapshot: TurnStreamSnapshot = {
  turn_id: "turn-1",
  conversation_id: "conversation-1",
  status: "processing",
  items: [
    {
      id: "assistant:response-1:0:0",
      type: "output_text",
      status: "streaming",
      content_text: "Before refresh",
      created_at: "2026-07-24T12:00:00Z",
    },
  ],
};

describe("active turn cache", () => {
  beforeEach(() => {
    clearActiveTurnSnapshot("conversation-1", "turn-1");
    window.sessionStorage.clear();
  });

  it("restores the active snapshot for the matching conversation and turn", () => {
    writeActiveTurnSnapshot(snapshot);

    expect(readActiveTurnSnapshot("conversation-1", "turn-1")).toEqual(snapshot);
    expect(readActiveTurnSnapshot("conversation-2", "turn-1")).toBeNull();
  });

  it("drops invalid cached data", () => {
    window.sessionStorage.setItem(
      "assistant_active_turn_snapshot:conversation-1:turn-1",
      JSON.stringify({ turn_id: "turn-1", conversation_id: "conversation-1" }),
    );

    expect(readActiveTurnSnapshot("conversation-1", "turn-1")).toBeNull();
    expect(
      window.sessionStorage.getItem("assistant_active_turn_snapshot:conversation-1:turn-1"),
    ).toBeNull();
  });
});
