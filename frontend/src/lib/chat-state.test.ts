import { describe, expect, it } from "vitest";
import {
  applyAssistantTimelineSnapshot,
  ensureStreamingThinkingMessage,
  upsertAssistantTextContent,
} from "./chat-state";
import type { Message, TimelineItem } from "./types";

const userMessage = {
  id: "message-1",
  conversation_id: "conversation-1",
  turn_id: "turn-1",
  seq: 1,
  role: "user",
  content_text: "Hello",
  metadata: {},
  created_at: "2026-01-01T00:00:00Z",
} as Message;

describe("chat state transformations", () => {
  it("inserts one thinking marker per turn", () => {
    const once = ensureStreamingThinkingMessage([userMessage], "turn-1", "conversation-1");
    const twice = ensureStreamingThinkingMessage(once, "turn-1", "conversation-1");
    expect(twice.filter((message) => message.metadata.display_kind === "thinking")).toHaveLength(1);
  });

  it("appends and then authoritatively replaces assistant output", () => {
    const appended = upsertAssistantTextContent(
      [userMessage],
      "turn-1",
      "conversation-1",
      "output-1",
      "Hel",
      "append",
    );
    const snapshot: TimelineItem[] = [
      {
        id: "output-1",
        type: "output_text",
        content_text: "Hello",
        created_at: "2026-01-01T00:00:01Z",
      },
    ];
    const replaced = applyAssistantTimelineSnapshot(appended, "turn-1", "conversation-1", snapshot);
    expect(replaced.at(-1)?.content_text).toBe("Hello");
  });
});
