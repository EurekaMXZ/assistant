"use client";

import { useEffect, useState, useCallback, useMemo, useRef, useSyncExternalStore } from "react";
import {
  answerToolCall,
  createConversationShare,
  createMessage,
  cancelTurn,
  editTurn,
  getConversation,
  getConversationTurnHistory,
  getTurn,
  isSessionUnauthorizedError,
  listConversationTurnSummaries,
  listConversationEvents,
  patchConversation,
  retryTurn,
  uploadConversationAttachment,
} from "@/lib/api";
import { openAuthDialog } from "@/lib/auth-dialog-events";
import { useAuth } from "@/hooks/use-auth";
import { useComposerPreferences } from "@/hooks/use-composer-preferences";
import { useTurnStream } from "@/hooks/use-turn-stream";
import { useTurnTimelineController } from "@/hooks/use-turn-timeline-controller";
import { useMobileHeader } from "@/components/layout/mobile-header-context";
import { emitConversationUpdated, subscribeConversationUpdated } from "@/lib/conversation-events";
import { takePendingHomeTurn } from "@/lib/pending-home-turn";
import { createIdempotencyKey } from "@/lib/idempotency-key";
import {
  clearConversationShareOperation,
  readConversationShareOperation,
  writeConversationShareOperation,
  type ConversationShareOperation,
} from "@/lib/conversation-share-operation";
import {
  createConversationPageCache,
  invalidateConversationPageCache,
  loadCachedPage,
  type ConversationPageCache,
} from "@/lib/conversation-page-cache";
import type {
  AskUserInteraction,
  Conversation,
  ConversationShare,
  InteractionTimelineItem,
  Message,
  ConversationTurnSummary,
  Turn,
} from "@/lib/types";
import { turnSchema } from "@/lib/api-schemas";
import {
  assistantInteractionFromMessage,
  buildThinkingMessage,
  ensurePendingHomeTurnMessages,
  ensureStreamingThinkingMessage,
  isTurnFailureEvent,
  messagesFromConversationEvents,
  upsertAssistantInteraction,
} from "@/lib/chat-state";
import type { ComposerShellAttachment } from "@/components/chat/composer-shell";
import { Share } from "lucide-react";
import { Spinner } from "@/components/shared/spinner";
import { toast } from "sonner";
import { useMediaQuery } from "@/hooks/use-media-query";

function subscribeVisualViewport(onChange: () => void) {
  const viewport = window.visualViewport;
  if (!viewport) return () => undefined;
  viewport.addEventListener("resize", onChange);
  viewport.addEventListener("scroll", onChange);
  window.addEventListener("resize", onChange);
  return () => {
    viewport.removeEventListener("resize", onChange);
    viewport.removeEventListener("scroll", onChange);
    window.removeEventListener("resize", onChange);
  };
}

function getVisualViewportBottomInset() {
  const viewport = typeof window !== "undefined" ? window.visualViewport : null;
  if (!viewport) return 0;
  return Math.max(0, Math.round(window.innerHeight - viewport.height - viewport.offsetTop));
}

async function inspectUnresolvedTurns(
  messages: Message[],
  conversationId: string,
  eventTurns: Turn[] = [],
) {
  const assistantTurnIds = new Set(
    messages
      .filter(
        (message) =>
          message.role === "assistant" &&
          message.turn_id &&
          message.metadata?.display_kind !== "ask_user",
      )
      .map((message) => message.turn_id as string),
  );
  const turnIds = Array.from(
    new Set(messages.flatMap((message) => (message.turn_id ? [message.turn_id] : []))),
  );

  const knownTurns = new Map(eventTurns.map((turn) => [turn.id, turn]));
  const turns = await Promise.all(
    turnIds.map(async (turnId) => {
      const known = knownTurns.get(turnId);
      if (known && ["completed", "failed", "cancelled"].includes(known.status)) return known;
      try {
        return await getTurn(turnId);
      } catch {
        return known || null;
      }
    }),
  );

  let nextMessages = [...messages];
  let activeTurnId: string | null = null;
  const terminalTurns: Turn[] = [];
  for (const turn of turns) {
    if (!turn) continue;
    const unresolved = !assistantTurnIds.has(turn.id);
    if (
      ["accepted", "context_ready", "processing", "awaiting_input", "cancel_requested"].includes(
        turn.status,
      )
    ) {
      nextMessages = ensureStreamingThinkingMessage(nextMessages, turn.id, conversationId);
      activeTurnId = turn.id;
      continue;
    }
    if (unresolved && ["failed", "completed", "cancelled"].includes(turn.status)) {
      terminalTurns.push(turn);
    }
  }
  return {
    activeTurnId,
    messages: nextMessages,
    terminalTurns,
    turns: turns.filter((turn): turn is Turn => turn !== null),
  };
}

function turnsFromConversationEvents(
  events: Awaited<ReturnType<typeof listConversationEvents>>["events"],
): Turn[] {
  const turns = new Map<string, Turn>();
  for (const event of events) {
    const parsed = turnSchema.safeParse(event.payload.turn);
    if (parsed.success) {
      turns.set(parsed.data.id, parsed.data);
      continue;
    }
    if (!event.turn_id) continue;
    const existing = turns.get(event.turn_id);
    if (isTurnFailureEvent(event)) {
      const errorCode =
        typeof event.payload.error_code === "string" ? event.payload.error_code : undefined;
      const error = typeof event.payload.error === "string" ? event.payload.error : undefined;
      turns.set(event.turn_id, {
        ...(existing || {
          id: event.turn_id,
          conversation_id: event.conversation_id,
          seq: 0,
          metadata: {},
          created_at: event.created_at,
          updated_at: event.created_at,
        }),
        status: "failed",
        ...(errorCode ? { error_code: errorCode } : {}),
        ...(error ? { error_message: error } : {}),
        failed_at: event.created_at,
        updated_at: event.created_at,
      });
      continue;
    }
    if (!existing) continue;
    if (event.event_type === "turn.cancelled")
      turns.set(existing.id, { ...existing, status: "cancelled" });
  }
  return Array.from(turns.values());
}

function mergeMessages(...groups: Message[][]) {
  const messages = new Map<string, Message>();
  for (const group of groups) {
    for (const message of group) messages.set(message.id, message);
  }
  const merged = Array.from(messages.values());
  const firstTurnSeq = new Map<string, number>();
  for (const message of merged) {
    if (!message.turn_id || message.seq <= 0) continue;
    const current = firstTurnSeq.get(message.turn_id);
    if (typeof current === "undefined" || message.seq < current) {
      firstTurnSeq.set(message.turn_id, message.seq);
    }
  }
  return merged
    .map((message, index) => ({
      message,
      index,
      order:
        message.seq > 0
          ? message.seq
          : (firstTurnSeq.get(message.turn_id || "") ?? Number.MAX_SAFE_INTEGER) + 0.5,
    }))
    .sort((left, right) => left.order - right.order || left.index - right.index)
    .map(({ message }) => message);
}

function mergeTurnSummaries(...groups: ConversationTurnSummary[][]) {
  const summaries = new Map<string, ConversationTurnSummary>();
  for (const group of groups) {
    for (const summary of group) summaries.set(summary.id, summary);
  }
  return Array.from(summaries.values()).sort((left, right) => left.seq - right.seq);
}

function turnFromSummary(summary: ConversationTurnSummary): Turn {
  return {
    id: summary.id,
    conversation_id: summary.conversation_id,
    seq: summary.seq,
    ...(summary.retry_of_turn_id ? { retry_of_turn_id: summary.retry_of_turn_id } : {}),
    variant_index: summary.variant_index,
    status: summary.status,
    metadata: {},
    created_at: summary.created_at,
    updated_at: summary.updated_at,
  };
}

function turnSummaryFromTurn(turn: Turn, userMessage: Message): ConversationTurnSummary {
  return {
    id: turn.id,
    conversation_id: turn.conversation_id,
    seq: turn.seq,
    ...(turn.retry_of_turn_id ? { retry_of_turn_id: turn.retry_of_turn_id } : {}),
    variant_index: turn.variant_index || 1,
    status: turn.status,
    user_message: userMessage,
    created_at: turn.created_at,
    updated_at: turn.updated_at,
  };
}

type ConversationEventOptions = {
  limit?: number;
  before?: string;
  after?: string;
};

type ConversationTurnSummaryOptions = {
  limit?: number;
  before?: string;
  after?: string;
  query?: string;
};

type ConversationTurnHistoryOptions = {
  turnSeq?: number;
  before?: number;
  after?: number;
  beforeSeq?: string;
  afterSeq?: string;
};

function pageKey(prefix: string, options: Record<string, number | string | undefined>) {
  return `${prefix}:${Object.entries(options)
    .filter(([, value]) => typeof value !== "undefined")
    .map(([key, value]) => `${key}=${value}`)
    .join("&")}`;
}

function getConversationPageCache(
  caches: Map<string, ConversationPageCache>,
  conversationId: string,
) {
  const existing = caches.get(conversationId);
  if (existing) return existing;
  const created = createConversationPageCache();
  caches.set(conversationId, created);
  return created;
}

export function useChatController(conversationId: string) {
  const { user, isLoading: authLoading } = useAuth();
  const {
    setAction: setMobileAction,
    setTitle: setMobileTitle,
    setTitleAction: setMobileTitleAction,
  } = useMobileHeader();
  const isMobileViewport = useMediaQuery({ breakpoint: "md", range: "below" });
  const visualViewportBottomInset = useSyncExternalStore(
    subscribeVisualViewport,
    getVisualViewportBottomInset,
    () => 0,
  );
  const composerPreferences = useComposerPreferences(Boolean(user) && !authLoading);
  const composerInputRef = useRef<HTMLTextAreaElement>(null);
  const [conversation, setConversation] = useState<Conversation | null>(null);
  const [draft, setDraft] = useState("");
  const [messages, setMessages] = useState<Message[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [attachments, setAttachments] = useState<ComposerShellAttachment[]>([]);
  const [editingMessage, setEditingMessage] = useState<Message | null>(null);
  const [isSubmittingEdit, setIsSubmittingEdit] = useState(false);
  const [isCancelling, setIsCancelling] = useState(false);
  const [isLoadingOlderEvents, setIsLoadingOlderEvents] = useState(false);
  const [hasOlderEvents, setHasOlderEvents] = useState(false);
  const [olderEventCursor, setOlderEventCursor] = useState<string | null>(null);
  const [turnSummaries, setTurnSummaries] = useState<ConversationTurnSummary[]>([]);
  const [isLoadingTurnSummaries, setIsLoadingTurnSummaries] = useState(false);
  const [hasOlderTurnSummaries, setHasOlderTurnSummaries] = useState(false);
  const [olderTurnSummaryCursor, setOlderTurnSummaryCursor] = useState<string | null>(null);
  const [isLoadingTurnContext, setIsLoadingTurnContext] = useState(false);
  const [hasOlderTurnContext, setHasOlderTurnContext] = useState(false);
  const [hasNewerTurnContext, setHasNewerTurnContext] = useState(false);
  const [olderTurnContextCursor, setOlderTurnContextCursor] = useState<string | null>(null);
  const [newerTurnContextCursor, setNewerTurnContextCursor] = useState<string | null>(null);
  const [scrollTargetMessageId, setScrollTargetMessageId] = useState<string | null>(null);
  const [activeNavigationTurnId, setActiveNavigationTurnId] = useState<string | null>(null);
  const [resumeTurnId, setResumeTurnId] = useState<string | null>(null);
  const [restoreTurns, setRestoreTurns] = useState<Turn[]>([]);
  const [renameOpen, setRenameOpen] = useState(false);
  const [newTitle, setNewTitle] = useState("");
  const [shareOpen, setShareOpen] = useState(false);
  const [conversationShare, setConversationShare] = useState<ConversationShare | null>(null);
  const [isSharing, setIsSharing] = useState(false);
  const [composerHeight, setComposerHeight] = useState(208);
  const resumeConversationIdRef = useRef<string | null>(null);
  const restoreConversationIdRef = useRef<string | null>(null);
  const shareCacheRef = useRef(new Map<string, ConversationShare>());
  const conversationPageCachesRef = useRef(new Map<string, ConversationPageCache>());
  const activeConversationIdRef = useRef(conversationId);
  const mountedRef = useRef(true);
  const retryInFlightRef = useRef(false);
  const answerOperationKeysRef = useRef(new Map<string, string>());
  const editBackupRef = useRef<{
    draft: string;
    attachments: ComposerShellAttachment[];
  } | null>(null);
  const isUploadingAttachments = attachments.some(
    (attachment) => attachment.status === "uploading",
  );

  const loadConversationEvents = useCallback(
    (id: string, options: ConversationEventOptions = {}) => {
      const cache = getConversationPageCache(conversationPageCachesRef.current, id);
      return loadCachedPage(cache.events, pageKey("events", options), () =>
        listConversationEvents(id, options),
      );
    },
    [],
  );

  const loadConversationTurnSummaries = useCallback(
    (id: string, options: ConversationTurnSummaryOptions = {}) => {
      const cache = getConversationPageCache(conversationPageCachesRef.current, id);
      return loadCachedPage(cache.turnSummaries, pageKey("turns", options), () =>
        listConversationTurnSummaries(id, options),
      );
    },
    [],
  );

  const loadConversationTurnHistory = useCallback(
    (id: string, options: ConversationTurnHistoryOptions) => {
      const cache = getConversationPageCache(conversationPageCachesRef.current, id);
      return loadCachedPage(cache.turnHistory, pageKey("turn-history", options), () =>
        getConversationTurnHistory(id, options),
      );
    },
    [],
  );

  const invalidateConversationPages = useCallback((id: string) => {
    const cache = getConversationPageCache(conversationPageCachesRef.current, id);
    invalidateConversationPageCache(cache);
  }, []);

  const loadConversationMessages = useCallback(
    async (id: string) => {
      const eventPage = await loadConversationEvents(id);
      return {
        eventPage,
        messages: messagesFromConversationEvents(eventPage.events),
        turns: turnsFromConversationEvents(eventPage.events),
      };
    },
    [loadConversationEvents],
  );

  useEffect(() => {
    activeConversationIdRef.current = conversationId;
  }, [conversationId]);

  const handleStreamConversationUpdated = useCallback(
    (update: { conversation_id: string; title?: string | null }) => {
      if (typeof update.title !== "undefined") {
        setConversation((previous) =>
          previous ? { ...previous, title: update.title ?? undefined } : previous,
        );
        setNewTitle(update.title || "");
      }
      emitConversationUpdated({
        id: update.conversation_id,
        title: update.title,
      });
    },
    [],
  );

  const {
    timelineTurnId,
    turnTimelines,
    turnsById,
    timelineLoading,
    timelineErrors,
    timelineActivityLabels,
    closeTimeline,
    dispatchActiveFrame,
    finishActiveTurn,
    hydrateTurn,
    initializeTurns,
    openTimeline,
    registerTurn,
    restoreActiveTurn,
  } = useTurnTimelineController({
    conversationId,
    setMessages,
    onConversationUpdated: handleStreamConversationUpdated,
  });

  useEffect(() => {
    setMobileTitle(conversation?.id === conversationId ? conversation.title || "新会话" : "新会话");
  }, [conversation?.id, conversation?.title, conversationId, setMobileTitle]);

  useEffect(() => () => setMobileTitle("Assistant"), [setMobileTitle]);

  useEffect(() => {
    setMobileTitleAction({
      conversationId,
      label: "修改对话标题",
      onLongPress: () => setRenameOpen(true),
    });
    return () => setMobileTitleAction(null);
  }, [conversationId, setMobileTitleAction]);

  const refreshMessages = useCallback(
    async (completedTurnId: string) => {
      const requestedConversationId = conversationId;
      try {
        invalidateConversationPages(requestedConversationId);
        const nextMessages = await loadConversationMessages(requestedConversationId);
        if (!mountedRef.current || activeConversationIdRef.current !== requestedConversationId) {
          return;
        }
        setMessages((previous) =>
          mergeMessages(
            previous.filter((message) => message.turn_id !== completedTurnId),
            nextMessages.messages,
          ),
        );
      } catch (error) {
        if (!isSessionUnauthorizedError(error)) {
          toast.error(error instanceof Error ? error.message : "刷新消息失败");
        }
      }
    },
    [conversationId, invalidateConversationPages, loadConversationMessages],
  );

  const { isStreaming, streamingTurnId, streamConnectionState, streamTurn } = useTurnStream({
    conversationId,
    onCompleted: refreshMessages,
    onEvent: dispatchActiveFrame,
    onFinished: finishActiveTurn,
  });

  const handleAnswerInteraction = useCallback(
    async (turnId: string, interaction: AskUserInteraction, optionId: string) => {
      if (!user) {
        openAuthDialog("login");
        return false;
      }
      const requestedConversationId = conversationId;
      const operationId = `${interaction.id}:${optionId}`;
      const idempotencyKey =
        answerOperationKeysRef.current.get(operationId) || createIdempotencyKey();
      answerOperationKeysRef.current.set(operationId, idempotencyKey);
      try {
        const result = await answerToolCall(
          turnId,
          interaction.tool_call_id,
          optionId,
          idempotencyKey,
        );
        if (!mountedRef.current || activeConversationIdRef.current !== requestedConversationId) {
          return false;
        }
        setMessages((previous) => {
          const createdAt = previous
            .map(assistantInteractionFromMessage)
            .find((item) => item?.id === interaction.id)?.created_at;
          const completedItem: InteractionTimelineItem = {
            ...result.interaction,
            type: "interaction",
            created_at: createdAt || new Date().toISOString(),
          };
          return upsertAssistantInteraction(
            previous,
            turnId,
            requestedConversationId,
            completedItem,
          );
        });
        answerOperationKeysRef.current.delete(operationId);
        void streamTurn(turnId, result.stream_path);
        return true;
      } catch (error) {
        if (!isSessionUnauthorizedError(error)) {
          toast.error(error instanceof Error ? error.message : "提交选择失败");
        }
        return false;
      }
    },
    [conversationId, streamTurn, user],
  );

  const handleCancelGeneration = useCallback(async () => {
    if (!streamingTurnId || isCancelling) return;
    const turnId = streamingTurnId;
    setIsCancelling(true);
    try {
      await cancelTurn(turnId);
      void streamTurn(turnId);
    } catch (error) {
      setIsCancelling(false);
      if (!isSessionUnauthorizedError(error)) {
        toast.error(error instanceof Error ? error.message : "停止生成失败");
      }
    }
  }, [isCancelling, streamTurn, streamingTurnId]);

  useEffect(() => {
    if (!isStreaming) setIsCancelling(false);
  }, [isStreaming]);

  const load = useCallback(async () => {
    if (authLoading) {
      return;
    }

    if (!user) {
      setIsLoading(false);
      return;
    }

    const requestedConversationId = conversationId;
    setIsLoading(true);
    try {
      const [conv, history, turnPage] = await Promise.all([
        getConversation(conversationId),
        loadConversationMessages(conversationId),
        loadConversationTurnSummaries(conversationId, { limit: 200 }),
      ]);
      if (!mountedRef.current || activeConversationIdRef.current !== requestedConversationId)
        return;
      const pending = takePendingHomeTurn(conversationId);
      const loadedMessages = pending
        ? ensurePendingHomeTurnMessages(history.messages, pending)
        : history.messages;
      const restored = await inspectUnresolvedTurns(loadedMessages, conversationId, history.turns);
      if (activeConversationIdRef.current !== requestedConversationId) return;
      setConversation(conv);
      setMessages(restored.messages);
      setHasOlderEvents(history.eventPage.has_more_before);
      setOlderEventCursor(history.eventPage.next_before || null);
      setTurnSummaries(turnPage.turns);
      setHasOlderTurnSummaries(turnPage.has_more_before);
      setOlderTurnSummaryCursor(turnPage.next_before || null);
      setHasOlderTurnContext(false);
      setHasNewerTurnContext(false);
      setOlderTurnContextCursor(null);
      setNewerTurnContextCursor(null);
      setScrollTargetMessageId(null);
      setActiveNavigationTurnId(
        restored.activeTurnId || turnPage.turns.at(-1)?.id || history.turns.at(-1)?.id || null,
      );
      initializeTurns(restored.turns);
      resumeConversationIdRef.current =
        pending?.turn.id || restored.activeTurnId ? requestedConversationId : null;
      restoreConversationIdRef.current =
        restored.terminalTurns.length > 0 ? requestedConversationId : null;
      setResumeTurnId(pending?.turn.id || restored.activeTurnId);
      setRestoreTurns(restored.terminalTurns);
      setNewTitle(conv.title || "");
    } catch (err) {
      if (activeConversationIdRef.current !== requestedConversationId) return;
      if (isSessionUnauthorizedError(err)) {
        return;
      }
      toast.error(err instanceof Error ? err.message : "加载会话失败");
    } finally {
      if (activeConversationIdRef.current === requestedConversationId) {
        setIsLoading(false);
      }
    }
  }, [
    authLoading,
    conversationId,
    initializeTurns,
    loadConversationMessages,
    loadConversationTurnSummaries,
    user,
  ]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    return subscribeConversationUpdated((event) => {
      if (event.id !== conversationId) {
        return;
      }

      setConversation((prev) => {
        if (!prev) {
          return prev;
        }
        return {
          ...prev,
          ...(typeof event.title !== "undefined" ? { title: event.title ?? undefined } : {}),
          ...(typeof event.archived_at !== "undefined"
            ? { archived_at: event.archived_at ?? undefined }
            : {}),
        };
      });
      if (typeof event.title !== "undefined") {
        setNewTitle(event.title || "");
      }
    });
  }, [conversationId]);

  useEffect(() => {
    setConversation(null);
    setMessages([]);
    setAttachments([]);
    setEditingMessage(null);
    setIsSubmittingEdit(false);
    setIsLoadingOlderEvents(false);
    setHasOlderEvents(false);
    setOlderEventCursor(null);
    setTurnSummaries([]);
    setIsLoadingTurnSummaries(false);
    setHasOlderTurnSummaries(false);
    setOlderTurnSummaryCursor(null);
    setIsLoadingTurnContext(false);
    setHasOlderTurnContext(false);
    setHasNewerTurnContext(false);
    setOlderTurnContextCursor(null);
    setNewerTurnContextCursor(null);
    setScrollTargetMessageId(null);
    setActiveNavigationTurnId(null);
    setResumeTurnId(null);
    setRestoreTurns([]);
    setConversationShare(shareCacheRef.current.get(conversationId) || null);
    setShareOpen(false);
    setIsSharing(false);
    editBackupRef.current = null;
    answerOperationKeysRef.current.clear();
    resumeConversationIdRef.current = null;
    restoreConversationIdRef.current = null;
  }, [conversationId]);

  const handleLoadOlderEvents = useCallback(async () => {
    if (!hasOlderEvents || !olderEventCursor || isLoadingOlderEvents) return;
    const requestedConversationId = conversationId;
    setIsLoadingOlderEvents(true);
    try {
      const page = await loadConversationEvents(conversationId, { before: olderEventCursor });
      if (!mountedRef.current || activeConversationIdRef.current !== requestedConversationId) {
        return;
      }
      setMessages((previous) =>
        mergeMessages(messagesFromConversationEvents(page.events), previous),
      );
      for (const turn of turnsFromConversationEvents(page.events)) registerTurn(turn);
      setHasOlderEvents(page.has_more_before);
      setOlderEventCursor(page.next_before || null);
    } catch (error) {
      if (!isSessionUnauthorizedError(error)) {
        toast.error(error instanceof Error ? error.message : "加载更早消息失败");
      }
    } finally {
      if (activeConversationIdRef.current === requestedConversationId) {
        setIsLoadingOlderEvents(false);
      }
    }
  }, [
    conversationId,
    hasOlderEvents,
    isLoadingOlderEvents,
    loadConversationEvents,
    olderEventCursor,
    registerTurn,
  ]);

  const handleLoadOlderTurnSummaries = useCallback(async () => {
    if (!hasOlderTurnSummaries || !olderTurnSummaryCursor || isLoadingTurnSummaries) return;
    const requestedConversationId = conversationId;
    setIsLoadingTurnSummaries(true);
    try {
      const page = await loadConversationTurnSummaries(conversationId, {
        limit: 200,
        before: olderTurnSummaryCursor,
      });
      if (!mountedRef.current || activeConversationIdRef.current !== requestedConversationId)
        return;
      setTurnSummaries((previous) => mergeTurnSummaries(previous, page.turns));
      setHasOlderTurnSummaries(page.has_more_before);
      setOlderTurnSummaryCursor(page.next_before || null);
    } catch (error) {
      if (!isSessionUnauthorizedError(error)) {
        toast.error(error instanceof Error ? error.message : "加载更多 turn 失败");
      }
    } finally {
      if (activeConversationIdRef.current === requestedConversationId) {
        setIsLoadingTurnSummaries(false);
      }
    }
  }, [
    conversationId,
    hasOlderTurnSummaries,
    isLoadingTurnSummaries,
    loadConversationTurnSummaries,
    olderTurnSummaryCursor,
  ]);

  const handleJumpToTurn = useCallback(
    async (summary: ConversationTurnSummary) => {
      if (isLoadingTurnContext || isStreaming) return;
      const requestedConversationId = conversationId;
      setActiveNavigationTurnId(summary.id);
      const loadedTargetMessage = messages.find(
        (message) =>
          message.role === "user" &&
          (message.id === summary.user_message?.id || message.turn_id === summary.id),
      );
      if (loadedTargetMessage) {
        setScrollTargetMessageId(loadedTargetMessage.id);
        return;
      }
      setIsLoadingTurnContext(true);
      try {
        const page = await loadConversationTurnHistory(conversationId, {
          turnSeq: summary.seq,
          before: 3,
          after: 3,
        });
        if (!mountedRef.current || activeConversationIdRef.current !== requestedConversationId)
          return;
        const nextMessages = messagesFromConversationEvents(page.events);
        setMessages(nextMessages);
        for (const turn of page.turns) registerTurn(turnFromSummary(turn));
        setTurnSummaries((previous) => mergeTurnSummaries(previous, page.turns));
        setHasOlderEvents(false);
        setOlderEventCursor(null);
        setHasOlderTurnContext(page.has_more_before);
        setHasNewerTurnContext(page.has_more_after);
        setOlderTurnContextCursor(page.next_before || null);
        setNewerTurnContextCursor(page.next_after || null);
        const targetMessage =
          summary.user_message ||
          page.turns.find((turn) => turn.id === summary.id)?.user_message ||
          nextMessages.find((message) => message.turn_id === summary.id && message.role === "user");
        setScrollTargetMessageId(targetMessage?.id || null);
      } catch (error) {
        if (!isSessionUnauthorizedError(error)) {
          toast.error(error instanceof Error ? error.message : "定位 turn 失败");
        }
      } finally {
        if (activeConversationIdRef.current === requestedConversationId) {
          setIsLoadingTurnContext(false);
        }
      }
    },
    [
      conversationId,
      isLoadingTurnContext,
      isStreaming,
      loadConversationTurnHistory,
      messages,
      registerTurn,
    ],
  );

  const handleLoadOlderTurnContext = useCallback(async () => {
    if (!hasOlderTurnContext || !olderTurnContextCursor || isLoadingTurnContext) return;
    const requestedConversationId = conversationId;
    setIsLoadingTurnContext(true);
    try {
      const page = await loadConversationTurnHistory(conversationId, {
        before: 3,
        beforeSeq: olderTurnContextCursor,
      });
      if (!mountedRef.current || activeConversationIdRef.current !== requestedConversationId)
        return;
      setMessages((previous) =>
        mergeMessages(messagesFromConversationEvents(page.events), previous),
      );
      for (const turn of page.turns) registerTurn(turnFromSummary(turn));
      setTurnSummaries((previous) => mergeTurnSummaries(previous, page.turns));
      setHasOlderTurnContext(page.has_more_before);
      setOlderTurnContextCursor(page.next_before || null);
    } catch (error) {
      if (!isSessionUnauthorizedError(error)) {
        toast.error(error instanceof Error ? error.message : "加载更早 turn 失败");
      }
    } finally {
      if (activeConversationIdRef.current === requestedConversationId) {
        setIsLoadingTurnContext(false);
      }
    }
  }, [
    conversationId,
    hasOlderTurnContext,
    isLoadingTurnContext,
    loadConversationTurnHistory,
    olderTurnContextCursor,
    registerTurn,
  ]);

  const handleLoadNewerTurnContext = useCallback(async () => {
    if (!hasNewerTurnContext || !newerTurnContextCursor || isLoadingTurnContext) return;
    const requestedConversationId = conversationId;
    setIsLoadingTurnContext(true);
    try {
      const page = await loadConversationTurnHistory(conversationId, {
        after: 3,
        afterSeq: newerTurnContextCursor,
      });
      if (!mountedRef.current || activeConversationIdRef.current !== requestedConversationId)
        return;
      setMessages((previous) =>
        mergeMessages(previous, messagesFromConversationEvents(page.events)),
      );
      for (const turn of page.turns) registerTurn(turnFromSummary(turn));
      setTurnSummaries((previous) => mergeTurnSummaries(previous, page.turns));
      setHasNewerTurnContext(page.has_more_after);
      setNewerTurnContextCursor(page.next_after || null);
    } catch (error) {
      if (!isSessionUnauthorizedError(error)) {
        toast.error(error instanceof Error ? error.message : "加载更新 turn 失败");
      }
    } finally {
      if (activeConversationIdRef.current === requestedConversationId) {
        setIsLoadingTurnContext(false);
      }
    }
  }, [
    conversationId,
    hasNewerTurnContext,
    isLoadingTurnContext,
    loadConversationTurnHistory,
    newerTurnContextCursor,
    registerTurn,
  ]);

  const handleLoadOlderMessages = useCallback(async () => {
    if (hasOlderTurnContext) {
      await handleLoadOlderTurnContext();
      return;
    }
    await handleLoadOlderEvents();
  }, [handleLoadOlderEvents, handleLoadOlderTurnContext, hasOlderTurnContext]);

  const updateComposerAttachment = useCallback(
    (key: string, update: Partial<ComposerShellAttachment>) => {
      if (!mountedRef.current || activeConversationIdRef.current !== conversationId) return;
      setAttachments((previous) =>
        previous.map((attachment) =>
          attachment.key === key ? { ...attachment, ...update } : attachment,
        ),
      );
    },
    [conversationId],
  );

  const handleUploadFiles = async (files: File[]) => {
    if (authLoading || isStreaming || isSubmittingEdit) return;

    if (!user) {
      openAuthDialog("login");
      return;
    }

    if (files.length === 0) {
      return;
    }

    const requestedConversationId = conversationId;
    const pendingAttachments = files.map<ComposerShellAttachment>((file) => ({
      contentType: file.type,
      file,
      key: createIdempotencyKey(),
      name: file.name,
      size: file.size,
      status: "uploading",
    }));
    setAttachments((previous) => [...previous, ...pendingAttachments]);

    await Promise.all(
      pendingAttachments.map(async (pending) => {
        try {
          let attachment;
          try {
            attachment = await uploadConversationAttachment(
              requestedConversationId,
              pending.file as File,
              pending.key,
            );
          } catch (error) {
            if (isSessionUnauthorizedError(error)) throw error;
            attachment = await uploadConversationAttachment(
              requestedConversationId,
              pending.file as File,
              pending.key,
            );
          }
          updateComposerAttachment(pending.key, {
            attachmentId: attachment.id,
            contentType: attachment.content_type,
            conversationId: attachment.conversation_id,
            status: "ready",
          });
        } catch (error) {
          updateComposerAttachment(pending.key, {
            error: isSessionUnauthorizedError(error)
              ? "登录状态已失效"
              : error instanceof Error
                ? error.message
                : "文件上传失败",
            status: "failed",
          });
        }
      }),
    );
  };

  useEffect(() => {
    if (!resumeTurnId || isLoading || authLoading || !user || isStreaming) {
      return;
    }
    if (resumeConversationIdRef.current !== conversationId) {
      resumeConversationIdRef.current = null;
      setResumeTurnId(null);
      return;
    }
    const turnId = resumeTurnId;
    resumeConversationIdRef.current = null;
    setResumeTurnId(null);
    restoreActiveTurn(turnId);
    void streamTurn(turnId);
  }, [
    authLoading,
    conversationId,
    isLoading,
    isStreaming,
    restoreActiveTurn,
    resumeTurnId,
    streamTurn,
    user,
  ]);

  useEffect(() => {
    if (restoreTurns.length === 0) return;
    if (restoreConversationIdRef.current !== conversationId) {
      restoreConversationIdRef.current = null;
      setRestoreTurns([]);
      return;
    }
    const turns = restoreTurns;
    let cancelled = false;
    void (async () => {
      for (const turn of turns) {
        if (cancelled) return;
        await hydrateTurn(turn.id, "restore", turn);
      }
      if (!cancelled) {
        restoreConversationIdRef.current = null;
        setRestoreTurns([]);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [conversationId, hydrateTurn, restoreTurns]);

  const handleSend = async (content: string, attachmentIds: string[] = []) => {
    if (authLoading || isStreaming) return;

    if (!user) {
      openAuthDialog("login");
      return;
    }

    if (editingMessage) {
      await handleSubmitEditedMessage(editingMessage, content);
      return;
    }

    if (!content.trim() && attachmentIds.length === 0) {
      return;
    }

    let turnId: string;
    let streamPath: string;
    let thinkingMsg: Message;
    const requestedConversationId = conversationId;
    try {
      const res = await createMessage(conversationId, {
        content,
        attachmentIds,
        modelId: composerPreferences.modelId,
        reasoningEffort: composerPreferences.reasoningEffort || undefined,
      });
      if (!mountedRef.current || activeConversationIdRef.current !== requestedConversationId)
        return;
      turnId = res.turn.id;
      streamPath = res.stream_path;
      thinkingMsg = buildThinkingMessage(turnId, res.conversation_id);
      invalidateConversationPages(requestedConversationId);
      setDraft("");
      const sentAttachmentIds = new Set(attachmentIds);
      setAttachments((previous) =>
        previous.filter(
          (attachment) =>
            !attachment.attachmentId || !sentAttachmentIds.has(attachment.attachmentId),
        ),
      );
      setResumeTurnId(null);
      resumeConversationIdRef.current = null;
      setMessages((prev) => [...prev, res.message, thinkingMsg]);
      registerTurn(res.turn);
      setTurnSummaries((previous) =>
        mergeTurnSummaries(previous, [turnSummaryFromTurn(res.turn, res.message)]),
      );
    } catch (err) {
      if (isSessionUnauthorizedError(err)) {
        return;
      }
      toast.error(err instanceof Error ? err.message : "发送失败");
      return;
    }

    await streamTurn(turnId, streamPath);
  };

  const handleRename = async () => {
    if (!user) {
      openAuthDialog("login");
      return;
    }

    if (!conversation) return;
    try {
      const updated = await patchConversation(conversation.id, {
        title: newTitle,
      });
      setConversation(updated);
      setNewTitle(updated.title || "");
      emitConversationUpdated({ id: updated.id, title: updated.title });
      setRenameOpen(false);
      toast.success("标题已更新");
    } catch (err) {
      if (isSessionUnauthorizedError(err)) {
        return;
      }
      toast.error(err instanceof Error ? err.message : "重命名失败");
    }
  };

  const handleShare = useCallback(async () => {
    if (authLoading || isSharing || isStreaming) return;
    if (!user) {
      openAuthDialog("login");
      return;
    }
    if (!conversation || conversation.id !== conversationId) return;

    const latestMessageSeq = messages.reduce((latest, message) => Math.max(latest, message.seq), 0);
    const cachedShare = shareCacheRef.current.get(conversation.id);
    if (
      cachedShare?.last_message_seq === latestMessageSeq &&
      (cachedShare.title || "") === (conversation.title || "")
    ) {
      setConversationShare(cachedShare);
      setShareOpen(true);
      return;
    }

    const requestedConversationId = conversation.id;
    const title = conversation.title || "";
    const pendingOperation = readConversationShareOperation(user.id, requestedConversationId);
    const operation: ConversationShareOperation =
      pendingOperation?.lastMessageSeq === latestMessageSeq && pendingOperation.title === title
        ? pendingOperation
        : {
            idempotencyKey: createIdempotencyKey(),
            lastMessageSeq: latestMessageSeq,
            title,
          };
    writeConversationShareOperation(user.id, requestedConversationId, operation);
    setIsSharing(true);
    try {
      const result = await createConversationShare(
        requestedConversationId,
        operation.idempotencyKey,
      );
      if (
        readConversationShareOperation(user.id, requestedConversationId)?.idempotencyKey !==
        operation.idempotencyKey
      ) {
        return;
      }
      if (!mountedRef.current || activeConversationIdRef.current !== requestedConversationId) {
        return;
      }
      clearConversationShareOperation(user.id, requestedConversationId, operation.idempotencyKey);
      shareCacheRef.current.set(requestedConversationId, result.share);
      setConversationShare(result.share);
      setShareOpen(true);
    } catch (error) {
      const operationIsCurrent =
        readConversationShareOperation(user.id, requestedConversationId)?.idempotencyKey ===
        operation.idempotencyKey;
      if (
        mountedRef.current &&
        activeConversationIdRef.current === requestedConversationId &&
        operationIsCurrent &&
        !isSessionUnauthorizedError(error)
      ) {
        toast.error(error instanceof Error ? error.message : "创建分享链接失败");
      }
    } finally {
      const currentOperation = readConversationShareOperation(user.id, requestedConversationId);
      if (
        activeConversationIdRef.current === requestedConversationId &&
        (currentOperation?.idempotencyKey === operation.idempotencyKey || currentOperation === null)
      ) {
        setIsSharing(false);
      }
    }
  }, [authLoading, conversation, conversationId, isSharing, isStreaming, messages, user]);

  useEffect(() => {
    setMobileAction({
      busy: isSharing,
      conversationId,
      disabled: conversation?.id !== conversationId || isSharing || isStreaming,
      icon: isSharing ? <Spinner /> : <Share className="size-4" />,
      label: isSharing ? "正在创建分享链接" : "分享对话",
      onClick: () => void handleShare(),
    });
    return () => setMobileAction(null);
  }, [conversation?.id, conversationId, handleShare, isSharing, isStreaming, setMobileAction]);

  const restoreComposerAfterEdit = () => {
    const backup = editBackupRef.current;
    setDraft(backup?.draft || "");
    setAttachments(backup?.attachments || []);
    setEditingMessage(null);
    editBackupRef.current = null;
  };

  const handleEditMessage = (message: Message) => {
    if (isStreaming || isUploadingAttachments || isSubmittingEdit || retryInFlightRef.current)
      return;
    if (!editBackupRef.current) {
      editBackupRef.current = { draft, attachments: [...attachments] };
    }
    setEditingMessage(message);
    setDraft(message.content_text || "");
    setAttachments([]);
    requestAnimationFrame(() => {
      const input = composerInputRef.current;
      if (!input) return;
      input.focus();
      input.setSelectionRange(input.value.length, input.value.length);
    });
  };

  const handleSubmitEditedMessage = async (message: Message, content: string) => {
    if (isStreaming || retryInFlightRef.current) return false;
    if (!user) {
      openAuthDialog("login");
      return false;
    }
    if (!message.turn_id) {
      toast.error("未找到可编辑的消息");
      return false;
    }
    if (content === (message.content_text || "").trim()) {
      restoreComposerAfterEdit();
      return true;
    }

    const requestedConversationId = conversationId;
    retryInFlightRef.current = true;
    setIsSubmittingEdit(true);
    try {
      const res = await editTurn(message.turn_id, content);
      if (activeConversationIdRef.current !== requestedConversationId) return false;
      invalidateConversationPages(requestedConversationId);
      setMessages((previous) => [
        ...previous,
        res.message,
        buildThinkingMessage(res.turn.id, res.conversation_id),
      ]);
      registerTurn(res.turn);
      setTurnSummaries((previous) =>
        mergeTurnSummaries(previous, [turnSummaryFromTurn(res.turn, res.message)]),
      );
      restoreComposerAfterEdit();
      void streamTurn(res.turn.id, res.stream_path);
      return true;
    } catch (error) {
      if (!isSessionUnauthorizedError(error)) {
        toast.error(error instanceof Error ? error.message : "编辑并重发失败");
      }
      return false;
    } finally {
      retryInFlightRef.current = false;
      setIsSubmittingEdit(false);
    }
  };

  const handleRetryMessage = async (message: Message) => {
    if (isStreaming || retryInFlightRef.current) return;
    if (!user) {
      openAuthDialog("login");
      return;
    }
    if (!message.turn_id) {
      toast.error("未找到可重试的回复");
      return;
    }

    const requestedConversationId = conversationId;
    retryInFlightRef.current = true;
    try {
      const res = await retryTurn(message.turn_id);
      if (activeConversationIdRef.current !== requestedConversationId) return;
      invalidateConversationPages(requestedConversationId);
      setMessages((previous) => [
        ...previous,
        res.message,
        buildThinkingMessage(res.turn.id, res.conversation_id),
      ]);
      registerTurn(res.turn);
      setTurnSummaries((previous) =>
        mergeTurnSummaries(previous, [turnSummaryFromTurn(res.turn, res.message)]),
      );
      await streamTurn(res.turn.id, res.stream_path);
    } catch (error) {
      if (!isSessionUnauthorizedError(error)) {
        toast.error(error instanceof Error ? error.message : "重试失败");
      }
    } finally {
      retryInFlightRef.current = false;
    }
  };

  const handleOpenTimeline = useCallback(
    (turnId: string) => openTimeline(turnId, streamingTurnId),
    [openTimeline, streamingTurnId],
  );

  const displayMessages = useMemo(
    () => ensureStreamingThinkingMessage(messages, streamingTurnId, conversationId),
    [conversationId, messages, streamingTurnId],
  );
  const handleScrollTargetComplete = useCallback(() => {
    setScrollTargetMessageId(null);
  }, []);
  const handleActiveNavigationTurnChange = useCallback((turnId: string) => {
    setActiveNavigationTurnId(turnId);
  }, []);
  const timelinePanelProps = timelineTurnId
    ? {
        isStreaming:
          (timelineTurnId === streamingTurnId && isStreaming) ||
          Boolean(timelineLoading[timelineTurnId]) ||
          ["accepted", "context_ready", "processing", "awaiting_input"].includes(
            turnTimelines[timelineTurnId]?.status || "",
          ),
        timeline: turnTimelines[timelineTurnId] || null,
        loading: timelineLoading[timelineTurnId] || false,
        error: timelineErrors[timelineTurnId] || null,
        turn: turnsById[timelineTurnId] || null,
        onClose: closeTimeline,
      }
    : null;
  return {
    attachments,
    authLoading,
    closeTimeline,
    composerHeight,
    composerInputRef,
    composerPreferences,
    conversation,
    conversationId,
    conversationShare,
    displayMessages,
    draft,
    editingMessage,
    handleAnswerInteraction,
    handleCancelGeneration,
    handleEditMessage,
    handleLoadOlderEvents,
    handleLoadOlderMessages,
    handleLoadNewerTurnContext,
    handleLoadOlderTurnSummaries,
    handleJumpToTurn,
    handleOpenTimeline,
    handleRename,
    handleRetryMessage,
    handleSend,
    handleShare,
    handleUploadFiles,
    hasOlderEvents: hasOlderEvents || hasOlderTurnContext,
    hasNewerTurnContext,
    isCancelling,
    isLoading,
    isLoadingOlderEvents: isLoadingOlderEvents || isLoadingTurnContext,
    isLoadingNewerTurnContext: isLoadingTurnContext,
    isLoadingTurnSummaries,
    isMobileViewport,
    isSharing,
    isStreaming,
    isSubmittingEdit,
    newTitle,
    renameOpen,
    restoreComposerAfterEdit,
    setAttachments,
    setComposerHeight,
    setDraft,
    setNewTitle,
    setRenameOpen,
    setShareOpen,
    shareOpen,
    scrollTargetMessageId,
    handleScrollTargetComplete,
    handleActiveNavigationTurnChange,
    streamConnectionState,
    streamingTurnId,
    timelineActivityLabels,
    timelinePanelProps,
    timelineTurnId,
    turnSummaries,
    activeNavigationTurnId,
    hasOlderTurnSummaries,
    turnsById,
    visualViewportBottomInset,
  };
}

export type ChatController = ReturnType<typeof useChatController>;
