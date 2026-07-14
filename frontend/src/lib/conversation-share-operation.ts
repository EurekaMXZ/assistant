export interface ConversationShareOperation {
  idempotencyKey: string;
  lastMessageSeq: number;
  title: string;
}

const storagePrefix = "assistant_conversation_share_operation";
const operations = new Map<string, ConversationShareOperation>();

function operationStorageKey(userId: string, conversationId: string) {
  return `${storagePrefix}:${encodeURIComponent(userId)}:${encodeURIComponent(conversationId)}`;
}

function isOperation(value: unknown): value is ConversationShareOperation {
  if (!value || typeof value !== "object") return false;
  const operation = value as Partial<ConversationShareOperation>;
  return (
    typeof operation.idempotencyKey === "string" &&
    operation.idempotencyKey.length > 0 &&
    operation.idempotencyKey.length <= 128 &&
    typeof operation.lastMessageSeq === "number" &&
    Number.isInteger(operation.lastMessageSeq) &&
    operation.lastMessageSeq >= 0 &&
    typeof operation.title === "string"
  );
}

export function readConversationShareOperation(userId: string, conversationId: string) {
  const key = operationStorageKey(userId, conversationId);
  const current = operations.get(key);
  if (current) return current;
  if (typeof window === "undefined") return null;

  try {
    const raw = window.sessionStorage.getItem(key);
    const parsed: unknown = raw ? JSON.parse(raw) : null;
    if (!isOperation(parsed)) return null;
    operations.set(key, parsed);
    return parsed;
  } catch {
    return null;
  }
}

export function writeConversationShareOperation(
  userId: string,
  conversationId: string,
  operation: ConversationShareOperation,
) {
  const key = operationStorageKey(userId, conversationId);
  operations.set(key, operation);
  if (typeof window === "undefined") return;

  try {
    window.sessionStorage.setItem(key, JSON.stringify(operation));
  } catch {
    // The in-memory registry still preserves retries for this page session.
  }
}

export function clearConversationShareOperation(
  userId: string,
  conversationId: string,
  expectedIdempotencyKey: string,
) {
  const key = operationStorageKey(userId, conversationId);
  const current = readConversationShareOperation(userId, conversationId);
  if (current?.idempotencyKey !== expectedIdempotencyKey) return false;

  operations.delete(key);
  if (typeof window !== "undefined") {
    try {
      window.sessionStorage.removeItem(key);
    } catch {
      // The in-memory entry has already been removed.
    }
  }
  return true;
}
