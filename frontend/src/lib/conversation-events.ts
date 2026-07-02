interface ConversationUpdatedEvent {
  id: string;
  title?: string | null;
  archived_at?: string | null;
}

const conversationUpdatedListeners = new Set<
  (event: ConversationUpdatedEvent) => void
>();

export function emitConversationUpdated(event: ConversationUpdatedEvent) {
  for (const listener of conversationUpdatedListeners) {
    listener(event);
  }
}

export function subscribeConversationUpdated(
  listener: (event: ConversationUpdatedEvent) => void
) {
  conversationUpdatedListeners.add(listener);
  return () => {
    conversationUpdatedListeners.delete(listener);
  };
}
