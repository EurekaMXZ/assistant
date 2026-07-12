import type { Message, Turn } from "./types";

const PENDING_HOME_TURN_KEY = "assistant_pending_home_turn";
const PENDING_HOME_TURN_TTL_MS = 10 * 60 * 1000;

export interface PendingHomeTurn {
  conversation_id: string;
  message: Message;
  turn: Turn;
  stream_path: string;
  created_at: string;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isPendingHomeTurn(value: unknown): value is PendingHomeTurn {
  if (!isRecord(value)) return false;
  const message = value.message;
  const turn = value.turn;
  return (
    typeof value.conversation_id === "string" &&
    typeof value.stream_path === "string" &&
    typeof value.created_at === "string" &&
    isRecord(message) &&
    typeof message.id === "string" &&
    isRecord(turn) &&
    typeof turn.id === "string"
  );
}

function isExpired(createdAt: string) {
  const timestamp = Date.parse(createdAt);
  return Number.isNaN(timestamp) || Date.now() - timestamp > PENDING_HOME_TURN_TTL_MS;
}

export function stashPendingHomeTurn(input: Omit<PendingHomeTurn, "created_at">) {
  if (typeof window === "undefined") return;

  sessionStorage.setItem(
    PENDING_HOME_TURN_KEY,
    JSON.stringify({
      ...input,
      created_at: new Date().toISOString(),
    }),
  );
}

export function takePendingHomeTurn(conversationId: string): PendingHomeTurn | null {
  if (typeof window === "undefined") return null;

  const raw = sessionStorage.getItem(PENDING_HOME_TURN_KEY);
  if (!raw) return null;

  try {
    const value = JSON.parse(raw) as unknown;
    if (!isPendingHomeTurn(value) || isExpired(value.created_at)) {
      sessionStorage.removeItem(PENDING_HOME_TURN_KEY);
      return null;
    }

    if (value.conversation_id !== conversationId) {
      return null;
    }

    sessionStorage.removeItem(PENDING_HOME_TURN_KEY);
    return value;
  } catch {
    sessionStorage.removeItem(PENDING_HOME_TURN_KEY);
    return null;
  }
}
