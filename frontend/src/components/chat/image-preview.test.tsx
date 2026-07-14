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
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
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
    const zoomIn = document.querySelector<HTMLButtonElement>('[aria-label="放大"]');
    expect(viewer?.style.transform).toContain("scale(1)");
    expect(zoomIn).not.toBeNull();

    await act(async () => zoomIn?.click());
    expect(viewer?.style.transform).toContain("scale(1.1)");
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
