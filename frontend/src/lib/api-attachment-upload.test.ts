import { beforeEach, describe, expect, it, vi } from "vitest";
import { uploadConversationAttachment } from "./api";

const pendingAttachment = {
  id: "attachment-1",
  conversation_id: "conversation-1",
  uploaded_by_user_id: "user-1",
  filename: "notes.txt",
  content_type: "text/plain",
  category: "text",
  size_bytes: 5,
  sha256: "",
  status: "pending",
  created_at: "2026-07-20T00:00:00Z",
  updated_at: "2026-07-20T00:00:00Z",
};

beforeEach(() => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
    clear: () => values.clear(),
    key: (index: number) => [...values.keys()][index] ?? null,
    get length() {
      return values.size;
    },
  } satisfies Storage;
  vi.stubGlobal("localStorage", storage);
  Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  storage.setItem("assistant_access_token", "api-token");
});

describe("attachment direct upload", () => {
  it("uploads to the presigned URL and returns only after completion", async () => {
    const file = new File(["hello"], "notes.txt", { type: "text/plain" });
    const fileBytes = new TextEncoder().encode("hello");
    Object.defineProperty(file, "slice", {
      value: (start: number, end: number) => ({
        arrayBuffer: async () => fileBytes.slice(start, end).buffer,
      }),
    });
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const url = String(input);
      if (url.endsWith("/conversations/conversation-1/attachments")) {
        return Response.json(
          {
            attachment: pendingAttachment,
            upload: {
              url: "https://objects.example/upload?signature=1",
              method: "PUT",
              headers: {
                "Content-Type": "text/plain",
                "Content-MD5": "XUFAKrxLKna5cZ2REBfFkg==",
              },
              expires_at: "2026-07-20T00:15:00Z",
            },
          },
          { status: 201 },
        );
      }
      if (url.startsWith("https://objects.example/upload")) {
        expect(init?.body).toBe(file);
        expect(new Headers(init?.headers).get("Authorization")).toBeNull();
        expect(new Headers(init?.headers).get("Content-MD5")).toBe("XUFAKrxLKna5cZ2REBfFkg==");
        return new Response(null, { status: 200 });
      }
      if (url.endsWith("/attachments/attachment-1/complete")) {
        return Response.json({
          attachment: {
            ...pendingAttachment,
            status: "ready",
            sha256: "0".repeat(64),
            upload_completed_at: "2026-07-20T00:01:00Z",
          },
        });
      }
      throw new Error(`unexpected request: ${url}`);
    });

    const attachment = await uploadConversationAttachment(
      "conversation-1",
      file,
      "upload-operation-1",
    );

    expect(attachment.status).toBe("ready");
    expect(fetchMock.mock.calls.map((call) => String(call[0]))).toEqual([
      "/api/v1/conversations/conversation-1/attachments",
      "https://objects.example/upload?signature=1",
      "/api/v1/conversations/conversation-1/attachments/attachment-1/complete",
    ]);
  });
});
