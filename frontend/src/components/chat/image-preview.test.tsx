import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AssistantImagePreview } from "./assistant-image-preview";
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
    vi.useRealTimers();
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
    const dialog = document.querySelector<HTMLElement>('[data-slot="dialog-content"]');
    const transform = document.querySelector<HTMLElement>(".image-preview-transform");
    const zoomIn = document.querySelector<HTMLButtonElement>('[aria-label="放大"]');
    expect(viewer).not.toBeNull();
    expect(dialog?.className).toContain("max-h-none");
    expect(dialog?.className).toContain("overflow-hidden");
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

  it("uses granular wheel deltas for continuous fullscreen zoom", async () => {
    await act(async () => {
      root.render(<ImagePreview src="/image.png" alt="滚轮图片" />);
    });
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[aria-label="预览 滚轮图片"]')?.click();
    });

    const viewer = document.querySelector<HTMLElement>('[aria-label="可缩放图片预览"]');
    const transform = document.querySelector<HTMLElement>(".image-preview-transform");
    await act(async () => {
      viewer?.dispatchEvent(
        new WheelEvent("wheel", { bubbles: true, cancelable: true, deltaY: -100 }),
      );
    });
    expect(transform?.style.transform).toContain("scale(1.1)");
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

  it("keeps partial images strongly blurred and disables preview actions", async () => {
    vi.stubGlobal(
      "matchMedia",
      vi.fn(() => ({ matches: true })),
    );

    await act(async () => {
      root.render(
        <AssistantImagePreview
          alt="生成中的图片"
          concealed
          height={1024}
          src="/partial.png"
          width={1536}
        />,
      );
    });

    const surface = container.querySelector<HTMLElement>('[data-image-mode="partial"]');
    const image = surface?.querySelector("img");
    const spotlight = surface?.querySelector<HTMLElement>("[data-partial-image-spotlight]");
    expect(surface?.style.aspectRatio).toBe("1536 / 1024");
    expect(image?.className).toContain("blur-[22px]");
    expect(spotlight?.className).toContain("transition-[left,top]");
    expect(container.querySelector('[aria-label="预览 生成中的图片"]')).toBeNull();
  });

  it("moves only the brightness spotlight while a partial image is active", async () => {
    vi.useFakeTimers();
    vi.stubGlobal(
      "matchMedia",
      vi.fn(() => ({ matches: false })),
    );
    vi.spyOn(Math, "random")
      .mockReturnValueOnce(0.5)
      .mockReturnValueOnce(0.75)
      .mockReturnValueOnce(0.25);

    await act(async () => {
      root.render(<AssistantImagePreview alt="生成中的图片" concealed src="/partial.png" />);
    });
    const image = container.querySelector("img");
    const spotlight = container.querySelector<HTMLElement>("[data-partial-image-spotlight]");
    expect(spotlight?.style.left).toBe("34%");

    await act(async () => vi.advanceTimersByTime(120));

    expect(spotlight?.style.left).toBe("67%");
    expect(spotlight?.style.top).toBe("33%");
    expect(spotlight?.style.transitionDuration).toBe("5500ms");
    expect(image?.className).toContain("blur-[22px]");
  });

  it("uses the same stable surface when a partial image becomes final", async () => {
    vi.stubGlobal(
      "matchMedia",
      vi.fn(() => ({ matches: true })),
    );

    await act(async () => {
      root.render(
        <AssistantImagePreview
          alt="生成中的图片"
          concealed
          height={1024}
          src="/partial.png"
          width={1024}
        />,
      );
    });
    const partial = container.querySelector<HTMLElement>('[data-image-mode="partial"]');
    const partialClassName = partial?.className;
    const partialAspectRatio = partial?.style.aspectRatio;
    const partialMaxWidth = partial?.style.maxWidth;

    await act(async () => {
      root.render(
        <AssistantImagePreview alt="最终图片" height={1024} src="/final.png" width={1024} />,
      );
    });
    const final = container.querySelector<HTMLElement>('[data-image-mode="final"]');
    expect(final?.className).toBe(partialClassName);
    expect(final?.style.aspectRatio).toBe(partialAspectRatio);
    expect(final?.style.maxWidth).toBe(partialMaxWidth);
    expect(final?.querySelector("img")?.className).not.toContain("blur-[22px]");
    expect(container.querySelector('[aria-label="预览 最终图片"]')).not.toBeNull();
  });
});
