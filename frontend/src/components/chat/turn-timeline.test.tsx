import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { TurnTimeline } from "./turn-timeline";
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
});
