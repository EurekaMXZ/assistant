"use client";

import { ComposerShell } from "@/components/chat/composer-shell";
import type { Model, ReasoningEffort } from "@/lib/types";

interface WelcomePanelProps {
  actions?: React.ReactNode;
  disabled?: boolean;
  files?: File[];
  models?: Model[];
  modelsLoading?: boolean;
  modelId?: string;
  placeholder?: string;
  prompt?: string;
  submitting?: boolean;
  value: string;
  onChange: (value: string) => void;
  onFilesChange?: (files: File[]) => void;
  onModelChange?: (modelId: string) => void;
  onModelReasoningEffortChange?: (modelId: string, effort: ReasoningEffort | "") => void;
  onSubmit: () => void;
  reasoningEfforts?: Record<string, ReasoningEffort>;
}

export function WelcomePanel({
  actions,
  disabled,
  files = [],
  models = [],
  modelsLoading,
  modelId = "",
  placeholder = "输入消息",
  prompt = "你好，今天想聊点什么？",
  submitting,
  value,
  onChange,
  onFilesChange,
  onModelChange,
  onModelReasoningEffortChange,
  onSubmit,
  reasoningEfforts = {},
}: WelcomePanelProps) {
  const keyedFiles = files.map((file, index) => ({
    contentType: file.type,
    file,
    key: `${file.name}-${file.size}-${file.lastModified}-${index}`,
    name: file.name,
    size: file.size,
  }));
  return (
    <div className="relative flex flex-1 items-center justify-center px-6">
      {actions ? (
        <div className="absolute right-6 top-6 z-10 hidden md:block">{actions}</div>
      ) : null}
      <div className="w-full max-w-2xl space-y-6 text-center">
        <h1 className="text-3xl font-semibold tracking-tight text-foreground">{prompt}</h1>
        <ComposerShell
          attachments={keyedFiles}
          autoFocus
          busy={submitting}
          disabled={disabled}
          models={models}
          modelsLoading={modelsLoading}
          modelId={modelId}
          onChange={onChange}
          onFilesSelected={(selected) => onFilesChange?.([...files, ...selected])}
          onModelChange={onModelChange}
          onModelReasoningEffortChange={onModelReasoningEffortChange}
          onRemoveAttachment={(key) => {
            const index = keyedFiles.findIndex((item) => item.key === key);
            if (index !== -1) onFilesChange?.(files.filter((_, fileIndex) => fileIndex !== index));
          }}
          onSubmit={onSubmit}
          placeholder={placeholder}
          reasoningEfforts={reasoningEfforts}
          value={value}
        />
      </div>
    </div>
  );
}
