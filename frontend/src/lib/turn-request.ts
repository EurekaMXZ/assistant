import { z } from "zod";
import type { Message, ReasoningEffort } from "./types";

export const TURN_REQUEST_METADATA_KEY = "request_descriptor";

export const turnRequestDescriptorSchema = z.object({
  content: z.string(),
  attachment_ids: z.array(z.string()),
  model_id: z.string().optional(),
  reasoning_effort: z.enum(["low", "medium", "high", "xhigh"]).optional(),
  metadata: z.record(z.string(), z.unknown()),
});

export type TurnRequestDescriptor = z.infer<typeof turnRequestDescriptorSchema>;

export function normalizeTurnRequest(input: {
  content: string;
  attachmentIds?: string[];
  modelId?: string;
  reasoningEffort?: ReasoningEffort;
  metadata?: Record<string, unknown>;
}): TurnRequestDescriptor {
  return {
    content: input.content.trim(),
    attachment_ids: [...(input.attachmentIds || [])],
    ...(input.modelId ? { model_id: input.modelId } : {}),
    ...(input.reasoningEffort ? { reasoning_effort: input.reasoningEffort } : {}),
    metadata: { ...(input.metadata || {}) },
  };
}

export function requestMetadata(descriptor: TurnRequestDescriptor) {
  return {
    ...descriptor.metadata,
    attachment_ids: descriptor.attachment_ids,
    [TURN_REQUEST_METADATA_KEY]: descriptor,
  };
}

export function requestDescriptorFromMessage(message: Message): TurnRequestDescriptor | null {
  const parsed = turnRequestDescriptorSchema.safeParse(
    message.metadata?.[TURN_REQUEST_METADATA_KEY],
  );
  if (parsed.success) return parsed.data;

  if (message.role !== "user") return null;
  const attachmentIds = message.metadata?.attachment_ids;
  return normalizeTurnRequest({
    content: message.content_text || "",
    attachmentIds: Array.isArray(attachmentIds)
      ? attachmentIds.filter((value): value is string => typeof value === "string")
      : [],
    metadata: message.metadata,
  });
}
