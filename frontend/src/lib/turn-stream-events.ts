import type {
  SseFrame,
  TimelineItem,
  TurnStreamDone,
  TurnStreamItemDelta,
  TurnStreamSnapshot,
} from "./types";
import { parseTurnStreamFrame } from "./api-schemas";

export interface ConversationPresentationUpdate {
  conversation_id: string;
  title?: string | null;
}

export interface TurnStreamDispatchContext {
  onConversationUpdated(update: ConversationPresentationUpdate): void;
  onItemDelta(delta: TurnStreamItemDelta): void;
  onItemDone(item: TimelineItem): void;
  onItemUpsert(item: TimelineItem): void;
  onSnapshot(snapshot: TurnStreamSnapshot): void;
  onTurnDone(done: TurnStreamDone): void;
}

type TurnStreamEventHandler = (context: TurnStreamDispatchContext, data: unknown) => void;

class TurnStreamEventRegistry {
  private readonly handlers = new Map<string, TurnStreamEventHandler>();

  register(event: string, handler: TurnStreamEventHandler) {
    this.handlers.set(event, handler);
    return this;
  }

  dispatch(context: TurnStreamDispatchContext, frame: SseFrame) {
    const parsed = parseTurnStreamFrame(frame.event, frame.data);
    if (parsed) this.handlers.get(parsed.event)?.(context, parsed.data);
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function timelineItem(value: unknown): TimelineItem | null {
  if (!isRecord(value) || typeof value.id !== "string" || typeof value.type !== "string") {
    return null;
  }
  return value as unknown as TimelineItem;
}

const turnStreamEvents = new TurnStreamEventRegistry()
  .register("turn.snapshot", (context, data) => {
    if (!isRecord(data) || typeof data.turn_id !== "string" || !Array.isArray(data.items)) {
      return;
    }
    context.onSnapshot(data as unknown as TurnStreamSnapshot);
  })
  .register("item.upsert", (context, data) => {
    const item = timelineItem(data);
    if (item) context.onItemUpsert(item);
  })
  .register("item.delta", (context, data) => {
    if (
      !isRecord(data) ||
      typeof data.item_id !== "string" ||
      typeof data.item_type !== "string" ||
      typeof data.delta !== "string" ||
      (typeof data.sequence_number !== "undefined" && typeof data.sequence_number !== "number")
    ) {
      return;
    }
    context.onItemDelta(data as unknown as TurnStreamItemDelta);
  })
  .register("item.done", (context, data) => {
    const item = timelineItem(data);
    if (item) context.onItemDone(item);
  })
  .register("turn.done", (context, data) => {
    if (!isRecord(data) || typeof data.turn_id !== "string" || typeof data.status !== "string") {
      return;
    }
    context.onTurnDone(data as unknown as TurnStreamDone);
  })
  .register("conversation.updated", (context, data) => {
    if (!isRecord(data) || typeof data.conversation_id !== "string") {
      return;
    }
    context.onConversationUpdated(data as unknown as ConversationPresentationUpdate);
  });

export function dispatchTurnStreamEvent(context: TurnStreamDispatchContext, frame: SseFrame) {
  turnStreamEvents.dispatch(context, frame);
}

export function isAssistantOutputType(itemType: string) {
  return itemType === "output_text";
}

export function isAssistantOutputItem(item: TimelineItem) {
  return isAssistantOutputType(item.type);
}

export function isTimelineItem(item: TimelineItem) {
  if (isAssistantOutputItem(item)) return false;
  return !(item.type === "tool_call" && item.title?.trim() === "conversation.rename_title");
}

export function upsertTimelineItem(items: TimelineItem[], item: TimelineItem) {
  const index = items.findIndex((candidate) => candidate.id === item.id);
  if (index === -1) return [...items, item];
  const next = [...items];
  next[index] = { ...next[index], ...item };
  return next;
}

export function appendTimelineDelta(items: TimelineItem[], delta: TurnStreamItemDelta) {
  const index = items.findIndex((candidate) => candidate.id === delta.item_id);
  if (index === -1) {
    return [
      ...items,
      {
        id: delta.item_id,
        type: delta.item_type,
        status: "streaming",
        content_text: delta.delta,
        metadata:
          typeof delta.sequence_number === "number"
            ? { sequence_number: delta.sequence_number }
            : undefined,
        created_at: delta.created_at,
      },
    ];
  }
  const previousSequence = items[index].metadata?.sequence_number;
  if (
    typeof delta.sequence_number === "number" &&
    typeof previousSequence === "number" &&
    delta.sequence_number <= previousSequence
  ) {
    return items;
  }
  const next = [...items];
  next[index] = {
    ...next[index],
    content_text: `${next[index].content_text || ""}${delta.delta}`,
    metadata: {
      ...next[index].metadata,
      ...(typeof delta.sequence_number === "number"
        ? { sequence_number: delta.sequence_number }
        : {}),
    },
  };
  return next;
}
