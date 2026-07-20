import { z } from "zod";
import type { Attachment, Conversation } from "./types";
import { sha256File, type InitialTurnResult } from "./api";
import { createIdempotencyKey } from "./idempotency-key";
import { normalizeTurnRequest, type TurnRequestDescriptor } from "./turn-request";

const STORAGE_KEY = "assistant_initial_turn_operation";
const operationFileSchema = z.object({
  fingerprint: z.string(),
  attachment_id: z.string().optional(),
  sha256: z.string().optional(),
  upload_key: z.string().optional(),
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
    metadata: z.record(z.string(), z.unknown()),
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

type InitialTurnUploadDependencies = Pick<InitialTurnDependencies, "prepare" | "uploadAttachment">;

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
  key = createIdempotencyKey(),
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
    files: files.map((file) => ({
      fingerprint: fileFingerprint(file),
      upload_key: `${key}:attachment:${createIdempotencyKey()}`,
    })),
    created_at: new Date().toISOString(),
  };
}

export function syncInitialTurnOperation(
  operation: InitialTurnOperation | null,
  input: Omit<TurnRequestDescriptor, "attachment_ids">,
  files: File[],
  ownerUserId: string,
) {
  if (!operation || operation.owner_user_id !== ownerUserId) {
    return createInitialTurnOperation(input, files, ownerUserId);
  }
  const descriptor = normalizeTurnRequest({
    content: input.content,
    modelId: input.model_id,
    reasoningEffort: input.reasoning_effort,
    metadata: input.metadata,
  });
  const available = [...operation.files];
  const syncedFiles = files.map((file) => {
    const fingerprint = fileFingerprint(file);
    const index = available.findIndex((candidate) => candidate.fingerprint === fingerprint);
    if (index === -1) {
      return {
        fingerprint,
        upload_key: `${operation.key}:attachment:${createIdempotencyKey()}`,
      };
    }
    const [existing] = available.splice(index, 1);
    return existing;
  });
  return {
    ...operation,
    descriptor,
    input_fingerprint: inputFingerprint(descriptor, files),
    files: syncedFiles,
  };
}

export function loadInitialTurnOperation() {
  if (typeof window === "undefined") return null;
  try {
    const parsed = operationSchema.safeParse(
      JSON.parse(sessionStorage.getItem(STORAGE_KEY) || "null"),
    );
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
  const current = await prepareInitialTurnAttachments(operation, files, dependencies);
  const conversationId = current.conversation_id;
  if (!conversationId) throw new Error("初始会话准备失败");
  const descriptor: TurnRequestDescriptor = {
    ...current.descriptor,
    attachment_ids: current.files.flatMap((item) =>
      item.attachment_id ? [item.attachment_id] : [],
    ),
  };
  const result = await dependencies.commit(conversationId, descriptor, current.key);
  clearInitialTurnOperation(current.key);
  return { operation: current, result };
}

export async function prepareInitialTurnAttachments(
  operation: InitialTurnOperation,
  files: File[],
  dependencies: InitialTurnUploadDependencies,
) {
  let current = operation;
  if (!current.conversation_id) {
    const { conversation } = await dependencies.prepare(current.key);
    current = { ...current, conversation_id: conversation.id };
    saveInitialTurnOperation(current);
  }
  const conversationId = current.conversation_id;
  if (!conversationId) throw new Error("初始会话准备失败");

  const usedFiles = new Set<number>();
  for (let index = 0; index < current.files.length; index += 1) {
    const expectedFingerprint = current.files[index].fingerprint;
    const fileIndex = files.findIndex(
      (candidate, candidateIndex) =>
        !usedFiles.has(candidateIndex) && fileFingerprint(candidate) === expectedFingerprint,
    );
    const file = fileIndex === -1 ? undefined : files[fileIndex];
    if (!file) throw new Error("请重新选择尚未上传的文件后重试");
    usedFiles.add(fileIndex);

    if (current.files[index].attachment_id) {
      const expectedSHA256 = current.files[index].sha256;
      if (expectedSHA256 && (await sha256File(file)) === expectedSHA256) continue;
      const nextFiles = current.files.map((item, itemIndex) =>
        itemIndex === index
          ? {
              fingerprint: item.fingerprint,
              upload_key: `${current.key}:attachment:${createIdempotencyKey()}`,
            }
          : item,
      );
      current = { ...current, files: nextFiles };
      saveInitialTurnOperation(current);
    }
    const attachment = await dependencies.uploadAttachment(
      conversationId,
      file,
      current.files[index].upload_key || `${current.key}:attachment:${index}`,
    );
    const nextFiles = current.files.map((item, itemIndex) =>
      itemIndex === index
        ? { ...item, attachment_id: attachment.id, sha256: attachment.sha256 || undefined }
        : item,
    );
    current = { ...current, files: nextFiles };
    saveInitialTurnOperation(current);
  }

  return current;
}

export async function uploadInitialTurnAttachment(
  operationKey: string,
  uploadKey: string,
  file: File,
  dependencies: InitialTurnUploadDependencies,
) {
  let current = loadInitialTurnOperation();
  if (!current || current.key !== operationKey) return null;

  let fileState = current.files.find((item) => item.upload_key === uploadKey);
  if (!fileState) return null;

  if (!current.conversation_id) {
    const { conversation } = await dependencies.prepare(operationKey);
    const latest = loadInitialTurnOperation();
    if (!latest || latest.key !== operationKey) return null;
    current = { ...latest, conversation_id: conversation.id };
    saveInitialTurnOperation(current);
    fileState = current.files.find((item) => item.upload_key === uploadKey);
    if (!fileState) return null;
  }

  if (fileState.attachment_id) {
    if (fileState.sha256 && (await sha256File(file)) !== fileState.sha256) {
      throw new Error("文件内容已变化，请重新选择后重试");
    }
    return {
      attachmentId: fileState.attachment_id,
      conversationId: current.conversation_id,
      operation: current,
    };
  }

  const conversationId = current.conversation_id;
  if (!conversationId) throw new Error("初始会话准备失败");
  const attachment = await dependencies.uploadAttachment(conversationId, file, uploadKey);
  const latest = loadInitialTurnOperation();
  if (!latest || latest.key !== operationKey) return null;
  const fileIndex = latest.files.findIndex((item) => item.upload_key === uploadKey);
  if (fileIndex === -1) return null;
  const nextFiles = latest.files.map((item, index) =>
    index === fileIndex
      ? { ...item, attachment_id: attachment.id, sha256: attachment.sha256 || undefined }
      : item,
  );
  const operation = { ...latest, files: nextFiles };
  saveInitialTurnOperation(operation);
  return {
    attachmentId: attachment.id,
    attachment,
    conversationId: operation.conversation_id,
    operation,
  };
}

export async function commitPreparedInitialTurn(
  operation: InitialTurnOperation,
  dependencies: Pick<InitialTurnDependencies, "commit">,
) {
  const conversationId = operation.conversation_id;
  if (!conversationId) throw new Error("初始会话尚未准备完成");
  const descriptor: TurnRequestDescriptor = {
    ...operation.descriptor,
    attachment_ids: operation.files.flatMap((item) =>
      item.attachment_id ? [item.attachment_id] : [],
    ),
  };
  const result = await dependencies.commit(conversationId, descriptor, operation.key);
  clearInitialTurnOperation(operation.key);
  return { operation, result };
}
