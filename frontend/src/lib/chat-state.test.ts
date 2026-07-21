import { describe, expect, it } from "vitest";
import {
  applyAssistantTimelineSnapshot,
  assistantTextMessageId,
  assistantTimelineThinkingState,
  ensureStreamingThinkingMessage,
  thinkingMessageId,
  upsertAssistantInteraction,
  upsertAssistantTextContent,
} from "./chat-state";
import type { InteractionTimelineItem, Message, TimelineItem } from "./types";

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

const pendingInteraction: InteractionTimelineItem = {
  id: "ask-user:tool-1",
  type: "interaction",
  status: "awaiting_input",
  tool_call_id: "tool-1",
  prompt: "Continue?",
  kind: "single_choice",
  options: [
    { id: "yes", label: "Yes", tone: "primary" },
    { id: "cancel", label: "Cancel", tone: "neutral" },
  ],
  created_at: "2026-01-01T00:00:01Z",
};

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

  it("projects one stable interaction message and hides thinking while awaiting input", () => {
    const streaming = ensureStreamingThinkingMessage([userMessage], "turn-1", "conversation-1");
    const once = upsertAssistantInteraction(
      streaming,
      "turn-1",
      "conversation-1",
      pendingInteraction,
    );
    const twice = upsertAssistantInteraction(once, "turn-1", "conversation-1", pendingInteraction);

    expect(twice.filter((message) => message.id === pendingInteraction.id)).toHaveLength(1);
    expect(twice.some((message) => message.id === thinkingMessageId("turn-1"))).toBe(false);
    expect(twice.at(-1)).toMatchObject({
      id: pendingInteraction.id,
      turn_id: "turn-1",
      metadata: { display_kind: "ask_user", interaction: pendingInteraction },
    });
  });

  it("keeps completed interaction state across duplicate and stale snapshots", () => {
    const completedInteraction: InteractionTimelineItem = {
      ...pendingInteraction,
      status: "completed",
      answer: {
        status: "answered",
        option_id: "yes",
        label: "Yes",
        user_reported: true,
      },
    };
    const completed = applyAssistantTimelineSnapshot([userMessage], "turn-1", "conversation-1", [
      completedInteraction,
    ]);
    const stale = applyAssistantTimelineSnapshot(completed, "turn-1", "conversation-1", [
      pendingInteraction,
    ]);
    const resumed = ensureStreamingThinkingMessage(stale, "turn-1", "conversation-1");

    expect(stale.filter((message) => message.id === pendingInteraction.id)).toHaveLength(1);
    expect(stale.at(-1)?.metadata.interaction).toMatchObject({ status: "completed" });
    expect(resumed.map((message) => message.id)).toEqual([
      userMessage.id,
      pendingInteraction.id,
      thinkingMessageId("turn-1"),
    ]);
  });

  it("keeps cancelled interaction state across a stale awaiting snapshot", () => {
    const cancelledInteraction: InteractionTimelineItem = {
      ...pendingInteraction,
      status: "cancelled",
      answer: {
        status: "cancelled",
        option_id: "cancelled",
        label: "已取消",
        user_reported: false,
      },
    };
    const cancelled = upsertAssistantInteraction(
      [userMessage],
      "turn-1",
      "conversation-1",
      cancelledInteraction,
    );
    const stale = upsertAssistantInteraction(
      cancelled,
      "turn-1",
      "conversation-1",
      pendingInteraction,
    );
    expect(stale.at(-1)?.metadata.interaction).toMatchObject({
      status: "cancelled",
      answer: { option_id: "cancelled" },
    });
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
      awaitingInput: false,
      pendingMessageId: assistantTextMessageId("turn-1", "output-1"),
    });
  });
});
