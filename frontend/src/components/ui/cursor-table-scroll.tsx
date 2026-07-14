"use client";

import { useEffect, useRef } from "react";
import { Loader2, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

type VerticalScrollMetrics = Pick<HTMLElement, "clientHeight" | "scrollHeight" | "scrollTop">;

export function isAtScrollBottom(element: VerticalScrollMetrics, tolerance = 1) {
  return element.scrollHeight - element.scrollTop - element.clientHeight <= tolerance;
}

interface CursorTableScrollProps extends React.ComponentProps<"div"> {
  hasMore?: boolean;
  loadingMore?: boolean;
  loadMoreError?: string;
  onLoadMore?: () => Promise<void>;
}

export function CursorTableScroll({
  hasMore = false,
  loadingMore = false,
  loadMoreError = "",
  onLoadMore,
  className,
  children,
  onWheel,
  onScroll,
  onKeyDown,
  onTouchStart,
  onTouchEnd,
  ...props
}: CursorTableScrollProps) {
  const requestPendingRef = useRef(false);
  const touchStartYRef = useRef<number | null>(null);
  const touchStartedAtBottomRef = useRef(false);

  useEffect(() => {
    if (!loadingMore) requestPendingRef.current = false;
  }, [loadingMore]);

  const requestNextPage = async () => {
    if (!hasMore || loadingMore || requestPendingRef.current || !onLoadMore) return;
    requestPendingRef.current = true;
    try {
      await onLoadMore();
    } finally {
      requestPendingRef.current = false;
    }
  };

  return (
    <div
      tabIndex={0}
      className={cn("relative", className)}
      onWheel={(event) => {
        onWheel?.(event);
        if (!event.defaultPrevented && event.deltaY > 0 && isAtScrollBottom(event.currentTarget)) {
          void requestNextPage();
        }
      }}
      onScroll={(event) => {
        onScroll?.(event);
      }}
      onKeyDown={(event) => {
        onKeyDown?.(event);
        if (
          !event.defaultPrevented &&
          ["ArrowDown", "End", "PageDown"].includes(event.key) &&
          isAtScrollBottom(event.currentTarget)
        ) {
          void requestNextPage();
        }
      }}
      onTouchStart={(event) => {
        onTouchStart?.(event);
        touchStartYRef.current = event.touches[0]?.clientY ?? null;
        touchStartedAtBottomRef.current = isAtScrollBottom(event.currentTarget);
      }}
      onTouchEnd={(event) => {
        onTouchEnd?.(event);
        const endY = event.changedTouches[0]?.clientY;
        const movedDown =
          touchStartYRef.current !== null && endY !== undefined && endY < touchStartYRef.current;
        touchStartYRef.current = null;
        if (
          !event.defaultPrevented &&
          movedDown &&
          touchStartedAtBottomRef.current &&
          isAtScrollBottom(event.currentTarget)
        ) {
          void requestNextPage();
        }
        touchStartedAtBottomRef.current = false;
      }}
      {...props}
    >
      {children}
      {loadingMore || loadMoreError ? (
        <div className="sticky bottom-0 left-0 z-20 flex h-10 w-full items-center justify-center gap-2 border-t bg-background/95 px-3 text-xs backdrop-blur-sm">
          {loadingMore ? (
            <>
              <Loader2 className="size-3.5 animate-spin" />
              <span role="status">正在加载</span>
            </>
          ) : (
            <>
              <span className="truncate text-destructive" role="alert" title={loadMoreError}>
                {loadMoreError}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="xs"
                onClick={() => void requestNextPage()}
              >
                <RefreshCw className="size-3.5" />
                重试
              </Button>
            </>
          )}
        </div>
      ) : null}
    </div>
  );
}
