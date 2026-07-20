import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  clearInitialTurnOperation,
  commitPreparedInitialTurn,
  createInitialTurnOperation,
  loadInitialTurnOperation,
  prepareInitialTurnAttachments,
  runInitialTurnOperation,
  saveInitialTurnOperation,
  syncInitialTurnOperation,
  uploadInitialTurnAttachment,
} from "./initial-turn-operation";
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
      return {
        id: `attachment-${file.name}`,
        sha256:
          file.name === "a.txt"
            ? "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb"
            : "3e23e8160039594a33894f6564e1b1348bbd7a0088d42c4acb73eeaed59c009d",
      } as Attachment;
    });
    const commit = vi.fn(async () => ({ conversation_id: "conversation-1" }) as InitialTurnResult);

    await expect(
      runInitialTurnOperation(operation, files, { prepare, uploadAttachment, commit }),
    ).rejects.toThrow("network uncertain");
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
    const operation = createInitialTurnOperation(
      { content: "hello", metadata: {} },
      [],
      "user-1",
      "operation-2",
    );
    const prepare = vi.fn(async () => ({ conversation: { id: "conversation-2" } as Conversation }));
    const uploadAttachment = vi.fn();
    const commit = vi
      .fn()
      .mockRejectedValueOnce(new Error("network uncertain"))
      .mockResolvedValue({ conversation_id: "conversation-2" } as InitialTurnResult);
    await expect(
      runInitialTurnOperation(operation, [], { prepare, uploadAttachment, commit }),
    ).rejects.toThrow();
    await runInitialTurnOperation(loadInitialTurnOperation()!, [], {
      prepare,
      uploadAttachment,
      commit,
    });
    expect(prepare).toHaveBeenCalledTimes(1);
    expect(commit.mock.calls.map((call) => call[2])).toEqual(["operation-2", "operation-2"]);
  });

  it("prepares and uploads attachments before send while preserving them across draft edits", async () => {
    const files = [new File(["a"], "a.txt", { type: "text/plain", lastModified: 1 })];
    const operation = createInitialTurnOperation(
      { content: "first draft", metadata: { source: "home" } },
      files,
      "user-1",
      "operation-3",
    );
    const prepared = await prepareInitialTurnAttachments(operation, files, {
      prepare: vi.fn(async () => ({ conversation: { id: "conversation-3" } as Conversation })),
      uploadAttachment: vi.fn(async () => ({ id: "attachment-a" }) as Attachment),
    });

    expect(prepared.conversation_id).toBe("conversation-3");
    expect(prepared.files[0].attachment_id).toBe("attachment-a");
    const edited = syncInitialTurnOperation(
      prepared,
      { content: "edited draft", metadata: { source: "home" } },
      files,
      "user-1",
    );
    expect(edited.conversation_id).toBe("conversation-3");
    expect(edited.files[0].attachment_id).toBe("attachment-a");
    expect(edited.descriptor.content).toBe("edited draft");
  });

  it("commits only attachments that are ready", async () => {
    const files = [
      new File(["ready"], "ready.txt", { type: "text/plain", lastModified: 1 }),
      new File(["pending"], "pending.txt", { type: "text/plain", lastModified: 2 }),
    ];
    const operation = createInitialTurnOperation(
      { content: "send now", metadata: {} },
      files,
      "user-1",
      "operation-4",
    );
    const prepared = {
      ...operation,
      conversation_id: "conversation-4",
      files: operation.files.map((file, index) =>
        index === 0 ? { ...file, attachment_id: "attachment-ready" } : file,
      ),
    };
    const commit = vi.fn(async () => ({ conversation_id: "conversation-4" }) as InitialTurnResult);

    await commitPreparedInitialTurn(prepared, { commit });

    expect(commit).toHaveBeenCalledWith(
      "conversation-4",
      expect.objectContaining({ attachment_ids: ["attachment-ready"] }),
      "operation-4",
    );
  });

  it("does not restore a cleared operation when an upload finishes late", async () => {
    const file = new File(["late"], "late.txt", { type: "text/plain", lastModified: 1 });
    const operation = createInitialTurnOperation(
      { content: "send first", metadata: {} },
      [file],
      "user-1",
      "operation-5",
    );
    saveInitialTurnOperation(operation);
    let finishUpload: ((attachment: Attachment) => void) | undefined;
    const uploadAttachment = vi.fn(
      () =>
        new Promise<Attachment>((resolve) => {
          finishUpload = resolve;
        }),
    );
    const uploading = uploadInitialTurnAttachment(
      operation.key,
      operation.files[0].upload_key!,
      file,
      {
        prepare: vi.fn(async () => ({ conversation: { id: "conversation-5" } as Conversation })),
        uploadAttachment,
      },
    );
    await vi.waitFor(() => expect(uploadAttachment).toHaveBeenCalledOnce());

    clearInitialTurnOperation(operation.key);
    finishUpload?.({ id: "attachment-late", sha256: "hash" } as Attachment);

    await expect(uploading).resolves.toBeNull();
    expect(loadInitialTurnOperation()).toBeNull();
  });
});
