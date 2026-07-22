"use client";

import { useEffect, useMemo, useState } from "react";
import { CircleAlert, Share2 } from "lucide-react";
import { EmptyState } from "@/components/shared/empty-state";
import { ErrorState } from "@/components/shared/error-state";
import { ApiError, getConversationShare } from "@/lib/api";
import type { ConversationShareSnapshot } from "@/lib/types";
import { useMobileHeader } from "@/components/layout/mobile-header-context";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ChatSkeleton } from "./chat-skeleton";
import { MessageBubble } from "./message-bubble";

export function SharedConversation({ shareId }: { shareId: string }) {
  const { setStatus: setMobileStatus, setTitle: setMobileTitle } = useMobileHeader();
  const [snapshot, setSnapshot] = useState<ConversationShareSnapshot | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    void getConversationShare(shareId, controller.signal)
      .then(setSnapshot)
      .catch((loadError) => {
        if ((loadError as Error).name === "AbortError") return;
        setError(
          loadError instanceof ApiError && loadError.status === 404
            ? "分享链接不存在或已失效"
            : "无法加载分享的对话",
        );
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [shareId]);

  useEffect(() => {
    setMobileTitle(snapshot?.title || "分享对话");
    return () => setMobileTitle("Assistant");
  }, [setMobileTitle, snapshot?.title]);

  useEffect(() => {
    setMobileStatus({
      icon: <Share2 className="size-3.5" />,
      label: "只读分享",
    });
    return () => setMobileStatus(null);
  }, [setMobileStatus]);

  const messages = useMemo(
    () =>
      (snapshot?.messages || []).filter(
        (message) =>
          ["user", "assistant"].includes(message.role) &&
          message.metadata?.display_kind !== "thinking",
      ),
    [snapshot?.messages],
  );

  if (loading) return <ChatSkeleton />;

  if (!snapshot || error) {
    return (
      <ErrorState
        icon={CircleAlert}
        message="无法打开分享链接"
        description={error || "分享内容不可用"}
        className="min-h-0 flex-1 border-0"
      />
    );
  }

  return (
    <section className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <header className="hidden h-14 shrink-0 items-center justify-between border-b px-5 md:flex">
        <h1 className="truncate text-base font-semibold">{snapshot.title || "分享对话"}</h1>
        <span className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground">
          <Share2 className="size-3.5" />
          只读分享
        </span>
      </header>

      <ScrollArea className="min-h-0 flex-1">
        <main className="mx-auto w-full max-w-2xl px-4 pb-20 pt-5 sm:px-6">
          {messages.length > 0 ? (
            <div className="space-y-6">
              {messages.map((message) => (
                <MessageBubble
                  key={message.id}
                  allowAttachmentPreviews={false}
                  message={message}
                  showActions={false}
                />
              ))}
            </div>
          ) : (
            <EmptyState
              title="该分享中没有消息"
              className="min-h-40 border-0"
              titleClassName="m-0 font-normal text-muted-foreground"
            />
          )}
        </main>
      </ScrollArea>
    </section>
  );
}
