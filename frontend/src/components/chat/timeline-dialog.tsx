"use client";

import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import type { ChatController } from "@/hooks/use-chat-controller";
import { TurnTimelinePanel } from "./turn-timeline";

export function TimelineDialog({
  mobile,
  panelProps,
  onClose,
}: {
  mobile: boolean;
  panelProps: ChatController["timelinePanelProps"];
  onClose: () => void;
}) {
  if (!mobile) return null;

  return (
    <Dialog open={panelProps !== null} onOpenChange={(open) => !open && onClose()}>
      {panelProps ? (
        <DialogContent className="h-[min(720px,calc(100dvh-2rem))] grid-rows-[minmax(0,1fr)] gap-0 overflow-hidden p-0 sm:max-w-[700px]">
          <DialogHeader className="sr-only">
            <DialogTitle>时间线详情</DialogTitle>
          </DialogHeader>
          <TurnTimelinePanel {...panelProps} variant="dialog" />
        </DialogContent>
      ) : null}
    </Dialog>
  );
}
