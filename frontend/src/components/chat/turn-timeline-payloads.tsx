"use client";

import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import type { TimelineItem } from "@/lib/types";
import { TimelineMarkdownRenderer } from "./markdown-renderer";
import {
  getMetadataString,
  reasoningSummary,
  splitLeadingReasoningTitle,
} from "./turn-timeline-utils";

export function TimelineToolPayload({ item }: { item: TimelineItem }) {
  if (getMetadataString(item, "tool_name") === "sandbox.exec") {
    return <SandboxCommandPayload item={item} />;
  }

  const inputText = item.input_text?.trim();
  const inputLabel = item.input_label?.trim();
  const links = item.links?.filter((link) => link.url && link.label) ?? [];

  if (!item.summary && !item.details?.length && !inputText && links.length === 0) return null;

  return (
    <div className="space-y-3 text-muted-foreground">
      {inputText && (
        <p className="break-words text-sm leading-6">
          {inputLabel && <span className="font-medium text-foreground">{inputLabel}: </span>}
          {inputText}
        </p>
      )}
      {links.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {links.map((link) => (
            <Button
              key={link.url}
              render={<a href={link.url} target="_blank" rel="noreferrer" />}
              nativeButton={false}
              variant="secondary"
              size="xs"
              className="max-w-full rounded-full hover:bg-primary hover:text-primary-foreground dark:hover:bg-primary dark:hover:text-primary-foreground"
            >
              <span className="truncate">{link.label}</span>
            </Button>
          ))}
        </div>
      )}
      {item.summary && <p className="leading-6">{item.summary}</p>}
      {item.details && item.details.length > 0 && (
        <ul className="space-y-1 text-xs leading-5 text-muted-foreground">
          {item.details.map((detail, index) => (
            <li key={`${item.id}-${index}`}>{detail}</li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function SandboxCommandPayload({ item }: { item: TimelineItem }) {
  const output = item.command_output;
  const running = item.status === "started" || item.status === "streaming";

  return (
    <Card className="overflow-hidden bg-muted/20 text-xs">
      <div className="flex items-center justify-between gap-3 border-b bg-muted/40 px-3 py-2 text-muted-foreground">
        <span className="font-medium text-foreground">命令</span>
        {item.working_directory ? (
          <span className="min-w-0 truncate font-mono" title={item.working_directory}>
            {item.working_directory}
          </span>
        ) : null}
      </div>
      <div className="overflow-x-auto px-3 py-2.5">
        <pre className="w-max min-w-full whitespace-pre font-mono leading-5 text-foreground">
          <code>{`$ ${item.command || "..."}`}</code>
        </pre>
      </div>

      <div className="border-t">
        <div className="flex items-center justify-between gap-3 bg-muted/30 px-3 py-2 text-muted-foreground">
          <span className="font-medium text-foreground">原始输出</span>
          <span className="flex shrink-0 items-center gap-2 font-mono">
            {item.timed_out ? <span>已超时</span> : null}
            {item.exit_code !== undefined ? <span>exit {item.exit_code}</span> : null}
          </span>
        </div>

        {output !== undefined ? (
          output ? (
            <div className="overflow-x-auto px-3 py-2.5">
              <pre className="w-max min-w-full whitespace-pre font-mono leading-5 text-foreground">
                {output}
              </pre>
            </div>
          ) : (
            <p className="px-3 py-2.5 text-muted-foreground">无输出</p>
          )
        ) : (
          <p className="px-3 py-2.5 text-muted-foreground">
            {running ? "等待命令完成…" : "未提供命令输出"}
          </p>
        )}
      </div>
    </Card>
  );
}

export function TimelineReasoningPayload({
  item,
  isStreaming,
}: {
  item: TimelineItem;
  isStreaming?: boolean;
}) {
  const summary = reasoningSummary(item);
  if (!summary) return <p className="leading-6 text-muted-foreground">暂无可展示的思考摘要。</p>;

  const section = splitLeadingReasoningTitle(summary);
  if (section.title && !section.body) return null;

  return (
    <div className="text-sm text-muted-foreground">
      <TimelineMarkdownRenderer content={section.body || summary} isStreaming={isStreaming} />
    </div>
  );
}
