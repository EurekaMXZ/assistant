import { describe, expect, it } from "vitest";
import {
  askUserInteractionSchema,
  billingToolPriceSchema,
  conversationSchema,
  parseTurnStreamFrame,
} from "./api-schemas";
import {
  isViewportNearBottom,
  isMessageAreaCoveringDisclaimer,
  latestTurnMinimumHeight,
  messageScrollAction,
  shouldFollowAfterScroll,
} from "./scroll-follow";
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

  it("strictly validates interaction snapshots and awaiting turn status", () => {
    const interaction = {
      id: "ask-user:tool-1",
      type: "interaction",
      status: "awaiting_input",
      tool_call_id: "tool-1",
      prompt: "Continue?",
      kind: "single_choice",
      options: [
        { id: "yes", label: "Yes", tone: "primary" },
        { id: "cancel", label: "Cancel", tone: "neutral" },
      ],
      created_at: "2026-01-01T00:00:00Z",
    };
    const snapshot = {
      turn_id: "turn-1",
      conversation_id: "conversation-1",
      status: "awaiting_input",
      items: [interaction],
    };

    expect(parseTurnStreamFrame("turn.snapshot", snapshot)).not.toBeNull();
    expect(
      parseTurnStreamFrame("turn.snapshot", {
        ...snapshot,
        items: [{ ...interaction, arbitrary_class: "bg-red-500" }],
      }),
    ).toBeNull();
  });

  it("accepts arbitrary deeplinks and rejects unsafe action targets", () => {
    const base = {
      id: "ask-user:tool-1",
      tool_call_id: "tool-1",
      prompt: "Continue?",
      kind: "external_action" as const,
      options: [
        { id: "yes", label: "Yes", tone: "primary" },
        { id: "cancel", label: "Cancel", tone: "neutral" },
      ],
      status: "cancelled" as const,
      answer: {
        status: "cancelled" as const,
        option_id: "cancelled",
        label: "已取消",
        user_reported: false,
      },
    };
    for (const url of [
      "weixin://wxpay/bizpayurl?pr=example",
      "weixin://dl/business/?ticket=example",
      "alipays://platformapi/startapp?appId=20000067",
      "my-company.app://orders/123",
      "intent://scan/#Intent;scheme=zxing;package=com.example;end",
      "mailto:support@example.com",
    ]) {
      expect(
        askUserInteractionSchema.safeParse({
          ...base,
          action: { label: "Pay", url },
        }).success,
      ).toBe(true);
    }
    for (const url of [
      "http://example.com/pay",
      "https://localhost/pay",
      "https://10.0.0.1/pay",
      "https://2130706433/pay",
      "https://169.254.169.254/latest/meta-data",
      "javascript:alert(1)",
      "data:text/html,<script>alert(1)</script>",
      "file:///etc/passwd",
      "vbscript:msgbox(1)",
      "not-a-url",
    ]) {
      expect(
        askUserInteractionSchema.safeParse({
          ...base,
          action: { label: "Pay", url },
        }).success,
      ).toBe(false);
    }
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
    expect(
      shouldFollowAfterScroll(
        { scrollHeight: 1000, scrollTop: 900, clientHeight: 100 },
        900,
        false,
      ),
    ).toBe(false);
  });

  it("anchors a newly sent user message instead of following the bottom", () => {
    expect(messageScrollAction(undefined, "message-1", true)).toBe("follow-bottom");
    expect(messageScrollAction(null, "message-1", true)).toBe("anchor-user");
    expect(messageScrollAction("message-1", "message-1", false)).toBe("none");
    expect(messageScrollAction("message-1", "message-1", true)).toBe("follow-bottom");
  });

  it("reserves the visible message area for the latest turn", () => {
    expect(latestTurnMinimumHeight(800, 160)).toBe(576);
    expect(latestTurnMinimumHeight(320, 200)).toBe(192);
    expect(latestTurnMinimumHeight(0, 160)).toBe(0);
  });

  it("covers the composer disclaimer only when messages enter its fixed line", () => {
    expect(
      isMessageAreaCoveringDisclaimer({ scrollHeight: 1000, scrollTop: 356, clientHeight: 600 }),
    ).toBe(false);
    expect(
      isMessageAreaCoveringDisclaimer({ scrollHeight: 1000, scrollTop: 355, clientHeight: 600 }),
    ).toBe(true);
  });
});
