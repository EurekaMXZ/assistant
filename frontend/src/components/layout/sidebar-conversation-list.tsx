"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import type { Conversation } from "@/lib/types";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { useCopyToClipboard } from "@/hooks/use-copy-to-clipboard";
import {
  Archive,
  ChevronDown,
  ChevronRight,
  Link2,
  MoreHorizontal,
  Pencil,
  Pin,
  Trash2,
} from "lucide-react";

const PINNED_CONVERSATIONS_KEY = "assistant_pinned_conversations";

function readPinnedConversationIds() {
  if (typeof window === "undefined") {
    return new Set<string>();
  }

  try {
    const raw = window.localStorage.getItem(PINNED_CONVERSATIONS_KEY);
    const ids = raw ? JSON.parse(raw) : [];
    return new Set(Array.isArray(ids) ? ids.filter((id) => typeof id === "string") : []);
  } catch {
    return new Set<string>();
  }
}

function writePinnedConversationIds(ids: Set<string>) {
  if (typeof window === "undefined") {
    return;
  }

  window.localStorage.setItem(PINNED_CONVERSATIONS_KEY, JSON.stringify([...ids]));
}

interface SidebarConversationListProps {
  authLoading: boolean;
  conversations: Conversation[];
  currentConversationId?: string | null;
  isLoading: boolean;
  isSignedIn: boolean;
  onSelectConversation: (conversationId: string) => void;
  onOpenArchive: (conversation: Conversation) => void;
  onOpenDelete: (conversation: Conversation) => void;
  onOpenRename: (conversation: Conversation) => void;
}

export function SidebarConversationList({
  authLoading,
  conversations,
  currentConversationId,
  isLoading,
  isSignedIn,
  onSelectConversation,
  onOpenArchive,
  onOpenDelete,
  onOpenRename,
}: SidebarConversationListProps) {
  const [collapsed, setCollapsed] = useState(false);
  const [pinnedConversationIds, setPinnedConversationIds] = useState(readPinnedConversationIds);
  const { copyToClipboard } = useCopyToClipboard({
    successMessage: "私有链接已复制",
    errorMessage: "复制私有链接失败",
  });
  const sortedConversations = useMemo(() => {
    return [...conversations].sort((a, b) => {
      const aPinned = pinnedConversationIds.has(a.id);
      const bPinned = pinnedConversationIds.has(b.id);

      if (aPinned === bPinned) {
        return 0;
      }

      return aPinned ? -1 : 1;
    });
  }, [conversations, pinnedConversationIds]);

  const togglePinned = (conversationId: string) => {
    setPinnedConversationIds((prev) => {
      const next = new Set(prev);

      if (next.has(conversationId)) {
        next.delete(conversationId);
      } else {
        next.add(conversationId);
      }

      writePinnedConversationIds(next);
      return next;
    });
  };

  const copyPrivateLink = async (conversationId: string) => {
    await copyToClipboard(`${window.location.origin}/c/${conversationId}`);
  };

  return (
    <ScrollArea className="min-h-0 flex-1 px-2 py-2">
      <div className="space-y-2">
        <Button
          type="button"
          variant="nav"
          size="sm"
          aria-expanded={!collapsed}
          className="group/recent w-full justify-start bg-transparent! py-2 pl-2.5 pr-2 text-left hover:bg-transparent! aria-expanded:bg-transparent!"
          onClick={() => setCollapsed((prev) => !prev)}
        >
          <span className="text-xs font-bold">最近对话</span>
          {collapsed ? (
            <ChevronRight className="size-4 shrink-0" />
          ) : (
            <ChevronDown className="size-4 shrink-0 opacity-0 transition-opacity group-hover/recent:opacity-100 group-focus-visible/recent:opacity-100" />
          )}
          <span className="min-w-0 flex-1" />
        </Button>

        {!collapsed ? (
          isLoading ? (
            <div className="space-y-2">
              {Array.from({ length: 6 }).map((_, index) => (
                <Skeleton key={index} className="h-9 w-full" />
              ))}
            </div>
          ) : !isSignedIn ? (
            <p className="py-2 pl-2.5 pr-2 text-muted-foreground">
              登录后查看、搜索和管理你的会话。
            </p>
          ) : sortedConversations.length === 0 ? (
            <p className="py-2 pl-2.5 pr-2 text-muted-foreground">暂无会话，点击新建会话。</p>
          ) : (
            <div className="space-y-1">
              {sortedConversations.map((conversation) => {
                const active = currentConversationId === conversation.id;
                const pinned = pinnedConversationIds.has(conversation.id);

                return (
                  <div
                    key={conversation.id}
                    className={cn(
                      "group/conversation flex min-h-9 w-full items-center rounded-lg py-1.5 pl-2.5 pr-2 transition-colors",
                      active
                        ? "bg-sidebar-accent text-sidebar-accent-foreground"
                        : "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                    )}
                  >
                    <Link
                      href={`/c/${conversation.id}`}
                      onClick={(event) => {
                        event.preventDefault();
                        onSelectConversation(conversation.id);
                      }}
                      className="flex min-w-0 flex-1 self-stretch items-center text-inherit"
                      title={conversation.title || "新会话"}
                    >
                      <span className="min-w-0 truncate">{conversation.title || "新会话"}</span>
                    </Link>

                    <Button
                      variant="nav"
                      size="icon-xs"
                      aria-pressed={pinned}
                      title={pinned ? "取消置顶" : "置顶"}
                      className={cn(
                        "hidden shrink-0 text-sidebar-foreground/70 transition-opacity md:inline-flex md:opacity-0 md:group-hover/conversation:opacity-100 md:group-focus-within/conversation:opacity-100",
                        pinned && "text-sidebar-accent-foreground",
                      )}
                      disabled={authLoading}
                      onClick={() => togglePinned(conversation.id)}
                    >
                      <Pin className="size-3.5" />
                      <span className="sr-only">{pinned ? "取消置顶" : "置顶"}</span>
                    </Button>

                    <DropdownMenu>
                      <DropdownMenuTrigger
                        render={
                          <Button
                            variant="nav"
                            size="icon-sm"
                            className="ml-1 shrink-0 text-sidebar-foreground/70 opacity-100 transition-opacity aria-expanded:opacity-100 md:opacity-0 md:group-hover/conversation:opacity-100 md:group-focus-within/conversation:opacity-100"
                            disabled={authLoading}
                          />
                        }
                      >
                        <MoreHorizontal className="size-4" />
                        <span className="sr-only">更多</span>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem
                          className="md:hidden"
                          onClick={() => togglePinned(conversation.id)}
                        >
                          <Pin className="mr-2 size-4" />
                          {pinned ? "取消置顶" : "置顶"}
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={() => void copyPrivateLink(conversation.id)}>
                          <Link2 className="mr-2 size-4" />
                          复制私有链接
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={() => onOpenRename(conversation)}>
                          <Pencil className="mr-2 size-4" />
                          重命名
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={() => onOpenArchive(conversation)}>
                          <Archive className="mr-2 size-4" />
                          归档
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          variant="destructive"
                          onClick={() => onOpenDelete(conversation)}
                        >
                          <Trash2 className="mr-2 size-4" />
                          删除
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                );
              })}
            </div>
          )
        ) : null}
      </div>
    </ScrollArea>
  );
}
