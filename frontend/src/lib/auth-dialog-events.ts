export type AuthDialogMode = "login" | "register" | "verification-pending" | "forgot-password";

const authDialogListeners = new Set<(mode: AuthDialogMode) => void>();

export function openAuthDialog(mode: AuthDialogMode = "login") {
  for (const listener of authDialogListeners) {
    listener(mode);
  }
}

export function subscribeAuthDialog(listener: (mode: AuthDialogMode) => void) {
  authDialogListeners.add(listener);
  return () => {
    authDialogListeners.delete(listener);
  };
}
