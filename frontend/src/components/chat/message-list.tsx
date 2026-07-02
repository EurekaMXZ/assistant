"use client";

import { useEffect, useRef } from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { AssistantTurnBubble, MessageBubble } from "./message-bubble";
import type { Message, Turn } from "@/lib/types";
import { isViewportNearBottom } from "@/lib/scroll-follow";

interface MessageListProps {
  activityLabels?: Record<string, string | null>;
  messages: Message[];
  onEditMessage: (message: Message) => void;
  onOpenTimeline: (turnId: string) => void;
  onRetryMessage: (message: Message) => void;
  streamingTurnId?: string | null;
  turnsById?: Record<string, Turn>;
}

type MessageListEntry =
  | { kind: "message"; message: Message }
  | { kind: "assistant-turn"; messages: Message[]; turnId: string };

function groupMessageEntries(messages: Message[]) {
  const entries: MessageListEntry[] = [];
  for (let index = 0; index < messages.length; ) {
    const message = messages[index];
    if (message.role !== "assistant" || !message.turn_id) {
      entries.push({ kind: "message", message });
      index += 1;
      continue;
    }

    const turnId = message.turn_id;
    const turnMessages: Message[] = [];
    while (
      index < messages.length &&
      messages[index].role === "assistant" &&
      messages[index].turn_id === turnId
    ) {
      turnMessages.push(messages[index]);
      index += 1;
    }
    entries.push({ kind: "assistant-turn", messages: turnMessages, turnId });
  }
  return entries;
}

export function MessageList({
  activityLabels,
  messages,
  onEditMessage,
  onOpenTimeline,
  onRetryMessage,
  streamingTurnId,
  turnsById = {},
}: MessageListProps) {
  const scrollRootRef = useRef<HTMLDivElement>(null);
  const shouldFollowRef = useRef(true);

  useEffect(() => {
    const viewport = scrollRootRef.current?.querySelector<HTMLElement>(
      '[data-slot="scroll-area-viewport"]',
    );
    if (!viewport) return;
    const updateFollow = () => {
      shouldFollowRef.current = isViewportNearBottom(viewport);
    };
    viewport.addEventListener("scroll", updateFollow, { passive: true });
    return () => viewport.removeEventListener("scroll", updateFollow);
  }, []);

  useEffect(() => {
    const viewport = scrollRootRef.current?.querySelector<HTMLElement>(
      '[data-slot="scroll-area-viewport"]'
    );
    if (!viewport || !shouldFollowRef.current) return;

    viewport.scrollTo({
      top: viewport.scrollHeight,
      behavior: "smooth",
    });
  }, [messages, streamingTurnId]);

  const entries = groupMessageEntries(messages);

  return (
    <div ref={scrollRootRef} className="min-h-0 flex-1">
      <ScrollArea className="h-full">
        <div className="mx-auto w-full max-w-2xl px-4 pt-4 pb-52 sm:px-6">
          {messages.length === 0 ? (
            <div className="flex h-40 items-center justify-center text-muted-foreground">
              发送第一条消息开始对话
            </div>
        ) : (
          entries.map((entry) =>
            entry.kind === "assistant-turn" ? (
              <AssistantTurnBubble
                key={`assistant-turn-${entry.turnId}-${entry.messages[0].id}`}
                activityLabel={activityLabels?.[entry.turnId]}
                messages={entry.messages}
                onOpenTimeline={onOpenTimeline}
                onRetry={onRetryMessage}
                isStreaming={streamingTurnId === entry.turnId}
                turn={turnsById[entry.turnId] || null}
                turnId={entry.turnId}
              />
            ) : (
              <MessageBubble
                key={entry.message.id}
                message={entry.message}
                onEdit={onEditMessage}
                onRetry={onRetryMessage}
                isStreaming={streamingTurnId === entry.message.turn_id}
              />
            ),
          )
        )}
        </div>
      </ScrollArea>
    </div>
  );
}
