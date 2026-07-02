export const DEFAULT_SCROLL_FOLLOW_THRESHOLD = 96;

export function isViewportNearBottom(
  viewport: Pick<HTMLElement, "clientHeight" | "scrollHeight" | "scrollTop">,
  threshold = DEFAULT_SCROLL_FOLLOW_THRESHOLD,
) {
  return viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight <= threshold;
}
