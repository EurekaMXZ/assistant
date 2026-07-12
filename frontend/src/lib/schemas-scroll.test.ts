import { describe, expect, it } from "vitest";
import { conversationSchema, parseTurnStreamFrame } from "./api-schemas";
import { isViewportNearBottom } from "./scroll-follow";
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
});
