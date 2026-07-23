"use client";

import type { BillingToolKey, BillingUsageEvent } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

const toolNames: Record<BillingToolKey, string> = {
  "sandbox.create": "Sandbox Create",
  image_generation: "图片生成",
  "tavily.search": "Tavily Search",
  "tavily.extract": "Tavily Extract",
};

const triggerClassName =
  "h-auto min-h-0 cursor-help rounded-none border-0 border-b border-dashed border-current/60 bg-transparent! px-0 py-0 font-mono tabular-nums outline-none hover:border-current hover:text-current focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40";

function formatCount(value: number) {
  return value.toLocaleString("zh-CN");
}

function BillingUsageTooltip({
  value,
  label,
  details,
  footer,
  className,
}: {
  value: React.ReactNode;
  label: string;
  details: ReadonlyArray<readonly [string, React.ReactNode]>;
  footer?: readonly [string, React.ReactNode];
  className?: string;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        openOnClick
        render={
          <Button
            type="button"
            variant="ghost"
            className={cn(triggerClassName, className)}
            aria-label={label}
          />
        }
      >
        {value}
      </TooltipTrigger>
      <TooltipContent className="block min-w-44 px-3 py-2" sideOffset={6}>
        <dl className="grid grid-cols-[auto_1fr] gap-x-5 gap-y-1.5">
          {details.map(([detailLabel, detailValue]) => (
            <div key={detailLabel} className="col-span-2 grid grid-cols-subgrid items-baseline">
              <dt className="text-background/70">{detailLabel}</dt>
              <dd className="text-right font-mono tabular-nums">{detailValue}</dd>
            </div>
          ))}
          {footer ? (
            <div className="col-span-2 mt-1 grid grid-cols-subgrid border-t border-background/20 pt-1.5">
              <dt className="text-background/70">{footer[0]}</dt>
              <dd className="text-right font-mono tabular-nums">{footer[1]}</dd>
            </div>
          ) : null}
        </dl>
      </TooltipContent>
    </Tooltip>
  );
}

type TokenUsage = Pick<
  BillingUsageEvent,
  | "total_tokens"
  | "input_tokens"
  | "cache_read_input_tokens"
  | "cache_creation_input_tokens"
  | "output_tokens"
>;

function BillingTokenUsage({ className, usage }: { className?: string; usage: TokenUsage }) {
  const details = [
    ["输入", formatCount(usage.input_tokens)],
    ["缓存读取", formatCount(usage.cache_read_input_tokens)],
    ["缓存创建", formatCount(usage.cache_creation_input_tokens)],
    ["输出", formatCount(usage.output_tokens)],
  ] as const;
  return (
    <BillingUsageTooltip
      value={formatCount(usage.total_tokens)}
      label={`总计 ${formatCount(usage.total_tokens)} Tokens，查看分类用量`}
      details={details}
      className={className}
    />
  );
}

type ToolUsage = Pick<BillingUsageEvent, "currency" | "tool_amount" | "tool_usage">;

function BillingToolUsage({ usage }: { usage: ToolUsage }) {
  const entries = Object.entries(usage.tool_usage)
    .filter((entry): entry is [BillingToolKey, number] => entry[1] != null && entry[1] > 0)
    .sort(([left], [right]) => left.localeCompare(right));
  const total = entries.reduce((sum, [, count]) => sum + count, 0);
  if (total === 0) return <span className="text-muted-foreground">-</span>;

  return (
    <BillingUsageTooltip
      value={formatCount(total)}
      label={`共 ${total} 次工具调用，查看分类用量`}
      details={entries.map(([key, count]) => [toolNames[key] || key, formatCount(count)])}
      footer={["工具费用", `${usage.currency || "-"} ${usage.tool_amount}`]}
    />
  );
}

export { BillingTokenUsage, BillingToolUsage, BillingUsageTooltip };
