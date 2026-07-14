import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Message, Turn } from "@/lib/types";
import { MessageBubble } from "./message-bubble";
import { Composer } from "./composer";
import { groupMessageEntries } from "./message-list";

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT =
  true;

function message(id: string, role: Message["role"], seq: number, turnId: string): Message {
  return {
    id,
    conversation_id: "conversation-1",
    turn_id: turnId,
    seq,
    role,
    content_text: id,
    metadata: {},
    created_at: "2026-07-14T00:00:00Z",
  };
}

function turn(id: string, variantIndex: number, retryOfTurnId?: string): Turn {
  return {
    id,
    conversation_id: "conversation-1",
    seq: variantIndex,
    retry_of_turn_id: retryOfTurnId,
    variant_index: variantIndex,
    status: "completed",
    metadata: {},
    created_at: "2026-07-14T00:00:00Z",
    updated_at: "2026-07-14T00:00:00Z",
  };
}

describe("message turn variants", () => {
  it("pairs each edited prompt with its answer in one logical turn", () => {
    const entries = groupMessageEntries(
      [
        message("user", "user", 1, "root-turn"),
        message("answer-1", "assistant", 2, "root-turn"),
        { ...message("edited-user", "user", 3, "retry-turn"), content_text: "edited" },
        message("answer-2", "assistant", 4, "retry-turn"),
      ],
      {
        "root-turn": turn("root-turn", 1),
        "retry-turn": turn("retry-turn", 2, "root-turn"),
      },
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "turn",
      rootTurnId: "root-turn",
      variants: [
        {
          turnId: "root-turn",
          variantIndex: 1,
          userMessage: { id: "user" },
          assistantMessages: [{ id: "answer-1" }],
        },
        {
          turnId: "retry-turn",
          variantIndex: 2,
          userMessage: { id: "edited-user", content_text: "edited" },
          assistantMessages: [{ id: "answer-2" }],
        },
      ],
    });
  });
});

describe("message editing", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });

  it("sends the selected message to the composer editor", async () => {
    const onEdit = vi.fn();
    const userMessage = message("user", "user", 1, "root-turn");
    await act(async () => {
      root.render(<MessageBubble message={userMessage} canEdit onEdit={onEdit} />);
    });

    const editButton = container.querySelector<HTMLButtonElement>('[aria-label="编辑消息"]');
    expect(editButton).not.toBeNull();
    await act(async () => editButton?.click());

    expect(onEdit).toHaveBeenCalledWith(userMessage);
    expect(container.querySelector("textarea")).toBeNull();
  });

  it("renders edit state around the bottom composer", () => {
    const markup = renderToStaticMarkup(
      <Composer
        editing
        value="user"
        onChange={() => undefined}
        onSend={() => undefined}
        onCancelEdit={() => undefined}
        placeholder="编辑消息"
      />,
    );

    expect(markup).toContain("编辑消息");
    expect(markup).toContain('aria-label="取消编辑"');
    expect(markup).toContain('placeholder="编辑消息"');
  });
});
