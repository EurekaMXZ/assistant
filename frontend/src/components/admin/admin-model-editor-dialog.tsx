"use client";

import { SavingIcon } from "@/components/admin/admin-shared";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { FormField } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import type { Model, ProviderCredential, ReasoningEffort } from "@/lib/types";

const efforts: ReasoningEffort[] = ["low", "medium", "high", "xhigh"];

export type ModelForm = {
  credentialId: string;
  slug: string;
  upstreamModel: string;
  displayName: string;
  description: string;
  contextWindow: string;
  maxOutput: string;
  supportsTools: boolean;
  supportsParallelTools: boolean;
  reasoningEfforts: ReasoningEffort[];
  defaultParameters: string;
};

export const emptyModelForm: ModelForm = {
  credentialId: "",
  slug: "",
  upstreamModel: "",
  displayName: "",
  description: "",
  contextWindow: "128000",
  maxOutput: "8192",
  supportsTools: true,
  supportsParallelTools: true,
  reasoningEfforts: [],
  defaultParameters: "{}",
};

export function modelFormFrom(item: Model): ModelForm {
  return {
    credentialId: item.credential_id || "",
    slug: item.slug,
    upstreamModel: item.upstream_model,
    displayName: item.display_name,
    description: item.description,
    contextWindow: String(item.context_window_tokens),
    maxOutput: String(item.max_output_tokens),
    supportsTools: item.supports_tools,
    supportsParallelTools: item.supports_parallel_tools,
    reasoningEfforts: item.supported_reasoning_efforts,
    defaultParameters: JSON.stringify(item.default_parameters || {}, null, 2),
  };
}

export function ModelEditorDialog({
  open,
  onOpenChange,
  editing,
  form,
  setForm,
  credentials,
  saving,
  onSave,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editing: Model | null;
  form: ModelForm;
  setForm: React.Dispatch<React.SetStateAction<ModelForm>>;
  credentials: ProviderCredential[];
  saving: boolean;
  onSave: () => void;
}) {
  const update = <K extends keyof ModelForm>(key: K, value: ModelForm[K]) =>
    setForm((current) => ({ ...current, [key]: value }));
  const toggleEffort = (effort: ReasoningEffort) =>
    update(
      "reasoningEfforts",
      form.reasoningEfforts.includes(effort)
        ? form.reasoningEfforts.filter((item) => item !== effort)
        : [...form.reasoningEfforts, effort],
    );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{editing ? "编辑模型" : "添加模型"}</DialogTitle>
        </DialogHeader>
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField label="提供方凭据" htmlFor="model-credential" className="sm:col-span-2">
            <Select
              items={credentials
                .filter((item) => item.status === "enabled")
                .map((item) => ({ value: item.id, label: `${item.name} · ${item.masked_key}` }))}
              value={form.credentialId}
              onValueChange={(value) => value && update("credentialId", value)}
            >
              <SelectTrigger id="model-credential">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {credentials
                  .filter((item) => item.status === "enabled")
                  .map((item) => (
                    <SelectItem key={item.id} value={item.id}>
                      {item.name} · {item.masked_key}
                    </SelectItem>
                  ))}
              </SelectContent>
            </Select>
          </FormField>
          <FormField label="标识" htmlFor="model-slug">
            <Input
              id="model-slug"
              disabled={!!editing}
              value={form.slug}
              onChange={(event) => update("slug", event.target.value)}
              placeholder="gpt-primary"
            />
          </FormField>
          <FormField label="上游模型" htmlFor="upstream-model">
            <Input
              id="upstream-model"
              disabled={!!editing}
              value={form.upstreamModel}
              onChange={(event) => update("upstreamModel", event.target.value)}
              placeholder="gpt-5.5"
            />
          </FormField>
          <FormField label="显示名称" htmlFor="model-name" className="sm:col-span-2">
            <Input
              id="model-name"
              value={form.displayName}
              onChange={(event) => update("displayName", event.target.value)}
              placeholder="GPT 5.5"
            />
          </FormField>
          <FormField label="说明" htmlFor="model-description" className="sm:col-span-2">
            <Input
              id="model-description"
              value={form.description}
              onChange={(event) => update("description", event.target.value)}
            />
          </FormField>
          <FormField label="上下文 Tokens" htmlFor="context-window">
            <Input
              id="context-window"
              type="number"
              value={form.contextWindow}
              onChange={(event) => update("contextWindow", event.target.value)}
            />
          </FormField>
          <FormField label="最大输出 Tokens" htmlFor="max-output">
            <Input
              id="max-output"
              type="number"
              value={form.maxOutput}
              onChange={(event) => update("maxOutput", event.target.value)}
            />
          </FormField>
          <FormField label="工具能力" className="sm:col-span-2">
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                size="sm"
                variant={form.supportsTools ? "secondary" : "outline"}
                onClick={() => update("supportsTools", !form.supportsTools)}
              >
                工具调用
              </Button>
              <Button
                type="button"
                size="sm"
                variant={form.supportsParallelTools ? "secondary" : "outline"}
                onClick={() => update("supportsParallelTools", !form.supportsParallelTools)}
              >
                并行工具
              </Button>
            </div>
          </FormField>
          <FormField label="支持的推理档位" className="sm:col-span-2">
            <div className="flex flex-wrap gap-2">
              {efforts.map((effort) => (
                <Button
                  key={effort}
                  type="button"
                  size="sm"
                  variant={form.reasoningEfforts.includes(effort) ? "secondary" : "outline"}
                  onClick={() => toggleEffort(effort)}
                >
                  {effort}
                </Button>
              ))}
            </div>
          </FormField>
          <FormField label="默认上游参数" htmlFor="default-parameters" className="sm:col-span-2">
            <Textarea
              id="default-parameters"
              className="min-h-32 font-mono text-xs"
              value={form.defaultParameters}
              onChange={(event) => update("defaultParameters", event.target.value)}
              spellCheck={false}
            />
          </FormField>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button
            disabled={
              saving ||
              !form.credentialId ||
              !form.slug.trim() ||
              !form.upstreamModel.trim() ||
              !form.displayName.trim()
            }
            onClick={onSave}
          >
            <SavingIcon saving={saving} />
            {editing ? "保存更改" : "创建模型"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
