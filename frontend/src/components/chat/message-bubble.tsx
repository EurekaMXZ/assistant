"use client";

import { useEffect, useState } from "react";
import {
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  CircleAlert,
  ExternalLink,
  Info,
  Pencil,
  RotateCcw,
} from "lucide-react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { MarkdownRenderer } from "./markdown-renderer";
import { CopyButton } from "@/components/shared/copy-button";
import { TurnTimeline } from "./turn-timeline";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { Spinner } from "@/components/shared/spinner";
import { getConversationAttachmentUrl } from "@/lib/api";
import { parseSafeAskUserActionURL } from "@/lib/ask-user-action";
import { assistantInteractionFromMessage } from "@/lib/chat-state";
import { cn } from "@/lib/utils";
import type { AskUserInteraction, AskUserOptionTone, Message, Turn } from "@/lib/types";
import { ImagePreview } from "./image-preview";

interface MessageBubbleProps {
  message: Message;
  allowAttachmentPreviews?: boolean;
  canEdit?: boolean;
  onEdit?: (message: Message) => void;
  onRetry?: (message: Message) => void;
  onAnswerInteraction?: (
    turnId: string,
    interaction: AskUserInteraction,
    optionId: string,
  ) => Promise<boolean>;
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
  onAnswerInteraction?: (
    turnId: string,
    interaction: AskUserInteraction,
    optionId: string,
  ) => Promise<boolean>;
  turnId: string;
  turn?: Turn | null;
  variantCount?: number;
  variantIndex?: number;
  onVariantChange?: (index: number) => void;
}

const assistantActionIconInsetPx = 6;
const askUserOptionVariants: Record<AskUserOptionTone, "default" | "outline" | "destructive"> = {
  primary: "default",
  neutral: "outline",
  danger: "destructive",
};

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
    setSrc(null);
    setHidden(false);

    const load = async () => {
      try {
        const objectUrl = await getConversationAttachmentUrl(conversationId, attachment.id);
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
    };
  }, [attachment.id, conversationId]);

  if (hidden) {
    return null;
  }

  return (
    <Card className="overflow-hidden bg-background/60">
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
    </Card>
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

function safeInteractionAction(action: AskUserInteraction["action"]) {
  if (!action) return null;
  const parsed = parseSafeAskUserActionURL(action.url);
  return parsed ? { ...action, ...parsed } : null;
}

function clippedInteractionPrompt(prompt: string, maximum = 96) {
  const characters = Array.from(prompt.trim());
  return characters.length <= maximum
    ? characters.join("")
    : `${characters.slice(0, maximum).join("")}…`;
}

export function AskUserInteractionView({
  interaction,
  onAnswer,
}: {
  interaction: AskUserInteraction;
  onAnswer?: (interaction: AskUserInteraction, optionId: string) => Promise<boolean>;
}) {
  const [submittingOptionId, setSubmittingOptionId] = useState<string | null>(null);
  const [confirmActionOpen, setConfirmActionOpen] = useState(false);
  const action = safeInteractionAction(interaction.action);

  if (interaction.status === "cancelled") {
    return (
      <div
        role="status"
        className="grid min-w-0 grid-cols-[1rem_minmax(0,1fr)] gap-2 font-medium text-muted-foreground"
      >
        <span className="flex h-5 items-center" aria-hidden="true">
          <Info className="size-4" />
        </span>
        <p className="min-w-0 break-words leading-5">已取消</p>
      </div>
    );
  }

  if (interaction.status === "completed") {
    const cancelled = interaction.answer?.status === "cancelled";
    const StatusIcon = cancelled ? Info : CheckCircle2;
    return (
      <div
        role="status"
        className="grid min-w-0 grid-cols-[1rem_minmax(0,1fr)] gap-2 font-medium text-muted-foreground"
      >
        <span className="flex h-5 items-center" aria-hidden="true">
          <StatusIcon className="size-4" />
        </span>
        <p className="min-w-0 break-words leading-5">
          询问用户「{clippedInteractionPrompt(interaction.prompt)}」：
          {interaction.answer?.label || "已完成"}
        </p>
      </div>
    );
  }

  const submitting = submittingOptionId !== null;
  const answer = async (optionId: string) => {
    if (submitting || !onAnswer) return;
    setSubmittingOptionId(optionId);
    try {
      const succeeded = await onAnswer(interaction, optionId);
      if (!succeeded) setSubmittingOptionId(null);
    } catch {
      setSubmittingOptionId(null);
    }
  };

  return (
    <Card className="min-w-0 rounded-xl bg-muted/20 p-4 shadow-xs">
      <p className="min-w-0 break-words font-medium leading-6">{interaction.prompt}</p>
      {action ? (
        <div className="mt-3 space-y-1.5">
          {action.protocol === "https" ? (
            <Button
              type="button"
              variant="secondary"
              size="sm"
              disabled={submitting}
              onClick={() => setConfirmActionOpen(true)}
            >
              {action.label}
              <ExternalLink className="size-3.5" />
            </Button>
          ) : (
            <Button
              render={
                <a href={action.url} onClick={(event) => submitting && event.preventDefault()} />
              }
              nativeButton={false}
              type={undefined}
              variant="secondary"
              size="sm"
              disabled={submitting}
              aria-disabled={submitting}
            >
              {action.label}
              <ExternalLink className="size-3.5" />
            </Button>
          )}
          <p className="break-all text-xs text-muted-foreground">目标：{action.host}</p>
          {action.protocol === "https" ? (
            <ConfirmDialog
              open={confirmActionOpen}
              onOpenChange={setConfirmActionOpen}
              title="打开外部网站？"
              description={`即将打开 ${action.host}，请确认这是你信任的支付目标。`}
              confirmText="继续打开"
              onConfirm={() => {
                const opened = window.open(action.url, "_blank", "noopener,noreferrer");
                if (opened) opened.opener = null;
              }}
            />
          ) : null}
        </div>
      ) : null}
      <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2">
        {interaction.options.map((option) => (
          <Button
            key={option.id}
            type="button"
            variant={askUserOptionVariants[option.tone]}
            className="h-auto min-w-0 justify-start whitespace-normal py-2.5 text-left"
            disabled={submitting || !onAnswer}
            aria-busy={submittingOptionId === option.id}
            onClick={() => void answer(option.id)}
          >
            {submittingOptionId === option.id ? <Spinner /> : null}
            <span className="min-w-0 break-words">{option.label}</span>
          </Button>
        ))}
      </div>
    </Card>
  );
}

function MessageBody({
  allowAttachmentPreviews = true,
  isStreaming,
  message,
  onAnswerInteraction,
}: {
  allowAttachmentPreviews?: boolean;
  isStreaming?: boolean;
  message: Message;
  onAnswerInteraction?: (interaction: AskUserInteraction, optionId: string) => Promise<boolean>;
}) {
  const isUser = message.role === "user";
  const metadata = message.metadata || {};
  const isError = metadata.display_kind === "assistant_error";
  const interaction = assistantInteractionFromMessage(message);
  const alignsWithTimeline =
    isError || interaction?.status === "completed" || interaction?.status === "cancelled";
  const attachments = attachmentsFromMetadata(metadata);
  const imageAttachments = allowAttachmentPreviews ? attachments.filter(isImageAttachment) : [];
  const attachmentCount = attachmentCountFromMetadata(metadata);
  const hiddenAttachmentCount = Math.max(0, attachmentCount - imageAttachments.length);

  return (
    <div
      className="min-w-0 max-w-full leading-relaxed"
      style={
        isUser || alignsWithTimeline
          ? undefined
          : { paddingLeft: `${assistantActionIconInsetPx}px` }
      }
    >
      {interaction ? (
        <AskUserInteractionView interaction={interaction} onAnswer={onAnswerInteraction} />
      ) : isError ? (
        <div
          role="alert"
          className="grid grid-cols-[1rem_minmax(0,1fr)] gap-2 font-medium text-destructive"
        >
          <span className="flex h-5 items-center" aria-hidden="true">
            <CircleAlert className="size-4" />
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
  onAnswerInteraction,
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
              onAnswerInteraction={
                message.turn_id && onAnswerInteraction
                  ? (interaction, optionId) =>
                      onAnswerInteraction(message.turn_id as string, interaction, optionId)
                  : undefined
              }
            />
          ) : null}
        </div>

        {!showActions || isThinkingBlock ? null : isUser ? (
          <div className="mt-1 flex w-full justify-end gap-1 opacity-100 transition-opacity md:opacity-0 md:group-hover/message:opacity-100 md:group-focus-within/message:opacity-100">
            <Tooltip>
              <TooltipTrigger render={<CopyButton text={message.content_text || ""} />} />
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
                      size="icon-md"
                      aria-label="编辑消息"
                      onClick={() => onEdit?.(message)}
                    >
                      <Pencil className="size-4" />
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
              <TooltipTrigger render={<CopyButton text={message.content_text || ""} />} />
              <TooltipContent>
                <p>复制</p>
              </TooltipContent>
            </Tooltip>

            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-md"
                    disabled={isStreaming}
                    onClick={() => onRetry?.(message)}
                  >
                    <RotateCcw className="size-4" />
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
  onAnswerInteraction,
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
    .filter((message) => message.metadata?.display_kind !== "ask_user")
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
                  <MessageBody
                    isStreaming={isStreaming}
                    message={message}
                    onAnswerInteraction={
                      onAnswerInteraction
                        ? (interaction, optionId) =>
                            onAnswerInteraction(turnId, interaction, optionId)
                        : undefined
                    }
                  />
                </div>
              ),
            )}
          </div>
        </div>

        {lastOutput ? (
          <div className="mt-1 flex w-full items-center justify-start gap-1">
            {copyText ? (
              <Tooltip>
                <TooltipTrigger render={<CopyButton text={copyText} />} />
                <TooltipContent>
                  <p>复制</p>
                </TooltipContent>
              </Tooltip>
            ) : null}

            {canRetry ? (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-md"
                      disabled={isStreaming}
                      onClick={() => onRetry?.(lastOutput)}
                    >
                      <RotateCcw className="size-4" />
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
                  size="icon-md"
                  aria-label="上一个回复版本"
                  disabled={variantIndex === 0}
                  onClick={() => onVariantChange?.(variantIndex - 1)}
                >
                  <ChevronLeft className="size-4" />
                </Button>
                <span className="min-w-9 text-center tabular-nums">
                  {variantIndex + 1} / {variantCount}
                </span>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-md"
                  aria-label="下一个回复版本"
                  disabled={variantIndex >= variantCount - 1}
                  onClick={() => onVariantChange?.(variantIndex + 1)}
                >
                  <ChevronRight className="size-4" />
                </Button>
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  );
}
