"use client";

import type { Attachment, Model, ReasoningEffort } from "@/lib/types";
import { ComposerShell } from "./composer-shell";

interface ComposerProps {
  attachments?: Attachment[];
  inputRef?: React.RefObject<HTMLTextAreaElement | null>;
  models?: Model[];
  modelsLoading?: boolean;
  modelId?: string;
  onChange: (content: string) => void;
  onRemoveAttachment?: (attachmentId: string) => void;
  onModelChange?: (modelId: string) => void;
  onModelReasoningEffortChange?: (modelId: string, effort: ReasoningEffort | "") => void;
  onSend: (content: string, attachmentIds: string[]) => Promise<void> | void;
  onUploadFiles?: (files: File[]) => Promise<void> | void;
  disabled?: boolean;
  placeholder?: string;
  reasoningEfforts?: Record<string, ReasoningEffort>;
  uploadingAttachments?: boolean;
  value: string;
}

export function Composer({
  attachments = [],
  inputRef,
  models = [],
  modelsLoading,
  modelId = "",
  onChange,
  onRemoveAttachment,
  onModelChange,
  onModelReasoningEffortChange,
  onSend,
  onUploadFiles,
  disabled,
  placeholder = "输入消息…",
  reasoningEfforts = {},
  uploadingAttachments,
  value,
}: ComposerProps) {
  return (
    <div className="pointer-events-none absolute inset-x-0 bottom-0 z-10 px-4 pb-4 pt-3 sm:px-6">
      <ComposerShell
        attachments={attachments.map((attachment) => ({
          attachmentId: attachment.id,
          contentType: attachment.content_type,
          conversationId: attachment.conversation_id,
          key: attachment.id,
          name: attachment.filename,
          size: attachment.size_bytes,
        }))}
        className="pointer-events-auto"
        disabled={disabled}
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
            attachments.map((attachment) => attachment.id),
          )
        }
        placeholder={placeholder}
        reasoningEfforts={reasoningEfforts}
        uploadBusy={uploadingAttachments}
        value={value}
      />
    </div>
  );
}
