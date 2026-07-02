import type { PendingHomeTurn } from "./pending-home-turn";
import type { Message, TimelineItem } from "./types";
import { isAssistantOutputItem } from "./turn-stream-events";

export function thinkingMessageId(turnId: string) {
  return `thinking-${turnId}`;
}

export function assistantTextMessageId(turnId: string, timelineItemId: string) {
  return `assistant-${turnId}-${timelineItemId}`;
}

function assistantErrorMessageId(turnId: string) {
  return `assistant-${turnId}-error`;
}

function buildAssistantMessage(
  turnId: string,
  conversationId: string,
  id: string,
  metadata: Record<string, unknown>,
  content = "",
): Message {
  return {
    id,
    conversation_id: conversationId,
    turn_id: turnId,
    seq: 0,
    role: "assistant",
    content_text: content,
    token_count: 0,
    metadata,
    created_at: new Date().toISOString(),
  };
}

export function buildThinkingMessage(turnId: string, conversationId: string) {
  return buildAssistantMessage(
    turnId,
    conversationId,
    thinkingMessageId(turnId),
    { display_kind: "thinking" },
  );
}

export function upsertTurnFailureMessage(
  messages: Message[],
  turnId: string,
  conversationId: string,
  error?: string,
  errorCode?: string,
) {
  const id = assistantErrorMessageId(turnId);
  const content = error?.trim() || "Request failed";
  const index = messages.findIndex((message) => message.id === id);
  if (index !== -1) {
    return messages.map((message, messageIndex) =>
      messageIndex === index
        ? {
            ...message,
            content_text: content,
            metadata: { ...message.metadata, error_code: errorCode },
          }
        : message,
    );
  }

  const next = [...messages];
  const lastTurnIndex = next.findLastIndex((message) => message.turn_id === turnId);
  next.splice(
    lastTurnIndex === -1 ? next.length : lastTurnIndex + 1,
    0,
    buildAssistantMessage(
      turnId,
      conversationId,
      id,
      { display_kind: "assistant_error", status: "failed", error_code: errorCode },
      content,
    ),
  );
  return next;
}

export function ensurePendingHomeTurnMessages(messages: Message[], pending: PendingHomeTurn) {
  const next = messages.some((message) => message.id === pending.message.id)
    ? [...messages]
    : [...messages, pending.message];
  return ensureStreamingThinkingMessage(next, pending.turn.id, pending.conversation_id);
}

export function ensureStreamingThinkingMessage(
  messages: Message[],
  turnId: string | null,
  conversationId: string,
) {
  if (!turnId || messages.some((message) => message.id === thinkingMessageId(turnId))) {
    return messages;
  }
  const next = [...messages];
  const marker = buildThinkingMessage(turnId, conversationId);
  const firstAssistantIndex = next.findIndex(
    (message) => message.turn_id === turnId && message.role === "assistant",
  );
  if (firstAssistantIndex !== -1) {
    next.splice(firstAssistantIndex, 0, marker);
    return next;
  }
  const lastTurnIndex = next.findLastIndex((message) => message.turn_id === turnId);
  if (lastTurnIndex !== -1) {
    next.splice(lastTurnIndex + 1, 0, marker);
    return next;
  }
  return [...next, marker];
}

export function upsertAssistantTextContent(
  messages: Message[],
  turnId: string,
  conversationId: string,
  timelineItemId: string,
  nextContent: string,
  mode: "append" | "replace",
) {
  const id = assistantTextMessageId(turnId, timelineItemId);
  const index = messages.findIndex((message) => message.id === id);
  if (index !== -1) {
    return messages.map((message, messageIndex) =>
      messageIndex === index
        ? {
            ...message,
            content_text: mode === "append"
              ? `${message.content_text || ""}${nextContent}`
              : nextContent,
          }
        : message,
    );
  }

  const next = [...messages];
  const lastTurnIndex = next.findLastIndex((message) => message.turn_id === turnId);
  next.splice(
    lastTurnIndex === -1 ? next.length : lastTurnIndex + 1,
    0,
    buildAssistantMessage(
      turnId,
      conversationId,
      id,
      { display_kind: "assistant_text", timeline_item_id: timelineItemId },
      nextContent,
    ),
  );
  return next;
}

export function moveThinkingAfter(messages: Message[], turnId: string, afterMessageId: string) {
  const markerId = thinkingMessageId(turnId);
  const marker = messages.find((message) => message.id === markerId);
  if (!marker) return messages;
  const withoutMarker = messages.filter((message) => message.id !== markerId);
  const targetIndex = withoutMarker.findIndex((message) => message.id === afterMessageId);
  if (targetIndex === -1) return messages;
  const next = [...withoutMarker];
  next.splice(targetIndex + 1, 0, marker);
  return next;
}

export function assistantOutputPhase(item: TimelineItem) {
  const phase = item.metadata?.phase;
  return phase === "commentary" || phase === "final_answer" ? phase : null;
}

export function applyAssistantTimelineSnapshot(
  messages: Message[],
  turnId: string,
  conversationId: string,
  items: TimelineItem[],
) {
  const assistantItemIds = new Set(items.filter(isAssistantOutputItem).map((item) => item.id));
  const retained = messages.filter(
    (message) =>
      message.turn_id !== turnId ||
      message.metadata?.display_kind !== "assistant_text" ||
      (typeof message.metadata?.timeline_item_id === "string" &&
        assistantItemIds.has(message.metadata.timeline_item_id)),
  );
  return items.reduce(
    (next, item) =>
      isAssistantOutputItem(item) && item.content_text != null
        ? upsertAssistantTextContent(
            next,
            turnId,
            conversationId,
            item.id,
            item.content_text,
            "replace",
          )
        : next,
    retained,
  );
}

export function statusTextFromItem(item: TimelineItem): string | null {
  if (item.type === "reasoning") return "模型思考中…";
  if (item.type === "tool_call") {
    return item.title?.trim() ? `调用 ${item.title.trim()}…` : "工具调用中…";
  }
  if (item.type === "status") return item.content_text?.trim() || null;
  return null;
}
