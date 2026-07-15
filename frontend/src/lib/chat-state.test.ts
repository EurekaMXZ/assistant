import { describe, expect, it } from "vitest";
import {
  applyAssistantTimelineSnapshot,
  assistantTextMessageId,
  assistantTimelineThinkingState,
  ensureStreamingThinkingMessage,
  thinkingMessageId,
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

  it("restores thinking after commentary when a snapshot has later activity", () => {
    const messages = ensureStreamingThinkingMessage([userMessage], "turn-1", "conversation-1");
    const snapshot: TimelineItem[] = [
      {
        id: "commentary-1",
        type: "output_text",
        status: "completed",
        content_text: "Checking sources.",
        created_at: "2026-01-01T00:00:01Z",
      },
      {
        id: "reasoning-1",
        type: "reasoning",
        status: "streaming",
        content_text: "Comparing results.",
        created_at: "2026-01-01T00:00:02Z",
      },
    ];

    const restored = applyAssistantTimelineSnapshot(messages, "turn-1", "conversation-1", snapshot);
    expect(restored.map((message) => message.id)).toEqual([
      userMessage.id,
      assistantTextMessageId("turn-1", "commentary-1"),
      thinkingMessageId("turn-1"),
    ]);
  });

  it("keeps final output after the restored thinking position", () => {
    const messages = ensureStreamingThinkingMessage([userMessage], "turn-1", "conversation-1");
    const snapshot: TimelineItem[] = [
      {
        id: "commentary-1",
        type: "output_text",
        status: "completed",
        content_text: "Checking sources.",
        metadata: { phase: "commentary" },
        created_at: "2026-01-01T00:00:01Z",
      },
      {
        id: "reasoning-1",
        type: "reasoning",
        status: "completed",
        content_text: "Compared results.",
        created_at: "2026-01-01T00:00:02Z",
      },
      {
        id: "final-1",
        type: "output_text",
        status: "completed",
        content_text: "Final answer.",
        metadata: { phase: "final_answer" },
        created_at: "2026-01-01T00:00:03Z",
      },
    ];

    const restored = applyAssistantTimelineSnapshot(messages, "turn-1", "conversation-1", snapshot);
    expect(restored.map((message) => message.id)).toEqual([
      userMessage.id,
      assistantTextMessageId("turn-1", "commentary-1"),
      thinkingMessageId("turn-1"),
      assistantTextMessageId("turn-1", "final-1"),
    ]);
  });

  it("restores a trailing unphased completed output as pending", () => {
    const snapshot: TimelineItem[] = [
      {
        id: "output-1",
        type: "output_text",
        status: "completed",
        content_text: "Checking sources.",
        created_at: "2026-01-01T00:00:01Z",
      },
    ];

    expect(assistantTimelineThinkingState("turn-1", snapshot)).toEqual({
      afterMessageId: null,
      pendingMessageId: assistantTextMessageId("turn-1", "output-1"),
    });
  });
});
