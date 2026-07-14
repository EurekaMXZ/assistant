import type { BillingAccount } from "./types";

const listeners = new Set<(account: BillingAccount) => void>();

export function emitBillingAccountUpdated(account: BillingAccount) {
  for (const listener of listeners) listener(account);
}

export function subscribeBillingAccountUpdated(listener: (account: BillingAccount) => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
