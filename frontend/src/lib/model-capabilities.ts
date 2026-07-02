import type { Model, ReasoningEffort } from "@/lib/types";

const allReasoningEfforts: ReasoningEffort[] = ["low", "medium", "high", "xhigh"];

export function supportedReasoningEfforts(model: Model | null): ReasoningEffort[] {
  if (!model) return [];
  return allReasoningEfforts.filter((effort) => model.supported_reasoning_efforts.includes(effort));
}
