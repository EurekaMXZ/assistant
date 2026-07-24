"use client";

import { useEffect, useState } from "react";
import { cn } from "@/lib/utils";
import { ImagePreview } from "./image-preview";

interface SpotlightState {
  duration: number;
  x: number;
  y: number;
}

interface AssistantImagePreviewProps {
  alt: string;
  concealed?: boolean;
  downloadName?: string;
  height?: number;
  onError?: () => void;
  src: string | null;
  width?: number;
}

const initialSpotlight: SpotlightState = { duration: 5600, x: 34, y: 38 };

function nextSpotlight(): SpotlightState {
  return {
    duration: 4000 + Math.round(Math.random() * 3000),
    x: 16 + Math.round(Math.random() * 68),
    y: 16 + Math.round(Math.random() * 68),
  };
}

function useMovingSpotlight(active: boolean) {
  const [spotlight, setSpotlight] = useState(initialSpotlight);

  useEffect(() => {
    if (!active) return;
    const reducedMotion = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    if (reducedMotion) {
      setSpotlight({ duration: 0, x: 46, y: 42 });
      return;
    }

    let timer = 0;
    const move = () => {
      const next = nextSpotlight();
      setSpotlight(next);
      timer = window.setTimeout(move, next.duration + 250);
    };
    timer = window.setTimeout(move, 120);
    return () => window.clearTimeout(timer);
  }, [active]);

  return spotlight;
}

function imageSurfaceWidth(width?: number, height?: number) {
  if (!width || !height || width >= height) return "36rem";
  return `${Math.max(18, (32 * width) / height)}rem`;
}

export function AssistantImagePreview({
  alt,
  concealed = false,
  downloadName,
  height,
  onError,
  src,
  width,
}: AssistantImagePreviewProps) {
  const spotlight = useMovingSpotlight(concealed && Boolean(src));
  const aspectRatio = width && height ? `${width} / ${height}` : "1 / 1";

  return (
    <div
      className="relative mb-2 w-full overflow-hidden rounded-lg border bg-muted/20"
      data-assistant-image-surface="true"
      data-image-mode={concealed ? "partial" : "final"}
      style={{ aspectRatio, maxWidth: imageSurfaceWidth(width, height) }}
    >
      {src ? (
        <ImagePreview
          src={src}
          alt={alt}
          downloadName={downloadName}
          previewEnabled={!concealed}
          showActions={!concealed}
          wrapperClassName="flex size-full"
          previewButtonClassName="size-full rounded-none"
          imageClassName={cn(
            "size-full rounded-none object-contain transition-[filter,transform] duration-700",
            concealed && "scale-[1.1] blur-[22px] brightness-[0.58] contrast-90 saturate-75",
          )}
          onError={onError}
        />
      ) : (
        <div className="size-full animate-pulse bg-muted" />
      )}

      {concealed && src ? (
        <>
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 bg-black/25"
            data-partial-image-veil="true"
          />
          <div
            aria-hidden="true"
            className="pointer-events-none absolute h-[72%] w-[72%] -translate-x-1/2 -translate-y-1/2 rounded-full opacity-90 blur-3xl mix-blend-screen transition-[left,top] ease-in-out will-change-[left,top]"
            data-partial-image-spotlight="true"
            style={{
              background:
                "radial-gradient(circle, rgba(255,255,255,0.5) 0%, rgba(255,255,255,0.2) 38%, rgba(255,255,255,0) 72%)",
              left: `${spotlight.x}%`,
              top: `${spotlight.y}%`,
              transitionDuration: `${spotlight.duration}ms`,
            }}
          />
        </>
      ) : null}
    </div>
  );
}
