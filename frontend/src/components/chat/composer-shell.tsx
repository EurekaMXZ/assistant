"use client";

import { useEffect, useRef, useState } from "react";
import {
  Archive,
  ArrowUp,
  FileIcon,
  FileSpreadsheet,
  FileText,
  ImageIcon,
  Loader2,
  Upload,
  X,
} from "lucide-react";
import { useAutosizeTextarea } from "@/hooks/use-autosize-textarea";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Textarea } from "@/components/ui/textarea";
import { getConversationAttachmentBlob } from "@/lib/api";
import { ComposerOptions } from "./composer-options";
import type { Model, ReasoningEffort } from "@/lib/types";
import { cn } from "@/lib/utils";

export interface ComposerShellAttachment {
  attachmentId?: string;
  contentType?: string;
  conversationId?: string;
  file?: File;
  key: string;
  name: string;
  size: number;
}

interface ComposerShellProps {
  attachments: ComposerShellAttachment[];
  autoFocus?: boolean;
  busy?: boolean;
  className?: string;
  disabled?: boolean;
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
  placeholder: string;
  reasoningEfforts: Record<string, ReasoningEffort>;
  uploadBusy?: boolean;
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
      iconClassName: "bg-emerald-500 text-white",
    };
  }
  if (["7z", "gz", "rar", "tar", "zip"].includes(extension)) {
    return {
      label: "压缩文件",
      icon: <Archive className="size-5" />,
      iconClassName: "bg-amber-500 text-white",
    };
  }
  if (extension === "pdf") {
    return {
      label: "PDF 文档",
      icon: <FileText className="size-5" />,
      iconClassName: "bg-red-500 text-white",
    };
  }
  if (
    attachment.contentType?.startsWith("text/") ||
    ["doc", "docx", "md", "rtf", "txt"].includes(extension)
  ) {
    return {
      label: "文档",
      icon: <FileText className="size-5" />,
      iconClassName: "bg-blue-500 text-white",
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
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const [previewFailed, setPreviewFailed] = useState(false);
  const [previewOpen, setPreviewOpen] = useState(false);

  useEffect(() => {
    if (!image) return;

    let cancelled = false;
    let objectUrl: string | null = null;
    setPreviewUrl(null);
    setPreviewFailed(false);

    const loadPreview = async () => {
      try {
        const blob = attachment.file
          ? attachment.file
          : attachment.conversationId && attachment.attachmentId
            ? await getConversationAttachmentBlob(
                attachment.conversationId,
                attachment.attachmentId,
              )
            : null;
        if (!blob) {
          if (!cancelled) setPreviewFailed(true);
          return;
        }
        objectUrl = URL.createObjectURL(blob);
        if (!cancelled) setPreviewUrl(objectUrl);
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
      className="absolute right-1 top-1 z-10 size-5 rounded-full bg-foreground p-0 text-background shadow-none hover:bg-foreground/80"
      onClick={onRemove}
    >
      <X className="size-3.5" />
    </Button>
  );

  if (image) {
    return (
      <>
        <div className="relative size-16 shrink-0">
          <button
            type="button"
            aria-label={`预览 ${attachment.name}`}
            className="flex size-16 items-center justify-center overflow-hidden rounded-lg border bg-muted transition-colors hover:border-foreground/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            disabled={!previewUrl}
            onClick={() => setPreviewOpen(true)}
          >
            {previewUrl ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img
                src={previewUrl}
                alt={attachment.name}
                className="size-full object-cover"
                onError={() => setPreviewFailed(true)}
              />
            ) : previewFailed ? (
              <ImageIcon className="size-5 text-muted-foreground" />
            ) : (
              <Loader2 className="size-4 animate-spin text-muted-foreground" />
            )}
          </button>
          {removeButton}
        </div>

        <Dialog open={previewOpen} onOpenChange={setPreviewOpen}>
          <DialogContent className="max-w-[calc(100%-2rem)] gap-0 bg-black p-2 text-white ring-white/15 sm:max-w-4xl">
            <DialogTitle className="sr-only">{attachment.name}</DialogTitle>
            {previewUrl ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img
                src={previewUrl}
                alt={attachment.name}
                className="max-h-[82vh] w-full object-contain"
              />
            ) : null}
          </DialogContent>
        </Dialog>
      </>
    );
  }

  const presentation = attachmentPresentation(attachment);
  return (
    <div className="relative flex h-16 w-60 shrink-0 items-center gap-3 rounded-lg border bg-background p-2 pr-8">
      <span
        className={cn(
          "flex size-11 shrink-0 items-center justify-center rounded-md",
          presentation.iconClassName,
        )}
      >
        {presentation.icon}
      </span>
      <span className="min-w-0 text-left">
        <span className="block truncate text-sm font-medium">{attachment.name}</span>
        <span className="mt-0.5 block truncate text-xs text-muted-foreground">
          {presentation.label} · {formatComposerFileSize(attachment.size)}
        </span>
      </span>
      {removeButton}
    </div>
  );
}

export function ComposerShell({
  attachments,
  autoFocus,
  busy,
  className,
  disabled,
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
  placeholder,
  reasoningEfforts,
  uploadBusy,
  value,
}: ComposerShellProps) {
  const fallbackRef = useRef<HTMLTextAreaElement>(null);
  const textareaRef = inputRef ?? fallbackRef;
  const fileInputRef = useRef<HTMLInputElement>(null);
  const inactive = Boolean(disabled || busy);
  const { currentHeight, isMultiline, singleLineHeight } = useAutosizeTextarea(
    textareaRef,
    value,
    5,
    { singleLineLeft: 52, singleLineRight: 224, multilineLeft: 12, multilineRight: 12 },
  );
  const attachmentOffset = attachments.length > 0 ? 76 : 0;
  const lowerAreaCenterY = attachmentOffset + 28;
  const shellHeight = isMultiline ? currentHeight + 64 + attachmentOffset : 56 + attachmentOffset;

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
        "relative mx-auto w-full max-w-2xl overflow-hidden rounded-[28px] border bg-background shadow-sm",
        className,
      )}
      style={{ height: shellHeight }}
    >
      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(event) => {
          const files = Array.from(event.target.files || []);
          event.target.value = "";
          if (files.length && !inactive && !uploadBusy) onFilesSelected?.(files);
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
        className={`absolute w-auto min-h-0 resize-none border-0 bg-transparent pl-2 py-0 leading-6 shadow-none [field-sizing:fixed] focus-visible:ring-0 ${isMultiline ? "right-3" : "right-[224px]"}`}
        style={
          isMultiline
            ? { top: 8 + attachmentOffset, left: 12, height: currentHeight }
            : {
                top: lowerAreaCenterY,
                left: 52,
                height: singleLineHeight,
                transform: "translateY(-50%)",
              }
        }
        autoFocus={autoFocus}
      />

      <Button
        type="button"
        size="icon"
        variant="ghost"
        className="absolute rounded-full text-muted-foreground hover:text-foreground"
        disabled={inactive || uploadBusy}
        onClick={() => fileInputRef.current?.click()}
        style={{ left: 12, bottom: 10 }}
      >
        {uploadBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
        <span className="sr-only">上传文件</span>
      </Button>

      <ComposerOptions
        className="absolute right-14"
        disabled={inactive || uploadBusy}
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
        onClick={onSubmit}
        disabled={(!value.trim() && attachments.length === 0) || inactive || uploadBusy}
        className="absolute rounded-full"
        style={{ right: 12, bottom: 10 }}
      >
        {busy || uploadBusy ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : (
          <ArrowUp className="h-4 w-4" />
        )}
        <span className="sr-only">发送</span>
      </Button>
    </div>
  );
}
