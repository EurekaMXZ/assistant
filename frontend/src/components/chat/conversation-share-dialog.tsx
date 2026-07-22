"use client";

import { useRef } from "react";
import { Copy } from "lucide-react";
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
import { useCopyToClipboard } from "@/hooks/use-copy-to-clipboard";

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
  const { copyToClipboard } = useCopyToClipboard({
    successMessage: "分享链接已复制",
    errorMessage: "无法自动复制，请手动复制链接",
  });
  if (!share) return null;

  const shareUrl = buildConversationShareUrl(
    share.id,
    typeof window === "undefined" ? undefined : window.location.origin,
  );

  const copyLink = async () => {
    if (!(await copyToClipboard(shareUrl))) {
      linkInputRef.current?.focus();
      linkInputRef.current?.select();
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
