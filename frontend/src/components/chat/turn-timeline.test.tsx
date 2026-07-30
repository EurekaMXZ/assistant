import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { TurnTimeline, TurnTimelinePanel } from "./turn-timeline";
import { TimelineToolPayload } from "./turn-timeline-payloads";

describe("sandbox command output", () => {
  it("renders persistent shell commands and their ordered output", () => {
    const markup = renderToStaticMarkup(
      <TimelineToolPayload
        item={{
          id: "tool-1",
          type: "tool_call",
          status: "completed",
          metadata: { tool_name: "sandbox.shell_connect" },
          command: "test-command",
          command_output: "first\nsecond\nthird\n",
          exit_code: 0,
          created_at: "2026-07-14T00:00:00Z",
        }}
      />,
    );

    expect(markup).toContain("first\nsecond\nthird\n");
    expect(markup).toContain("test-command");
    expect(markup).toContain("exit 0");
    expect(markup.match(/<pre/g)).toHaveLength(2);
    expect(markup).not.toContain("text-destructive");
    expect(markup).not.toContain(">stderr<");
  });
});

describe("turn timing", () => {
  it("renders the duration reconciled by the terminal snapshot", () => {
    const markup = renderToStaticMarkup(
      <TurnTimeline
        turnId="turn-1"
        turn={{
          id: "turn-1",
          conversation_id: "conversation-1",
          seq: 1,
          status: "completed",
          metadata: {},
          started_at: "2026-07-14T10:00:00Z",
          completed_at: "2026-07-14T10:00:12Z",
          created_at: "2026-07-14T09:59:59Z",
          updated_at: "2026-07-14T10:00:12Z",
        }}
        onOpen={() => undefined}
      />,
    );

    expect(markup).toContain("Thought for 12 seconds");
  });

  it("allows long activity labels to wrap within the timeline control", () => {
    const markup = renderToStaticMarkup(
      <TurnTimeline
        turnId="turn-1"
        isStreaming
        activityLabel="Inspecting a long activity label that must fit a narrow mobile viewport"
        onOpen={() => undefined}
      />,
    );

    expect(markup).toContain("max-w-full");
    expect(markup).toContain("whitespace-normal");
    expect(markup).toContain("min-w-0");
    expect(markup).toContain("break-words");
  });
});

describe("upstream retry state", () => {
  it("labels upstream retries in the timeline", () => {
    const markup = renderToStaticMarkup(
      <TurnTimelinePanel
        timeline={{
          turn_id: "turn-1",
          conversation_id: "conversation-1",
          status: "processing",
          items: [
            {
              id: "status:response-retrying:run-1",
              type: "status",
              status: "retrying",
              content_text: "第 2/6 次尝试，1 秒后重试（502 Bad Gateway）",
              created_at: "2026-07-14T10:00:00Z",
            },
          ],
        }}
        onClose={() => undefined}
      />,
    );

    expect(markup).toContain("正在重试上游请求");
    expect(markup).toContain("第 2/6 次尝试，1 秒后重试（502 Bad Gateway）");
  });
});
