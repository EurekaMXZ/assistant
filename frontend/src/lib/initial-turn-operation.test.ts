import { beforeEach, describe, expect, it, vi } from "vitest";
import { createInitialTurnOperation, loadInitialTurnOperation, runInitialTurnOperation } from "./initial-turn-operation";
import type { Attachment, Conversation } from "./types";
import type { InitialTurnResult } from "./api";

describe("initial turn operation", () => {
  beforeEach(() => sessionStorage.clear());

  it("reuses the prepared conversation and completed uploads after partial failure", async () => {
    const files = [
      new File(["a"], "a.txt", { type: "text/plain", lastModified: 1 }),
      new File(["b"], "b.txt", { type: "text/plain", lastModified: 2 }),
    ];
    const operation = createInitialTurnOperation(
      { content: "hello", metadata: { source: "home" } },
      files,
      "user-1",
      "operation-1",
    );
    const prepare = vi.fn(async () => ({ conversation: { id: "conversation-1" } as Conversation }));
    let uploads = 0;
    const uploadAttachment = vi.fn(async (_conversationId: string, file: File) => {
      uploads += 1;
      if (file.name === "b.txt" && uploads === 2) throw new Error("network uncertain");
      return { id: `attachment-${file.name}` } as Attachment;
    });
    const commit = vi.fn(async () => ({ conversation_id: "conversation-1" } as InitialTurnResult));

    await expect(runInitialTurnOperation(operation, files, { prepare, uploadAttachment, commit })).rejects.toThrow("network uncertain");
    const saved = loadInitialTurnOperation();
    expect(saved?.conversation_id).toBe("conversation-1");
    expect(saved?.files[0].attachment_id).toBe("attachment-a.txt");

    await runInitialTurnOperation(saved!, files, { prepare, uploadAttachment, commit });
    expect(prepare).toHaveBeenCalledTimes(1);
    expect(uploadAttachment.mock.calls.filter((call) => call[1].name === "a.txt")).toHaveLength(1);
    expect(commit).toHaveBeenCalledWith(
      "conversation-1",
      expect.objectContaining({ attachment_ids: ["attachment-a.txt", "attachment-b.txt"] }),
      "operation-1",
    );
  });

  it("retries uncertain commit with the same operation key", async () => {
    const operation = createInitialTurnOperation({ content: "hello", metadata: {} }, [], "user-1", "operation-2");
    const prepare = vi.fn(async () => ({ conversation: { id: "conversation-2" } as Conversation }));
    const uploadAttachment = vi.fn();
    const commit = vi.fn()
      .mockRejectedValueOnce(new Error("network uncertain"))
      .mockResolvedValue({ conversation_id: "conversation-2" } as InitialTurnResult);
    await expect(runInitialTurnOperation(operation, [], { prepare, uploadAttachment, commit })).rejects.toThrow();
    await runInitialTurnOperation(loadInitialTurnOperation()!, [], { prepare, uploadAttachment, commit });
    expect(prepare).toHaveBeenCalledTimes(1);
    expect(commit.mock.calls.map((call) => call[2])).toEqual(["operation-2", "operation-2"]);
  });
});
