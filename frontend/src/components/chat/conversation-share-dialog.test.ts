import { describe, expect, it } from "vitest";
import { buildConversationShareUrl } from "./conversation-share-dialog";

describe("conversation share URL", () => {
  it("builds the public share route and escapes the identifier", () => {
    expect(buildConversationShareUrl("share/id", "https://assistant.example")).toBe(
      "https://assistant.example/share/share%2Fid",
    );
  });
});
