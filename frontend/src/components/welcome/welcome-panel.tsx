"use client";

import { ComposerShell, type ComposerShellAttachment } from "@/components/chat/composer-shell";
import type { Model, ReasoningEffort } from "@/lib/types";

interface WelcomePanelProps {
  actions?: React.ReactNode;
  attachments?: ComposerShellAttachment[];
  disabled?: boolean;
  models?: Model[];
  modelsLoading?: boolean;
  modelId?: string;
  placeholder?: string;
  prompt?: string;
  submitting?: boolean;
  value: string;
  onChange: (value: string) => void;
  onRemoveAttachment?: (key: string) => void;
  onFilesSelected?: (files: File[]) => void;
  onModelChange?: (modelId: string) => void;
  onModelReasoningEffortChange?: (modelId: string, effort: ReasoningEffort | "") => void;
  onSubmit: () => void;
  reasoningEfforts?: Record<string, ReasoningEffort>;
}

export function WelcomePanel({
  actions,
  disabled,
  attachments = [],
  models = [],
  modelsLoading,
  modelId = "",
  placeholder = "输入消息",
  prompt = "你好，今天想聊点什么？",
  submitting,
  value,
  onChange,
  onRemoveAttachment,
  onFilesSelected,
  onModelChange,
  onModelReasoningEffortChange,
  onSubmit,
  reasoningEfforts = {},
}: WelcomePanelProps) {
  return (
    <div className="relative flex w-full min-w-0 flex-1 items-center justify-center overflow-hidden px-6">
      {actions ? (
        <div className="absolute right-4 top-0 z-10 hidden h-14 items-center md:flex">
          {actions}
        </div>
      ) : null}
      <div className="w-full max-w-2xl space-y-6 text-center">
        <h1 className="text-3xl font-semibold tracking-tight text-foreground">{prompt}</h1>
        <ComposerShell
          attachments={attachments}
          autoFocus
          busy={submitting}
          disabled={disabled}
          models={models}
          modelsLoading={modelsLoading}
          modelId={modelId}
          onChange={onChange}
          onFilesSelected={onFilesSelected}
          onModelChange={onModelChange}
          onModelReasoningEffortChange={onModelReasoningEffortChange}
          onRemoveAttachment={onRemoveAttachment}
          onSubmit={onSubmit}
          placeholder={placeholder}
          reasoningEfforts={reasoningEfforts}
          value={value}
        />
      </div>
    </div>
  );
}
