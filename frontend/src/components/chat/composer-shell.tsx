"use client";

import { useRef } from "react";
import { ArrowUp, Loader2, Upload, X } from "lucide-react";
import { useAutosizeTextarea } from "@/hooks/use-autosize-textarea";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { ComposerOptions } from "./composer-options";
import type { Model, ReasoningEffort } from "@/lib/types";
import { cn } from "@/lib/utils";

export interface ComposerShellAttachment {
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
  const { currentHeight, isMultiline, maxHeight, singleLineHeight } = useAutosizeTextarea(
    textareaRef,
    value,
    5,
  );
  const attachmentOffset = attachments.length > 0 ? 40 : 0;
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
    <div className={cn("relative mx-auto w-full max-w-2xl overflow-hidden rounded-[28px] border bg-background shadow-sm", className)} style={{ height: shellHeight }}>
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
        <div className="absolute left-3 right-3 top-2 flex min-w-0 gap-1.5 overflow-x-auto">
          {attachments.map((attachment) => (
            <span key={attachment.key} className="inline-flex max-w-48 shrink-0 items-center gap-1 rounded-full border bg-muted px-2 py-1 text-xs text-muted-foreground" title={attachment.name}>
              <span className="truncate">{attachment.name}</span>
              <span className="shrink-0 text-muted-foreground/70">{formatComposerFileSize(attachment.size)}</span>
              <button type="button" className="ml-0.5 inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full hover:bg-background hover:text-foreground" onClick={() => onRemoveAttachment?.(attachment.key)}>
                <X className="h-3 w-3" />
                <span className="sr-only">移除文件</span>
              </button>
            </span>
          ))}
        </div>
      ) : null}

      <Textarea
        ref={textareaRef}
        rows={1}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
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
        className={`absolute min-h-0 resize-none border-0 bg-transparent pl-2 py-0 leading-6 shadow-none [field-sizing:fixed] focus-visible:ring-0 ${isMultiline ? "right-3" : "right-[224px]"}`}
        style={isMultiline
          ? { top: 8 + attachmentOffset, left: 52, height: maxHeight }
          : { top: lowerAreaCenterY, left: 52, height: singleLineHeight, transform: "translateY(-50%)" }}
        autoFocus={autoFocus}
      />

      <Button
        type="button"
        size="icon"
        variant="ghost"
        className="absolute rounded-full text-muted-foreground hover:text-foreground"
        disabled={inactive || uploadBusy}
        onClick={() => fileInputRef.current?.click()}
        style={isMultiline ? { left: 12, bottom: 12 } : { left: 12, top: lowerAreaCenterY, transform: "translateY(-50%)" }}
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
        onModelReasoningEffortChange={(targetModelId, effort) => onModelReasoningEffortChange?.(targetModelId, effort)}
        style={isMultiline ? { bottom: 14 } : { top: lowerAreaCenterY, transform: "translateY(-50%)" }}
      />

      <Button
        size="icon"
        onClick={onSubmit}
        disabled={(!value.trim() && attachments.length === 0) || inactive || uploadBusy}
        className="absolute rounded-full"
        style={isMultiline ? { right: 12, bottom: 12 } : { right: 12, top: lowerAreaCenterY, transform: "translateY(-50%)" }}
      >
        {busy || uploadBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : <ArrowUp className="h-4 w-4" />}
        <span className="sr-only">发送</span>
      </Button>
    </div>
  );
}
