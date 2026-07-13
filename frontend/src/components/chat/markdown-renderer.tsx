"use client";

import { cjk } from "@streamdown/cjk";
import { code } from "@streamdown/code";
import { createMathPlugin } from "@streamdown/math";
import { Streamdown } from "streamdown";

interface MarkdownRendererProps {
  content: string;
  isStreaming?: boolean;
}

const chatPlugins = { cjk, code, math: createMathPlugin({ singleDollarTextMath: true }) };
const timelinePlugins = { code };

export function MarkdownRenderer({ content, isStreaming = false }: MarkdownRendererProps) {
  return (
    <Streamdown
      className="chat-markdown"
      mode={isStreaming ? "streaming" : "static"}
      isAnimating={isStreaming}
      lineNumbers
      plugins={chatPlugins}
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
    >
      {content}
    </Streamdown>
  );
}
