type AuthStateChangeReason = "logout" | "unauthorized";

interface AuthStateChangeEvent {
  reason: AuthStateChangeReason;
}

const authStateChangeListeners = new Set<(event: AuthStateChangeEvent) => void>();

export function emitAuthStateChange(event: AuthStateChangeEvent) {
  for (const listener of authStateChangeListeners) {
    listener(event);
  }
}

export function subscribeAuthStateChange(listener: (event: AuthStateChangeEvent) => void) {
  authStateChangeListeners.add(listener);
  return () => {
    authStateChangeListeners.delete(listener);
  };
}
