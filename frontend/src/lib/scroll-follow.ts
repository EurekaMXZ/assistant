export const DEFAULT_SCROLL_FOLLOW_THRESHOLD = 96;
export const DEFAULT_MESSAGE_BOTTOM_GAP = 64;
export const MINIMUM_LATEST_TURN_HEIGHT = 192;

export type MessageScrollAction = "anchor-user" | "follow-bottom" | "none";

export function isViewportNearBottom(
  viewport: Pick<HTMLElement, "clientHeight" | "scrollHeight" | "scrollTop">,
  threshold = DEFAULT_SCROLL_FOLLOW_THRESHOLD,
) {
  return viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight <= threshold;
}

export function shouldFollowAfterScroll(
  viewport: Pick<HTMLElement, "clientHeight" | "scrollHeight" | "scrollTop">,
  previousScrollTop: number,
  wasFollowing = true,
) {
  if (viewport.scrollTop < previousScrollTop - 1) return false;
  if (viewport.scrollTop > previousScrollTop + 1) return isViewportNearBottom(viewport);
  return wasFollowing;
}

export function messageScrollAction(
  previousUserMessageId: string | null | undefined,
  currentUserMessageId: string | null,
  shouldFollow: boolean,
): MessageScrollAction {
  if (
    previousUserMessageId !== undefined &&
    currentUserMessageId !== null &&
    currentUserMessageId !== previousUserMessageId
  ) {
    return "anchor-user";
  }
  return shouldFollow ? "follow-bottom" : "none";
}

export function latestTurnMinimumHeight(viewportHeight: number, bottomInset: number) {
  if (viewportHeight <= 0) return 0;
  return Math.max(
    MINIMUM_LATEST_TURN_HEIGHT,
    viewportHeight - Math.max(0, bottomInset) - DEFAULT_MESSAGE_BOTTOM_GAP,
  );
}
