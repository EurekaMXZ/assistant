"use client";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { BillingToolKey, BillingUsageEvent } from "@/lib/types";

const toolNames: Record<BillingToolKey, string> = {
  "sandbox.create": "Sandbox Create",
  image_generation: "图片生成",
  "tavily.search": "Tavily Search",
  "tavily.extract": "Tavily Extract",
};

type ToolUsage = Pick<BillingUsageEvent, "currency" | "tool_amount" | "tool_usage">;

export function BillingToolUsage({ usage }: { usage: ToolUsage }) {
  const details = Object.entries(usage.tool_usage)
    .filter((entry): entry is [BillingToolKey, number] => entry[1] != null && entry[1] > 0)
    .sort(([left], [right]) => left.localeCompare(right));
  const total = details.reduce((sum, [, count]) => sum + count, 0);
  if (total === 0) return <span className="text-muted-foreground">-</span>;

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            className="cursor-help border-b border-dashed border-current/60 font-mono tabular-nums outline-none hover:border-current focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40"
            aria-label={`共 ${total} 次工具调用，查看分类用量`}
          />
        }
      >
        {total.toLocaleString("zh-CN")}
      </TooltipTrigger>
      <TooltipContent className="block min-w-48 px-3 py-2" sideOffset={6}>
        <dl className="grid grid-cols-[auto_1fr] gap-x-5 gap-y-1.5">
          {details.map(([key, count]) => (
            <div key={key} className="col-span-2 grid grid-cols-subgrid items-baseline">
              <dt className="text-background/70">{toolNames[key] || key}</dt>
              <dd className="text-right font-mono tabular-nums">{count.toLocaleString("zh-CN")}</dd>
            </div>
          ))}
          <div className="col-span-2 mt-1 grid grid-cols-subgrid border-t border-background/20 pt-1.5">
            <dt className="text-background/70">工具费用</dt>
            <dd className="text-right font-mono tabular-nums">
              {usage.currency || "-"} {usage.tool_amount}
            </dd>
          </div>
        </dl>
      </TooltipContent>
    </Tooltip>
  );
}
