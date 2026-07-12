import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  applyAdminBillingAdjustment,
  handleSessionUnauthorized,
  isSessionUnauthorizedError,
} from "./api";

beforeEach(() => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
    clear: () => values.clear(),
    key: (index: number) => [...values.keys()][index] ?? null,
    get length() {
      return values.size;
    },
  } satisfies Storage;
  vi.stubGlobal("localStorage", storage);
  Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  storage.setItem("assistant_access_token", "token");
});

describe("authentication error handling", () => {
  it("clears credentials only for actual session unauthorized responses", () => {
    expect(handleSessionUnauthorized(503, "temporarily unavailable")).toBe(false);
    expect(window.localStorage.getItem("assistant_access_token")).toBe("token");
    expect(isSessionUnauthorizedError(new ApiError("temporarily unavailable", 503))).toBe(false);
    expect(handleSessionUnauthorized(401, "invalid access token")).toBe(true);
    expect(window.localStorage.getItem("assistant_access_token")).toBeNull();
  });
});

describe("billing idempotency", () => {
  it("uses the caller-provided operation key", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({
          transaction: {
            id: "transaction-1",
            account_id: "account-1",
            user_id: "user-1",
            currency: "USD",
            account_sequence: 1,
            kind: "manual_topup",
            direction: "credit",
            amount_nanos: 1_000_000_000,
            amount: "1.00",
            balance_after_nanos: 1_000_000_000,
            balance_after: "1.00",
            reason: "test",
            reference: "ticket",
            created_at: "2026-01-01T00:00:00Z",
          },
        }),
        { status: 201, headers: { "Content-Type": "application/json" } },
      ),
    );
    await applyAdminBillingAdjustment(
      "user-1",
      "topups",
      {
        amount: "1.00",
        currency: "USD",
        reason: "test",
        reference: "ticket",
      },
      "billing-operation-1",
    );
    expect(new Headers(fetchMock.mock.calls[0][1]?.headers).get("Idempotency-Key")).toBe(
      "billing-operation-1",
    );
  });
});
