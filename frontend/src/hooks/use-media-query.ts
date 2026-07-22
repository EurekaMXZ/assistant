"use client";

import { useCallback, useSyncExternalStore } from "react";

type TailwindBreakpoint = "sm" | "md" | "lg" | "xl" | "2xl";

type MediaQuery =
  | string
  | {
      breakpoint: TailwindBreakpoint;
      range?: "at-least" | "below";
    };

function resolveQuery(query: MediaQuery) {
  if (typeof query === "string") return query;
  if (typeof window === "undefined") return null;

  const value = getComputedStyle(document.documentElement)
    .getPropertyValue(`--breakpoint-${query.breakpoint}`)
    .trim();
  if (!value) return null;

  return query.range === "below" ? `(width < ${value})` : `(width >= ${value})`;
}

export function useMediaQuery(query: MediaQuery) {
  const directQuery = typeof query === "string" ? query : null;
  const breakpoint = typeof query === "string" ? null : query.breakpoint;
  const range = typeof query === "string" ? null : (query.range ?? "at-least");

  const getQuery = useCallback(
    () => resolveQuery(directQuery ?? { breakpoint: breakpoint!, range: range! }),
    [breakpoint, directQuery, range],
  );

  const subscribe = useCallback(
    (onChange: () => void) => {
      const resolved = getQuery();
      if (!resolved || typeof window.matchMedia !== "function") return () => undefined;

      const mediaQuery = window.matchMedia(resolved);
      mediaQuery.addEventListener("change", onChange);
      return () => mediaQuery.removeEventListener("change", onChange);
    },
    [getQuery],
  );

  const getSnapshot = useCallback(() => {
    const resolved = getQuery();
    return Boolean(resolved && window.matchMedia?.(resolved).matches);
  }, [getQuery]);

  return useSyncExternalStore(subscribe, getSnapshot, () => false);
}
