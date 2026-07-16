import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MarkdownRenderer } from "./markdown-renderer";

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT =
  true;

describe("markdown renderer", () => {
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
    vi.restoreAllMocks();
  });

  it("portals the link safety dialog outside the markdown paragraph", async () => {
    const open = vi.spyOn(window, "open").mockImplementation(() => null);

    await act(async () => {
      root.render(<MarkdownRenderer content="查看[天气预报](https://example.com/weather)。" />);
    });

    const link = container.querySelector<HTMLButtonElement>('[data-streamdown="link"]');
    expect(link?.closest("p")).not.toBeNull();

    await act(async () => link?.click());

    const dialog = document.querySelector<HTMLElement>('[data-slot="dialog-content"]');
    expect(dialog).not.toBeNull();
    expect(dialog?.closest("p")).toBeNull();
    expect(dialog?.textContent).toContain("https://example.com/weather");

    const confirm = Array.from(dialog?.querySelectorAll("button") ?? []).find(
      (button) => button.textContent === "打开链接",
    );
    await act(async () => confirm?.click());

    expect(open).toHaveBeenCalledWith("https://example.com/weather", "_blank", "noreferrer");
  });
});
