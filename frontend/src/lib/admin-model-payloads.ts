import type { AdminModelCreatePayload, AdminModelUpdatePayload } from "./api";
import type { Model, ProviderCredential, ReasoningEffort } from "./types";

export interface AdminModelFormValues {
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
}

function positiveSafeInteger(value: string, label: string) {
  if (!/^\d+$/.test(value.trim())) throw new Error(`${label}必须是正整数`);
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed <= 0) {
    throw new Error(`${label}必须是安全范围内的正整数`);
  }
  return parsed;
}

function parseParameters(value: string) {
  const parsed = JSON.parse(value || "{}") as unknown;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("默认参数必须是 JSON 对象");
  }
  return parsed as Record<string, unknown>;
}

function editableModelFields(form: AdminModelFormValues) {
  return {
    credential_id: form.credentialId,
    display_name: form.displayName.trim(),
    description: form.description.trim(),
    supports_tools: form.supportsTools,
    supports_parallel_tools: form.supportsParallelTools,
    supported_reasoning_efforts: [...form.reasoningEfforts],
    context_window_tokens: positiveSafeInteger(form.contextWindow, "上下文 Tokens"),
    max_output_tokens: positiveSafeInteger(form.maxOutput, "最大输出 Tokens"),
    default_parameters: parseParameters(form.defaultParameters),
  };
}

export function buildAdminModelCreatePayload(
  form: AdminModelFormValues,
  credential: ProviderCredential | undefined,
): AdminModelCreatePayload {
  if (!credential) throw new Error("请选择有效的提供方凭据");
  const slug = form.slug.trim();
  const upstreamModel = form.upstreamModel.trim();
  if (!slug || !upstreamModel || !form.displayName.trim()) {
    throw new Error("模型标识、上游模型和显示名称不能为空");
  }
  return {
    provider: credential.provider,
    slug,
    upstream_model: upstreamModel,
    input_modalities: ["text"],
    output_modalities: ["text"],
    ...editableModelFields(form),
  };
}

export function buildAdminModelUpdatePayload(
  form: AdminModelFormValues,
  original: Model,
): AdminModelUpdatePayload {
  const editable = editableModelFields(form);
  return {
    ...editable,
    input_modalities: [...original.input_modalities],
    output_modalities: [...original.output_modalities],
  };
}
