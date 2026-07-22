import { act } from "react";
import { createRoot } from "react-dom/client";
import { describe, expect, it, vi } from "vitest";
import { CursorTableScroll, isAtScrollBottom } from "./cursor-table-scroll";

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT =
  true;

describe("cursor table scrolling", () => {
  it("detects only the bottom edge", () => {
    expect(isAtScrollBottom({ clientHeight: 100, scrollHeight: 300, scrollTop: 199 })).toBe(true);
    expect(isAtScrollBottom({ clientHeight: 100, scrollHeight: 300, scrollTop: 150 })).toBe(false);
  });

  it("loads on a further downward action at the bottom, not on a scroll event", async () => {
    const host = document.createElement("div");
    document.body.appendChild(host);
    const root = createRoot(host);
    const loadMore = vi.fn(async () => undefined);

    await act(async () => {
      root.render(
        <CursorTableScroll hasMore onLoadMore={loadMore}>
          <div>rows</div>
        </CursorTableScroll>,
      );
    });

    const viewport = host.firstElementChild as HTMLDivElement;
    Object.defineProperties(viewport, {
      clientHeight: { configurable: true, value: 100 },
      scrollHeight: { configurable: true, value: 300 },
      scrollTop: { configurable: true, value: 200 },
    });

    await act(async () => {
      viewport.dispatchEvent(new Event("scroll", { bubbles: true }));
      viewport.dispatchEvent(new WheelEvent("wheel", { bubbles: true, deltaY: 20 }));
    });
    expect(loadMore).toHaveBeenCalledTimes(1);

    await act(async () => {
      viewport.dispatchEvent(new WheelEvent("wheel", { bubbles: true, deltaX: 20 }));
      viewport.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, key: "PageDown" }));
    });
    expect(loadMore).toHaveBeenCalledTimes(2);

    await act(async () => root.unmount());
    host.remove();
  });
});
