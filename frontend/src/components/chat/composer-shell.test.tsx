import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Composer } from "./composer";

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT =
  true;

describe("composer attachment uploads", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.stubGlobal(
      "ResizeObserver",
      class ResizeObserverMock {
        disconnect() {}
        observe() {}
        unobserve() {}
      },
    );
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
  });

  it("shows each upload state and sends only ready attachment IDs", async () => {
    const onSend = vi.fn();
    await act(async () => {
      root.render(
        <Composer
          attachments={[
            {
              file: new File(["pending"], "pending.txt", { type: "text/plain" }),
              key: "pending",
              name: "pending.txt",
              size: 7,
              status: "uploading",
            },
            {
              error: "network error",
              file: new File(["failed"], "failed.txt", { type: "text/plain" }),
              key: "failed",
              name: "failed.txt",
              size: 6,
              status: "failed",
            },
            {
              attachmentId: "attachment-ready",
              key: "ready",
              name: "ready.txt",
              size: 5,
              status: "ready",
            },
          ]}
          onChange={() => undefined}
          onSend={onSend}
          value="send now"
        />,
      );
    });

    expect(container.querySelector('[data-upload-state="uploading"]')).not.toBeNull();
    expect(container.querySelector('[aria-label="正在上传 pending.txt"]')).not.toBeNull();
    expect(container.querySelector('[data-upload-state="failed"]')).not.toBeNull();
    expect(container.querySelector('[aria-label="failed.txt 上传失败"]')).not.toBeNull();

    const sendButton = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("发送"),
    );
    expect(sendButton?.disabled).toBe(false);
    await act(async () => sendButton?.click());
    expect(onSend).toHaveBeenCalledWith("send now", ["attachment-ready"]);
  });
});
