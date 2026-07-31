import { describe, expect, it, vi } from "vitest";
import {
  createConversationPageCache,
  invalidateConversationPageCache,
  loadCachedPage,
} from "./conversation-page-cache";

describe("conversation page cache", () => {
  it("deduplicates in-flight requests and reuses successful pages", async () => {
    const cache = createConversationPageCache();
    const loader = vi.fn(async () => ({ value: 1 }));

    const [first, second] = await Promise.all([
      loadCachedPage(cache.events, "initial", loader),
      loadCachedPage(cache.events, "initial", loader),
    ]);

    expect(loader).toHaveBeenCalledTimes(1);
    expect(first).toBe(second);
    await loadCachedPage(cache.events, "initial", loader);
    expect(loader).toHaveBeenCalledTimes(1);
  });

  it("does not cache failed requests", async () => {
    const cache = createConversationPageCache();
    const loader = vi
      .fn<() => Promise<{ value: number }>>()
      .mockRejectedValueOnce(new Error("temporary failure"))
      .mockResolvedValueOnce({ value: 2 });

    await expect(loadCachedPage(cache.events, "initial", loader)).rejects.toThrow(
      "temporary failure",
    );
    await expect(loadCachedPage(cache.events, "initial", loader)).resolves.toEqual({ value: 2 });
    expect(loader).toHaveBeenCalledTimes(2);
  });

  it("ignores a stale response after invalidation", async () => {
    const cache = createConversationPageCache();
    let resolveRequest: ((value: { value: number }) => void) | undefined;
    const loader = vi.fn(
      () =>
        new Promise<{ value: number }>((resolve) => {
          resolveRequest = resolve;
        }),
    );

    const request = loadCachedPage(cache.events, "initial", loader);
    invalidateConversationPageCache(cache);
    resolveRequest?.({ value: 1 });
    await expect(request).resolves.toEqual({ value: 1 });

    const next = loadCachedPage(cache.events, "initial", async () => ({ value: 2 }));
    await expect(next).resolves.toEqual({ value: 2 });
    expect(loader).toHaveBeenCalledTimes(1);
  });
});
