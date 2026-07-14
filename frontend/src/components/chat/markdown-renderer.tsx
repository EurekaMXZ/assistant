"use client";

import type { ComponentProps } from "react";
import { cjk } from "@streamdown/cjk";
import { code } from "@streamdown/code";
import { createMathPlugin } from "@streamdown/math";
import { Streamdown, type Components } from "streamdown";
import { ImagePreview } from "./image-preview";

interface MarkdownRendererProps {
  content: string;
  isStreaming?: boolean;
}

const chatPlugins = { cjk, code, math: createMathPlugin({ singleDollarTextMath: true }) };
const timelinePlugins = { code };

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
    >
      {content}
    </Streamdown>
  );
}
