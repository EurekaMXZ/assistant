import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "./tooltip";

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT =
  true;

describe("tooltip", () => {
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

  it("opens an opted-in tooltip after a touch click", async () => {
    await act(async () => {
      root.render(
        <Tooltip>
          <TooltipTrigger openOnClick render={<Button type="button" />}>
            详情
          </TooltipTrigger>
          <TooltipContent>用量详情</TooltipContent>
        </Tooltip>,
      );
    });

    const trigger = container.querySelector<HTMLButtonElement>("button");
    expect(trigger).not.toBeNull();

    await act(async () => {
      trigger?.dispatchEvent(new Event("touchstart", { bubbles: true, cancelable: true }));
      trigger?.dispatchEvent(new Event("touchend", { bubbles: true, cancelable: true }));
      trigger?.click();
    });

    expect(document.querySelector('[data-slot="tooltip-content"]')?.textContent).toContain(
      "用量详情",
    );
    expect(trigger?.hasAttribute("data-popup-open")).toBe(true);
  });
});
