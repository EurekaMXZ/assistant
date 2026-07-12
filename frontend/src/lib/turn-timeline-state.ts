import type {
  Timeline,
  TimelineItem,
  Turn,
  TurnStreamDone,
  TurnStreamItemDelta,
  TurnStreamSnapshot,
} from "./types";
import {
  appendTimelineDelta,
  isAssistantOutputType,
  isTimelineItem,
  upsertTimelineItem,
} from "./turn-stream-events";

export interface TurnTimelineState {
  timelines: Record<string, Timeline>;
  turnsById: Record<string, Turn>;
  loading: Record<string, boolean>;
  errors: Record<string, string | null>;
  itemSequences: Record<string, number>;
}

export type TurnTimelineAction =
  | { type: "reset" }
  | { type: "initialize-turns"; turns: Turn[] }
  | { type: "register-turn"; turn: Turn }
  | { type: "set-loading"; turnId: string; loading: boolean }
  | { type: "set-error"; turnId: string; error: string | null }
  | { type: "snapshot"; turnId: string; snapshot: TurnStreamSnapshot }
  | { type: "item-upsert"; turnId: string; conversationId: string; item: TimelineItem }
  | { type: "item-delta"; turnId: string; conversationId: string; delta: TurnStreamItemDelta }
  | { type: "item-done"; turnId: string; conversationId: string; item: TimelineItem }
  | { type: "turn-done"; turnId: string; done: TurnStreamDone };

export interface TurnTimelineTransition {
  accepted: boolean;
  state: TurnTimelineState;
}

export function createTurnTimelineState(): TurnTimelineState {
  return {
    timelines: {},
    turnsById: {},
    loading: {},
    errors: {},
    itemSequences: {},
  };
}

function sequenceKey(turnId: string, itemId: string) {
  return `${turnId}:${itemId}`;
}

function recordSequence(
  state: TurnTimelineState,
  turnId: string,
  itemId: string,
  sequence: unknown,
) {
  if (typeof sequence !== "number") {
    return { accepted: true, itemSequences: state.itemSequences };
  }
  const key = sequenceKey(turnId, itemId);
  const previous = state.itemSequences[key];
  if (typeof previous === "number" && sequence <= previous) {
    return { accepted: false, itemSequences: state.itemSequences };
  }
  return {
    accepted: true,
    itemSequences: { ...state.itemSequences, [key]: sequence },
  };
}

function currentTimeline(state: TurnTimelineState, turnId: string, conversationId: string) {
  return (
    state.timelines[turnId] || {
      turn_id: turnId,
      conversation_id: conversationId,
      status: "processing" as const,
      items: [],
    }
  );
}

function updateItem(
  state: TurnTimelineState,
  turnId: string,
  conversationId: string,
  item: TimelineItem,
) {
  if (!isTimelineItem(item)) return state.timelines;
  const timeline = currentTimeline(state, turnId, conversationId);
  return {
    ...state.timelines,
    [turnId]: {
      ...timeline,
      items: upsertTimelineItem(timeline.items, item),
    },
  };
}

export function transitionTurnTimelineState(
  state: TurnTimelineState,
  action: TurnTimelineAction,
): TurnTimelineTransition {
  if (action.type === "reset") {
    return { accepted: true, state: createTurnTimelineState() };
  }
  if (action.type === "initialize-turns") {
    return {
      accepted: true,
      state: {
        ...state,
        turnsById: Object.fromEntries(action.turns.map((turn) => [turn.id, turn])),
      },
    };
  }
  if (action.type === "register-turn") {
    return {
      accepted: true,
      state: {
        ...state,
        turnsById: { ...state.turnsById, [action.turn.id]: action.turn },
      },
    };
  }
  if (action.type === "set-loading") {
    return {
      accepted: true,
      state: {
        ...state,
        loading: { ...state.loading, [action.turnId]: action.loading },
      },
    };
  }
  if (action.type === "set-error") {
    return {
      accepted: true,
      state: {
        ...state,
        errors: { ...state.errors, [action.turnId]: action.error },
      },
    };
  }
  if (action.type === "snapshot") {
    const prefix = `${action.turnId}:`;
    const itemSequences = Object.fromEntries(
      Object.entries(state.itemSequences).filter(([key]) => !key.startsWith(prefix)),
    );
    for (const item of action.snapshot.items) {
      const sequence = item.metadata?.sequence_number;
      if (typeof sequence === "number") {
        itemSequences[sequenceKey(action.turnId, item.id)] = sequence;
      }
    }
    const turn = state.turnsById[action.turnId];
    return {
      accepted: true,
      state: {
        ...state,
        itemSequences,
        timelines: {
          ...state.timelines,
          [action.turnId]: {
            turn_id: action.snapshot.turn_id,
            conversation_id: action.snapshot.conversation_id,
            status: action.snapshot.status,
            items: action.snapshot.items.filter(isTimelineItem),
          },
        },
        turnsById: turn
          ? {
              ...state.turnsById,
              [action.turnId]: { ...turn, status: action.snapshot.status },
            }
          : state.turnsById,
        loading: { ...state.loading, [action.turnId]: false },
        errors: { ...state.errors, [action.turnId]: null },
      },
    };
  }
  if (action.type === "turn-done") {
    const timeline = state.timelines[action.turnId];
    const turn = state.turnsById[action.turnId];
    return {
      accepted: true,
      state: {
        ...state,
        timelines: timeline
          ? {
              ...state.timelines,
              [action.turnId]: { ...timeline, status: action.done.status },
            }
          : state.timelines,
        turnsById: turn
          ? {
              ...state.turnsById,
              [action.turnId]: {
                ...turn,
                status: action.done.status,
                error_code: action.done.error_code,
                error_message: action.done.error,
              },
            }
          : state.turnsById,
      },
    };
  }

  const itemId = action.type === "item-delta" ? action.delta.item_id : action.item.id;
  const sequence =
    action.type === "item-delta"
      ? action.delta.sequence_number
      : action.item.metadata?.sequence_number;
  const recorded = recordSequence(state, action.turnId, itemId, sequence);
  if (!recorded.accepted) return { accepted: false, state };

  if (action.type === "item-delta") {
    if (isAssistantOutputType(action.delta.item_type)) {
      return {
        accepted: true,
        state: { ...state, itemSequences: recorded.itemSequences },
      };
    }
    const timeline = currentTimeline(state, action.turnId, action.conversationId);
    return {
      accepted: true,
      state: {
        ...state,
        itemSequences: recorded.itemSequences,
        timelines: {
          ...state.timelines,
          [action.turnId]: {
            ...timeline,
            items: appendTimelineDelta(timeline.items, action.delta),
          },
        },
      },
    };
  }

  return {
    accepted: true,
    state: {
      ...state,
      itemSequences: recorded.itemSequences,
      timelines: updateItem(state, action.turnId, action.conversationId, action.item),
    },
  };
}
