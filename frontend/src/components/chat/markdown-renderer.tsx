"use client";

import { cjk } from "@streamdown/cjk";
import { code } from "@streamdown/code";
import { createMathPlugin } from "@streamdown/math";
import { Streamdown } from "streamdown";

interface MarkdownRendererProps {
  content: string;
  isStreaming?: boolean;
}

const plugins = { cjk, code, math: createMathPlugin({ singleDollarTextMath: true }) };

export function MarkdownRenderer({ content, isStreaming = false }: MarkdownRendererProps) {
  return (
    <Streamdown
      className="chat-markdown"
      mode={isStreaming ? "streaming" : "static"}
      isAnimating={isStreaming}
      lineNumbers
      plugins={plugins}
    >
      {content}
    </Streamdown>
  );
}
