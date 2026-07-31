"use client";

import { useMemo, type UIEvent } from "react";
import { ListTree } from "lucide-react";
import { Spinner } from "@/components/shared/spinner";
import { Button } from "@/components/ui/button";
import type { ConversationTurnSummary } from "@/lib/types";
import { cn } from "@/lib/utils";

interface TurnNavigatorProps {
  activeTurnId?: string | null;
  hasMoreOlder?: boolean;
  loading?: boolean;
  loadingMore?: boolean;
  onLoadMoreOlder?: () => Promise<void>;
  onSelect: (turn: ConversationTurnSummary) => void;
  turns: ConversationTurnSummary[];
}

function railProgress(turns: ConversationTurnSummary[], activeTurnId?: string | null) {
  const ordered = logicalTurnSummaries(turns);
  const count = Math.min(5, ordered.length);
  if (count === 0) return { activeIndex: 0, count: 0 };
  const turnIndex = Math.max(
    0,
    ordered.findIndex((turn) => logicalTurnKey(turn) === activeTurnId),
  );
  const activeIndex =
    ordered.length === 1
      ? 0
      : Math.min(count - 1, Math.floor((turnIndex / (ordered.length - 1)) * count));
  return { activeIndex, count };
}

function logicalTurnKey(turn: ConversationTurnSummary) {
  return turn.retry_of_turn_id || turn.id;
}

function logicalTurnSummaries(turns: ConversationTurnSummary[]) {
  const groups = new Map<string, ConversationTurnSummary>();
  for (const turn of turns) {
    const key = logicalTurnKey(turn);
    const current = groups.get(key);
    if (!current || turn.variant_index >= current.variant_index) groups.set(key, turn);
  }
  return Array.from(groups.values()).sort((left, right) => left.seq - right.seq);
}

function turnLabel(turn: ConversationTurnSummary) {
  return turn.user_message?.content_text?.trim() || "未命名 turn";
}

export function TurnNavigator({
  activeTurnId,
  hasMoreOlder = false,
  loading = false,
  loadingMore = false,
  onLoadMoreOlder,
  onSelect,
  turns,
}: TurnNavigatorProps) {
  const logicalTurns = useMemo(() => logicalTurnSummaries(turns), [turns]);
  const activeLogicalTurnId = useMemo(() => {
    const active = turns.find((turn) => turn.id === activeTurnId);
    return active ? logicalTurnKey(active) : activeTurnId;
  }, [activeTurnId, turns]);
  const activeLogicalIndex = logicalTurns.findIndex(
    (turn) => logicalTurnKey(turn) === activeLogicalTurnId,
  );
  const compactRail = useMemo(
    () => railProgress(logicalTurns, activeLogicalTurnId),
    [activeLogicalTurnId, logicalTurns],
  );
  const menuTurns = logicalTurns;

  const handleMenuScroll = (event: UIEvent<HTMLDivElement>) => {
    if (!hasMoreOlder || loadingMore || !onLoadMoreOlder) return;
    const viewport = event.currentTarget;
    if (viewport.scrollTop < 56) {
      void onLoadMoreOlder();
    }
  };

  return (
    <div className="group/turn-navigator pointer-events-none absolute right-4 top-1/2 z-30 hidden h-[88px] -translate-y-1/2 md:block">
      <div className="relative h-full w-8 transition-[width] duration-200 ease-out group-hover/turn-navigator:w-52 group-focus-within/turn-navigator:w-52 motion-reduce:transition-none">
        <div className="pointer-events-auto absolute inset-y-0 right-0 flex w-8 flex-col items-center justify-center gap-1">
          {loading && turns.length === 0 ? (
            <Spinner className="size-3.5 text-muted-foreground" />
          ) : compactRail.count > 0 ? (
            <div
              data-slot="turn-rail"
              aria-label={`当前位于第 ${Math.max(1, activeLogicalIndex + 1)} 个 turn，共 ${logicalTurns.length} 个 turn`}
              className="flex flex-col items-center gap-1.5"
              role="img"
            >
              {Array.from({ length: compactRail.count }, (_, index) => (
                <span
                  key={index}
                  data-active={index === compactRail.activeIndex}
                  className={cn(
                    "block h-1 rounded-full transition-[width,background-color] duration-200",
                    index === compactRail.activeIndex ? "w-5 bg-primary" : "w-2.5 bg-border",
                  )}
                />
              ))}
            </div>
          ) : (
            <ListTree className="size-4 text-muted-foreground" aria-hidden="true" />
          )}
        </div>

        <div className="pointer-events-none absolute right-0 top-1/2 h-56 w-52 -translate-y-1/2 opacity-0 transition-[opacity,transform] duration-200 ease-out group-hover/turn-navigator:pointer-events-auto group-hover/turn-navigator:translate-x-0 group-hover/turn-navigator:opacity-100 group-focus-within/turn-navigator:pointer-events-auto group-focus-within/turn-navigator:translate-x-0 group-focus-within/turn-navigator:opacity-100 motion-reduce:transition-none">
          <div className="h-full overflow-hidden rounded-2xl border border-border/70 bg-background/95 p-1.5 shadow-xl shadow-black/10 backdrop-blur-md dark:shadow-black/30">
            <div
              className="h-full overflow-y-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
              onScroll={handleMenuScroll}
            >
              <div className="flex flex-col gap-1">
                {menuTurns.map((turn) => (
                  <Button
                    key={turn.id}
                    type="button"
                    variant="ghost"
                    size="sm"
                    aria-current={logicalTurnKey(turn) === activeLogicalTurnId ? "true" : undefined}
                    className={cn(
                      "min-h-8 w-full justify-start rounded-lg px-3 text-right font-normal",
                      logicalTurnKey(turn) === activeLogicalTurnId
                        ? "text-primary hover:text-primary"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                    onClick={() => onSelect(turn)}
                  >
                    <span className="min-w-0 flex-1 truncate">{turnLabel(turn)}</span>
                  </Button>
                ))}
                {loadingMore ? (
                  <Spinner className="my-1 self-center text-muted-foreground" />
                ) : null}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
