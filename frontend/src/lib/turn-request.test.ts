import { describe, expect, it } from "vitest";
import { normalizeTurnRequest, requestDescriptorFromMessage, requestMetadata } from "./turn-request";
import type { Message } from "./types";

describe("turn request descriptors", () => {
  it("round trips retry-critical request fields through message metadata", () => {
    const descriptor = normalizeTurnRequest({
      content: " retry me ",
      attachmentIds: ["attachment-1"],
      modelId: "model-7",
      reasoningEffort: "xhigh",
      metadata: { source: "composer", trace: "request-1" },
    });
    const message = {
      id: "message-1",
      conversation_id: "conversation-1",
      seq: 1,
      role: "user",
      content_text: descriptor.content,
      metadata: requestMetadata(descriptor),
      created_at: "2026-01-01T00:00:00Z",
    } satisfies Message;
    expect(requestDescriptorFromMessage(message)).toEqual(descriptor);
  });
});
