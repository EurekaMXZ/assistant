"use client";

import { useEffect, useRef, type PointerEvent as ReactPointerEvent } from "react";
import { cn } from "@/lib/utils";

const longPressDelay = 500;
const movementTolerance = 8;

export function MobileHeaderTitle({
  actionLabel,
  onLongPress,
  title,
}: {
  actionLabel?: string;
  onLongPress?: () => void;
  title: string;
}) {
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const startRef = useRef({ x: 0, y: 0 });
  const triggeredRef = useRef(false);

  const clearTimer = () => {
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = null;
  };

  const trigger = () => {
    clearTimer();
    if (!onLongPress || triggeredRef.current) return;
    triggeredRef.current = true;
    onLongPress();
  };

  useEffect(() => clearTimer, [onLongPress]);

  const handlePointerDown = (event: ReactPointerEvent<HTMLHeadingElement>) => {
    if (!onLongPress || event.button !== 0) return;
    clearTimer();
    triggeredRef.current = false;
    startRef.current = { x: event.clientX, y: event.clientY };
    timerRef.current = setTimeout(trigger, longPressDelay);
  };

  const handlePointerMove = (event: ReactPointerEvent<HTMLHeadingElement>) => {
    if (!timerRef.current) return;
    const movedX = Math.abs(event.clientX - startRef.current.x);
    const movedY = Math.abs(event.clientY - startRef.current.y);
    if (movedX > movementTolerance || movedY > movementTolerance) clearTimer();
  };

  return (
    <h1
      className={cn(
        "flex min-h-10 items-center justify-center truncate px-3 text-center text-sm font-medium",
        onLongPress &&
          "select-none rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
      )}
      role={onLongPress ? "button" : undefined}
      tabIndex={onLongPress ? 0 : undefined}
      aria-label={onLongPress ? actionLabel : undefined}
      onBlur={clearTimer}
      onContextMenu={(event) => {
        if (!onLongPress) return;
        event.preventDefault();
        trigger();
      }}
      onKeyDown={(event) => {
        if (!onLongPress || (event.key !== "Enter" && event.key !== " ")) return;
        event.preventDefault();
        onLongPress();
      }}
      onPointerCancel={clearTimer}
      onPointerDown={handlePointerDown}
      onPointerLeave={clearTimer}
      onPointerMove={handlePointerMove}
      onPointerUp={clearTimer}
    >
      {title}
    </h1>
  );
}
