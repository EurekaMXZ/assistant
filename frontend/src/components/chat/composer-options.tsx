"use client";

import { Check, Loader2, Settings2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import type { Model, ReasoningEffort } from "@/lib/types";
import { supportedReasoningEfforts } from "@/lib/model-capabilities";

interface ComposerOptionsProps {
  className?: string;
  disabled?: boolean;
  models: Model[];
  modelsLoading?: boolean;
  modelId: string;
  reasoningEfforts: Record<string, ReasoningEffort>;
  onModelChange: (value: string) => void;
  onModelReasoningEffortChange: (modelId: string, value: ReasoningEffort | "") => void;
  style?: React.CSSProperties;
}

const reasoningOptions: Array<{ value: ReasoningEffort | ""; label: string }> = [
  { value: "", label: "默认强度" },
  { value: "low", label: "low" },
  { value: "medium", label: "medium" },
  { value: "high", label: "high" },
  { value: "xhigh", label: "xhigh" },
];

export function ComposerOptions({
  className,
  disabled,
  models,
  modelsLoading,
  modelId,
  reasoningEfforts,
  onModelChange,
  onModelReasoningEffortChange,
  style,
}: ComposerOptionsProps) {
  const defaultModel = models.find((item) => item.is_default) || null;
  const selectedModel = (modelId ? models.find((item) => item.id === modelId) : defaultModel) || null;
  const selectedEffort = selectedModel ? reasoningEfforts[selectedModel.id] || "" : "";
  const buttonLabel = selectedModel
    ? `${selectedModel.upstream_model}${selectedEffort ? ` ${selectedEffort}` : ""}`
    : modelsLoading
      ? "加载模型"
      : "默认模型";

  return (
    <div className={cn("flex items-center", className)} style={style}>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-8 max-w-40 rounded-full px-2.5 text-muted-foreground hover:text-foreground"
              disabled={disabled}
            />
          }
        >
          <span className="truncate">{buttonLabel}</span>
          <span className="sr-only">选择模型和推理强度</span>
        </DropdownMenuTrigger>

        <DropdownMenuContent side="top" align="end" className="w-72">
          <DropdownMenuGroup>
            <DropdownMenuLabel className="flex items-center justify-between">
              <span>模型</span>
              {modelsLoading ? <Loader2 className="size-3 animate-spin" /> : null}
            </DropdownMenuLabel>
            {models.map((model) => {
            const isDefault = model.id === defaultModel?.id;
            const preferenceValue = isDefault ? "" : model.id;
            const selected = model.id === selectedModel?.id;
            const supportedEfforts = supportedReasoningEfforts(model);
            const configuredEffort = reasoningEfforts[model.id];
            const effort = configuredEffort && supportedEfforts.includes(configuredEffort) ? configuredEffort : "";

            return (
              <div key={model.id} className="flex min-w-0 items-stretch" role="none">
                <DropdownMenuItem
                  className="min-w-0 flex-1 py-2"
                  onClick={() => onModelChange(preferenceValue)}
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-sm font-medium">{model.upstream_model}</span>
                      {isDefault ? <span className="text-xs text-muted-foreground">默认</span> : null}
                    </div>
                    <p className="mt-0.5 truncate text-xs text-muted-foreground">
                      {effort ? `推理强度 ${effort}` : model.display_name}
                    </p>
                  </div>
                  {selected ? <Check className="size-4 shrink-0" /> : null}
                </DropdownMenuItem>

                {supportedEfforts.length > 0 ? (
                  <DropdownMenuSub>
                    <DropdownMenuSubTrigger
                      aria-label={`设置 ${model.upstream_model} 推理强度`}
                      className="my-0.5 w-8 justify-center px-0 text-muted-foreground [&>svg:last-child]:hidden"
                    >
                      <Settings2 className="size-4" />
                    </DropdownMenuSubTrigger>
                    <DropdownMenuSubContent className="w-40">
                      <DropdownMenuRadioGroup
                        value={effort}
                        onValueChange={(value) => onModelReasoningEffortChange(model.id, value as ReasoningEffort | "")}
                      >
                        <DropdownMenuLabel>推理强度</DropdownMenuLabel>
                        {reasoningOptions.filter((option) => !option.value || supportedEfforts.includes(option.value)).map((option) => (
                          <DropdownMenuRadioItem key={option.value || "default"} value={option.value}>
                            {option.label}
                          </DropdownMenuRadioItem>
                        ))}
                      </DropdownMenuRadioGroup>
                    </DropdownMenuSubContent>
                  </DropdownMenuSub>
                ) : null}
              </div>
            );
            })}
            {!modelsLoading && models.length === 0 ? (
              <p className="px-2 py-3 text-sm text-muted-foreground">暂无可用模型</p>
            ) : null}
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
