import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ComposerShell } from "./composer-shell";
import { ImagePreview } from "./image-preview";
import { MarkdownRenderer } from "./markdown-renderer";

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT =
  true;

describe("image preview", () => {
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

  it("uses the preview component for markdown images", () => {
    const markup = renderToStaticMarkup(
      <MarkdownRenderer content="![示例图片](https://example.com/image.png)" />,
    );

    expect(markup).toContain('data-image-preview="root"');
    expect(markup).toContain('data-streamdown="image-wrapper"');
    expect(markup).toContain('aria-label="预览 示例图片"');
  });

  it("opens the fullscreen viewer and zooms the image", async () => {
    await act(async () => {
      root.render(<ImagePreview src="/image.png" alt="示例图片" />);
    });

    const trigger = container.querySelector<HTMLButtonElement>('[aria-label="预览 示例图片"]');
    expect(trigger).not.toBeNull();

    await act(async () => trigger?.click());

    const viewer = document.querySelector<HTMLElement>('[aria-label="可缩放图片预览"]');
    const transform = document.querySelector<HTMLElement>(".image-preview-transform");
    const zoomIn = document.querySelector<HTMLButtonElement>('[aria-label="放大"]');
    expect(viewer).not.toBeNull();
    expect(transform?.style.transform).toContain("scale(1)");
    expect(zoomIn).not.toBeNull();

    await act(async () => zoomIn?.click());
    expect(transform?.style.transform).toContain("scale(1.1)");
  });

  it("zooms the fullscreen image with a two-finger gesture", async () => {
    await act(async () => {
      root.render(<ImagePreview src="/image.png" alt="手势图片" />);
    });

    await act(async () => {
      container.querySelector<HTMLButtonElement>('[aria-label="预览 手势图片"]')?.click();
    });

    const viewer = document.querySelector<HTMLElement>('[aria-label="可缩放图片预览"]');
    const transform = document.querySelector<HTMLElement>(".image-preview-transform");
    expect(viewer).not.toBeNull();

    const dispatchTouch = (type: string, points: Array<{ clientX: number; clientY: number }>) => {
      const event = new Event(type, { bubbles: true, cancelable: true });
      Object.defineProperty(event, "touches", {
        value: points.map((point) => ({
          ...point,
          pageX: point.clientX,
          pageY: point.clientY,
        })),
      });
      viewer?.dispatchEvent(event);
    };

    await act(async () => {
      dispatchTouch("touchstart", [
        { clientX: 100, clientY: 100 },
        { clientX: 200, clientY: 100 },
      ]);
      dispatchTouch("touchmove", [
        { clientX: 100, clientY: 100 },
        { clientX: 250, clientY: 100 },
      ]);
    });
    expect(transform?.style.transform).toContain("scale(1.5)");

    await act(async () => {
      dispatchTouch("touchmove", [
        { clientX: 100, clientY: 100 },
        { clientX: 180, clientY: 100 },
      ]);
      dispatchTouch("touchend", []);
    });
    expect(transform?.style.transform).toContain("scale(0.8)");
  });

  it("uses the same preview for composer image attachments", async () => {
    vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:composer-image");
    vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined);

    await act(async () => {
      root.render(
        <ComposerShell
          attachments={[
            {
              contentType: "image/png",
              file: new File(["image"], "upload.png", { type: "image/png" }),
              key: "upload.png",
              name: "upload.png",
              size: 5,
              status: "ready",
            },
          ]}
          modelId=""
          models={[]}
          onChange={() => undefined}
          onSubmit={() => undefined}
          placeholder="输入消息"
          reasoningEfforts={{}}
          value=""
        />,
      );
      await Promise.resolve();
    });

    expect(container.querySelector('[aria-label="预览 upload.png"]')).not.toBeNull();
  });
});
