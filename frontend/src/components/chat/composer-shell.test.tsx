import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Composer } from "./composer";
import { ComposerAttachmentList } from "./composer-shell";

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

  it("smoothly scrolls attachments and contains wheel events at both edges", async () => {
    await act(async () => {
      root.render(
        <ComposerAttachmentList
          attachments={[
            { key: "one", name: "one.txt", size: 1, status: "ready" },
            { key: "two", name: "two.txt", size: 1, status: "ready" },
            { key: "three", name: "three.txt", size: 1, status: "ready" },
          ]}
        />,
      );
    });

    const list = container.querySelector<HTMLElement>('[data-slot="attachment-list"]');
    expect(list).not.toBeNull();
    Object.defineProperties(list as HTMLElement, {
      clientWidth: { configurable: true, value: 300 },
      scrollWidth: { configurable: true, value: 700 },
      scrollLeft: { configurable: true, value: 0, writable: true },
    });
    const moveRight = new WheelEvent("wheel", { deltaY: 120, bubbles: true, cancelable: true });
    await act(async () => list?.dispatchEvent(moveRight));
    expect(moveRight.defaultPrevented).toBe(true);
    await act(async () => {
      await new Promise((resolve) => window.setTimeout(resolve, 120));
    });
    expect((list as HTMLElement).scrollLeft).toBeGreaterThan(0);

    await act(async () => {
      list?.dispatchEvent(new Event("pointerdown"));
      (list as HTMLElement).scrollLeft = 120;
      list?.dispatchEvent(new Event("scroll"));
    });
    expect(container.querySelector(".attachment-overflow-mask-left")?.dataset.visible).toBe("true");
    expect(container.querySelector(".attachment-overflow-mask-right")?.dataset.visible).toBe(
      "true",
    );

    await act(async () => {
      (list as HTMLElement).scrollLeft = 400;
      list?.dispatchEvent(new Event("scroll"));
    });
    const atEnd = new WheelEvent("wheel", { deltaY: 100, bubbles: true, cancelable: true });
    await act(async () => list?.dispatchEvent(atEnd));
    expect((list as HTMLElement).scrollLeft).toBe(400);
    expect(atEnd.defaultPrevented).toBe(true);

    await act(async () => {
      (list as HTMLElement).scrollLeft = 0;
      list?.dispatchEvent(new Event("scroll"));
    });
    const atStart = new WheelEvent("wheel", { deltaY: -100, bubbles: true, cancelable: true });
    await act(async () => list?.dispatchEvent(atStart));
    expect(atStart.defaultPrevented).toBe(true);
  });
});
