"use client";

import { useEffect, useState, useCallback, useMemo, useRef, useSyncExternalStore } from "react";
import {
  createConversationShare,
  createMessage,
  cancelTurn,
  editTurn,
  getConversation,
  getTurn,
  isSessionUnauthorizedError,
  listConversationEvents,
  listMessages,
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
import type { Conversation, ConversationShare, Message, Turn } from "@/lib/types";
import { messageSchema, turnSchema } from "@/lib/api-schemas";
import {
  buildThinkingMessage,
  ensurePendingHomeTurnMessages,
  ensureStreamingThinkingMessage,
} from "@/lib/chat-state";
import { MessageList } from "./message-list";
import { Composer } from "./composer";
import type { ComposerShellAttachment } from "./composer-shell";
import { ConversationShareDialog } from "./conversation-share-dialog";
import { ChatSkeleton } from "./chat-skeleton";
import { TurnTimelinePanel } from "./turn-timeline";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Loader2, Pencil, Share } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";

interface ChatContainerProps {
  conversationId: string;
}

const mobileViewportQuery = "(max-width: 767px)";

function subscribeMobileViewport(onChange: () => void) {
  const mediaQuery = window.matchMedia(mobileViewportQuery);
  mediaQuery.addEventListener("change", onChange);
  return () => mediaQuery.removeEventListener("change", onChange);
}

function getMobileViewportSnapshot() {
  return typeof window !== "undefined" && window.matchMedia(mobileViewportQuery).matches;
}

async function inspectUnresolvedTurns(
  messages: Message[],
  conversationId: string,
  eventTurns: Turn[] = [],
) {
  const assistantTurnIds = new Set(
    messages
      .filter((message) => message.role === "assistant" && message.turn_id)
      .map((message) => message.turn_id as string),
  );
  const turnIds = Array.from(
    new Set(messages.flatMap((message) => (message.turn_id ? [message.turn_id] : []))),
  );

  const knownTurns = new Map(eventTurns.map((turn) => [turn.id, turn]));
  const turns = await Promise.all(
    turnIds.map(async (turnId) => {
      const known = knownTurns.get(turnId);
      if (known) return known;
      try {
        return await getTurn(turnId);
      } catch {
        return null;
      }
    }),
  );

  let nextMessages = [...messages];
  let activeTurnId: string | null = null;
  const terminalTurns: Turn[] = [];
  for (const turn of turns) {
    if (!turn) continue;
    const unresolved = !assistantTurnIds.has(turn.id);
    if (["accepted", "context_ready", "processing", "cancel_requested"].includes(turn.status)) {
      nextMessages = ensureStreamingThinkingMessage(nextMessages, turn.id, conversationId);
      if (unresolved) activeTurnId = turn.id;
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

function messagesFromConversationEvents(
  events: Awaited<ReturnType<typeof listConversationEvents>>["events"],
): Message[] {
  return events.flatMap((event) => {
    if (event.event_type !== "message.completed") return [];
    const parsed = messageSchema.safeParse(event.payload.message);
    return parsed.success ? [parsed.data] : [];
  });
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
    if (!existing) continue;
    if (event.event_type === "turn.failed")
      turns.set(existing.id, { ...existing, status: "failed" });
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
  return Array.from(messages.values()).sort((left, right) => left.seq - right.seq);
}

async function loadConversationMessages(conversationId: string) {
  const [eventPage, legacyMessages] = await Promise.all([
    listConversationEvents(conversationId),
    listMessages(conversationId),
  ]);
  const eventMessages = messagesFromConversationEvents(eventPage.events);
  return {
    eventPage,
    messages: mergeMessages(legacyMessages, eventMessages),
    turns: turnsFromConversationEvents(eventPage.events),
  };
}

export function ChatContainer({ conversationId }: ChatContainerProps) {
  const { user, isLoading: authLoading } = useAuth();
  const {
    setAction: setMobileAction,
    setTitle: setMobileTitle,
    setTitleAction: setMobileTitleAction,
  } = useMobileHeader();
  const isMobileViewport = useSyncExternalStore(
    subscribeMobileViewport,
    getMobileViewportSnapshot,
    () => false,
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
  const [resumeTurnId, setResumeTurnId] = useState<string | null>(null);
  const [restoreTurns, setRestoreTurns] = useState<Turn[]>([]);
  const [renameOpen, setRenameOpen] = useState(false);
  const [newTitle, setNewTitle] = useState("");
  const [shareOpen, setShareOpen] = useState(false);
  const [conversationShare, setConversationShare] = useState<ConversationShare | null>(null);
  const [isSharing, setIsSharing] = useState(false);
  const resumeConversationIdRef = useRef<string | null>(null);
  const restoreConversationIdRef = useRef<string | null>(null);
  const shareCacheRef = useRef(new Map<string, ConversationShare>());
  const activeConversationIdRef = useRef(conversationId);
  const mountedRef = useRef(true);
  const retryInFlightRef = useRef(false);
  const editBackupRef = useRef<{
    draft: string;
    attachments: ComposerShellAttachment[];
  } | null>(null);
  const isUploadingAttachments = attachments.some(
    (attachment) => attachment.status === "uploading",
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

  const refreshMessages = useCallback(async () => {
    const requestedConversationId = conversationId;
    try {
      const nextMessages = await loadConversationMessages(conversationId);
      if (!mountedRef.current || activeConversationIdRef.current !== requestedConversationId) {
        return;
      }
      setMessages((previous) => mergeMessages(previous, nextMessages.messages));
    } catch (error) {
      if (!isSessionUnauthorizedError(error)) {
        toast.error(error instanceof Error ? error.message : "刷新消息失败");
      }
    }
  }, [conversationId]);

  const { isStreaming, streamingTurnId, streamConnectionState, streamTurn } = useTurnStream({
    conversationId,
    onCompleted: refreshMessages,
    onEvent: dispatchActiveFrame,
    onFinished: finishActiveTurn,
  });

  const handleCancelGeneration = useCallback(async () => {
    if (!streamingTurnId || isCancelling) return;
    setIsCancelling(true);
    try {
      await cancelTurn(streamingTurnId);
    } catch (error) {
      setIsCancelling(false);
      if (!isSessionUnauthorizedError(error)) {
        toast.error(error instanceof Error ? error.message : "停止生成失败");
      }
    }
  }, [isCancelling, streamingTurnId]);

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
      const [conv, history] = await Promise.all([
        getConversation(conversationId),
        loadConversationMessages(conversationId),
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
  }, [authLoading, conversationId, initializeTurns, user]);

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
    setResumeTurnId(null);
    setRestoreTurns([]);
    setConversationShare(shareCacheRef.current.get(conversationId) || null);
    setShareOpen(false);
    setIsSharing(false);
    editBackupRef.current = null;
    resumeConversationIdRef.current = null;
    restoreConversationIdRef.current = null;
  }, [conversationId]);

  const handleLoadOlderEvents = useCallback(async () => {
    if (!hasOlderEvents || !olderEventCursor || isLoadingOlderEvents) return;
    const requestedConversationId = conversationId;
    setIsLoadingOlderEvents(true);
    try {
      const page = await listConversationEvents(conversationId, { before: olderEventCursor });
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
  }, [conversationId, hasOlderEvents, isLoadingOlderEvents, olderEventCursor, registerTurn]);

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
    void streamTurn(turnId);
  }, [authLoading, conversationId, isLoading, isStreaming, resumeTurnId, streamTurn, user]);

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
      icon: isSharing ? <Loader2 className="size-4 animate-spin" /> : <Share className="size-4" />,
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
      setMessages((previous) => [
        ...previous,
        res.message,
        buildThinkingMessage(res.turn.id, res.conversation_id),
      ]);
      registerTurn(res.turn);
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
      setMessages((previous) => [
        ...previous,
        res.message,
        buildThinkingMessage(res.turn.id, res.conversation_id),
      ]);
      registerTurn(res.turn);
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
  const timelinePanelProps = timelineTurnId
    ? {
        isStreaming:
          (timelineTurnId === streamingTurnId && isStreaming) ||
          Boolean(timelineLoading[timelineTurnId]) ||
          ["accepted", "context_ready", "processing"].includes(
            turnTimelines[timelineTurnId]?.status || "",
          ),
        timeline: turnTimelines[timelineTurnId] || null,
        loading: timelineLoading[timelineTurnId] || false,
        error: timelineErrors[timelineTurnId] || null,
        turn: turnsById[timelineTurnId] || null,
        onClose: closeTimeline,
      }
    : null;
  if (authLoading || isLoading || !conversation) {
    return <ChatSkeleton />;
  }

  return (
    <>
      <div
        data-stream-state={streamConnectionState}
        className={cn(
          "grid h-full min-h-0 w-full grid-cols-1 overflow-hidden transition-[grid-template-columns] duration-500 ease-in-out",
          timelineTurnId
            ? "md:grid-cols-[minmax(0,42rem)_minmax(0,1fr)]"
            : "md:grid-cols-[minmax(0,1fr)_minmax(0,0fr)]",
        )}
      >
        <section className="flex min-h-0 min-w-0 flex-col overflow-hidden">
          <header className="hidden h-14 shrink-0 items-center justify-between border-b px-4 md:flex">
            <div className="flex min-w-0 items-center gap-2">
              <h2 className="truncate text-base font-semibold">{conversation.title || "新会话"}</h2>
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 shrink-0"
                disabled={authLoading}
                onClick={() => setRenameOpen(true)}
              >
                <Pencil className="h-3.5 w-3.5" />
                <span className="sr-only">重命名</span>
              </Button>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-7 w-7 shrink-0"
              aria-busy={isSharing}
              aria-label={isSharing ? "正在创建分享链接" : "分享对话"}
              disabled={conversation.id !== conversationId || isSharing || isStreaming}
              onClick={() => void handleShare()}
            >
              {isSharing ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Share className="h-3.5 w-3.5" />
              )}
              <span className="sr-only">{isSharing ? "正在创建分享链接" : "分享对话"}</span>
            </Button>
          </header>

          <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
            <MessageList
              activityLabels={timelineActivityLabels}
              hasOlderMessages={hasOlderEvents}
              loadingOlderMessages={isLoadingOlderEvents}
              messages={displayMessages}
              onEditMessage={handleEditMessage}
              onLoadOlderMessages={handleLoadOlderEvents}
              onOpenTimeline={handleOpenTimeline}
              onRetryMessage={handleRetryMessage}
              dimmed={Boolean(editingMessage)}
              streamingTurnId={streamingTurnId}
              turnsById={turnsById}
            />

            <Composer
              allowEmpty={Boolean(
                editingMessage &&
                Array.isArray(editingMessage.metadata?.attachment_ids) &&
                editingMessage.metadata.attachment_ids.length > 0,
              )}
              attachments={attachments}
              cancelling={isCancelling}
              editing={Boolean(editingMessage)}
              editingBusy={isSubmittingEdit}
              inputRef={composerInputRef}
              models={composerPreferences.models}
              modelsLoading={composerPreferences.modelsLoading}
              modelId={composerPreferences.modelId}
              onChange={setDraft}
              onCancelEdit={restoreComposerAfterEdit}
              onRemoveAttachment={(attachmentKey) => {
                setAttachments((prev) =>
                  prev.filter((attachment) => attachment.key !== attachmentKey),
                );
              }}
              onModelChange={composerPreferences.setModelId}
              onModelReasoningEffortChange={composerPreferences.setModelReasoningEffort}
              onSend={handleSend}
              onCancelGeneration={() => void handleCancelGeneration()}
              onUploadFiles={handleUploadFiles}
              disabled={authLoading || isStreaming || isSubmittingEdit || isCancelling}
              streaming={isStreaming}
              placeholder={editingMessage ? "编辑消息" : "输入消息"}
              reasoningEfforts={composerPreferences.reasoningEfforts}
              value={draft}
            />
          </div>
        </section>

        <div className="hidden min-w-0 overflow-hidden md:block">
          {timelinePanelProps ? <TurnTimelinePanel {...timelinePanelProps} /> : null}
        </div>
      </div>

      {isMobileViewport ? (
        <Dialog
          open={timelinePanelProps !== null}
          onOpenChange={(open) => {
            if (!open) closeTimeline();
          }}
        >
          {timelinePanelProps ? (
            <DialogContent className="h-[min(720px,calc(100dvh-2rem))] grid-rows-[minmax(0,1fr)] gap-0 overflow-hidden p-0 sm:max-w-[700px]">
              <DialogHeader className="sr-only">
                <DialogTitle>时间线详情</DialogTitle>
              </DialogHeader>
              <TurnTimelinePanel {...timelinePanelProps} variant="dialog" />
            </DialogContent>
          ) : null}
        </Dialog>
      ) : null}

      <Dialog open={renameOpen} onOpenChange={setRenameOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>重命名会话</DialogTitle>
          </DialogHeader>
          <div className="grid gap-2 py-4">
            <Label htmlFor="title">标题</Label>
            <Input
              id="title"
              value={newTitle}
              onChange={(e) => setNewTitle(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && handleRename()}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRenameOpen(false)}>
              取消
            </Button>
            <Button onClick={handleRename}>保存</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConversationShareDialog
        open={shareOpen}
        share={conversationShare}
        onOpenChange={setShareOpen}
      />
    </>
  );
}
