"use client";

import { useRef } from "react";
import { Copy } from "lucide-react";
import { toast } from "sonner";
import type { ConversationShare } from "@/lib/types";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";

export function buildConversationShareUrl(shareId: string, origin?: string) {
  const path = `/share/${encodeURIComponent(shareId)}`;
  return origin ? new URL(path, origin).toString() : path;
}

export function ConversationShareDialog({
  open,
  share,
  onOpenChange,
}: {
  open: boolean;
  share: ConversationShare | null;
  onOpenChange: (open: boolean) => void;
}) {
  const linkInputRef = useRef<HTMLInputElement>(null);
  if (!share) return null;

  const shareUrl = buildConversationShareUrl(
    share.id,
    typeof window === "undefined" ? undefined : window.location.origin,
  );

  const copyLink = async () => {
    let copied = false;
    try {
      if (!navigator.clipboard?.writeText) throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText(shareUrl);
      copied = true;
    } catch {
      const input = linkInputRef.current;
      try {
        input?.focus();
        input?.select();
        copied = document.execCommand("copy");
      } catch {
        copied = false;
      }
    }
    if (copied) {
      toast.success("分享链接已复制");
    } else {
      linkInputRef.current?.focus();
      linkInputRef.current?.select();
      toast.error("无法自动复制，请手动复制链接");
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>分享对话</DialogTitle>
          <DialogDescription>
            链接仅包含创建时已有的消息，之后在此对话中发送的内容不会显示。
          </DialogDescription>
        </DialogHeader>
        <div className="flex min-w-0 items-center gap-2">
          <Input
            ref={linkInputRef}
            aria-label="分享链接"
            className="min-w-0 font-mono text-xs"
            readOnly
            value={shareUrl}
            onFocus={(event) => event.currentTarget.select()}
          />
          <Button
            type="button"
            variant="outline"
            size="icon"
            className="shrink-0"
            aria-label="复制分享链接"
            title="复制分享链接"
            onClick={() => void copyLink()}
          >
            <Copy className="size-4" />
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
