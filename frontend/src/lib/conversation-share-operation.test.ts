import { beforeEach, describe, expect, it } from "vitest";
import {
  clearConversationShareOperation,
  readConversationShareOperation,
  writeConversationShareOperation,
} from "./conversation-share-operation";

describe("conversation share operation", () => {
  beforeEach(() => window.sessionStorage.clear());

  it("preserves a pending idempotency key until the matching operation completes", () => {
    const operation = {
      idempotencyKey: "share-operation-1",
      lastMessageSeq: 4,
      title: "Shared conversation",
    };
    writeConversationShareOperation("user-1", "conversation-1", operation);

    expect(readConversationShareOperation("user-1", "conversation-1")).toEqual(operation);
    expect(clearConversationShareOperation("user-1", "conversation-1", "different-operation")).toBe(
      false,
    );
    expect(readConversationShareOperation("user-1", "conversation-1")).toEqual(operation);
    expect(clearConversationShareOperation("user-1", "conversation-1", "share-operation-1")).toBe(
      true,
    );
    expect(readConversationShareOperation("user-1", "conversation-1")).toBeNull();
  });
});
