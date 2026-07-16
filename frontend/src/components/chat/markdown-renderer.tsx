"use client";

import type { ComponentProps } from "react";
import { cjk } from "@streamdown/cjk";
import { code } from "@streamdown/code";
import { createMathPlugin } from "@streamdown/math";
import {
  Streamdown,
  type Components,
  type LinkSafetyConfig,
  type LinkSafetyModalProps,
} from "streamdown";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ImagePreview } from "./image-preview";

interface MarkdownRendererProps {
  content: string;
  isStreaming?: boolean;
}

const chatPlugins = { cjk, code, math: createMathPlugin({ singleDollarTextMath: true }) };
const timelinePlugins = { code };

function MarkdownLinkSafetyModal({ isOpen, onClose, onConfirm, url }: LinkSafetyModalProps) {
  return (
    <Dialog
      open={isOpen}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>打开外部链接？</DialogTitle>
          <DialogDescription>链接来自助手生成的内容，请确认目标地址可信。</DialogDescription>
        </DialogHeader>
        <div className="break-all rounded-md bg-muted px-3 py-2 font-mono text-sm">{url}</div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button
            onClick={() => {
              onConfirm();
              onClose();
            }}
          >
            打开链接
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const linkSafety = {
  enabled: true,
  renderModal: (props: LinkSafetyModalProps) => <MarkdownLinkSafetyModal {...props} />,
} satisfies LinkSafetyConfig;

function MarkdownImage({
  node,
  src,
  alt = "",
  ...props
}: ComponentProps<"img"> & { node?: unknown }) {
  void node;
  if (typeof src !== "string") return null;

  return <ImagePreview {...props} src={src} alt={alt} wrapperClassName="my-4" streamdown />;
}

const markdownComponents = { img: MarkdownImage } satisfies Components;

export function MarkdownRenderer({ content, isStreaming = false }: MarkdownRendererProps) {
  return (
    <Streamdown
      className="chat-markdown"
      mode={isStreaming ? "streaming" : "static"}
      isAnimating={isStreaming}
      lineNumbers
      plugins={chatPlugins}
      components={markdownComponents}
      linkSafety={linkSafety}
    >
      {content}
    </Streamdown>
  );
}

export function TimelineMarkdownRenderer({ content, isStreaming = false }: MarkdownRendererProps) {
  return (
    <Streamdown
      className="chat-markdown"
      mode={isStreaming ? "streaming" : "static"}
      isAnimating={isStreaming}
      lineNumbers
      plugins={timelinePlugins}
      components={markdownComponents}
      linkSafety={linkSafety}
    >
      {content}
    </Streamdown>
  );
}
