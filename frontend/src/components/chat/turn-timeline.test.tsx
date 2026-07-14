import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { SandboxCommandPayload } from "./turn-timeline";

describe("sandbox command output", () => {
  it("renders the ordered command output as one stream", () => {
    const markup = renderToStaticMarkup(
      <SandboxCommandPayload
        item={{
          id: "tool-1",
          type: "tool_call",
          status: "completed",
          command: "test-command",
          command_output: "first\nsecond\nthird\n",
          created_at: "2026-07-14T00:00:00Z",
        }}
      />,
    );

    expect(markup).toContain("first\nsecond\nthird\n");
    expect(markup.match(/<pre/g)).toHaveLength(2);
    expect(markup).not.toContain("text-destructive");
    expect(markup).not.toContain(">stderr<");
  });
});
