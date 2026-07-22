"use client";

import type { Conversation } from "@/lib/types";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";

interface SidebarSearchDialogProps {
  open: boolean;
  query: string;
  results: Conversation[];
  onOpenChange: (open: boolean) => void;
  onQueryChange: (query: string) => void;
  onSelectConversation: (conversationId: string) => void;
}

export function SidebarSearchDialog({
  open,
  query,
  results,
  onOpenChange,
  onQueryChange,
  onSelectConversation,
}: SidebarSearchDialogProps) {
  const hasQuery = query.trim().length > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>搜索会话</DialogTitle>
          <DialogDescription>按标题关键词查找会话。</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <Input
            autoFocus
            placeholder="输入标题关键词"
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
          />

          <ScrollArea className="max-h-[min(60vh,28rem)]">
            {!hasQuery ? (
              <div className="py-10 text-center text-muted-foreground">
                输入标题关键词以搜索会话。
              </div>
            ) : results.length === 0 ? (
              <div className="py-10 text-center text-muted-foreground">没有找到匹配的会话。</div>
            ) : (
              <div className="space-y-1">
                {results.map((conversation) => {
                  const title = conversation.title || "新会话";

                  return (
                    <Button
                      key={conversation.id}
                      type="button"
                      variant="ghost"
                      className="h-auto w-full justify-between rounded-lg px-3 py-2 text-left"
                      onClick={() => onSelectConversation(conversation.id)}
                    >
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-foreground">{title}</p>
                      </div>
                      {conversation.archived_at ? (
                        <span className="ml-3 shrink-0 text-xs text-muted-foreground">已归档</span>
                      ) : null}
                    </Button>
                  );
                })}
              </div>
            )}
          </ScrollArea>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
