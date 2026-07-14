import { afterEach, describe, expect, it, vi } from "vitest";
import { createIdempotencyKey } from "./idempotency-key";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("createIdempotencyKey", () => {
  it("uses randomUUID when available", () => {
    const randomUUID = vi.fn(() => "native-uuid");
    vi.stubGlobal("crypto", { randomUUID });

    expect(createIdempotencyKey()).toBe("native-uuid");
    expect(randomUUID).toHaveBeenCalledOnce();
  });

  it("creates a UUID v4 with getRandomValues when randomUUID is unavailable", () => {
    vi.stubGlobal("crypto", {
      getRandomValues(bytes: Uint8Array) {
        bytes.forEach((_, index) => {
          bytes[index] = index;
        });
        return bytes;
      },
    });

    expect(createIdempotencyKey()).toBe("00010203-0405-4607-8809-0a0b0c0d0e0f");
  });
});
