"use client";

import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeHighlight from "rehype-highlight";
import rehypeKatex from "rehype-katex";
import { CopyButton } from "./copy-button";

interface MarkdownRendererProps {
  content: string;
}

function preprocessMath(input: string): string {
  // 把后端常用的 LaTeX 定界符 \[...\] / \(...\) 转成 remark-math 认识的 $$...$$ / $...$
  return input
    .replace(/\\\[/g, "$$$")
    .replace(/\\\]/g, "$$$")
    .replace(/\\\(/g, "$")
    .replace(/\\\)/g, "$");
}

export function MarkdownRenderer({ content }: MarkdownRendererProps) {
  const processed = preprocessMath(content);
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm, remarkMath]}
      rehypePlugins={[rehypeHighlight, rehypeKatex]}
      components={{
        pre({ children }) {
          return (
            <div className="relative my-3 overflow-x-auto rounded-lg border bg-card p-4">
              {children}
            </div>
          );
        },
        code({ children, className }) {
          const isInline = !className;
          const text = String(children).replace(/\n$/, "");
          if (isInline) {
            return (
              <code className="rounded bg-muted px-1 py-0.5 font-mono">
                {children}
              </code>
            );
          }
          return (
            <div className="relative">
              <div className="absolute right-2 top-2">
                <CopyButton text={text} />
              </div>
              <code
                className={`${className} block whitespace-pre font-mono`}
              >
                {children}
              </code>
            </div>
          );
        },
        p({ children }) {
          return <p className="mb-3 leading-7 last:mb-0">{children}</p>;
        },
        ul({ children }) {
          return <ul className="mb-3 list-disc space-y-1 pl-5">{children}</ul>;
        },
        ol({ children }) {
          return (
            <ol className="mb-3 list-decimal space-y-1 pl-5">{children}</ol>
          );
        },
        li({ children }) {
          return <li className="leading-7">{children}</li>;
        },
        h1({ children }) {
          return <h1 className="mb-2 mt-4 text-2xl font-semibold">{children}</h1>;
        },
        h2({ children }) {
          return <h2 className="mb-2 mt-4 text-xl font-semibold">{children}</h2>;
        },
        h3({ children }) {
          return <h3 className="mb-2 mt-3 text-lg font-semibold">{children}</h3>;
        },
        blockquote({ children }) {
          return (
            <blockquote className="mb-3 border-l-2 border-muted-foreground/30 pl-4 italic text-muted-foreground">
              {children}
            </blockquote>
          );
        },
        a({ href, children }) {
          return (
            <a
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              className="text-primary underline"
            >
              {children}
            </a>
          );
        },
        table({ children }) {
          return (
            <div className="my-3 overflow-auto">
              <table className="w-full border-collapse">
                {children}
              </table>
            </div>
          );
        },
        th({ children }) {
          return (
            <th className="border border-border bg-muted px-3 py-2 text-left font-semibold">
              {children}
            </th>
          );
        },
        td({ children }) {
          return (
            <td className="border border-border px-3 py-2">{children}</td>
          );
        },
      }}
    >
      {processed}
    </ReactMarkdown>
  );
}
