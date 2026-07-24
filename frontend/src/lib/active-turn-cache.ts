import { turnStreamEventSchemas } from "./api-schemas";
import type { TurnStreamSnapshot } from "./types";

const storagePrefix = "assistant_active_turn_snapshot";
const snapshots = new Map<string, TurnStreamSnapshot>();

function storageKey(conversationId: string, turnId: string) {
  return `${storagePrefix}:${encodeURIComponent(conversationId)}:${encodeURIComponent(turnId)}`;
}

export function readActiveTurnSnapshot(conversationId: string, turnId: string) {
  const key = storageKey(conversationId, turnId);
  const inMemory = snapshots.get(key);
  if (inMemory) return inMemory;
  if (typeof window === "undefined") return null;

  try {
    const raw = window.sessionStorage.getItem(key);
    const parsed = turnStreamEventSchemas["turn.snapshot"].safeParse(raw ? JSON.parse(raw) : null);
    if (
      !parsed.success ||
      parsed.data.conversation_id !== conversationId ||
      parsed.data.turn_id !== turnId
    ) {
      window.sessionStorage.removeItem(key);
      return null;
    }
    snapshots.set(key, parsed.data);
    return parsed.data;
  } catch {
    return null;
  }
}

export function writeActiveTurnSnapshot(snapshot: TurnStreamSnapshot) {
  const key = storageKey(snapshot.conversation_id, snapshot.turn_id);
  snapshots.set(key, snapshot);
  if (typeof window === "undefined") return;

  try {
    window.sessionStorage.setItem(key, JSON.stringify(snapshot));
  } catch {
    // The in-memory cache still preserves the current page's streamed output.
  }
}

export function clearActiveTurnSnapshot(conversationId: string, turnId: string) {
  const key = storageKey(conversationId, turnId);
  snapshots.delete(key);
  if (typeof window === "undefined") return;

  try {
    window.sessionStorage.removeItem(key);
  } catch {
    // The in-memory cache has already been cleared.
  }
}
