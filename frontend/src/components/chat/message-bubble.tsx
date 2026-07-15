"use client";

import { useEffect, useState } from "react";
import { ChevronLeft, ChevronRight, CircleAlert, Pencil, RotateCcw } from "lucide-react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { MarkdownRenderer } from "./markdown-renderer";
import { CopyButton } from "./copy-button";
import { TurnTimeline } from "./turn-timeline";
import { Button } from "@/components/ui/button";
import { getConversationAttachmentBlob } from "@/lib/api";
import { cn } from "@/lib/utils";
import type { Message, Turn } from "@/lib/types";
import { ImagePreview } from "./image-preview";

interface MessageBubbleProps {
  message: Message;
  allowAttachmentPreviews?: boolean;
  canEdit?: boolean;
  onEdit?: (message: Message) => void;
  onRetry?: (message: Message) => void;
  isStreaming?: boolean;
  showActions?: boolean;
}

interface AssistantTurnBubbleProps {
  activityLabel?: string | null;
  canRetry?: boolean;
  isStreaming?: boolean;
  messages: Message[];
  onOpenTimeline?: (turnId: string) => void;
  onRetry?: (message: Message) => void;
  turnId: string;
  turn?: Turn | null;
  variantCount?: number;
  variantIndex?: number;
  onVariantChange?: (index: number) => void;
}

const assistantActionIconInsetPx = 6;

type MessageAttachment = {
  id: string;
  filename?: string;
  content_type?: string;
  category?: string;
  size_bytes?: number;
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function attachmentFromMetadataItem(value: unknown): MessageAttachment | null {
  if (!isRecord(value) || typeof value.id !== "string" || !value.id.trim()) {
    return null;
  }

  return {
    id: value.id.trim(),
    filename: typeof value.filename === "string" ? value.filename : undefined,
    content_type: typeof value.content_type === "string" ? value.content_type : undefined,
    category: typeof value.category === "string" ? value.category : undefined,
    size_bytes: typeof value.size_bytes === "number" ? value.size_bytes : undefined,
  };
}

function attachmentsFromMetadata(metadata: Record<string, unknown>) {
  const attachments = metadata.attachments;
  if (Array.isArray(attachments)) {
    return attachments
      .map(attachmentFromMetadataItem)
      .filter((attachment): attachment is MessageAttachment => attachment !== null);
  }

  const attachmentIds = metadata.attachment_ids;
  if (!Array.isArray(attachmentIds)) {
    return [];
  }

  return attachmentIds
    .filter((id): id is string => typeof id === "string" && !!id.trim())
    .map((id) => ({ id: id.trim() }));
}

function attachmentCountFromMetadata(metadata: Record<string, unknown>) {
  const attachmentIds = metadata.attachment_ids;
  return Array.isArray(attachmentIds)
    ? attachmentIds.filter((id) => typeof id === "string" && id.trim()).length
    : 0;
}

function isImageAttachment(attachment: MessageAttachment) {
  return attachment.category === "image" || attachment.content_type?.startsWith("image/");
}

function AttachmentImagePreview({
  attachment,
  conversationId,
}: {
  attachment: MessageAttachment;
  conversationId: string;
}) {
  const [src, setSrc] = useState<string | null>(null);
  const [hidden, setHidden] = useState(false);

  useEffect(() => {
    let cancelled = false;
    let objectUrl: string | null = null;
    setSrc(null);
    setHidden(false);

    const load = async () => {
      try {
        const blob = await getConversationAttachmentBlob(conversationId, attachment.id);
        if (!blob.type.startsWith("image/")) {
          if (!cancelled) setHidden(true);
          return;
        }
        objectUrl = URL.createObjectURL(blob);
        if (!cancelled) {
          setSrc(objectUrl);
        }
      } catch {
        if (!cancelled) setHidden(true);
      }
    };

    void load();

    return () => {
      cancelled = true;
      if (objectUrl) {
        URL.revokeObjectURL(objectUrl);
      }
    };
  }, [attachment.id, conversationId]);

  if (hidden) {
    return null;
  }

  return (
    <div className="overflow-hidden rounded-lg border bg-background/60">
      {src ? (
        <ImagePreview
          src={src}
          alt={attachment.filename || "附件图片"}
          wrapperClassName="flex w-full"
          previewButtonClassName="w-full"
          imageClassName="max-h-72 w-full object-contain"
          downloadName={attachment.filename}
          onError={() => setHidden(true)}
        />
      ) : (
        <div className="h-32 w-48 animate-pulse bg-muted" />
      )}
    </div>
  );
}

function AttachmentImagePreviews({
  attachments,
  conversationId,
}: {
  attachments: MessageAttachment[];
  conversationId: string;
}) {
  if (attachments.length === 0) {
    return null;
  }

  return (
    <div className="mb-2 grid max-w-xs grid-cols-1 gap-2 sm:max-w-sm">
      {attachments.map((attachment) => (
        <AttachmentImagePreview
          key={attachment.id}
          attachment={attachment}
          conversationId={conversationId}
        />
      ))}
    </div>
  );
}

function MessageBody({
  allowAttachmentPreviews = true,
  isStreaming,
  message,
}: {
  allowAttachmentPreviews?: boolean;
  isStreaming?: boolean;
  message: Message;
}) {
  const isUser = message.role === "user";
  const metadata = message.metadata || {};
  const isError = metadata.display_kind === "assistant_error";
  const attachments = attachmentsFromMetadata(metadata);
  const imageAttachments = allowAttachmentPreviews ? attachments.filter(isImageAttachment) : [];
  const attachmentCount = attachmentCountFromMetadata(metadata);
  const hiddenAttachmentCount = Math.max(0, attachmentCount - imageAttachments.length);

  return (
    <div
      className="min-w-0 max-w-full leading-relaxed"
      style={isUser ? undefined : { paddingLeft: `${assistantActionIconInsetPx}px` }}
    >
      {isError ? (
        <div
          role="alert"
          className="grid grid-cols-[1rem_minmax(0,1fr)] gap-2 font-medium text-destructive"
        >
          <span className="flex h-5 items-center" aria-hidden="true">
            <CircleAlert className="h-4 w-4" />
          </span>
          <p className="min-w-0 break-words leading-5">
            {message.content_text || "Request failed"}
          </p>
        </div>
      ) : message.content_text ? (
        <>
          <AttachmentImagePreviews
            attachments={imageAttachments}
            conversationId={message.conversation_id}
          />
          <MarkdownRenderer content={message.content_text} isStreaming={isStreaming} />
          {hiddenAttachmentCount > 0 ? (
            <p className="mt-2 text-muted-foreground">已附加 {hiddenAttachmentCount} 个文件</p>
          ) : null}
        </>
      ) : attachmentCount > 0 ? (
        imageAttachments.length > 0 ? (
          <>
            <AttachmentImagePreviews
              attachments={imageAttachments}
              conversationId={message.conversation_id}
            />
            {hiddenAttachmentCount > 0 ? (
              <span className="text-muted-foreground">已发送 {hiddenAttachmentCount} 个文件</span>
            ) : null}
          </>
        ) : (
          <span className="text-muted-foreground">已发送 {attachmentCount} 个文件</span>
        )
      ) : isStreaming ? (
        <span className="inline-block h-4 w-1 animate-pulse bg-current" />
      ) : (
        <span className="text-muted-foreground">空消息</span>
      )}
    </div>
  );
}

export function MessageBubble({
  message,
  allowAttachmentPreviews = true,
  canEdit = false,
  onEdit,
  onRetry,
  isStreaming,
  showActions = true,
}: MessageBubbleProps) {
  const isUser = message.role === "user";
  const displayKind =
    typeof message.metadata?.display_kind === "string" ? message.metadata.display_kind : null;
  const isThinkingBlock = !isUser && displayKind === "thinking";

  return (
    <div
      className={cn("group/message flex min-w-0 w-full", isUser ? "justify-end" : "justify-start")}
    >
      <div className={cn("flex min-w-0 w-full flex-col", isUser ? "items-end" : "items-start")}>
        <div
          className={cn(
            "relative min-w-0 max-w-full",
            isUser
              ? "ml-auto max-w-full rounded-2xl bg-muted px-4 py-3 text-foreground"
              : "w-full text-foreground",
          )}
        >
          {!isThinkingBlock ? (
            <MessageBody
              allowAttachmentPreviews={allowAttachmentPreviews}
              isStreaming={isStreaming}
              message={message}
            />
          ) : null}
        </div>

        {!showActions || isThinkingBlock ? null : isUser ? (
          <div className="mt-1 flex w-full justify-end gap-1 opacity-0 transition-opacity group-hover/message:opacity-100">
            <Tooltip>
              <TooltipTrigger
                render={<CopyButton text={message.content_text || ""} className="h-7 w-7" />}
              />
              <TooltipContent>
                <p>复制</p>
              </TooltipContent>
            </Tooltip>

            {canEdit ? (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7"
                      aria-label="编辑消息"
                      onClick={() => onEdit?.(message)}
                    >
                      <Pencil className="h-4 w-4" />
                      <span className="sr-only">编辑</span>
                    </Button>
                  }
                />
                <TooltipContent>
                  <p>编辑</p>
                </TooltipContent>
              </Tooltip>
            ) : null}
          </div>
        ) : (
          <div className="mt-1 flex w-full items-center justify-start gap-1">
            <Tooltip>
              <TooltipTrigger
                render={<CopyButton text={message.content_text || ""} className="h-7 w-7" />}
              />
              <TooltipContent>
                <p>复制</p>
              </TooltipContent>
            </Tooltip>

            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-7 w-7"
                    disabled={isStreaming}
                    onClick={() => onRetry?.(message)}
                  >
                    <RotateCcw className="h-4 w-4" />
                    <span className="sr-only">重试</span>
                  </Button>
                }
              />
              <TooltipContent>
                <p>重试</p>
              </TooltipContent>
            </Tooltip>
          </div>
        )}
      </div>
    </div>
  );
}

export function AssistantTurnBubble({
  activityLabel,
  canRetry = true,
  isStreaming,
  messages,
  onOpenTimeline,
  onRetry,
  turnId,
  turn,
  variantCount = 1,
  variantIndex = 0,
  onVariantChange,
}: AssistantTurnBubbleProps) {
  const hasThinkingMarker = messages.some(
    (message) => message.metadata?.display_kind === "thinking",
  );
  const outputMessages = messages.filter(
    (message) => message.metadata?.display_kind !== "thinking",
  );
  const lastOutput = outputMessages.at(-1);
  const copyText = outputMessages
    .map((message) => message.content_text?.trim() || "")
    .filter(Boolean)
    .join("\n\n");

  const timelineControl = (
    <TurnTimeline
      activityLabel={activityLabel}
      turn={turn}
      turnId={turnId}
      isStreaming={isStreaming}
      onOpen={(nextTurnId) => onOpenTimeline?.(nextTurnId)}
    />
  );

  return (
    <div className="group/message flex w-full justify-start">
      <div className="flex w-full flex-col items-start">
        <div className="relative w-full text-foreground">
          <div className="space-y-4">
            {messages.map((message) =>
              message.metadata?.display_kind === "thinking" ? (
                <div key={message.id}>{timelineControl}</div>
              ) : (
                <div key={message.id} className="space-y-4">
                  {!hasThinkingMarker && message.id === lastOutput?.id ? timelineControl : null}
                  <MessageBody isStreaming={isStreaming} message={message} />
                </div>
              ),
            )}
          </div>
        </div>

        {lastOutput ? (
          <div className="mt-1 flex w-full items-center justify-start gap-1">
            <Tooltip>
              <TooltipTrigger render={<CopyButton text={copyText} className="h-7 w-7" />} />
              <TooltipContent>
                <p>复制</p>
              </TooltipContent>
            </Tooltip>

            {canRetry ? (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7"
                      disabled={isStreaming}
                      onClick={() => onRetry?.(lastOutput)}
                    >
                      <RotateCcw className="h-4 w-4" />
                      <span className="sr-only">重试</span>
                    </Button>
                  }
                />
                <TooltipContent>
                  <p>重试</p>
                </TooltipContent>
              </Tooltip>
            ) : null}

            {variantCount > 1 && !isStreaming ? (
              <div className="ml-1 flex h-7 items-center gap-0.5 text-xs text-muted-foreground">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7"
                  aria-label="上一个回复版本"
                  disabled={variantIndex === 0}
                  onClick={() => onVariantChange?.(variantIndex - 1)}
                >
                  <ChevronLeft className="h-4 w-4" />
                </Button>
                <span className="min-w-9 text-center tabular-nums">
                  {variantIndex + 1} / {variantCount}
                </span>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7"
                  aria-label="下一个回复版本"
                  disabled={variantIndex >= variantCount - 1}
                  onClick={() => onVariantChange?.(variantIndex + 1)}
                >
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  );
}
