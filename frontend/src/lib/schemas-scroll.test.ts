import { describe, expect, it } from "vitest";
import { billingToolPriceSchema, conversationSchema, parseTurnStreamFrame } from "./api-schemas";
import { isViewportNearBottom, shouldFollowAfterScroll } from "./scroll-follow";
import { parseSseFrame, SseValidationError } from "./sse";

describe("runtime schemas", () => {
  it("distinguishes omitted API fields from invalid nulls", () => {
    const base = {
      id: "conversation-1",
      status: "active",
      metadata: {},
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    expect(conversationSchema.safeParse(base).success).toBe(true);
    expect(conversationSchema.safeParse({ ...base, title: null }).success).toBe(false);
  });

  it("validates known stream events and ignores unknown events", () => {
    expect(
      parseTurnStreamFrame("turn.done", { turn_id: "turn-1", status: "completed" }),
    ).not.toBeNull();
    expect(parseTurnStreamFrame("internal.event", {})).toBeNull();
    expect(() => parseSseFrame('event: turn.done\ndata: {"status":"completed"}\n\n')).toThrow(
      SseValidationError,
    );
  });

  it("validates supported billing tool keys", () => {
    const base = {
      currency: "USD",
      price_per_call_nanos: 250_000_000,
      price_per_call: "0.25",
      enabled: true,
      version: 1,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    expect(billingToolPriceSchema.safeParse({ ...base, tool_key: "sandbox.create" }).success).toBe(
      true,
    );
    expect(billingToolPriceSchema.safeParse({ ...base, tool_key: "sandbox.exec" }).success).toBe(
      false,
    );
  });
});

describe("scroll following", () => {
  it("follows only while the viewport is near the bottom", () => {
    expect(isViewportNearBottom({ scrollHeight: 1000, scrollTop: 810, clientHeight: 100 })).toBe(
      true,
    );
    expect(isViewportNearBottom({ scrollHeight: 1000, scrollTop: 400, clientHeight: 100 })).toBe(
      false,
    );
  });

  it("stops following as soon as the user scrolls upward near the bottom", () => {
    expect(
      shouldFollowAfterScroll({ scrollHeight: 1000, scrollTop: 850, clientHeight: 100 }, 900),
    ).toBe(false);
    expect(
      shouldFollowAfterScroll({ scrollHeight: 1000, scrollTop: 900, clientHeight: 100 }, 850),
    ).toBe(true);
  });
});
