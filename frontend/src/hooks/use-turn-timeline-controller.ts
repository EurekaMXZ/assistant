"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import { getStreamUrl, getToken, getTurn, isSessionUnauthorizedError } from "@/lib/api";
import type { TurnStreamFrame } from "@/lib/api-schemas";
import {
  applyAssistantTimelineSnapshot,
  assistantOutputPhase,
  assistantTextMessageId,
  assistantTimelineThinkingState,
  moveThinkingAfter,
  upsertAssistantTextContent,
  upsertTurnFailureMessage,
} from "@/lib/chat-state";
import { streamEvents } from "@/lib/sse";
import {
  createTurnTimelineState,
  transitionTurnTimelineState,
  type TurnTimelineAction,
} from "@/lib/turn-timeline-state";
import { runTurnStreamController } from "@/lib/turn-stream-controller";
import {
  dispatchTurnStreamEvent,
  isAssistantOutputItem,
  isTimelineItem,
  type ConversationPresentationUpdate,
  type TurnStreamDispatchContext,
} from "@/lib/turn-stream-events";
import type { Message, TimelineItem, Turn, TurnStreamSnapshot } from "@/lib/types";
import { getTimelineTitle } from "@/components/chat/turn-timeline";

type TurnStreamMode = "active" | "panel" | "restore";

interface UseTurnTimelineControllerOptions {
  conversationId: string;
  setMessages: Dispatch<SetStateAction<Message[]>>;
  onConversationUpdated: (update: ConversationPresentationUpdate) => void;
}

export interface TurnTimelineController {
  timelineTurnId: string | null;
  turnTimelines: ReturnType<typeof createTurnTimelineState>["timelines"];
  turnsById: ReturnType<typeof createTurnTimelineState>["turnsById"];
  timelineLoading: ReturnType<typeof createTurnTimelineState>["loading"];
  timelineErrors: ReturnType<typeof createTurnTimelineState>["errors"];
  timelineActivityLabels: Record<string, string | null>;
  closeTimeline: () => void;
  dispatchActiveFrame: (frame: TurnStreamFrame, turnId: string) => void;
  finishActiveTurn: (turnId: string) => void;
  hydrateTurn: (turnId: string, mode: "panel" | "restore", fallbackTurn?: Turn) => Promise<void>;
  initializeTurns: (turns: Turn[]) => void;
  openTimeline: (turnId: string, activeTurnId: string | null) => void;
  registerTurn: (turn: Turn) => void;
}

export function useTurnTimelineController({
  conversationId,
  setMessages,
  onConversationUpdated,
}: UseTurnTimelineControllerOptions): TurnTimelineController {
  const [projection, setProjection] = useState(createTurnTimelineState);
  const projectionRef = useRef(projection);
  const [timelineTurnId, setTimelineTurnId] = useState<string | null>(null);
  const pendingDoneTextMessageIdRef = useRef<string | null>(null);
  const hydrationControllersRef = useRef(new Map<string, AbortController>());
  const activeConversationIdRef = useRef(conversationId);
  const mountedRef = useRef(true);

  const applyAction = useCallback((action: TurnTimelineAction) => {
    const transition = transitionTurnTimelineState(projectionRef.current, action);
    if (transition.state !== projectionRef.current) {
      projectionRef.current = transition.state;
      setProjection(transition.state);
    }
    return transition.accepted;
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    const controllers = hydrationControllersRef.current;
    return () => {
      mountedRef.current = false;
      for (const controller of controllers.values()) controller.abort();
      controllers.clear();
    };
  }, []);

  useEffect(() => {
    activeConversationIdRef.current = conversationId;
    for (const controller of hydrationControllersRef.current.values()) {
      controller.abort();
    }
    hydrationControllersRef.current.clear();
    pendingDoneTextMessageIdRef.current = null;
    const initial = createTurnTimelineState();
    projectionRef.current = initial;
    setProjection(initial);
    setTimelineTurnId(null);
  }, [conversationId]);

  const settlePendingThinkingForSnapshot = useCallback(
    (snapshot: TurnStreamSnapshot, turnId: string) => {
      const pendingMessageId = pendingDoneTextMessageIdRef.current;
      if (!pendingMessageId) return;

      const pendingIndex = snapshot.items.findIndex(
        (item) => pendingMessageId === assistantTextMessageId(turnId, item.id),
      );
      if (pendingIndex === -1) return;
      const pendingItem = snapshot.items[pendingIndex];
      const phase = assistantOutputPhase(pendingItem);
      if (phase) {
        pendingDoneTextMessageIdRef.current = null;
        if (phase === "commentary") {
          setMessages((previous) => moveThinkingAfter(previous, turnId, pendingMessageId));
        }
        return;
      }
      const hasContinuation = snapshot.items.slice(pendingIndex + 1).some((item) => {
        return isAssistantOutputItem(item) || isTimelineItem(item);
      });
      if (!hasContinuation) return;
      pendingDoneTextMessageIdRef.current = null;
      setMessages((previous) => moveThinkingAfter(previous, turnId, pendingMessageId));
    },
    [setMessages],
  );

  const settlePendingThinkingForItem = useCallback(
    (item: TimelineItem, turnId: string) => {
      const pendingMessageId = pendingDoneTextMessageIdRef.current;
      if (!pendingMessageId || (!isTimelineItem(item) && !isAssistantOutputItem(item))) return;

      if (pendingMessageId === assistantTextMessageId(turnId, item.id)) {
        const phase = assistantOutputPhase(item);
        if (!phase) return;
        pendingDoneTextMessageIdRef.current = null;
        if (phase === "commentary") {
          setMessages((previous) => moveThinkingAfter(previous, turnId, pendingMessageId));
        }
        return;
      }

      pendingDoneTextMessageIdRef.current = null;
      setMessages((previous) => moveThinkingAfter(previous, turnId, pendingMessageId));
    },
    [setMessages],
  );

  const settlePendingThinkingForOtherEvent = useCallback(
    (turnId: string, moveThinking: boolean) => {
      const pendingMessageId = pendingDoneTextMessageIdRef.current;
      if (!pendingMessageId) return;
      pendingDoneTextMessageIdRef.current = null;
      if (moveThinking) {
        setMessages((previous) => moveThinkingAfter(previous, turnId, pendingMessageId));
      }
    },
    [setMessages],
  );

  const createStreamContext = useCallback(
    (turnId: string, mode: TurnStreamMode): TurnStreamDispatchContext => {
      const mirrorMessages = mode !== "panel";
      return {
        onSnapshot(snapshot) {
          if (mirrorMessages) settlePendingThinkingForSnapshot(snapshot, turnId);
          applyAction({ type: "snapshot", turnId, snapshot });
          if (mirrorMessages) {
            const thinkingState = assistantTimelineThinkingState(turnId, snapshot.items);
            pendingDoneTextMessageIdRef.current =
              snapshot.status === "processing" ? thinkingState.pendingMessageId : null;
            setMessages((previous) =>
              applyAssistantTimelineSnapshot(previous, turnId, conversationId, snapshot.items),
            );
          }
        },
        onItemUpsert(item) {
          if (mirrorMessages) settlePendingThinkingForItem(item, turnId);
          const accepted = applyAction({
            type: "item-upsert",
            turnId,
            conversationId,
            item,
          });
          if (!accepted) return;
          const content = item.content_text;
          if (mirrorMessages && isAssistantOutputItem(item) && content != null) {
            setMessages((previous) =>
              upsertAssistantTextContent(
                previous,
                turnId,
                conversationId,
                item.id,
                content,
                "replace",
              ),
            );
          }
        },
        onItemDelta(delta) {
          if (mirrorMessages) settlePendingThinkingForOtherEvent(turnId, true);
          const accepted = applyAction({
            type: "item-delta",
            turnId,
            conversationId,
            delta,
          });
          if (!accepted) return;
          if (mirrorMessages && delta.item_type === "output_text") {
            setMessages((previous) =>
              upsertAssistantTextContent(
                previous,
                turnId,
                conversationId,
                delta.item_id,
                delta.delta,
                "append",
              ),
            );
          }
        },
        onItemDone(item) {
          if (mirrorMessages) settlePendingThinkingForItem(item, turnId);
          const accepted = applyAction({
            type: "item-done",
            turnId,
            conversationId,
            item,
          });
          if (!accepted) return;
          const content = item.content_text;
          if (mirrorMessages && isAssistantOutputItem(item) && content != null) {
            setMessages((previous) =>
              upsertAssistantTextContent(
                previous,
                turnId,
                conversationId,
                item.id,
                content,
                "replace",
              ),
            );
            if (mode === "active") {
              const messageId = assistantTextMessageId(turnId, item.id);
              const phase = assistantOutputPhase(item);
              pendingDoneTextMessageIdRef.current = phase ? null : messageId;
              if (phase === "commentary") {
                setMessages((previous) => moveThinkingAfter(previous, turnId, messageId));
              }
            }
          }
        },
        onTurnDone(done) {
          if (mirrorMessages) settlePendingThinkingForOtherEvent(turnId, false);
          applyAction({ type: "turn-done", turnId, done });
          if (mode === "active") pendingDoneTextMessageIdRef.current = null;
          if (mirrorMessages && done.status === "failed") {
            setMessages((previous) =>
              upsertTurnFailureMessage(
                previous,
                turnId,
                conversationId,
                done.error,
                done.error_code,
              ),
            );
          }
        },
        onConversationUpdated(update) {
          if (update.conversation_id === conversationId) {
            onConversationUpdated(update);
          }
        },
      };
    },
    [
      applyAction,
      conversationId,
      onConversationUpdated,
      setMessages,
      settlePendingThinkingForItem,
      settlePendingThinkingForOtherEvent,
      settlePendingThinkingForSnapshot,
    ],
  );

  const dispatchActiveFrame = useCallback(
    (frame: TurnStreamFrame, turnId: string) => {
      if (!mountedRef.current || activeConversationIdRef.current !== conversationId) {
        return;
      }
      dispatchTurnStreamEvent(createStreamContext(turnId, "active"), frame);
    },
    [conversationId, createStreamContext],
  );

  const hydrateTurn = useCallback(
    async (turnId: string, mode: "panel" | "restore", fallbackTurn?: Turn) => {
      const requestedConversationId = conversationId;
      if (hydrationControllersRef.current.has(turnId)) return;
      const controller = new AbortController();
      hydrationControllersRef.current.set(turnId, controller);
      applyAction({ type: "set-loading", turnId, loading: true });
      applyAction({ type: "set-error", turnId, error: null });

      try {
        const context = createStreamContext(turnId, mode);
        const result = await runTurnStreamController({
          turnId,
          signal: controller.signal,
          openStream: (signal) =>
            streamEvents(getStreamUrl(`/turns/${turnId}/stream`), getToken(), signal),
          getTurn,
          onEvent: (event) => {
            if (activeConversationIdRef.current === requestedConversationId) {
              dispatchTurnStreamEvent(context, event);
            }
          },
          shouldReconnect: (error) => !isSessionUnauthorizedError(error),
        });
        if (result.kind === "retryable") throw result.error;
      } catch (error) {
        if ((error as Error).name !== "AbortError" && !isSessionUnauthorizedError(error)) {
          applyAction({
            type: "set-error",
            turnId,
            error: error instanceof Error ? error.message : "加载失败",
          });
          if (mode === "restore" && fallbackTurn?.status === "failed") {
            setMessages((previous) => upsertTurnFailureMessage(previous, turnId, conversationId));
          }
        }
      } finally {
        if (hydrationControllersRef.current.get(turnId) === controller) {
          hydrationControllersRef.current.delete(turnId);
          applyAction({ type: "set-loading", turnId, loading: false });
        }
      }
    },
    [applyAction, conversationId, createStreamContext, setMessages],
  );

  const openTimeline = useCallback(
    (turnId: string, activeTurnId: string | null) => {
      setTimelineTurnId(turnId);
      if (turnId === activeTurnId) {
        if (!projection.timelines[turnId]) {
          applyAction({ type: "set-loading", turnId, loading: true });
        }
        return;
      }
      if (
        projection.errors[turnId] ||
        !projection.timelines[turnId] ||
        !["completed", "failed"].includes(projection.timelines[turnId].status)
      ) {
        void hydrateTurn(turnId, "panel");
      }
    },
    [applyAction, hydrateTurn, projection.errors, projection.timelines],
  );

  const initializeTurns = useCallback(
    (turns: Turn[]) => applyAction({ type: "initialize-turns", turns }),
    [applyAction],
  );
  const registerTurn = useCallback(
    (turn: Turn) => {
      pendingDoneTextMessageIdRef.current = null;
      applyAction({ type: "register-turn", turn });
    },
    [applyAction],
  );
  const finishActiveTurn = useCallback(
    (turnId: string) => {
      pendingDoneTextMessageIdRef.current = null;
      applyAction({ type: "set-loading", turnId, loading: false });
    },
    [applyAction],
  );
  const timelineActivityLabels = useMemo(() => {
    const labels: Record<string, string | null> = {};
    for (const [turnId, timeline] of Object.entries(projection.timelines)) {
      const latestItem = timeline.items.at(-1);
      labels[turnId] = latestItem ? getTimelineTitle(latestItem) : null;
    }
    return labels;
  }, [projection.timelines]);

  return {
    timelineTurnId,
    turnTimelines: projection.timelines,
    turnsById: projection.turnsById,
    timelineLoading: projection.loading,
    timelineErrors: projection.errors,
    timelineActivityLabels,
    closeTimeline: useCallback(() => setTimelineTurnId(null), []),
    dispatchActiveFrame,
    finishActiveTurn,
    hydrateTurn,
    initializeTurns,
    openTimeline,
    registerTurn,
  };
}
