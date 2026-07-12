"use client";

import { useEffect, useState } from "react";
import {
  Activity,
  ArrowDownLeft,
  ArrowUpRight,
  Loader2,
  ReceiptText,
  RefreshCw,
  WalletCards,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  getBillingAccount,
  isSessionUnauthorizedError,
  listBillingTransactions,
  listBillingUsageEvents,
} from "@/lib/api";
import { cn } from "@/lib/utils";
import type {
  BillingAccount,
  BillingTransaction,
  BillingUsageEvent,
  CursorPage,
} from "@/lib/types";

type ExpenseView = "transactions" | "usage";

function formatDate(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

function transactionLabel(kind: BillingTransaction["kind"]) {
  const labels: Record<BillingTransaction["kind"], string> = {
    manual_topup: "账户充值",
    manual_refund: "余额扣减",
    model_usage_charge: "模型用量",
  };
  return labels[kind];
}

function usageAmount(event: BillingUsageEvent) {
  if (!event.currency || event.amount_nanos == null) return "未计价";
  const value = event.amount_nanos / 1_000_000_000;
  return `${event.currency} ${value.toLocaleString("zh-CN", { maximumFractionDigits: 9 })}`;
}

export function ExpensesSettings() {
  const [view, setView] = useState<ExpenseView>("transactions");
  const [account, setAccount] = useState<BillingAccount | null>(null);
  const [transactions, setTransactions] = useState<BillingTransaction[]>([]);
  const [usageEvents, setUsageEvents] = useState<BillingUsageEvent[]>([]);
  const [transactionPage, setTransactionPage] = useState<CursorPage>({ has_more: false });
  const [usagePage, setUsagePage] = useState<CursorPage>({ has_more: false });
  const [isLoading, setIsLoading] = useState(true);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [error, setError] = useState("");

  const loadInitial = async () => {
    setIsLoading(true);
    setError("");
    try {
      const [nextAccount, nextTransactions, nextUsage] = await Promise.all([
        getBillingAccount(),
        listBillingTransactions(),
        listBillingUsageEvents(),
      ]);
      setAccount(nextAccount);
      setTransactions(nextTransactions.data);
      setTransactionPage(nextTransactions.page);
      setUsageEvents(nextUsage.data);
      setUsagePage(nextUsage.page);
    } catch (err) {
      if (!isSessionUnauthorizedError(err)) {
        setError(err instanceof Error ? err.message : "费用数据加载失败");
      }
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    void loadInitial();
  }, []);

  const loadMore = async () => {
    const page = view === "transactions" ? transactionPage : usagePage;
    if (!page.next_cursor || isLoadingMore) return;
    setIsLoadingMore(true);
    try {
      if (view === "transactions") {
        const next = await listBillingTransactions(page.next_cursor);
        setTransactions((items) => [...items, ...next.data]);
        setTransactionPage(next.page);
      } else {
        const next = await listBillingUsageEvents(page.next_cursor);
        setUsageEvents((items) => [...items, ...next.data]);
        setUsagePage(next.page);
      }
    } catch (err) {
      if (!isSessionUnauthorizedError(err)) {
        setError(err instanceof Error ? err.message : "更多记录加载失败");
      }
    } finally {
      setIsLoadingMore(false);
    }
  };

  if (isLoading) {
    return (
      <div className="space-y-7">
        <Skeleton className="h-7 w-16" />
        <Skeleton className="h-28 w-full rounded-lg" />
        <Skeleton className="h-9 w-52 rounded-md" />
        <div className="space-y-3">
          {Array.from({ length: 5 }).map((_, index) => (
            <Skeleton key={index} className="h-12 w-full" />
          ))}
        </div>
      </div>
    );
  }

  if (error && !account) {
    return (
      <div className="flex min-h-72 flex-col items-center justify-center text-center">
        <ReceiptText className="size-6 text-muted-foreground" />
        <p className="mt-3 text-sm font-medium">{error}</p>
        <Button type="button" variant="outline" size="sm" className="mt-4" onClick={loadInitial}>
          <RefreshCw data-icon="inline-start" />
          重新加载
        </Button>
      </div>
    );
  }

  const page = view === "transactions" ? transactionPage : usagePage;

  return (
    <div className="space-y-7">
      <header>
        <h2 className="text-xl font-semibold">费用</h2>
      </header>

      {account ? (
        <section className="grid gap-5 border-y py-5 sm:grid-cols-[1fr_auto] sm:items-end">
          <div>
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <WalletCards className="size-4" />
              当前余额
            </div>
            <p className="mt-2 font-mono text-3xl font-semibold leading-none">
              {account.currency} {account.balance}
            </p>
          </div>
          <div className="flex flex-wrap gap-2 sm:justify-end">
            <Badge variant="outline">预付费</Badge>
            <Badge variant={account.status === "active" ? "secondary" : "destructive"}>
              {account.status === "active" ? "正常" : "已冻结"}
            </Badge>
          </div>
        </section>
      ) : null}

      <section>
        <div className="mb-4 flex items-center justify-between gap-3">
          <div className="inline-flex rounded-md bg-muted p-1" role="tablist" aria-label="费用记录">
            <Button
              type="button"
              role="tab"
              aria-selected={view === "transactions"}
              size="sm"
              variant={view === "transactions" ? "secondary" : "ghost"}
              className="h-7 min-h-7 rounded px-2.5 text-xs text-muted-foreground hover:bg-background hover:text-foreground aria-selected:bg-background aria-selected:text-foreground aria-selected:shadow-xs"
              onClick={() => setView("transactions")}
            >
              <ReceiptText className="size-3.5" />
              资金流水
            </Button>
            <Button
              type="button"
              role="tab"
              aria-selected={view === "usage"}
              size="sm"
              variant={view === "usage" ? "secondary" : "ghost"}
              className="h-7 min-h-7 rounded px-2.5 text-xs text-muted-foreground hover:bg-background hover:text-foreground aria-selected:bg-background aria-selected:text-foreground aria-selected:shadow-xs"
              onClick={() => setView("usage")}
            >
              <Activity className="size-3.5" />
              模型用量
            </Button>
          </div>
        </div>

        {view === "transactions" ? (
          transactions.length ? (
            <>
              <div className="divide-y border-y sm:hidden">
                {transactions.map((item) => (
                  <div key={item.id} className="flex items-center justify-between gap-4 py-3">
                    <div className="min-w-0">
                      <p className="flex items-center gap-2 truncate text-sm font-medium">
                        {item.direction === "credit" ? (
                          <ArrowDownLeft className="size-3.5 shrink-0 text-emerald-600" />
                        ) : (
                          <ArrowUpRight className="size-3.5 shrink-0 text-amber-600" />
                        )}
                        {transactionLabel(item.kind)}
                      </p>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {formatDate(item.created_at)}
                      </p>
                    </div>
                    <div className="shrink-0 text-right">
                      <p
                        className={cn(
                          "whitespace-nowrap font-mono text-sm",
                          item.direction === "credit" ? "text-emerald-700" : "text-foreground",
                        )}
                      >
                        {item.direction === "credit" ? "+" : "-"}
                        {item.currency} {item.amount}
                      </p>
                      <p className="mt-1 whitespace-nowrap font-mono text-xs text-muted-foreground">
                        余额 {item.balance_after}
                      </p>
                    </div>
                  </div>
                ))}
              </div>
              <div className="hidden overflow-x-auto border-y sm:block">
                <table className="w-full min-w-[560px] text-left text-sm">
                  <thead className="text-xs text-muted-foreground">
                    <tr className="border-b">
                      <th className="py-3 pr-4 font-medium">时间</th>
                      <th className="px-4 py-3 font-medium">类型</th>
                      <th className="px-4 py-3 text-right font-medium">金额</th>
                      <th className="py-3 pl-4 text-right font-medium">余额</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {transactions.map((item) => (
                      <tr key={item.id}>
                        <td className="whitespace-nowrap py-3 pr-4 text-xs text-muted-foreground">
                          {formatDate(item.created_at)}
                        </td>
                        <td className="px-4 py-3">
                          <span className="inline-flex items-center gap-2 font-medium">
                            {item.direction === "credit" ? (
                              <ArrowDownLeft className="size-3.5 text-emerald-600" />
                            ) : (
                              <ArrowUpRight className="size-3.5 text-amber-600" />
                            )}
                            {transactionLabel(item.kind)}
                          </span>
                        </td>
                        <td
                          className={cn(
                            "whitespace-nowrap px-4 py-3 text-right font-mono",
                            item.direction === "credit" ? "text-emerald-700" : "text-foreground",
                          )}
                        >
                          {item.direction === "credit" ? "+" : "-"}
                          {item.currency} {item.amount}
                        </td>
                        <td className="whitespace-nowrap py-3 pl-4 text-right font-mono text-muted-foreground">
                          {item.currency} {item.balance_after}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          ) : (
            <EmptyExpenses icon={ReceiptText} label="暂无资金流水" />
          )
        ) : usageEvents.length ? (
          <>
            <div className="divide-y border-y sm:hidden">
              {usageEvents.map((item) => (
                <div key={item.id} className="flex items-center justify-between gap-4 py-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <p className="truncate text-sm font-medium">{item.upstream_model}</p>
                      <Badge variant={item.status === "failed" ? "destructive" : "secondary"}>
                        {item.status === "completed" ? "已计费" : "失败"}
                      </Badge>
                    </div>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {formatDate(item.created_at)} · {item.total_tokens.toLocaleString("zh-CN")}{" "}
                      tokens
                    </p>
                  </div>
                  <p className="shrink-0 whitespace-nowrap font-mono text-sm">
                    {usageAmount(item)}
                  </p>
                </div>
              ))}
            </div>
            <div className="hidden overflow-x-auto border-y sm:block">
              <table className="w-full min-w-[600px] text-left text-sm">
                <thead className="text-xs text-muted-foreground">
                  <tr className="border-b">
                    <th className="py-3 pr-4 font-medium">时间</th>
                    <th className="px-4 py-3 font-medium">模型</th>
                    <th className="px-4 py-3 text-right font-medium">Tokens</th>
                    <th className="px-4 py-3 text-right font-medium">费用</th>
                    <th className="py-3 pl-4 text-right font-medium">状态</th>
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {usageEvents.map((item) => (
                    <tr key={item.id}>
                      <td className="whitespace-nowrap py-3 pr-4 text-xs text-muted-foreground">
                        {formatDate(item.created_at)}
                      </td>
                      <td className="max-w-48 truncate px-4 py-3 font-medium">
                        {item.upstream_model}
                      </td>
                      <td className="whitespace-nowrap px-4 py-3 text-right font-mono text-muted-foreground">
                        {item.total_tokens.toLocaleString("zh-CN")}
                      </td>
                      <td className="whitespace-nowrap px-4 py-3 text-right font-mono">
                        {usageAmount(item)}
                      </td>
                      <td className="py-3 pl-4 text-right">
                        <Badge variant={item.status === "failed" ? "destructive" : "secondary"}>
                          {item.status === "completed" ? "已计费" : "失败"}
                        </Badge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        ) : (
          <EmptyExpenses icon={Activity} label="暂无模型用量" />
        )}

        {error ? <p className="mt-3 text-sm text-destructive">{error}</p> : null}
        {page.has_more ? (
          <div className="mt-4 flex justify-center">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={isLoadingMore}
              onClick={loadMore}
            >
              {isLoadingMore ? <Loader2 className="animate-spin" /> : null}
              加载更多
            </Button>
          </div>
        ) : null}
      </section>
    </div>
  );
}

function EmptyExpenses({ icon: Icon, label }: { icon: typeof ReceiptText; label: string }) {
  return (
    <div className="flex min-h-40 flex-col items-center justify-center border-y text-center">
      <Icon className="size-5 text-muted-foreground" />
      <p className="mt-2 text-sm text-muted-foreground">{label}</p>
    </div>
  );
}
