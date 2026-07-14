"use client";

import {
  useEffect,
  useRef,
  useState,
  type ComponentProps,
  type PointerEvent as ReactPointerEvent,
} from "react";
import { Download, Maximize2, RotateCcw, X, ZoomIn, ZoomOut } from "lucide-react";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { cn } from "@/lib/utils";

const minScale = 0.5;
const maxScale = 3;
const scaleStep = 0.1;

interface ImagePreviewProps extends Omit<ComponentProps<"img">, "alt" | "src"> {
  alt: string;
  downloadName?: string;
  fallbackClassName?: string;
  imageClassName?: string;
  previewButtonClassName?: string;
  showActions?: boolean;
  src: string;
  streamdown?: boolean;
  wrapperClassName?: string;
}

interface ViewportState {
  scale: number;
  x: number;
  y: number;
}

function clampScale(scale: number) {
  return Math.min(maxScale, Math.max(minScale, Number(scale.toFixed(1))));
}

function extensionForBlob(blob: Blob) {
  if (blob.type.includes("jpeg")) return "jpg";
  if (blob.type.includes("svg")) return "svg";
  if (blob.type.includes("gif")) return "gif";
  if (blob.type.includes("webp")) return "webp";
  if (blob.type.includes("avif")) return "avif";
  return "png";
}

function downloadFilename(src: string, alt: string, blob: Blob, requestedName?: string) {
  if (requestedName?.trim()) return requestedName.trim();

  try {
    const name = decodeURIComponent(
      new URL(src, window.location.origin).pathname.split("/").pop() || "",
    );
    if (/\.[a-z0-9]{2,5}$/i.test(name)) return name;
  } catch {
    // Blob and data URLs fall back to the image label.
  }

  const baseName = (alt.trim() || "image")
    .replace(/[\\/:*?"<>|]/g, "-")
    .replace(/\.[a-z0-9]{2,5}$/i, "");
  return `${baseName}.${extensionForBlob(blob)}`;
}

async function downloadImage(src: string, alt: string, requestedName?: string) {
  try {
    const response = await fetch(src);
    if (!response.ok) throw new Error(`Image download failed: ${response.status}`);
    const blob = await response.blob();
    const objectUrl = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = objectUrl;
    link.download = downloadFilename(src, alt, blob, requestedName);
    document.body.appendChild(link);
    link.click();
    link.remove();
    requestAnimationFrame(() => URL.revokeObjectURL(objectUrl));
  } catch {
    window.open(src, "_blank", "noopener,noreferrer");
  }
}

function ImagePanZoom({ alt, src }: { alt: string; src: string }) {
  const [viewport, setViewport] = useState<ViewportState>({ scale: 1, x: 0, y: 0 });
  const [isPanning, setIsPanning] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const dragStartRef = useRef({ pointerX: 0, pointerY: 0, x: 0, y: 0 });

  const changeScale = (delta: number) => {
    setViewport((current) => ({ ...current, scale: clampScale(current.scale + delta) }));
  };

  const reset = () => setViewport({ scale: 1, x: 0, y: 0 });

  const handlePointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || !event.isPrimary) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    dragStartRef.current = {
      pointerX: event.clientX,
      pointerY: event.clientY,
      x: viewport.x,
      y: viewport.y,
    };
    setIsPanning(true);
  };

  const handlePointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!isPanning) return;
    event.preventDefault();
    const start = dragStartRef.current;
    setViewport((current) => ({
      ...current,
      x: start.x + event.clientX - start.pointerX,
      y: start.y + event.clientY - start.pointerY,
    }));
  };

  const handlePointerEnd = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!isPanning) return;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    setIsPanning(false);
  };

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const handleWheel = (event: WheelEvent) => {
      event.preventDefault();
      const delta = event.deltaY > 0 ? -scaleStep : scaleStep;
      setViewport((current) => ({
        ...current,
        scale: clampScale(current.scale + delta),
      }));
    };

    container.addEventListener("wheel", handleWheel, { passive: false });
    return () => container.removeEventListener("wheel", handleWheel);
  }, []);

  return (
    <div
      className="relative size-full overflow-hidden"
      data-image-preview="viewport"
      ref={containerRef}
    >
      <div className="absolute bottom-4 left-4 z-10 flex flex-col gap-1 rounded-md border border-border bg-background/80 p-1 supports-[backdrop-filter]:bg-background/70 supports-[backdrop-filter]:backdrop-blur-sm">
        <button
          type="button"
          title="放大"
          aria-label="放大"
          className="flex cursor-pointer items-center justify-center rounded p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
          disabled={viewport.scale >= maxScale}
          onClick={() => changeScale(scaleStep)}
        >
          <ZoomIn className="size-4" />
        </button>
        <button
          type="button"
          title="缩小"
          aria-label="缩小"
          className="flex cursor-pointer items-center justify-center rounded p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
          disabled={viewport.scale <= minScale}
          onClick={() => changeScale(-scaleStep)}
        >
          <ZoomOut className="size-4" />
        </button>
        <button
          type="button"
          title="重置缩放与位置"
          aria-label="重置缩放与位置"
          className="flex cursor-pointer items-center justify-center rounded p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          onClick={reset}
        >
          <RotateCcw className="size-4" />
        </button>
      </div>

      <div
        role="application"
        aria-label="可缩放图片预览"
        className={cn(
          "flex size-full touch-none select-none items-center justify-center origin-center transition-transform duration-150 ease-out",
          isPanning ? "cursor-grabbing" : "cursor-grab",
        )}
        onPointerCancel={handlePointerEnd}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerEnd}
        style={{
          transform: `translate(${viewport.x}px, ${viewport.y}px) scale(${viewport.scale})`,
          transformOrigin: "center center",
          willChange: "transform",
        }}
      >
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={src}
          alt={alt}
          className="pointer-events-none max-h-[calc(100dvh-2rem)] max-w-[calc(100vw-2rem)] object-contain"
          draggable={false}
        />
      </div>
    </div>
  );
}

function ImagePreviewDialog({
  alt,
  onOpenChange,
  open,
  src,
}: {
  alt: string;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  src: string;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className="inset-0 left-0 top-0 h-dvh w-screen max-w-none translate-x-0 translate-y-0 gap-0 rounded-none bg-background/95 p-0 ring-0 backdrop-blur-sm sm:max-w-none"
      >
        <DialogTitle className="sr-only">{alt || "图片预览"}</DialogTitle>
        <button
          type="button"
          title="退出全屏"
          aria-label="退出全屏"
          className="absolute right-4 top-4 z-20 cursor-pointer rounded-md p-2 text-muted-foreground transition-all hover:bg-muted hover:text-foreground"
          onClick={() => onOpenChange(false)}
        >
          <X className="size-5" />
        </button>
        <ImagePanZoom alt={alt} src={src} />
      </DialogContent>
    </Dialog>
  );
}

export function ImagePreview({
  alt,
  className,
  downloadName,
  fallbackClassName,
  imageClassName,
  onError,
  onLoad,
  previewButtonClassName,
  showActions = true,
  src,
  streamdown = false,
  wrapperClassName,
  ...imageProps
}: ImagePreviewProps) {
  const [open, setOpen] = useState(false);
  const [loadedSrc, setLoadedSrc] = useState<string | null>(null);
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const imageRef = useRef<HTMLImageElement>(null);
  const loaded = loadedSrc === src;
  const failed = failedSrc === src;

  useEffect(() => {
    const image = imageRef.current;
    if (!image?.complete) return;
    if (image.naturalWidth > 0) setLoadedSrc(src);
    else setFailedSrc(src);
  }, [src]);

  if (failed) {
    return (
      <span
        className={cn("text-xs italic text-muted-foreground", fallbackClassName)}
        data-streamdown={streamdown ? "image-fallback" : undefined}
      >
        图片无法加载
      </span>
    );
  }

  return (
    <span
      className={cn("group/image relative inline-flex max-w-full align-middle", wrapperClassName)}
      data-image-preview="root"
      data-streamdown={streamdown ? "image-wrapper" : undefined}
    >
      <button
        type="button"
        aria-label={`预览 ${alt || "图片"}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        className={cn(
          "relative flex max-w-full cursor-zoom-in items-center justify-center overflow-hidden rounded-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
          previewButtonClassName,
        )}
        onClick={(event) => {
          event.preventDefault();
          event.stopPropagation();
          setOpen(true);
        }}
      >
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          {...imageProps}
          src={src}
          alt={alt}
          className={cn("max-w-full rounded-lg", imageClassName, className)}
          data-streamdown={streamdown ? "image" : undefined}
          draggable={false}
          ref={imageRef}
          onError={(event) => {
            setFailedSrc(src);
            onError?.(event);
          }}
          onLoad={(event) => {
            setLoadedSrc(src);
            onLoad?.(event);
          }}
        />
        <span
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 rounded-lg bg-black/0 transition-colors group-hover/image:bg-black/10"
        />
      </button>

      {showActions && loaded ? (
        <span className="absolute right-2 top-2 z-10 flex gap-1 rounded-md border border-border bg-background/80 p-1 opacity-0 transition-opacity group-focus-within/image:opacity-100 group-hover/image:opacity-100 supports-[backdrop-filter]:bg-background/70 supports-[backdrop-filter]:backdrop-blur-sm">
          <button
            type="button"
            title="下载图片"
            aria-label="下载图片"
            className="flex cursor-pointer items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              void downloadImage(src, alt, downloadName);
            }}
          >
            <Download className="size-3.5" />
          </button>
          <button
            type="button"
            title="全屏查看"
            aria-label="全屏查看"
            className="flex cursor-pointer items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              setOpen(true);
            }}
          >
            <Maximize2 className="size-3.5" />
          </button>
        </span>
      ) : null}

      <ImagePreviewDialog alt={alt} src={src} open={open} onOpenChange={setOpen} />
    </span>
  );
}
