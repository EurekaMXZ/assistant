"use client";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { BillingUsageEvent } from "@/lib/types";
import { cn } from "@/lib/utils";

type TokenUsage = Pick<
  BillingUsageEvent,
  | "total_tokens"
  | "input_tokens"
  | "cache_read_input_tokens"
  | "cache_creation_input_tokens"
  | "output_tokens"
>;

interface BillingTokenUsageProps {
  className?: string;
  usage: TokenUsage;
}

function formatTokens(value: number) {
  return value.toLocaleString("zh-CN");
}

export function BillingTokenUsage({ className, usage }: BillingTokenUsageProps) {
  const details = [
    ["输入", usage.input_tokens],
    ["缓存读取", usage.cache_read_input_tokens],
    ["缓存创建", usage.cache_creation_input_tokens],
    ["输出", usage.output_tokens],
  ] as const;

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            className={cn(
              "cursor-help border-b border-dashed border-current/60 font-mono tabular-nums outline-none hover:border-current focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40",
              className,
            )}
            aria-label={`总计 ${formatTokens(usage.total_tokens)} Tokens，查看分类用量`}
          />
        }
      >
        {formatTokens(usage.total_tokens)}
      </TooltipTrigger>
      <TooltipContent className="block min-w-44 px-3 py-2" sideOffset={6}>
        <dl className="grid grid-cols-[auto_1fr] gap-x-5 gap-y-1.5">
          {details.map(([label, value]) => (
            <div key={label} className="col-span-2 grid grid-cols-subgrid items-baseline">
              <dt className="text-background/70">{label}</dt>
              <dd className="text-right font-mono tabular-nums">{formatTokens(value)}</dd>
            </div>
          ))}
        </dl>
      </TooltipContent>
    </Tooltip>
  );
}
