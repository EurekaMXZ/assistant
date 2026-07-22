import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Message, Turn } from "@/lib/types";
import { AskUserInteractionView, AssistantTurnBubble, MessageBubble } from "./message-bubble";
import { Composer } from "./composer";
import { groupMessageEntries, MessageList } from "./message-list";

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

  it("only shows retry on the latest logical turn", () => {
    const markup = renderToStaticMarkup(
      <MessageList
        messages={[
          message("user-1", "user", 1, "turn-1"),
          message("answer-1", "assistant", 2, "turn-1"),
          message("user-2", "user", 3, "turn-2"),
          message("answer-2", "assistant", 4, "turn-2"),
        ]}
        turnsById={{
          "turn-1": turn("turn-1", 1),
          "turn-2": turn("turn-2", 1),
        }}
        onEditMessage={() => undefined}
        onAnswerInteraction={async () => true}
        onOpenTimeline={() => undefined}
        onRetryMessage={() => undefined}
      />,
    );

    expect(markup.match(/<span class="sr-only">重试<\/span>/g)).toHaveLength(1);
  });
});

describe("user message presentation", () => {
  it("renders user content as plain text instead of Markdown", () => {
    const userMessage = {
      ...message("user-markdown", "user", 1, "turn-1"),
      content_text: "# Heading\n\n**not bold**",
    };

    const markup = renderToStaticMarkup(
      <MessageBubble message={userMessage} showActions={false} />,
    );

    expect(markup).toContain("whitespace-pre-wrap");
    expect(markup).toContain("# Heading");
    expect(markup).toContain("**not bold**");
    expect(markup).not.toContain("<h1");
    expect(markup).not.toContain("<strong");
  });

  it("renders user attachments below the bubble with composer attachment styles", () => {
    const userMessage = {
      ...message("user-attachments", "user", 1, "turn-1"),
      content_text: "See attachments",
      metadata: {
        attachment_ids: ["image-1", "document-1"],
        attachments: [
          {
            id: "image-1",
            filename: "photo.png",
            content_type: "image/png",
            category: "image",
            size_bytes: 1024,
          },
          {
            id: "document-1",
            filename: "report.pdf",
            content_type: "application/pdf",
            category: "document",
            size_bytes: 2048,
          },
        ],
      },
    };

    const markup = renderToStaticMarkup(
      <MessageBubble message={userMessage} allowAttachmentPreviews={false} showActions={false} />,
    );

    expect(markup).toContain('data-slot="attachment-list"');
    expect(markup).toContain("size-16");
    expect(markup).toContain("report.pdf");
    expect(markup.indexOf("See attachments")).toBeLessThan(markup.indexOf("attachment-list"));
    expect(markup).not.toContain("max-h-72");
    expect(markup).not.toContain("已附加");
    expect(markup).not.toContain("移除 photo.png");
  });
});

describe("assistant message alignment", () => {
  it("uses the same visual inset for timeline controls and status messages", () => {
    const errorMarkup = renderToStaticMarkup(
      <MessageBubble
        message={{
          ...message("error", "assistant", 1, "turn-1"),
          content_text: "Upstream request failed",
          metadata: { display_kind: "assistant_error" },
        }}
        showActions={false}
      />,
    );
    const interactionMarkup = renderToStaticMarkup(
      <MessageBubble
        message={{
          ...message("interaction", "assistant", 2, "turn-1"),
          metadata: {
            display_kind: "ask_user",
            interaction: {
              type: "interaction",
              id: "ask-user:tool-1",
              tool_call_id: "tool-1",
              prompt: "Continue?",
              kind: "single_choice",
              options: [
                { id: "yes", label: "Yes", tone: "primary" },
                { id: "no", label: "No", tone: "neutral" },
              ],
              status: "completed",
              answer: {
                status: "answered",
                option_id: "yes",
                label: "Yes",
                user_reported: true,
              },
            },
          },
        }}
        showActions={false}
      />,
    );
    const timelineMarkup = renderToStaticMarkup(
      <AssistantTurnBubble
        messages={[
          {
            ...message("thinking", "assistant", 1, "turn-1"),
            content_text: "",
            metadata: { display_kind: "thinking" },
          },
        ]}
        turnId="turn-1"
        isStreaming
      />,
    );

    expect(errorMarkup).toContain('style="padding-left:6px"');
    expect(interactionMarkup).toContain('style="padding-left:6px"');
    expect(timelineMarkup).toContain('style="padding-left:6px"');
    expect(timelineMarkup).toContain("Thinking...");
  });
});

describe("ask user interaction", () => {
  let container: HTMLDivElement;
  let root: Root;

  const pendingInteraction = {
    id: "ask-user:tool-1",
    tool_call_id: "tool-1",
    prompt: "Choose a route",
    kind: "external_action" as const,
    status: "awaiting_input" as const,
    action: { label: "Open settings", url: "https://example.com/settings" },
    options: [
      { id: "continue", label: "Continue", tone: "primary" as const },
      { id: "cancel", label: "Cancel", tone: "neutral" as const },
    ],
  };

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("renders pending options and submits the selected option", async () => {
    const onAnswer = vi.fn(async () => false);
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    await act(async () => {
      root.render(<AskUserInteractionView interaction={pendingInteraction} onAnswer={onAnswer} />);
    });

    const option = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent === "Continue",
    );
    expect(option).toBeDefined();
    expect(container.textContent).toContain("目标：example.com");
    const externalAction = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("Open settings"),
    );
    await act(async () => externalAction?.click());
    const confirm = Array.from(document.body.querySelectorAll("button")).find(
      (button) => button.textContent === "继续打开",
    );
    expect(confirm).toBeDefined();
    await act(async () => confirm?.click());
    expect(open).toHaveBeenCalledWith(
      "https://example.com/settings",
      "_blank",
      "noopener,noreferrer",
    );
    await act(async () => option?.click());

    expect(onAnswer).toHaveBeenCalledWith(pendingInteraction, "continue");
  });

  it("renders a durably cancelled interaction as a neutral disabled status", () => {
    const markup = renderToStaticMarkup(
      <AskUserInteractionView
        interaction={{
          ...pendingInteraction,
          status: "cancelled",
          answer: {
            status: "cancelled",
            option_id: "cancelled",
            label: "已取消",
            user_reported: false,
          },
        }}
      />,
    );

    expect(markup).toContain("已取消");
    expect(markup).toContain("text-muted-foreground");
    expect(markup).not.toContain("<button");
  });

  it("warns before opening an arbitrary deeplink", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    const deeplink = "weixin://wxpay/bizpayurl?pr=example";
    await act(async () => {
      root.render(
        <AskUserInteractionView
          interaction={{
            ...pendingInteraction,
            action: { label: "Open payment", url: deeplink },
          }}
          onAnswer={vi.fn(async () => false)}
        />,
      );
    });

    expect(container.querySelector(`a[href^="weixin:"]`)).toBeNull();
    expect(container.textContent).toContain("目标：weixin://wxpay");
    const externalAction = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("Open payment"),
    );
    await act(async () => externalAction?.click());
    expect(document.body.textContent).toContain("即将通过 weixin: 协议唤起外部应用");

    const confirm = Array.from(document.body.querySelectorAll("button")).find(
      (button) => button.textContent === "继续打开",
    );
    await act(async () => confirm?.click());
    expect(open).toHaveBeenCalledWith(deeplink, "_blank", "noopener,noreferrer");
  });

  it("renders completed and cancelled interactions as button-free status messages", () => {
    const markup = renderToStaticMarkup(
      <AskUserInteractionView
        interaction={{
          ...pendingInteraction,
          status: "completed",
          answer: {
            status: "cancelled",
            option_id: "cancel",
            label: "Cancel",
            user_reported: true,
          },
        }}
      />,
    );

    expect(markup).toContain('role="status"');
    expect(markup).toContain("询问用户「Choose a route」：Cancel");
    expect(markup).not.toContain("<button");
    expect(markup).not.toContain("Open settings");
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
