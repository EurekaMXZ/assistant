import type {
  TimelineItem,
  InteractionTimelineItem,
  TurnStreamDone,
  TurnStreamItemDelta,
  TurnStreamSnapshot,
} from "./types";
import type { KnownTurnStreamEvent, TurnStreamFrame } from "./api-schemas";

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

type TurnStreamEventData<Event extends KnownTurnStreamEvent> = Extract<
  TurnStreamFrame,
  { event: Event }
>["data"];

export interface TurnStreamEventHandler {
  readonly eventTypes: readonly KnownTurnStreamEvent[];
  handle(context: TurnStreamDispatchContext, data: unknown): void;
}

export function createTurnStreamEventHandler<Event extends KnownTurnStreamEvent>(
  eventType: Event,
  handle: (context: TurnStreamDispatchContext, data: TurnStreamEventData<Event>) => void,
): TurnStreamEventHandler {
  return {
    eventTypes: [eventType],
    handle(context, data) {
      handle(context, data as TurnStreamEventData<Event>);
    },
  };
}

export class TurnStreamEventChain {
  private readonly handlers: readonly TurnStreamEventHandler[];

  constructor(handlers: readonly TurnStreamEventHandler[]) {
    const registered = new Set<KnownTurnStreamEvent>();
    for (const handler of handlers) {
      for (const eventType of handler.eventTypes) {
        if (registered.has(eventType)) {
          throw new Error(`Duplicate turn stream event handler: ${eventType}`);
        }
        registered.add(eventType);
      }
    }
    this.handlers = [...handlers];
  }

  dispatch(context: TurnStreamDispatchContext, frame: TurnStreamFrame) {
    for (const handler of this.handlers) {
      if (!handler.eventTypes.includes(frame.event)) continue;
      handler.handle(context, frame.data);
      return true;
    }
    return false;
  }
}

const turnStreamEvents = new TurnStreamEventChain([
  createTurnStreamEventHandler("turn.snapshot", (context, data) => context.onSnapshot(data)),
  createTurnStreamEventHandler("item.upsert", (context, data) => context.onItemUpsert(data)),
  createTurnStreamEventHandler("item.delta", (context, data) => context.onItemDelta(data)),
  createTurnStreamEventHandler("item.done", (context, data) => context.onItemDone(data)),
  createTurnStreamEventHandler("turn.done", (context, data) => context.onTurnDone(data)),
  createTurnStreamEventHandler("conversation.updated", (context, data) =>
    context.onConversationUpdated(data),
  ),
]);

export function dispatchTurnStreamEvent(
  context: TurnStreamDispatchContext,
  frame: TurnStreamFrame,
) {
  return turnStreamEvents.dispatch(context, frame);
}

export function isAssistantOutputType(itemType: string) {
  return itemType === "output_text";
}

export function isAssistantOutputItem(item: TimelineItem) {
  return isAssistantOutputType(item.type);
}

export function isAssistantInteractionType(itemType: string) {
  return itemType === "interaction";
}

export function isAssistantInteractionItem(item: TimelineItem): item is InteractionTimelineItem {
  return (
    isAssistantInteractionType(item.type) &&
    typeof item.tool_call_id === "string" &&
    typeof item.prompt === "string" &&
    typeof item.kind === "string" &&
    Array.isArray(item.options) &&
    (item.status === "awaiting_input" || item.status === "completed" || item.status === "cancelled")
  );
}

export function isAssistantImageItem(item: TimelineItem) {
  return item.type === "image_generation" && item.image != null;
}

export function isTimelineItem(item: TimelineItem) {
  if (isAssistantOutputItem(item) || isAssistantInteractionItem(item)) return false;
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
