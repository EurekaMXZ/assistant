"use client";

import type { Model, ReasoningEffort } from "@/lib/types";
import { Pencil, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { ComposerShell, type ComposerShellAttachment } from "./composer-shell";

interface ComposerProps {
  attachments?: ComposerShellAttachment[];
  allowEmpty?: boolean;
  editing?: boolean;
  editingBusy?: boolean;
  inputRef?: React.RefObject<HTMLTextAreaElement | null>;
  models?: Model[];
  modelsLoading?: boolean;
  modelId?: string;
  onChange: (content: string) => void;
  onCancelEdit?: () => void;
  onRemoveAttachment?: (attachmentId: string) => void;
  onModelChange?: (modelId: string) => void;
  onModelReasoningEffortChange?: (modelId: string, effort: ReasoningEffort | "") => void;
  onSend: (content: string, attachmentIds: string[]) => Promise<void> | void;
  onUploadFiles?: (files: File[]) => Promise<void> | void;
  disabled?: boolean;
  placeholder?: string;
  reasoningEfforts?: Record<string, ReasoningEffort>;
  value: string;
}

export function Composer({
  attachments = [],
  allowEmpty,
  editing,
  editingBusy,
  inputRef,
  models = [],
  modelsLoading,
  modelId = "",
  onChange,
  onCancelEdit,
  onRemoveAttachment,
  onModelChange,
  onModelReasoningEffortChange,
  onSend,
  onUploadFiles,
  disabled,
  placeholder = "输入消息…",
  reasoningEfforts = {},
  value,
}: ComposerProps) {
  return (
    <div className="pointer-events-none absolute inset-x-0 bottom-0 z-10 px-4 pb-4 pt-3 sm:px-6">
      {editing ? (
        <div className="pointer-events-auto mx-auto mb-2 flex h-9 w-full max-w-2xl items-center gap-2 rounded-lg border bg-background/95 px-3 text-xs font-medium shadow-sm backdrop-blur-md">
          <Pencil className="size-3.5 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate">编辑消息</span>
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            className="size-6"
            aria-label="取消编辑"
            disabled={editingBusy}
            onClick={onCancelEdit}
          >
            <X className="size-3.5" />
          </Button>
        </div>
      ) : null}
      <ComposerShell
        allowEmpty={allowEmpty}
        attachments={attachments}
        className={cn(
          "pointer-events-auto transition-[box-shadow,border-color] duration-200",
          editing && "border-foreground/35 shadow-lg ring-2 ring-foreground/10",
        )}
        busy={editingBusy}
        disabled={disabled}
        editing={editing}
        inputRef={inputRef}
        models={models}
        modelsLoading={modelsLoading}
        modelId={modelId}
        onChange={onChange}
        onFilesSelected={(files) => void onUploadFiles?.(files)}
        onModelChange={onModelChange}
        onModelReasoningEffortChange={onModelReasoningEffortChange}
        onRemoveAttachment={onRemoveAttachment}
        onSubmit={() =>
          onSend(
            value.trim(),
            attachments.flatMap((attachment) =>
              attachment.status === "ready" && attachment.attachmentId
                ? [attachment.attachmentId]
                : [],
            ),
          )
        }
        placeholder={placeholder}
        reasoningEfforts={reasoningEfforts}
        value={value}
      />
    </div>
  );
}
