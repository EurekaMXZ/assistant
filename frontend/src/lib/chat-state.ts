import type { PendingHomeTurn } from "./pending-home-turn";
import type { InteractionTimelineItem, Message, TimelineItem } from "./types";
import {
  isAssistantInteractionItem,
  isAssistantOutputItem,
  isTimelineItem,
} from "./turn-stream-events";

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
  createdAt = new Date().toISOString(),
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
    created_at: createdAt,
  };
}

export function buildThinkingMessage(turnId: string, conversationId: string) {
  return buildAssistantMessage(turnId, conversationId, thinkingMessageId(turnId), {
    display_kind: "thinking",
  });
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
  if (!turnId) {
    return messages;
  }
  const interactionMessages = messages.filter(
    (message) => message.turn_id === turnId && assistantInteractionFromMessage(message),
  );
  const latestInteraction = interactionMessages.at(-1);
  if (
    latestInteraction &&
    assistantInteractionFromMessage(latestInteraction)?.status === "awaiting_input"
  ) {
    return removeThinkingMessage(messages, turnId);
  }
  if (messages.some((message) => message.id === thinkingMessageId(turnId))) return messages;

  const next = [...messages];
  const marker = buildThinkingMessage(turnId, conversationId);
  if (latestInteraction) {
    const interactionIndex = next.findIndex((message) => message.id === latestInteraction.id);
    next.splice(interactionIndex + 1, 0, marker);
    return next;
  }
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

export function removeThinkingMessage(messages: Message[], turnId: string) {
  const markerId = thinkingMessageId(turnId);
  return messages.some((message) => message.id === markerId)
    ? messages.filter((message) => message.id !== markerId)
    : messages;
}

export function assistantInteractionFromMessage(message: Message) {
  if (message.metadata?.display_kind !== "ask_user") return null;
  const interaction = message.metadata.interaction;
  if (!interaction || typeof interaction !== "object" || Array.isArray(interaction)) return null;
  const candidate = interaction as Partial<InteractionTimelineItem>;
  return candidate.type === "interaction" &&
    typeof candidate.id === "string" &&
    typeof candidate.tool_call_id === "string" &&
    typeof candidate.prompt === "string" &&
    Array.isArray(candidate.options) &&
    (candidate.status === "awaiting_input" ||
      candidate.status === "completed" ||
      candidate.status === "cancelled")
    ? (candidate as InteractionTimelineItem)
    : null;
}

export function upsertAssistantInteraction(
  messages: Message[],
  turnId: string,
  conversationId: string,
  interaction: InteractionTimelineItem,
) {
  const index = messages.findIndex((message) => message.id === interaction.id);
  if (index !== -1) {
    const existing = assistantInteractionFromMessage(messages[index]);
    if (
      (existing?.status === "completed" || existing?.status === "cancelled") &&
      interaction.status === "awaiting_input"
    ) {
      return messages;
    }
    const updated = messages.map((message, messageIndex) =>
      messageIndex === index
        ? {
            ...message,
            metadata: { ...message.metadata, display_kind: "ask_user", interaction },
          }
        : message,
    );
    return interaction.status === "awaiting_input"
      ? removeThinkingMessage(updated, turnId)
      : updated;
  }

  const next = [...messages];
  const lastTurnIndex = next.findLastIndex((message) => message.turn_id === turnId);
  next.splice(
    lastTurnIndex === -1 ? next.length : lastTurnIndex + 1,
    0,
    buildAssistantMessage(
      turnId,
      conversationId,
      interaction.id,
      { display_kind: "ask_user", interaction },
      "",
      interaction.created_at,
    ),
  );
  return interaction.status === "awaiting_input" ? removeThinkingMessage(next, turnId) : next;
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
            content_text:
              mode === "append" ? `${message.content_text || ""}${nextContent}` : nextContent,
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

export function assistantTimelineThinkingState(turnId: string, items: TimelineItem[]) {
  let afterMessageId: string | null = null;
  let pendingMessageId: string | null = null;
  let awaitingInput = false;

  items.forEach((item, index) => {
    if (isAssistantInteractionItem(item)) {
      pendingMessageId = null;
      if (item.status === "awaiting_input") {
        awaitingInput = true;
        return;
      }
      awaitingInput = false;
      afterMessageId = item.id;
      return;
    }
    if (!isAssistantOutputItem(item) || item.status !== "completed" || item.content_text == null) {
      return;
    }

    awaitingInput = false;

    const messageId = assistantTextMessageId(turnId, item.id);
    const phase = assistantOutputPhase(item);
    if (phase === "commentary") {
      afterMessageId = messageId;
      pendingMessageId = null;
      return;
    }
    if (phase === "final_answer") {
      pendingMessageId = null;
      return;
    }

    const hasContinuation = items
      .slice(index + 1)
      .some(
        (next) =>
          isAssistantOutputItem(next) || isAssistantInteractionItem(next) || isTimelineItem(next),
      );
    if (hasContinuation) {
      afterMessageId = messageId;
      pendingMessageId = null;
      return;
    }
    pendingMessageId = messageId;
  });

  return { afterMessageId, awaitingInput, pendingMessageId };
}

export function applyAssistantTimelineSnapshot(
  messages: Message[],
  turnId: string,
  conversationId: string,
  items: TimelineItem[],
) {
  const assistantTextItemIds = new Set(items.filter(isAssistantOutputItem).map((item) => item.id));
  const interactionItemIds = new Set(
    items.filter(isAssistantInteractionItem).map((item) => item.id),
  );
  const retained = messages.filter((message) => {
    if (message.turn_id !== turnId) return true;
    if (message.metadata?.display_kind === "assistant_text") {
      return (
        typeof message.metadata?.timeline_item_id === "string" &&
        assistantTextItemIds.has(message.metadata.timeline_item_id)
      );
    }
    if (message.metadata?.display_kind === "ask_user") {
      return interactionItemIds.has(message.id);
    }
    return true;
  });
  const projected = items.reduce((next, item) => {
    if (isAssistantInteractionItem(item)) {
      return upsertAssistantInteraction(next, turnId, conversationId, item);
    }
    return isAssistantOutputItem(item) && item.content_text != null
      ? upsertAssistantTextContent(
          next,
          turnId,
          conversationId,
          item.id,
          item.content_text,
          "replace",
        )
      : next;
  }, retained);
  const { afterMessageId, awaitingInput } = assistantTimelineThinkingState(turnId, items);
  if (awaitingInput) return removeThinkingMessage(projected, turnId);
  return afterMessageId ? moveThinkingAfter(projected, turnId, afterMessageId) : projected;
}
