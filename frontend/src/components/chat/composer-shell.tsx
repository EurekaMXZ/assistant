"use client";

import { useEffect, useRef, useState } from "react";
import {
  Archive,
  ArrowUp,
  CircleAlert,
  FileIcon,
  FileSpreadsheet,
  FileText,
  ImageIcon,
  Square,
  Upload,
  X,
} from "lucide-react";
import { Spinner } from "@/components/shared/spinner";
import { useAutosizeTextarea } from "@/hooks/use-autosize-textarea";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { getConversationAttachmentUrl } from "@/lib/api";
import { ImagePreview } from "./image-preview";
import { ComposerOptions } from "./composer-options";
import type { Model, ReasoningEffort } from "@/lib/types";
import { cn } from "@/lib/utils";

export interface ComposerShellAttachment {
  attachmentId?: string;
  contentType?: string;
  conversationId?: string;
  error?: string;
  file?: File;
  key: string;
  name: string;
  size: number;
  status: "uploading" | "ready" | "failed";
}

interface ComposerShellProps {
  allowEmpty?: boolean;
  attachments: ComposerShellAttachment[];
  autoFocus?: boolean;
  busy?: boolean;
  cancelling?: boolean;
  className?: string;
  disabled?: boolean;
  editing?: boolean;
  inputRef?: React.RefObject<HTMLTextAreaElement | null>;
  models: Model[];
  modelsLoading?: boolean;
  modelId: string;
  onChange: (value: string) => void;
  onFilesSelected?: (files: File[]) => void;
  onModelChange?: (modelId: string) => void;
  onModelReasoningEffortChange?: (modelId: string, effort: ReasoningEffort | "") => void;
  onRemoveAttachment?: (key: string) => void;
  onSubmit: () => void;
  onCancel?: () => void;
  placeholder: string;
  reasoningEfforts: Record<string, ReasoningEffort>;
  streaming?: boolean;
  value: string;
}

export function formatComposerFileSize(size: number) {
  if (!Number.isFinite(size) || size <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = size;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  return `${value >= 10 || unitIndex === 0 ? Math.round(value) : value.toFixed(1)} ${units[unitIndex]}`;
}

function attachmentExtension(name: string) {
  return name.split(".").pop()?.toLowerCase() || "";
}

function isImageAttachment(attachment: ComposerShellAttachment) {
  return (
    attachment.contentType?.startsWith("image/") ||
    ["avif", "bmp", "gif", "heic", "jpeg", "jpg", "png", "svg", "webp"].includes(
      attachmentExtension(attachment.name),
    )
  );
}

function attachmentPresentation(attachment: ComposerShellAttachment) {
  const extension = attachmentExtension(attachment.name);

  if (["csv", "ods", "xls", "xlsx"].includes(extension)) {
    return {
      label: "电子表格",
      icon: <FileSpreadsheet className="size-5" />,
      iconClassName: "bg-file-spreadsheet text-white",
    };
  }
  if (["7z", "gz", "rar", "tar", "zip"].includes(extension)) {
    return {
      label: "压缩文件",
      icon: <Archive className="size-5" />,
      iconClassName: "bg-file-archive text-white",
    };
  }
  if (extension === "pdf") {
    return {
      label: "PDF 文档",
      icon: <FileText className="size-5" />,
      iconClassName: "bg-file-pdf text-white",
    };
  }
  if (
    attachment.contentType?.startsWith("text/") ||
    ["doc", "docx", "md", "rtf", "txt"].includes(extension)
  ) {
    return {
      label: "文档",
      icon: <FileText className="size-5" />,
      iconClassName: "bg-file-document text-white",
    };
  }
  return {
    label: "文件",
    icon: <FileIcon className="size-5" />,
    iconClassName: "bg-muted text-muted-foreground",
  };
}

function ComposerAttachmentItem({
  attachment,
  onRemove,
}: {
  attachment: ComposerShellAttachment;
  onRemove: () => void;
}) {
  const image = isImageAttachment(attachment);
  const uploading = attachment.status === "uploading";
  const failed = attachment.status === "failed";
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const [previewFailed, setPreviewFailed] = useState(false);

  useEffect(() => {
    if (!image) return;

    let cancelled = false;
    let objectUrl: string | null = null;
    setPreviewUrl(null);
    setPreviewFailed(false);

    const loadPreview = async () => {
      try {
        const source = attachment.file
          ? URL.createObjectURL(attachment.file)
          : attachment.conversationId && attachment.attachmentId
            ? await getConversationAttachmentUrl(attachment.conversationId, attachment.attachmentId)
            : null;
        if (!source) {
          if (!cancelled) setPreviewFailed(true);
          return;
        }
        if (attachment.file) objectUrl = source;
        if (!cancelled) setPreviewUrl(source);
      } catch {
        if (!cancelled) setPreviewFailed(true);
      }
    };

    void loadPreview();
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [attachment.attachmentId, attachment.conversationId, attachment.file, image]);

  const removeButton = (
    <Button
      type="button"
      variant="default"
      size="icon-xs"
      aria-label={`移除 ${attachment.name}`}
      className="absolute right-1 top-1 z-10 rounded-full bg-transparent p-0 shadow-none hover:bg-transparent md:size-5"
      onClick={onRemove}
    >
      <span className="flex size-5 items-center justify-center rounded-full bg-foreground text-background hover:bg-foreground/80">
        <X className="size-3.5" />
      </span>
    </Button>
  );

  if (image) {
    return (
      <div
        className="relative size-16 shrink-0"
        data-upload-state={attachment.status}
        title={failed ? attachment.error || "上传失败" : undefined}
      >
        <div
          className={cn(
            "size-16 transition-opacity",
            uploading && "opacity-45",
            failed && "opacity-35",
          )}
        >
          {previewUrl && !previewFailed ? (
            <ImagePreview
              src={previewUrl}
              alt={attachment.name}
              wrapperClassName="size-16"
              previewButtonClassName="size-16 border bg-muted transition-colors hover:border-foreground/30"
              imageClassName="size-full object-cover"
              showActions={false}
              onError={() => setPreviewFailed(true)}
            />
          ) : (
            <div className="flex size-16 items-center justify-center overflow-hidden rounded-lg border bg-muted">
              {previewFailed ? (
                <ImageIcon className="size-5 text-muted-foreground" />
              ) : (
                <Spinner className="text-muted-foreground" />
              )}
            </div>
          )}
        </div>
        {uploading ? (
          <span
            className="pointer-events-none absolute inset-0 flex items-center justify-center text-foreground"
            aria-label={`正在上传 ${attachment.name}`}
          >
            <Spinner className="size-5" />
          </span>
        ) : null}
        {failed ? (
          <span
            className="pointer-events-none absolute inset-0 flex items-center justify-center text-destructive"
            aria-label={`${attachment.name} 上传失败`}
          >
            <CircleAlert className="size-5" />
          </span>
        ) : null}
        {removeButton}
      </div>
    );
  }

  const presentation = attachmentPresentation(attachment);
  return (
    <div
      className={cn(
        "relative flex h-16 w-60 shrink-0 items-center gap-3 rounded-lg border p-2 pr-8 transition-colors",
        uploading ? "bg-background/45" : "bg-background",
      )}
      data-upload-state={attachment.status}
      title={failed ? attachment.error || "上传失败" : undefined}
    >
      <span
        className={cn(
          "flex size-11 shrink-0 items-center justify-center rounded-md",
          uploading
            ? "bg-muted text-muted-foreground"
            : failed
              ? "bg-destructive/10 text-destructive"
              : presentation.iconClassName,
        )}
        aria-label={
          uploading
            ? `正在上传 ${attachment.name}`
            : failed
              ? `${attachment.name} 上传失败`
              : undefined
        }
      >
        {uploading ? (
          <Spinner className="size-5" />
        ) : failed ? (
          <CircleAlert className="size-5" />
        ) : (
          presentation.icon
        )}
      </span>
      <span className={cn("min-w-0 text-left", uploading && "opacity-55")}>
        <span className="block truncate text-sm font-medium">{attachment.name}</span>
        <span
          className={cn(
            "mt-0.5 block truncate text-xs text-muted-foreground",
            failed && "text-destructive",
          )}
        >
          {uploading
            ? `正在上传 · ${formatComposerFileSize(attachment.size)}`
            : failed
              ? `上传失败 · ${formatComposerFileSize(attachment.size)}`
              : `${presentation.label} · ${formatComposerFileSize(attachment.size)}`}
        </span>
      </span>
      {removeButton}
    </div>
  );
}

export function ComposerShell({
  allowEmpty,
  attachments,
  autoFocus,
  busy,
  cancelling,
  className,
  disabled,
  editing,
  inputRef,
  models,
  modelsLoading,
  modelId,
  onChange,
  onFilesSelected,
  onModelChange,
  onModelReasoningEffortChange,
  onRemoveAttachment,
  onSubmit,
  onCancel,
  placeholder,
  reasoningEfforts,
  streaming,
  value,
}: ComposerShellProps) {
  const fallbackRef = useRef<HTMLTextAreaElement>(null);
  const textareaRef = inputRef ?? fallbackRef;
  const fileInputRef = useRef<HTMLInputElement>(null);
  const inactive = Boolean(disabled || busy);
  const hasReadyAttachment = attachments.some(
    (attachment) => attachment.status === "ready" && attachment.attachmentId,
  );
  const { currentHeight, isMultiline } = useAutosizeTextarea(textareaRef, value, 5, {
    singleLineLeft: 52,
    singleLineRight: 224,
    multilineLeft: 12,
    multilineRight: 12,
  });
  const attachmentOffset = attachments.length > 0 ? 76 : 0;
  const lowerAreaCenterY = attachmentOffset + 28;
  const multilineShellHeight = currentHeight + 64 + attachmentOffset;
  const desktopShellHeight = isMultiline ? multilineShellHeight : 56 + attachmentOffset;
  const shellStyle = {
    "--composer-mobile-height": `${multilineShellHeight}px`,
    "--composer-desktop-height": `${desktopShellHeight}px`,
  } as React.CSSProperties;
  const textareaStyle = {
    "--composer-mobile-input-top": `${8 + attachmentOffset}px`,
    "--composer-desktop-input-top": `${isMultiline ? 8 + attachmentOffset : lowerAreaCenterY}px`,
    height: currentHeight,
  } as React.CSSProperties;

  const insertText = (text: string) => {
    const textarea = textareaRef.current;
    if (!textarea || inactive) return;
    const start = textarea.selectionStart ?? value.length;
    const end = textarea.selectionEnd ?? value.length;
    onChange(`${value.slice(0, start)}${text}${value.slice(end)}`);
    requestAnimationFrame(() => {
      textarea.focus();
      textarea.setSelectionRange(start + text.length, start + text.length);
    });
  };

  return (
    <div
      className={cn(
        "relative mx-auto h-[var(--composer-mobile-height)] w-full max-w-2xl overflow-hidden rounded-[28px] border bg-background shadow-sm md:h-[var(--composer-desktop-height)]",
        className,
      )}
      style={shellStyle}
    >
      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(event) => {
          const files = Array.from(event.target.files || []);
          event.target.value = "";
          if (files.length && !inactive) onFilesSelected?.(files);
        }}
      />

      {attachments.length > 0 ? (
        <div className="absolute left-3 right-3 top-2 flex min-w-0 gap-2 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
          {attachments.map((attachment) => (
            <ComposerAttachmentItem
              key={attachment.key}
              attachment={attachment}
              onRemove={() => onRemoveAttachment?.(attachment.key)}
            />
          ))}
        </div>
      ) : null}

      <Textarea
        ref={textareaRef}
        rows={1}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Backspace" && value.length === 0 && attachments.length > 0) {
            event.preventDefault();
            onRemoveAttachment?.(attachments[attachments.length - 1].key);
            return;
          }
          if (event.key !== "Enter" || event.nativeEvent.isComposing) return;
          if (event.ctrlKey) {
            event.preventDefault();
            insertText("\n");
          } else if (!event.shiftKey && !event.metaKey && !event.altKey) {
            event.preventDefault();
            onSubmit();
          }
        }}
        onPaste={(event) => {
          const text = event.clipboardData.getData("text");
          if (!text) return;
          event.preventDefault();
          insertText(text);
        }}
        placeholder={placeholder}
        disabled={inactive}
        className={cn(
          "absolute left-3 right-3 top-[var(--composer-mobile-input-top)] w-auto min-h-0 translate-y-0 resize-none border-0 bg-transparent py-0 pl-2 leading-6 shadow-none [field-sizing:fixed] focus-visible:ring-0 md:top-[var(--composer-desktop-input-top)]",
          !isMultiline && "md:left-[52px] md:right-[224px] md:-translate-y-1/2",
        )}
        style={textareaStyle}
        autoFocus={autoFocus}
      />

      <Button
        type="button"
        size="icon"
        variant="ghost"
        className="absolute rounded-full text-muted-foreground hover:text-foreground"
        disabled={inactive || editing}
        onClick={() => fileInputRef.current?.click()}
        style={{ left: 12, bottom: 10 }}
      >
        <Upload className="size-4" />
        <span className="sr-only">上传文件</span>
      </Button>

      <ComposerOptions
        className="absolute right-14"
        disabled={inactive || editing}
        models={models}
        modelsLoading={modelsLoading}
        modelId={modelId}
        reasoningEfforts={reasoningEfforts}
        onModelChange={(next) => onModelChange?.(next)}
        onModelReasoningEffortChange={(targetModelId, effort) =>
          onModelReasoningEffortChange?.(targetModelId, effort)
        }
        style={{ bottom: 10 }}
      />

      <Button
        size="icon"
        onClick={streaming ? onCancel : onSubmit}
        disabled={
          streaming
            ? cancelling || !onCancel
            : (!allowEmpty && !value.trim() && !hasReadyAttachment) || inactive
        }
        className="absolute rounded-full"
        style={{ right: 12, bottom: 10 }}
      >
        {streaming && cancelling ? (
          <Spinner />
        ) : streaming ? (
          <Square className="size-3.5 fill-current" />
        ) : busy ? (
          <Spinner />
        ) : (
          <ArrowUp className="size-4" />
        )}
        <span className="sr-only">
          {streaming ? (cancelling ? "正在停止生成" : "停止生成") : "发送"}
        </span>
      </Button>
    </div>
  );
}
