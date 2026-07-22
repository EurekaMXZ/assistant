import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useMediaQuery } from "./use-media-query";

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT =
  true;

function Probe() {
  const matches = useMediaQuery({ breakpoint: "md", range: "below" });
  return <span>{matches ? "mobile" : "desktop"}</span>;
}

describe("useMediaQuery", () => {
  let container: HTMLDivElement;
  let root: Root;
  let matches: boolean;
  let listeners: Set<() => void>;

  beforeEach(() => {
    matches = true;
    listeners = new Set();
    document.documentElement.style.setProperty("--breakpoint-md", "48rem");
    vi.stubGlobal(
      "matchMedia",
      vi.fn(
        (query: string) =>
          ({
            matches,
            media: query,
            addEventListener: (_type: string, listener: () => void) => {
              listeners.add(listener);
            },
            removeEventListener: (_type: string, listener: () => void) => {
              listeners.delete(listener);
            },
          }) as unknown as MediaQueryList,
      ),
    );
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    document.documentElement.style.removeProperty("--breakpoint-md");
    vi.unstubAllGlobals();
  });

  it("reads a Tailwind breakpoint token and responds to changes", async () => {
    await act(async () => root.render(<Probe />));

    expect(container.textContent).toBe("mobile");
    expect(window.matchMedia).toHaveBeenCalledWith("(width < 48rem)");

    matches = false;
    await act(async () => listeners.forEach((listener) => listener()));

    expect(container.textContent).toBe("desktop");
  });
});
