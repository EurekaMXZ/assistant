"use client";

import { useEffect, useState, useCallback, useMemo, useRef } from "react";
import {
  createMessage,
  getConversation,
  getTurn,
  isSessionUnauthorizedError,
  listMessages,
  patchConversation,
  uploadConversationAttachment,
} from "@/lib/api";
import { openAuthDialog } from "@/lib/auth-dialog-events";
import { useAuth } from "@/hooks/use-auth";
import { useComposerPreferences } from "@/hooks/use-composer-preferences";
import { useTurnStream } from "@/hooks/use-turn-stream";
import { useTurnTimelineController } from "@/hooks/use-turn-timeline-controller";
import {
  emitConversationUpdated,
  subscribeConversationUpdated,
} from "@/lib/conversation-events";
import {
  takePendingHomeTurn,
} from "@/lib/pending-home-turn";
import type {
  Conversation,
  Attachment,
  Message,
  Turn,
} from "@/lib/types";
import {
  buildThinkingMessage,
  ensurePendingHomeTurnMessages,
  ensureStreamingThinkingMessage,
} from "@/lib/chat-state";
import { requestDescriptorFromMessage } from "@/lib/turn-request";
import { MessageList } from "./message-list";
import { Composer } from "./composer";
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
import { Loader2, Pencil } from "lucide-react";
import { toast } from "sonner";

interface ChatContainerProps {
  conversationId: string;
}

async function inspectUnresolvedTurns(
  messages: Message[],
  conversationId: string,
) {
  const assistantTurnIds = new Set(
    messages
      .filter((message) => message.role === "assistant" && message.turn_id)
      .map((message) => message.turn_id as string),
  );
  const turnIds = Array.from(new Set(messages.flatMap((message) => message.turn_id ? [message.turn_id] : [])));

  const turns = await Promise.all(
    turnIds.map(async (turnId) => {
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
    if (["accepted", "context_ready", "processing"].includes(turn.status)) {
      nextMessages = ensureStreamingThinkingMessage(
        nextMessages,
        turn.id,
        conversationId,
      );
      if (unresolved) activeTurnId = turn.id;
      continue;
    }
    if (unresolved && (turn.status === "failed" || turn.status === "completed")) {
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


export function ChatContainer({ conversationId }: ChatContainerProps) {
  const { user, isLoading: authLoading } = useAuth();
  const composerPreferences = useComposerPreferences(
    Boolean(user) && !authLoading,
  );
  const composerInputRef = useRef<HTMLTextAreaElement>(null);
  const [conversation, setConversation] = useState<Conversation | null>(null);
  const [draft, setDraft] = useState("");
  const [messages, setMessages] = useState<Message[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [isUploadingAttachments, setIsUploadingAttachments] = useState(false);
  const [resumeTurnId, setResumeTurnId] = useState<string | null>(null);
  const [restoreTurns, setRestoreTurns] = useState<Turn[]>([]);
  const [renameOpen, setRenameOpen] = useState(false);
  const [newTitle, setNewTitle] = useState("");
  const resumeConversationIdRef = useRef<string | null>(null);
  const restoreConversationIdRef = useRef<string | null>(null);
  const activeConversationIdRef = useRef(conversationId);
  const mountedRef = useRef(true);

  useEffect(() => {
    activeConversationIdRef.current = conversationId;
  }, [conversationId]);

  const handleStreamConversationUpdated = useCallback(
    (update: { conversation_id: string; title?: string | null }) => {
      if (typeof update.title !== "undefined") {
        setConversation((previous) =>
          previous
            ? { ...previous, title: update.title ?? undefined }
            : previous,
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
    statusText,
    clearStatus,
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

  const refreshMessages = useCallback(async () => {
    const requestedConversationId = conversationId;
    try {
      const nextMessages = await listMessages(conversationId);
      if (
        !mountedRef.current ||
        activeConversationIdRef.current !== requestedConversationId
      ) {
        return;
      }
      setMessages(nextMessages);
    } catch (error) {
      if (!isSessionUnauthorizedError(error)) {
        toast.error(error instanceof Error ? error.message : "刷新消息失败");
      }
    }
  }, [conversationId]);

  const {
    isStreaming,
    streamingTurnId,
    streamConnectionState,
    streamTurn,
  } = useTurnStream({
    conversationId,
    onCompleted: refreshMessages,
    onEvent: dispatchActiveFrame,
    onFinished: finishActiveTurn,
  });

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
      const [conv, msgs] = await Promise.all([
        getConversation(conversationId),
        listMessages(conversationId),
      ]);
      if (
        !mountedRef.current ||
        activeConversationIdRef.current !== requestedConversationId
      )
        return;
      const pending = takePendingHomeTurn(conversationId);
      const loadedMessages = pending
        ? ensurePendingHomeTurnMessages(msgs, pending)
        : msgs;
      const restored = await inspectUnresolvedTurns(
        loadedMessages,
        conversationId,
      );
      if (activeConversationIdRef.current !== requestedConversationId) return;
      setConversation(conv);
      setMessages(restored.messages);
      initializeTurns(restored.turns);
      resumeConversationIdRef.current =
        pending?.turn.id || restored.activeTurnId
          ? requestedConversationId
          : null;
      restoreConversationIdRef.current = restored.terminalTurns.length > 0
        ? requestedConversationId
        : null;
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
    setAttachments([]);
    setIsUploadingAttachments(false);
    setResumeTurnId(null);
    setRestoreTurns([]);
    resumeConversationIdRef.current = null;
    restoreConversationIdRef.current = null;
  }, [conversationId]);

  const handleUploadFiles = async (files: File[]) => {
    if (authLoading || isStreaming || isUploadingAttachments) return;

    if (!user) {
      openAuthDialog("login");
      return;
    }

    if (files.length === 0) {
      return;
    }

    const requestedConversationId = conversationId;
    setIsUploadingAttachments(true);
    try {
      const uploaded = await Promise.all(
        files.map((file) => uploadConversationAttachment(conversationId, file)),
      );
      if (
        !mountedRef.current ||
        activeConversationIdRef.current !== requestedConversationId
      )
        return;
      setAttachments((prev) => [...prev, ...uploaded]);
      toast.success(
        uploaded.length === 1
          ? "文件已上传"
          : `${uploaded.length} 个文件已上传`,
      );
    } catch (err) {
      if (activeConversationIdRef.current !== requestedConversationId) return;
      if (isSessionUnauthorizedError(err)) {
        return;
      }
      toast.error(err instanceof Error ? err.message : "文件上传失败");
    } finally {
      if (activeConversationIdRef.current === requestedConversationId) {
        setIsUploadingAttachments(false);
      }
    }
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
  }, [
    authLoading,
    conversationId,
    isLoading,
    isStreaming,
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
    if (authLoading || isStreaming || isUploadingAttachments) return;

    if (!user) {
      openAuthDialog("login");
      return;
    }

    if (!content.trim() && attachmentIds.length === 0) {
      return;
    }

    clearStatus();

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
      if (
        !mountedRef.current ||
        activeConversationIdRef.current !== requestedConversationId
      )
        return;
      turnId = res.turn.id;
      streamPath = res.stream_path;
      thinkingMsg = buildThinkingMessage(turnId, res.conversation_id);
      setDraft("");
      setAttachments([]);
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

  const handleEditMessage = (message: Message) => {
    setDraft(message.content_text || "");

    requestAnimationFrame(() => {
      const input = composerInputRef.current;
      if (!input) return;
      input.focus();
      const length = input.value.length;
      input.setSelectionRange(length, length);
    });
  };

  const handleRetryMessage = async (message: Message) => {
    if (!user) {
      openAuthDialog("login");
      return;
    }

    const messageIndex = messages.findIndex(
      (candidate) => candidate.id === message.id,
    );
    if (messageIndex === -1) {
      toast.error("未找到可重试的消息");
      return;
    }

    const retrySource = [...messages.slice(0, messageIndex)]
      .reverse()
      .find(
        (candidate) =>
          candidate.role === "user" &&
          (!!candidate.content_text?.trim() ||
            requestDescriptorFromMessage(candidate)?.attachment_ids.length),
      );

    const descriptor = retrySource
      ? requestDescriptorFromMessage(retrySource)
      : null;
    if (!descriptor) {
      toast.error("未找到可重试的用户消息");
      return;
    }

    setDraft(descriptor.content);
    const requestedConversationId = conversationId;
    try {
      const res = await createMessage(conversationId, descriptor);
      if (activeConversationIdRef.current !== requestedConversationId) return;
      setDraft("");
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
    }
  };

  const handleOpenTimeline = useCallback(
    (turnId: string) => openTimeline(turnId, streamingTurnId),
    [openTimeline, streamingTurnId],
  );

  const displayMessages = useMemo(
    () =>
      ensureStreamingThinkingMessage(messages, streamingTurnId, conversationId),
    [conversationId, messages, streamingTurnId],
  );
  if (authLoading || isLoading || !conversation) {
    return <ChatSkeleton />;
  }

  return (
    <>
      <div
        data-stream-state={streamConnectionState}
        className="grid h-full min-h-0 w-full overflow-hidden transition-[grid-template-columns] duration-500 ease-in-out"
        style={{
          gridTemplateColumns: timelineTurnId
            ? "minmax(0, 42rem) minmax(0, 1fr)"
            : "minmax(0, 1fr) minmax(0, 0fr)",
        }}
      >
        <section className="flex min-h-0 min-w-0 flex-col overflow-hidden">
          <header className="flex h-14 shrink-0 items-center justify-between border-b px-4">
            <div className="flex min-w-0 items-center gap-2">
              <h2 className="truncate text-base font-semibold">
                {conversation.title || "新会话"}
              </h2>
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
          </header>

          <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
            <MessageList
              activityLabels={timelineActivityLabels}
              messages={displayMessages}
              onEditMessage={handleEditMessage}
              onOpenTimeline={handleOpenTimeline}
              onRetryMessage={handleRetryMessage}
              streamingTurnId={streamingTurnId}
              turnsById={turnsById}
            />

            {statusText && (
              <div className="flex shrink-0 items-center justify-center gap-2 border-t py-2 text-xs text-muted-foreground sm:hidden">
                <Loader2 className="h-3 w-3 animate-spin" />
                {statusText}
              </div>
            )}

            <Composer
              attachments={attachments}
              inputRef={composerInputRef}
              models={composerPreferences.models}
              modelsLoading={composerPreferences.modelsLoading}
              modelId={composerPreferences.modelId}
              onChange={setDraft}
              onRemoveAttachment={(attachmentId) => {
                setAttachments((prev) =>
                  prev.filter((attachment) => attachment.id !== attachmentId),
                );
              }}
              onModelChange={composerPreferences.setModelId}
              onModelReasoningEffortChange={composerPreferences.setModelReasoningEffort}
              onSend={handleSend}
              onUploadFiles={handleUploadFiles}
              disabled={authLoading || isStreaming}
              placeholder="输入消息"
              reasoningEfforts={composerPreferences.reasoningEfforts}
              uploadingAttachments={isUploadingAttachments}
              value={draft}
            />
          </div>
        </section>

        <div className="min-w-0 overflow-hidden">
          {timelineTurnId ? (
            <TurnTimelinePanel
              isStreaming={
                (timelineTurnId === streamingTurnId && isStreaming) ||
                Boolean(timelineLoading[timelineTurnId]) ||
                ["accepted", "context_ready", "processing"].includes(
                  turnTimelines[timelineTurnId]?.status || "",
                )
              }
              timeline={turnTimelines[timelineTurnId] || null}
              loading={timelineLoading[timelineTurnId] || false}
              error={timelineErrors[timelineTurnId] || null}
              turn={turnsById[timelineTurnId] || null}
              onClose={closeTimeline}
            />
          ) : null}
        </div>
      </div>

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
    </>
  );
}
