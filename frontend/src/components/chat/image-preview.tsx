"use client";

import { useEffect, useRef, useState, type ComponentProps } from "react";
import { Download, Maximize2, RotateCcw, X, ZoomIn, ZoomOut } from "lucide-react";
import {
  TransformComponent,
  TransformWrapper,
  useControls,
  useTransformComponent,
} from "react-zoom-pan-pinch";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
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

function ImagePanZoomControls() {
  const { resetTransform, zoomIn, zoomOut } = useControls();
  const scale = useTransformComponent(({ state }) => state.scale);

  return (
    <div className="absolute bottom-4 left-4 z-10 flex flex-col gap-1 rounded-md border border-border bg-background/80 p-1 supports-[backdrop-filter]:bg-background/70 supports-[backdrop-filter]:backdrop-blur-sm">
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        title="放大"
        aria-label="放大"
        className="text-muted-foreground hover:text-foreground disabled:cursor-not-allowed"
        disabled={scale >= maxScale}
        onClick={() => zoomIn(scaleStep, 0)}
      >
        <ZoomIn className="size-4" />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        title="缩小"
        aria-label="缩小"
        className="text-muted-foreground hover:text-foreground disabled:cursor-not-allowed"
        disabled={scale <= minScale}
        onClick={() => zoomOut(scaleStep, 0)}
      >
        <ZoomOut className="size-4" />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        title="重置缩放与位置"
        aria-label="重置缩放与位置"
        className="text-muted-foreground hover:text-foreground"
        onClick={() => resetTransform(0)}
      >
        <RotateCcw className="size-4" />
      </Button>
    </div>
  );
}

function ImagePanZoom({ alt, src }: { alt: string; src: string }) {
  return (
    <div className="relative size-full overflow-hidden" data-image-preview="viewport">
      <TransformWrapper
        centerOnInit
        centerZoomedOut
        disablePadding
        initialScale={1}
        limitToBounds
        maxScale={maxScale}
        minScale={minScale}
        doubleClick={{ disabled: true }}
        pinch={{ allowPanning: true }}
        wheel={{ step: 0.015 }}
      >
        <ImagePanZoomControls />
        <TransformComponent
          contentClass="image-preview-transform !size-full items-center justify-center"
          contentStyle={{ height: "100%", width: "100%" }}
          wrapperClass="!size-full touch-none cursor-grab active:cursor-grabbing"
          wrapperStyle={{ height: "100%", width: "100%" }}
          wrapperProps={{ role: "application", "aria-label": "可缩放图片预览" }}
        >
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img
            src={src}
            alt={alt}
            className="max-h-[calc(100dvh-2rem)] max-w-[calc(100vw-2rem)] object-contain"
            draggable={false}
          />
        </TransformComponent>
      </TransformWrapper>
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
        <Button
          type="button"
          variant="ghost"
          size="icon"
          title="退出全屏"
          aria-label="退出全屏"
          className="absolute right-4 top-4 z-20 text-muted-foreground hover:text-foreground"
          onClick={() => onOpenChange(false)}
        >
          <X className="size-5" />
        </Button>
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
      <Button
        type="button"
        variant="ghost"
        aria-label={`预览 ${alt || "图片"}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        className={cn(
          "relative h-auto min-h-0 max-w-full cursor-zoom-in overflow-hidden rounded-lg bg-transparent! p-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
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
      </Button>

      {showActions && loaded ? (
        <span className="absolute right-2 top-2 z-10 flex gap-1 rounded-md border border-border bg-background/80 p-1 opacity-100 transition-opacity md:opacity-0 md:group-focus-within/image:opacity-100 md:group-hover/image:opacity-100 supports-[backdrop-filter]:bg-background/70 supports-[backdrop-filter]:backdrop-blur-sm">
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            title="下载图片"
            aria-label="下载图片"
            className="text-muted-foreground hover:text-foreground"
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              void downloadImage(src, alt, downloadName);
            }}
          >
            <Download className="size-3.5" />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            title="全屏查看"
            aria-label="全屏查看"
            className="text-muted-foreground hover:text-foreground"
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
              setOpen(true);
            }}
          >
            <Maximize2 className="size-3.5" />
          </Button>
        </span>
      ) : null}

      <ImagePreviewDialog alt={alt} src={src} open={open} onOpenChange={setOpen} />
    </span>
  );
}
