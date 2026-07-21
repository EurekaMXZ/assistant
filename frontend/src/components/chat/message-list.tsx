"use client";

import { Fragment, useEffect, useRef, useState } from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Button } from "@/components/ui/button";
import { AssistantTurnBubble, MessageBubble } from "./message-bubble";
import type { Message, Turn } from "@/lib/types";
import { shouldFollowAfterScroll } from "@/lib/scroll-follow";
import { cn } from "@/lib/utils";
import { ChevronUp, Loader2 } from "lucide-react";

interface MessageListProps {
  activityLabels?: Record<string, string | null>;
  dimmed?: boolean;
  hasOlderMessages?: boolean;
  loadingOlderMessages?: boolean;
  messages: Message[];
  onEditMessage: (message: Message) => void;
  onLoadOlderMessages?: () => Promise<void>;
  onOpenTimeline: (turnId: string) => void;
  onRetryMessage: (message: Message) => void;
  streamingTurnId?: string | null;
  turnsById?: Record<string, Turn>;
}

type MessageListEntry =
  | { kind: "message"; message: Message }
  | {
      kind: "turn";
      rootTurnId: string;
      variants: {
        assistantMessages: Message[];
        turnId: string;
        userMessage?: Message;
        variantIndex: number;
      }[];
    };

export function groupMessageEntries(messages: Message[], turnsById: Record<string, Turn> = {}) {
  const groups = new Map<
    string,
    Map<string, { assistantMessages: Message[]; userMessage?: Message }>
  >();
  for (const message of messages) {
    if (!message.turn_id || !["user", "assistant"].includes(message.role)) continue;
    const turn = turnsById[message.turn_id];
    const rootTurnId = turn?.retry_of_turn_id || message.turn_id;
    const variants =
      groups.get(rootTurnId) ||
      new Map<string, { assistantMessages: Message[]; userMessage?: Message }>();
    const variant = variants.get(message.turn_id) || { assistantMessages: [] };
    if (message.role === "user") {
      variant.userMessage = message;
    } else {
      variant.assistantMessages.push(message);
    }
    variants.set(message.turn_id, variant);
    groups.set(rootTurnId, variants);
  }

  const entries: MessageListEntry[] = [];
  const renderedRoots = new Set<string>();
  for (const message of messages) {
    if (!message.turn_id || !["user", "assistant"].includes(message.role)) {
      entries.push({ kind: "message", message });
      continue;
    }

    const rootTurnId = turnsById[message.turn_id]?.retry_of_turn_id || message.turn_id;
    if (renderedRoots.has(rootTurnId)) continue;
    renderedRoots.add(rootTurnId);
    const rootUserMessage = groups.get(rootTurnId)?.get(rootTurnId)?.userMessage;
    const variants = Array.from(groups.get(rootTurnId)?.entries() || [])
      .map(([turnId, variant]) => ({
        assistantMessages: variant.assistantMessages,
        turnId,
        userMessage: variant.userMessage || rootUserMessage,
        variantIndex: turnsById[turnId]?.variant_index || 1,
      }))
      .filter((variant) => variant.assistantMessages.length > 0)
      .sort((left, right) => left.variantIndex - right.variantIndex);
    entries.push({ kind: "turn", rootTurnId, variants });
  }
  return entries;
}

export function MessageList({
  activityLabels,
  dimmed,
  hasOlderMessages = false,
  loadingOlderMessages = false,
  messages,
  onEditMessage,
  onLoadOlderMessages,
  onOpenTimeline,
  onRetryMessage,
  streamingTurnId,
  turnsById = {},
}: MessageListProps) {
  const scrollRootRef = useRef<HTMLDivElement>(null);
  const shouldFollowRef = useRef(true);
  const lastScrollTopRef = useRef(0);
  const lastTouchYRef = useRef<number | null>(null);
  const [selectedVariants, setSelectedVariants] = useState<Record<string, string>>({});

  useEffect(() => {
    const viewport = scrollRootRef.current?.querySelector<HTMLElement>(
      '[data-slot="scroll-area-viewport"]',
    );
    if (!viewport) return;
    const updateFollow = () => {
      shouldFollowRef.current = shouldFollowAfterScroll(viewport, lastScrollTopRef.current);
      lastScrollTopRef.current = viewport.scrollTop;
    };
    const stopFollowing = () => {
      shouldFollowRef.current = false;
    };
    const handleWheel = (event: WheelEvent) => {
      if (event.deltaY < 0) stopFollowing();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (["ArrowUp", "Home", "PageUp"].includes(event.key)) stopFollowing();
    };
    const handleTouchStart = (event: TouchEvent) => {
      lastTouchYRef.current = event.touches[0]?.clientY ?? null;
    };
    const handleTouchMove = (event: TouchEvent) => {
      const nextY = event.touches[0]?.clientY;
      if (nextY === undefined) return;
      if (lastTouchYRef.current !== null && nextY > lastTouchYRef.current) stopFollowing();
      lastTouchYRef.current = nextY;
    };
    const handleTouchEnd = () => {
      lastTouchYRef.current = null;
    };

    lastScrollTopRef.current = viewport.scrollTop;
    viewport.addEventListener("scroll", updateFollow, { passive: true });
    viewport.addEventListener("wheel", handleWheel, { passive: true });
    viewport.addEventListener("keydown", handleKeyDown);
    viewport.addEventListener("touchstart", handleTouchStart, { passive: true });
    viewport.addEventListener("touchmove", handleTouchMove, { passive: true });
    viewport.addEventListener("touchend", handleTouchEnd, { passive: true });
    return () => {
      viewport.removeEventListener("scroll", updateFollow);
      viewport.removeEventListener("wheel", handleWheel);
      viewport.removeEventListener("keydown", handleKeyDown);
      viewport.removeEventListener("touchstart", handleTouchStart);
      viewport.removeEventListener("touchmove", handleTouchMove);
      viewport.removeEventListener("touchend", handleTouchEnd);
    };
  }, []);

  useEffect(() => {
    const viewport = scrollRootRef.current?.querySelector<HTMLElement>(
      '[data-slot="scroll-area-viewport"]',
    );
    if (!viewport) return;
    if (messages.length === 0) {
      shouldFollowRef.current = true;
      lastScrollTopRef.current = 0;
      return;
    }
    if (!shouldFollowRef.current) return;

    viewport.scrollTo({
      top: viewport.scrollHeight,
      behavior: "auto",
    });
    lastScrollTopRef.current = viewport.scrollTop;
  }, [messages, streamingTurnId]);

  useEffect(() => {
    if (!streamingTurnId) return;
    const rootTurnId = turnsById[streamingTurnId]?.retry_of_turn_id || streamingTurnId;
    setSelectedVariants((current) => ({ ...current, [rootTurnId]: streamingTurnId }));
  }, [streamingTurnId, turnsById]);

  const entries = groupMessageEntries(messages, turnsById);
  const editableRootTurnId = entries.findLast((entry) => entry.kind === "turn")?.rootTurnId;
  const loadOlderMessages = async () => {
    const viewport = scrollRootRef.current?.querySelector<HTMLElement>(
      '[data-slot="scroll-area-viewport"]',
    );
    const previousHeight = viewport?.scrollHeight || 0;
    const previousTop = viewport?.scrollTop || 0;
    shouldFollowRef.current = false;
    await onLoadOlderMessages?.();
    requestAnimationFrame(() => {
      if (!viewport) return;
      viewport.scrollTop = previousTop + viewport.scrollHeight - previousHeight;
      lastScrollTopRef.current = viewport.scrollTop;
    });
  };

  return (
    <div
      ref={scrollRootRef}
      className={cn(
        "min-h-0 flex-1 transition-[filter,opacity,transform] duration-200 motion-reduce:transition-none",
        dimmed && "pointer-events-none scale-[0.995] opacity-45 blur-[2px]",
      )}
    >
      <ScrollArea className="h-full">
        <div className="mx-auto min-w-0 w-full max-w-2xl px-4 pt-4 pb-52 sm:px-6">
          {hasOlderMessages ? (
            <div className="mb-4 flex justify-center">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={loadingOlderMessages}
                onClick={() => void loadOlderMessages()}
              >
                {loadingOlderMessages ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <ChevronUp className="size-4" />
                )}
                更早消息
              </Button>
            </div>
          ) : null}
          {messages.length === 0 ? (
            <div className="flex h-40 items-center justify-center text-muted-foreground">
              发送第一条消息开始对话
            </div>
          ) : (
            entries.map((entry) => {
              if (entry.kind === "turn") {
                const selectedTurnId = entry.variants.some(
                  (variant) => variant.turnId === selectedVariants[entry.rootTurnId],
                )
                  ? selectedVariants[entry.rootTurnId]
                  : entry.variants.at(-1)?.turnId;
                const selectedIndex = Math.max(
                  0,
                  entry.variants.findIndex((variant) => variant.turnId === selectedTurnId),
                );
                const variant = entry.variants[selectedIndex];
                if (!variant) return null;
                const terminal = ["completed", "failed"].includes(
                  turnsById[variant.turnId]?.status || "",
                );
                const canEdit =
                  entry.rootTurnId === editableRootTurnId && !streamingTurnId && terminal;
                const canRetry =
                  entry.rootTurnId === editableRootTurnId && !streamingTurnId && terminal;
                return (
                  <Fragment key={`turn-${entry.rootTurnId}`}>
                    {variant.userMessage ? (
                      <MessageBubble
                        key={variant.userMessage.id}
                        message={variant.userMessage}
                        onEdit={onEditMessage}
                        canEdit={canEdit}
                      />
                    ) : null}
                    {variant.assistantMessages.length > 0 ? (
                      <AssistantTurnBubble
                        activityLabel={activityLabels?.[variant.turnId]}
                        messages={variant.assistantMessages}
                        onOpenTimeline={onOpenTimeline}
                        onRetry={onRetryMessage}
                        canRetry={canRetry}
                        isStreaming={streamingTurnId === variant.turnId}
                        turn={turnsById[variant.turnId] || null}
                        turnId={variant.turnId}
                        variantCount={entry.variants.length}
                        variantIndex={selectedIndex}
                        onVariantChange={(nextIndex) => {
                          const next = entry.variants[nextIndex];
                          if (!next) return;
                          setSelectedVariants((current) => ({
                            ...current,
                            [entry.rootTurnId]: next.turnId,
                          }));
                        }}
                      />
                    ) : null}
                  </Fragment>
                );
              }
              return (
                <MessageBubble
                  key={entry.message.id}
                  message={entry.message}
                  onEdit={onEditMessage}
                  isStreaming={streamingTurnId === entry.message.turn_id}
                />
              );
            })
          )}
        </div>
      </ScrollArea>
    </div>
  );
}
