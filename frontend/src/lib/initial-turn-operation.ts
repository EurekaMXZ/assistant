import { z } from "zod";
import type { Attachment, Conversation } from "./types";
import type { InitialTurnResult } from "./api";
import { normalizeTurnRequest, type TurnRequestDescriptor } from "./turn-request";

const STORAGE_KEY = "assistant_initial_turn_operation";
const operationFileSchema = z.object({
  fingerprint: z.string(),
  attachment_id: z.string().optional(),
});
const operationSchema = z.object({
  key: z.string(),
  owner_user_id: z.string(),
  input_fingerprint: z.string(),
  descriptor: z.object({
    content: z.string(),
    attachment_ids: z.array(z.string()),
    model_id: z.string().optional(),
    reasoning_effort: z.enum(["low", "medium", "high", "xhigh"]).optional(),
    metadata: z.record(z.unknown()),
  }),
  files: z.array(operationFileSchema),
  conversation_id: z.string().optional(),
  created_at: z.string(),
});

export type InitialTurnOperation = z.infer<typeof operationSchema>;

export interface InitialTurnDependencies {
  prepare: (key: string) => Promise<{ conversation: Conversation }>;
  uploadAttachment: (conversationId: string, file: File, key: string) => Promise<Attachment>;
  commit: (
    conversationId: string,
    descriptor: TurnRequestDescriptor,
    key: string,
  ) => Promise<InitialTurnResult>;
}

export function fileFingerprint(file: Pick<File, "lastModified" | "name" | "size" | "type">) {
  return [file.name, file.size, file.type, file.lastModified].join(":");
}

function inputFingerprint(descriptor: TurnRequestDescriptor, files: File[]) {
  return JSON.stringify({
    ...descriptor,
    attachment_ids: [],
    files: files.map(fileFingerprint),
  });
}

export function createInitialTurnOperation(
  input: Omit<TurnRequestDescriptor, "attachment_ids">,
  files: File[],
  ownerUserId: string,
  key = crypto.randomUUID(),
): InitialTurnOperation {
  const descriptor = normalizeTurnRequest({
    content: input.content,
    modelId: input.model_id,
    reasoningEffort: input.reasoning_effort,
    metadata: input.metadata,
  });
  return {
    key,
    owner_user_id: ownerUserId,
    input_fingerprint: inputFingerprint(descriptor, files),
    descriptor,
    files: files.map((file) => ({ fingerprint: fileFingerprint(file) })),
    created_at: new Date().toISOString(),
  };
}

export function operationMatches(
  operation: InitialTurnOperation,
  input: Omit<TurnRequestDescriptor, "attachment_ids">,
  files: File[],
  ownerUserId: string,
) {
  const descriptor = normalizeTurnRequest({
    content: input.content,
    modelId: input.model_id,
    reasoningEffort: input.reasoning_effort,
    metadata: input.metadata,
  });
  return operation.owner_user_id === ownerUserId
    && operation.input_fingerprint === inputFingerprint(descriptor, files);
}

export function loadInitialTurnOperation() {
  if (typeof window === "undefined") return null;
  try {
    const parsed = operationSchema.safeParse(JSON.parse(sessionStorage.getItem(STORAGE_KEY) || "null"));
    return parsed.success ? parsed.data : null;
  } catch {
    return null;
  }
}

export function saveInitialTurnOperation(operation: InitialTurnOperation) {
  if (typeof window !== "undefined") {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(operation));
  }
}

export function clearInitialTurnOperation(key: string) {
  const current = loadInitialTurnOperation();
  if (current?.key === key) sessionStorage.removeItem(STORAGE_KEY);
}

export async function runInitialTurnOperation(
  operation: InitialTurnOperation,
  files: File[],
  dependencies: InitialTurnDependencies,
) {
  let current = operation;
  if (!current.conversation_id) {
    const { conversation } = await dependencies.prepare(current.key);
    current = { ...current, conversation_id: conversation.id };
    saveInitialTurnOperation(current);
  }
  const conversationId = current.conversation_id;
  if (!conversationId) throw new Error("初始会话准备失败");

  for (let index = 0; index < current.files.length; index += 1) {
    if (current.files[index].attachment_id) continue;
    const expectedFingerprint = current.files[index].fingerprint;
    const file = files.find((candidate) => fileFingerprint(candidate) === expectedFingerprint);
    if (!file) throw new Error("请重新选择尚未上传的文件后重试");
    const attachment = await dependencies.uploadAttachment(
      conversationId,
      file,
      `${current.key}:attachment:${index}`,
    );
    const nextFiles = current.files.map((item, itemIndex) =>
      itemIndex === index ? { ...item, attachment_id: attachment.id } : item,
    );
    current = { ...current, files: nextFiles };
    saveInitialTurnOperation(current);
  }

  const descriptor: TurnRequestDescriptor = {
    ...current.descriptor,
    attachment_ids: current.files.flatMap((item) => item.attachment_id ? [item.attachment_id] : []),
  };
  const result = await dependencies.commit(
    conversationId,
    descriptor,
    current.key,
  );
  clearInitialTurnOperation(current.key);
  return { operation: current, result };
}
