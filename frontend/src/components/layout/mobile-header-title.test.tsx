import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MobileHeaderTitle } from "./mobile-header-title";

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT =
  true;

describe("mobile header title", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.useFakeTimers();
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.useRealTimers();
  });

  it("invokes the title action after a long press", async () => {
    const onLongPress = vi.fn();
    await act(async () => {
      root.render(
        <MobileHeaderTitle
          actionLabel="修改对话标题"
          onLongPress={onLongPress}
          title="Long conversation title"
        />,
      );
    });
    const title = container.querySelector("h1");
    expect(title).not.toBeNull();

    await act(async () => {
      title?.dispatchEvent(
        new MouseEvent("pointerdown", { bubbles: true, button: 0, clientX: 10, clientY: 10 }),
      );
      vi.advanceTimersByTime(500);
    });

    expect(onLongPress).toHaveBeenCalledTimes(1);
  });
});
