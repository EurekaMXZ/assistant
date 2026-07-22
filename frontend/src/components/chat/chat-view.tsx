"use client";

import { useState } from "react";
import { Pencil, Share } from "lucide-react";
import { Spinner } from "@/components/shared/spinner";
import { Button } from "@/components/ui/button";
import type { ChatController } from "@/hooks/use-chat-controller";
import { cn } from "@/lib/utils";
import { ChatSkeleton } from "./chat-skeleton";
import { Composer } from "./composer";
import { ConversationShareDialog } from "./conversation-share-dialog";
import { MessageList } from "./message-list";
import { RenameDialog } from "./rename-dialog";
import { TimelineDialog } from "./timeline-dialog";
import { TurnTimelinePanel } from "./turn-timeline";

export function ChatView({ controller }: { controller: ChatController }) {
  const [disclaimerCovered, setDisclaimerCovered] = useState(false);
  const {
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
    handleOpenTimeline,
    handleRename,
    handleRetryMessage,
    handleSend,
    handleShare,
    handleUploadFiles,
    hasOlderEvents,
    isCancelling,
    isLoading,
    isLoadingOlderEvents,
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
    streamConnectionState,
    streamingTurnId,
    timelineActivityLabels,
    timelinePanelProps,
    timelineTurnId,
    turnsById,
    visualViewportBottomInset,
  } = controller;

  if (authLoading || isLoading || !conversation) return <ChatSkeleton />;

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
                size="icon-md"
                className="shrink-0"
                disabled={authLoading}
                onClick={() => setRenameOpen(true)}
              >
                <Pencil className="size-3.5" />
                <span className="sr-only">重命名</span>
              </Button>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon-md"
              className="shrink-0"
              aria-busy={isSharing}
              aria-label={isSharing ? "正在创建分享链接" : "分享对话"}
              disabled={conversation.id !== conversationId || isSharing || isStreaming}
              onClick={() => void handleShare()}
            >
              {isSharing ? <Spinner className="size-3.5" /> : <Share className="size-3.5" />}
              <span className="sr-only">{isSharing ? "正在创建分享链接" : "分享对话"}</span>
            </Button>
          </header>

          <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
            <MessageList
              activityLabels={timelineActivityLabels}
              hasOlderMessages={hasOlderEvents}
              loadingOlderMessages={isLoadingOlderEvents}
              messages={displayMessages}
              bottomInset={composerHeight + visualViewportBottomInset}
              onEditMessage={handleEditMessage}
              onDisclaimerCoveredChange={setDisclaimerCovered}
              onAnswerInteraction={handleAnswerInteraction}
              onLoadOlderMessages={handleLoadOlderEvents}
              onOpenTimeline={handleOpenTimeline}
              onRetryMessage={handleRetryMessage}
              dimmed={Boolean(editingMessage)}
              streamingTurnId={streamingTurnId}
              turnsById={turnsById}
            />

            <div
              data-slot="message-list-disclaimer"
              aria-hidden={disclaimerCovered || displayMessages.length === 0}
              className={cn(
                "pointer-events-none absolute inset-x-0 z-10 px-4 text-center text-xs leading-5 text-muted-foreground transition-opacity duration-200 motion-reduce:transition-none sm:px-6",
                disclaimerCovered || displayMessages.length === 0 || composerHeight <= 0
                  ? "opacity-0"
                  : "opacity-100",
              )}
              style={{ bottom: composerHeight + visualViewportBottomInset }}
            >
              Assistant 也可能会犯错，请核查重要信息
            </div>

            <Composer
              allowEmpty={Boolean(
                editingMessage &&
                Array.isArray(editingMessage.metadata?.attachment_ids) &&
                editingMessage.metadata.attachment_ids.length > 0,
              )}
              attachments={attachments}
              bottomInset={visualViewportBottomInset}
              cancelling={isCancelling}
              editing={Boolean(editingMessage)}
              editingBusy={isSubmittingEdit}
              inputRef={composerInputRef}
              models={composerPreferences.models}
              modelsLoading={composerPreferences.modelsLoading}
              modelId={composerPreferences.modelId}
              onChange={setDraft}
              onCancelEdit={restoreComposerAfterEdit}
              onRemoveAttachment={(attachmentKey) =>
                setAttachments((previous) =>
                  previous.filter((attachment) => attachment.key !== attachmentKey),
                )
              }
              onModelChange={composerPreferences.setModelId}
              onModelReasoningEffortChange={composerPreferences.setModelReasoningEffort}
              onHeightChange={setComposerHeight}
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

      <TimelineDialog
        mobile={isMobileViewport}
        panelProps={timelinePanelProps}
        onClose={closeTimeline}
      />
      <RenameDialog
        open={renameOpen}
        title={newTitle}
        onOpenChange={setRenameOpen}
        onSave={() => void handleRename()}
        onTitleChange={setNewTitle}
      />
      <ConversationShareDialog
        open={shareOpen}
        share={conversationShare}
        onOpenChange={setShareOpen}
      />
    </>
  );
}
