"use client";

import { useEffect, useMemo, useState } from "react";
import type { Timeline, TimelineItem, Turn } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { TimelineMarkdownRenderer } from "./markdown-renderer";
import {
  AlertCircle,
  Bot,
  Box,
  Brain,
  Check,
  ImageIcon,
  Info,
  Sparkles,
  Terminal,
  Wrench,
  X,
} from "lucide-react";
import { Spinner } from "@/components/shared/spinner";
import { TimelineReasoningPayload, TimelineToolPayload } from "./turn-timeline-payloads";
import {
  clipText,
  getMetadataString,
  reasoningSummary,
  splitLeadingReasoningTitle,
} from "./turn-timeline-utils";

export { SandboxCommandPayload } from "./turn-timeline-payloads";

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
  variant?: "dialog" | "panel";
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

function reasoningSectionStarts(summary: string) {
  const starts: number[] = [];
  const titlePattern = /\*\*([^*\r\n]+)\*\*/g;
  for (let match = titlePattern.exec(summary); match; match = titlePattern.exec(summary)) {
    const prefix = summary.slice(0, match.index);
    const startsAtLineStart = /(?:^|\r?\n)[ \t]*$/.test(prefix);
    if (match.index === 0 || startsAtLineStart || prefix.endsWith("**")) {
      starts.push(match.index);
    }
  }
  return starts;
}

function splitReasoningTimelineItem(item: TimelineItem) {
  if (item.type !== "reasoning" && item.type !== "reasoning_summary") return [item];
  const content = item.content_text?.trim();
  if (!content) return [item];

  const titleStarts = reasoningSectionStarts(content);
  if (titleStarts.length < 2) return [item];

  const starts = content.slice(0, titleStarts[0]).trim() ? [0, ...titleStarts] : titleStarts;
  return starts.flatMap((start, index) => {
    const section = content.slice(start, starts[index + 1]).trim();
    if (!section) return [];
    return [
      {
        ...item,
        id: `${item.id}:section:${index}`,
        content_text: section,
        metadata: { ...item.metadata, section_index: index },
      },
    ];
  });
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
    turn?.status === "processing" ||
    turn?.status === "awaiting_input",
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

function TimelineStep({
  item,
  isFinalAssistant,
  isLast,
  isStreaming,
}: {
  item: TimelineItem;
  isFinalAssistant: boolean;
  isLast: boolean;
  isStreaming?: boolean;
}) {
  const toolName = getMetadataString(item, "tool_name");
  const Icon =
    toolName === "sandbox.exec"
      ? Terminal
      : toolName.startsWith("sandbox.")
        ? Box
        : (timelineIcons[item.type as keyof typeof timelineIcons] ?? Sparkles);

  return (
    <div className={cn("relative pl-7", !isLast && "pb-5")}>
      {!isLast && (
        <div className="absolute bottom-0 left-2.5 top-5 w-px -translate-x-1/2 bg-border" />
      )}
      <div className="absolute left-0 top-0 flex size-5 items-center justify-center rounded-full border bg-background text-muted-foreground">
        <Icon className="size-3" />
      </div>

      <div className="space-y-2">
        <div className="flex min-h-5 items-center leading-5">
          <span className="font-medium leading-5">{getTimelineTitle(item, isFinalAssistant)}</span>
        </div>

        {item.type === "reasoning" || item.type === "reasoning_summary" ? (
          <TimelineReasoningPayload item={item} isStreaming={isStreaming} />
        ) : item.type === "tool_call" ? (
          <TimelineToolPayload item={item} />
        ) : item.type === "output_text" || item.type === "final_answer" ? (
          item.content_text ? (
            <div className="text-sm text-foreground">
              <TimelineMarkdownRenderer content={item.content_text} isStreaming={isStreaming} />
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
      <div className="absolute left-0 top-0 flex size-5 items-center justify-center rounded-full border border-foreground/20 bg-foreground text-background">
        <Check className="size-3" strokeWidth={2.5} />
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
      <Sparkles className={cn("size-3.5", active && "animate-pulse")} />
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
  variant = "panel",
}: TurnTimelinePanelProps) {
  const steps = useMemo(
    () => (timeline?.items ?? []).flatMap(splitReasoningTimelineItem),
    [timeline],
  );
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
    <aside
      className={cn(
        "flex h-full min-h-0 w-full min-w-0 flex-col overflow-hidden bg-background",
        variant === "panel" &&
          "animate-in slide-in-from-right-8 fade-in-0 border-l duration-500 ease-in-out",
      )}
    >
      <header
        className={cn(
          "flex h-14 shrink-0 items-center justify-between gap-4 border-b px-5",
          variant === "dialog" && "pr-14",
        )}
      >
        <div className="min-w-0">
          <h3 className="truncate font-medium text-foreground">{panelTitle}</h3>
          {isTurnActive(turn, isStreaming) && durationLabel ? (
            <p className="mt-1 text-xs tabular-nums text-muted-foreground">{durationLabel}</p>
          ) : null}
        </div>
        {variant === "panel" ? (
          <Button variant="ghost" size="icon-sm" className="shrink-0" onClick={onClose}>
            <X className="size-4" />
            <span className="sr-only">关闭时间轴</span>
          </Button>
        ) : null}
      </header>

      <ScrollArea className="min-h-0 flex-1">
        <div className="px-5 py-5">
          {loading && !timeline ? (
            <div className="flex items-center gap-2 text-muted-foreground">
              <Spinner />
              加载中…
            </div>
          ) : error ? (
            <div className="flex items-center gap-2 text-destructive">
              <AlertCircle className="size-4" />
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
                  isStreaming={isStreaming && item.status === "streaming"}
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
