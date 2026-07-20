export const DEFAULT_SCROLL_FOLLOW_THRESHOLD = 96;

export function isViewportNearBottom(
  viewport: Pick<HTMLElement, "clientHeight" | "scrollHeight" | "scrollTop">,
  threshold = DEFAULT_SCROLL_FOLLOW_THRESHOLD,
) {
  return viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight <= threshold;
}

export function shouldFollowAfterScroll(
  viewport: Pick<HTMLElement, "clientHeight" | "scrollHeight" | "scrollTop">,
  previousScrollTop: number,
) {
  if (viewport.scrollTop < previousScrollTop - 1) return false;
  return isViewportNearBottom(viewport);
}
