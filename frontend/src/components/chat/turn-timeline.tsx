"use client";

import { useEffect, useMemo, useState } from "react";
import type { Timeline, TimelineItem, Turn } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { MarkdownRenderer } from "./markdown-renderer";
import {
  AlertCircle,
  Bot,
  Brain,
  Check,
  ImageIcon,
  Info,
  Loader2,
  Sparkles,
  Wrench,
  X,
} from "lucide-react";

interface TurnTimelineProps {
  activityLabel?: string | null;
  isStreaming?: boolean;
  onOpen: (turnId: string) => void;
  turnId: string;
  turn?: Turn | null;
}

interface TurnTimelinePanelProps {
  error?: string | null;
  isStreaming?: boolean;
  loading?: boolean;
  onClose: () => void;
  timeline?: Timeline | null;
  turn?: Turn | null;
}

const timelineIcons = {
  commentary: Info,
  status: Info,
  reasoning: Brain,
  reasoning_summary: Brain,
  tool_call: Wrench,
  image_generation: ImageIcon,
  final_answer: Bot,
  output_text: Bot,
} as const;

const genericTitles = new Set(["Assistant", "Commentary", "Final answer", "Status"]);

function reasoningSummary(item: TimelineItem) {
  return item.content_text?.trim() || "";
}

function splitLeadingReasoningTitle(summary: string) {
  const match = summary.match(/^\s*\*\*([^*\n]+)\*\*[ \t]*(?:\r?\n+|$)/);
  if (!match) {
    return { body: summary, title: "" };
  }
  return {
    body: summary.slice(match[0].length).trimStart(),
    title: match[1].trim(),
  };
}

function clipText(value: string, limit = 72) {
  if (value.length <= limit) return value;
  return `${value.slice(0, limit).trimEnd()}...`;
}

function getMetadataString(item: TimelineItem, key: string) {
  const value = item.metadata?.[key];
  return typeof value === "string" ? value.trim() : "";
}

export function getTimelineTitle(item: TimelineItem, isFinalAssistant = false) {
  const toolName = item.title?.trim() || getMetadataString(item, "tool_name");

  if (item.type === "tool_call" && toolName) {
    return toolName;
  }

  if (item.type === "reasoning" || item.type === "reasoning_summary") {
    const summary = reasoningSummary(item);
    if (summary) {
      const section = splitLeadingReasoningTitle(summary);
      return clipText(section.title || summary.split(/\n+/)[0] || summary);
    }
    return "思考";
  }

  if (item.title?.trim() && !genericTitles.has(item.title.trim())) {
    return item.title;
  }

  switch (item.type) {
    case "tool_call":
      return "工具调用";
    case "image_generation":
      return "图片生成";
    case "output_text":
    case "final_answer":
      return isFinalAssistant ? "最终回答" : "进度更新";
    case "status":
    case "commentary":
      return item.status === "failed" ? "响应失败" : "状态更新";
    default:
      return "步骤";
  }
}

function thoughtDurationSeconds(turn: Turn | null, now: number) {
  if (!turn?.started_at) {
    return null;
  }

  const end = turn.completed_at || turn.failed_at;
  const elapsedMs = (end ? new Date(end).getTime() : now) - new Date(turn.started_at).getTime();
  if (!Number.isFinite(elapsedMs) || elapsedMs <= 0) {
    return 1;
  }

  return Math.max(1, Math.round(elapsedMs / 1000));
}

function thoughtDurationLabel(turn: Turn | null, now: number) {
  const seconds = thoughtDurationSeconds(turn, now);
  if (seconds == null) return null;
  return `Thought for ${seconds} ${seconds === 1 ? "second" : "seconds"}`;
}

function useThoughtTimer(turn: Turn | null, isStreaming?: boolean) {
  const active = isTurnActive(turn, isStreaming);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    setNow(Date.now());
    if (!active) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active, turn?.started_at]);

  return thoughtDurationLabel(turn, now);
}

function isTurnActive(turn: Turn | null, isStreaming?: boolean) {
  return Boolean(
    isStreaming ||
    turn?.status === "accepted" ||
    turn?.status === "context_ready" ||
    turn?.status === "processing",
  );
}

function thoughtButtonLabel(
  turn: Turn | null,
  isStreaming?: boolean,
  activityLabel?: string | null,
  durationLabel?: string | null,
) {
  if (isTurnActive(turn, isStreaming)) {
    return activityLabel?.trim() || "Thinking...";
  }
  if (!turn) {
    return activityLabel?.trim() || "Thinking...";
  }

  return durationLabel || "Thought for 1 second";
}

function TimelineToolPayload({ item }: { item: TimelineItem }) {
  const inputText = item.input_text?.trim();
  const inputLabel = item.input_label?.trim();
  const links = item.links?.filter((link) => link.url && link.label) ?? [];

  if (!item.summary && !item.details?.length && !inputText && links.length === 0) {
    return null;
  }

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

function TimelineReasoningPayload({ item }: { item: TimelineItem }) {
  const summary = reasoningSummary(item);
  if (!summary) {
    return <p className="leading-6 text-muted-foreground">暂无可展示的思考摘要。</p>;
  }

  const section = splitLeadingReasoningTitle(summary);
  if (section.title && !section.body) {
    return null;
  }
  const body = section.body || summary;
  return (
    <div className="text-sm text-muted-foreground">
      <MarkdownRenderer content={body} />
    </div>
  );
}

function TimelineStep({
  item,
  isFinalAssistant,
  isLast,
}: {
  item: TimelineItem;
  isFinalAssistant: boolean;
  isLast: boolean;
}) {
  const Icon = timelineIcons[item.type as keyof typeof timelineIcons] ?? Sparkles;

  return (
    <div className={cn("relative pl-7", !isLast && "pb-5")}>
      {!isLast && (
        <div className="absolute bottom-0 left-2.5 top-5 w-px -translate-x-1/2 bg-border" />
      )}
      <div className="absolute left-0 top-0 flex h-5 w-5 items-center justify-center rounded-full border bg-background text-muted-foreground">
        <Icon className="h-3 w-3" />
      </div>

      <div className="space-y-2">
        <div className="flex min-h-5 items-center leading-5">
          <span className="font-medium leading-5">{getTimelineTitle(item, isFinalAssistant)}</span>
        </div>

        {item.type === "reasoning" || item.type === "reasoning_summary" ? (
          <TimelineReasoningPayload item={item} />
        ) : item.type === "tool_call" ? (
          <TimelineToolPayload item={item} />
        ) : item.type === "output_text" || item.type === "final_answer" ? (
          item.content_text ? (
            <div className="text-sm text-foreground">
              <MarkdownRenderer content={item.content_text} />
            </div>
          ) : null
        ) : item.content_text ? (
          <p className="leading-6 text-muted-foreground">{item.content_text}</p>
        ) : item.summary ? (
          <p className="leading-6 text-muted-foreground">{item.summary}</p>
        ) : item.details && item.details.length > 0 ? (
          <ul className="space-y-1 text-xs leading-5 text-muted-foreground">
            {item.details.map((detail, index) => (
              <li key={`${item.id}-${index}`}>{detail}</li>
            ))}
          </ul>
        ) : null}
      </div>
    </div>
  );
}

function TimelineCompletionStep() {
  return (
    <div className="relative pl-7">
      <div className="absolute left-0 top-0 flex h-5 w-5 items-center justify-center rounded-full border border-foreground/20 bg-foreground text-background">
        <Check className="h-3 w-3" strokeWidth={2.5} />
      </div>
      <div className="flex min-h-5 items-center font-medium leading-5">Done</div>
    </div>
  );
}

export function TurnTimeline({
  activityLabel,
  turnId,
  turn = null,
  isStreaming,
  onOpen,
}: TurnTimelineProps) {
  const active = isTurnActive(turn, isStreaming);
  const durationLabel = useThoughtTimer(turn, isStreaming);
  const buttonLabel = thoughtButtonLabel(turn, isStreaming, activityLabel, durationLabel);

  return (
    <Button
      variant="ghost"
      size="sm"
      className="h-auto justify-start gap-1.5 bg-transparent! px-0 py-0 font-normal text-muted-foreground hover:bg-transparent! hover:text-foreground dark:hover:bg-transparent!"
      onClick={(event) => {
        event.preventDefault();
        event.stopPropagation();
        onOpen(turnId);
      }}
    >
      <Sparkles className={cn("h-3.5 w-3.5", active && "animate-pulse")} />
      <span className={cn("px-1 py-0.5", active && "animate-pulse")}>{buttonLabel}</span>
    </Button>
  );
}

export function TurnTimelinePanel({
  error,
  isStreaming,
  loading,
  onClose,
  timeline,
  turn = null,
}: TurnTimelinePanelProps) {
  const steps = useMemo(() => timeline?.items ?? [], [timeline]);
  const isCompleted = turn?.status === "completed";
  const panelActivityLabel = steps.length ? getTimelineTitle(steps[steps.length - 1]) : null;
  const durationLabel = useThoughtTimer(turn, isStreaming);
  const panelTitle = thoughtButtonLabel(turn, isStreaming, panelActivityLabel, durationLabel);
  const finalAssistantIndex = useMemo(
    () =>
      steps.findLastIndex((item) => item.type === "output_text" || item.type === "final_answer"),
    [steps],
  );

  return (
    <aside className="animate-in slide-in-from-right-8 fade-in-0 flex h-full w-full min-w-0 flex-col border-l bg-background duration-500 ease-in-out">
      <header className="flex h-14 shrink-0 items-center justify-between gap-4 border-b px-5">
        <div className="min-w-0">
          <h3 className="truncate font-medium text-foreground">{panelTitle}</h3>
          {isTurnActive(turn, isStreaming) && durationLabel ? (
            <p className="mt-1 text-xs tabular-nums text-muted-foreground">{durationLabel}</p>
          ) : null}
        </div>
        <Button variant="ghost" size="icon-sm" className="shrink-0" onClick={onClose}>
          <X className="h-4 w-4" />
          <span className="sr-only">关闭时间轴</span>
        </Button>
      </header>

      <ScrollArea className="min-h-0 flex-1">
        <div className="px-5 py-5">
          {loading && !timeline ? (
            <div className="flex items-center gap-2 text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              加载中…
            </div>
          ) : error ? (
            <div className="flex items-center gap-2 text-destructive">
              <AlertCircle className="h-4 w-4" />
              {error}
            </div>
          ) : steps.length > 0 || isCompleted ? (
            <div className="space-y-0">
              {steps.map((item, index) => (
                <TimelineStep
                  key={item.id}
                  item={item}
                  isFinalAssistant={index === finalAssistantIndex}
                  isLast={!isCompleted && index === steps.length - 1}
                />
              ))}
              {isCompleted ? <TimelineCompletionStep /> : null}
            </div>
          ) : (
            <p className="text-muted-foreground">暂无可展示的步骤。</p>
          )}
        </div>
      </ScrollArea>
    </aside>
  );
}
