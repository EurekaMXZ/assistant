import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { ConversationTurnSummary } from "@/lib/types";
import { TurnNavigator } from "./turn-navigator";

function summary(seq: number): ConversationTurnSummary {
  return {
    id: `turn-${seq}`,
    conversation_id: "conversation-1",
    seq,
    variant_index: 1,
    status: "completed",
    user_message: {
      id: `message-${seq}`,
      conversation_id: "conversation-1",
      turn_id: `turn-${seq}`,
      seq,
      role: "user",
      content_text: `用户输入 ${seq}`,
      metadata: {},
      created_at: "2026-01-01T00:00:00Z",
    },
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

describe("turn navigator", () => {
  it("shows a compact five-turn rail and an expandable menu", () => {
    const markup = renderToStaticMarkup(
      <TurnNavigator
        activeTurnId="turn-4"
        turns={Array.from({ length: 8 }, (_, index) => summary(index + 1))}
        onSelect={() => undefined}
        hasMoreOlder
      />,
    );

    expect(markup).toContain("group/turn-navigator");
    expect(markup).toContain('data-slot="turn-rail"');
    expect(markup.match(/data-active=/g)).toHaveLength(5);
    expect(markup).toContain("用户输入 4");
    expect(markup).toContain('aria-current="true"');
    expect(markup).not.toContain("搜索");
    expect(markup).not.toContain("可继续滚动");
  });
});
