"use client";

import { useEffect, useState } from "react";
import {
  Activity,
  ArrowDownLeft,
  ArrowUpRight,
  Gift,
  ReceiptText,
  RefreshCw,
  WalletCards,
} from "lucide-react";
import { BillingTokenUsage, BillingToolUsage } from "@/components/billing/billing-usage-tooltip";
import { Badge } from "@/components/ui/badge";
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
import { FormField } from "@/components/ui/form-field";
import { Skeleton } from "@/components/ui/skeleton";
import { CursorTableScroll } from "@/components/shared/cursor-table-scroll";
import { EmptyState } from "@/components/shared/empty-state";
import { SettingsSection } from "@/components/shared/settings-section";
import { tableClasses, tableHeadClass } from "@/components/shared/table-styles";
import { Spinner } from "@/components/shared/spinner";
import {
  getBillingAccount,
  isSessionUnauthorizedError,
  listBillingTransactions,
  listBillingUsageEvents,
  redeemBillingCode,
} from "@/lib/api";
import { emitBillingAccountUpdated } from "@/lib/billing-account-events";
import { cn } from "@/lib/utils";
import { formatDateTime } from "@/lib/format";
import type {
  BillingAccount,
  BillingTransaction,
  BillingUsageEvent,
  CursorPage,
} from "@/lib/types";
import { toast } from "sonner";

type ExpenseView = "transactions" | "usage";
const redemptionCodePattern = /^(?:[0-9a-f]{48}|ASST-[A-Za-z0-9_-]{32})$/;

function transactionLabel(kind: BillingTransaction["kind"]) {
  const labels: Record<BillingTransaction["kind"], string> = {
    manual_topup: "账户充值",
    manual_refund: "余额扣减",
    model_usage_charge: "模型与工具用量",
    redemption_credit: "兑换码充值",
  };
  return labels[kind];
}

function usageAmount(event: BillingUsageEvent) {
  if (!event.currency || event.amount_nanos == null) return "未计价";
  const value = event.amount_nanos / 1_000_000_000;
  return `${event.currency} ${value.toLocaleString("zh-CN", { maximumFractionDigits: 9 })}`;
}

function toolUsageAmount(event: BillingUsageEvent) {
  if (!event.currency) return "未计价";
  return `${event.currency} ${event.tool_amount}`;
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
  const [transactionLoadMoreError, setTransactionLoadMoreError] = useState("");
  const [usageLoadMoreError, setUsageLoadMoreError] = useState("");
  const [redemptionOpen, setRedemptionOpen] = useState(false);
  const [redemptionCode, setRedemptionCode] = useState("");
  const [redemptionError, setRedemptionError] = useState("");
  const [isRedeeming, setIsRedeeming] = useState(false);

  const loadInitial = async () => {
    setIsLoading(true);
    setError("");
    setTransactionLoadMoreError("");
    setUsageLoadMoreError("");
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
    if (view === "transactions") setTransactionLoadMoreError("");
    else setUsageLoadMoreError("");
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
        const message = err instanceof Error ? err.message : "更多记录加载失败";
        if (view === "transactions") setTransactionLoadMoreError(message);
        else setUsageLoadMoreError(message);
      }
    } finally {
      setIsLoadingMore(false);
    }
  };

  const redeem = async (event: React.FormEvent) => {
    event.preventDefault();
    const code = redemptionCode.trim();
    if (!code || isRedeeming) return;
    if (!redemptionCodePattern.test(code)) {
      setRedemptionError("兑换码格式不正确");
      return;
    }
    setIsRedeeming(true);
    setRedemptionError("");
    try {
      const result = await redeemBillingCode(code);
      setAccount(result.account);
      if (!result.replayed) {
        setTransactions((items) => [result.transaction, ...items]);
      }
      emitBillingAccountUpdated(result.account);
      setRedemptionOpen(false);
      setRedemptionCode("");
      toast.success(
        result.replayed
          ? "该兑换码已兑换，余额未重复增加"
          : `已兑换 ${result.transaction.currency} ${result.transaction.amount}`,
      );
    } catch (err) {
      if (!isSessionUnauthorizedError(err)) {
        setRedemptionError(err instanceof Error ? err.message : "兑换失败");
      }
    } finally {
      setIsRedeeming(false);
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

  return (
    <SettingsSection title="费用" className="space-y-7">
      {account ? (
        <section className="grid gap-5 py-5 sm:grid-cols-[1fr_auto] sm:items-end">
          <div>
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <WalletCards className="size-4" />
              当前余额
              <Badge variant={account.status === "active" ? "secondary" : "destructive"}>
                {account.status === "active" ? "正常" : "已冻结"}
              </Badge>
            </div>
            <p className="mt-2 font-mono text-3xl font-semibold leading-none">
              {account.currency} {account.balance}
            </p>
          </div>
          <div className="flex flex-wrap gap-2 sm:justify-end">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={account.status !== "active"}
              onClick={() => setRedemptionOpen(true)}
            >
              <Gift className="size-4" />
              兑换余额
            </Button>
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
              用量明细
            </Button>
          </div>
        </div>

        {view === "transactions" ? (
          transactions.length ? (
            <>
              <CursorTableScroll
                className="max-h-[min(55vh,32rem)] overflow-auto border-y"
                hasMore={transactionPage.has_more}
                loadingMore={isLoadingMore}
                loadMoreError={transactionLoadMoreError}
                onLoadMore={loadMore}
                aria-label="资金流水"
              >
                <div className="divide-y sm:hidden">
                  {transactions.map((item) => (
                    <div key={item.id} className="flex items-center justify-between gap-4 py-3">
                      <div className="min-w-0">
                        <p className="flex items-center gap-2 truncate text-sm font-medium">
                          {item.direction === "credit" ? (
                            <ArrowDownLeft className="size-3.5 shrink-0 text-credit" />
                          ) : (
                            <ArrowUpRight className="size-3.5 shrink-0 text-debit" />
                          )}
                          {transactionLabel(item.kind)}
                        </p>
                        <p className="mt-1 text-xs text-muted-foreground">
                          {formatDateTime(item.created_at, { includeYear: false })}
                        </p>
                      </div>
                      <div className="shrink-0 text-right">
                        <p
                          className={cn(
                            "whitespace-nowrap font-mono text-sm",
                            item.direction === "credit" ? "text-credit-strong" : "text-foreground",
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
                <table className="hidden w-[44rem] min-w-full table-fixed text-left text-sm sm:table">
                  <colgroup>
                    <col className="w-[10rem]" />
                    <col className="w-[14rem]" />
                    <col className="w-[10rem]" />
                    <col className="w-[10rem]" />
                  </colgroup>
                  <thead className={tableHeadClass}>
                    <tr className="border-b">
                      <th className={tableClasses.headStart}>时间</th>
                      <th className={tableClasses.head}>类型</th>
                      <th className={`${tableClasses.head} text-right`}>金额</th>
                      <th className={tableClasses.headEnd}>余额</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {transactions.map((item) => (
                      <tr key={item.id}>
                        <td
                          className={`${tableClasses.cellStart} whitespace-nowrap text-xs text-muted-foreground`}
                        >
                          {formatDateTime(item.created_at, { includeYear: false })}
                        </td>
                        <td className={`${tableClasses.cell} whitespace-nowrap`}>
                          <span className="inline-flex items-center gap-2 whitespace-nowrap font-medium">
                            {item.direction === "credit" ? (
                              <ArrowDownLeft className="size-3.5 text-credit" />
                            ) : (
                              <ArrowUpRight className="size-3.5 text-debit" />
                            )}
                            {transactionLabel(item.kind)}
                          </span>
                        </td>
                        <td
                          className={cn(
                            `${tableClasses.cell} whitespace-nowrap text-right font-mono`,
                            item.direction === "credit" ? "text-credit-strong" : "text-foreground",
                          )}
                        >
                          {item.direction === "credit" ? "+" : "-"}
                          {item.currency} {item.amount}
                        </td>
                        <td
                          className={`${tableClasses.cellEnd} whitespace-nowrap font-mono text-muted-foreground`}
                        >
                          {item.currency} {item.balance_after}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </CursorTableScroll>
            </>
          ) : (
            <EmptyState
              icon={ReceiptText}
              title="暂无资金流水"
              className="min-h-40"
              titleClassName="mt-2 font-normal text-muted-foreground"
            />
          )
        ) : usageEvents.length ? (
          <>
            <CursorTableScroll
              className="max-h-[min(55vh,32rem)] overflow-auto border-y"
              hasMore={usagePage.has_more}
              loadingMore={isLoadingMore}
              loadMoreError={usageLoadMoreError}
              onLoadMore={loadMore}
              aria-label="用量明细"
            >
              <div className="divide-y sm:hidden">
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
                        {formatDateTime(item.created_at, { includeYear: false })} ·{" "}
                        <BillingTokenUsage usage={item} /> tokens ·{` `}
                        <BillingToolUsage usage={item} /> tools
                      </p>
                    </div>
                    <div className="shrink-0 text-right">
                      <p className="whitespace-nowrap font-mono text-sm">{usageAmount(item)}</p>
                      <p className="mt-1 whitespace-nowrap text-xs text-muted-foreground">
                        工具 {toolUsageAmount(item)}
                      </p>
                    </div>
                  </div>
                ))}
              </div>
              <table className="hidden w-[72rem] min-w-full table-fixed text-left text-sm sm:table">
                <colgroup>
                  <col className="w-[10rem]" />
                  <col className="w-[18rem]" />
                  <col className="w-[8rem]" />
                  <col className="w-[6rem]" />
                  <col className="w-[11rem]" />
                  <col className="w-[11rem]" />
                  <col className="w-[8rem]" />
                </colgroup>
                <thead className={tableHeadClass}>
                  <tr className="border-b">
                    <th className={tableClasses.headStart}>时间</th>
                    <th className={tableClasses.head}>模型</th>
                    <th className={`${tableClasses.head} text-right`}>Tokens</th>
                    <th className={`${tableClasses.head} text-right`}>工具</th>
                    <th className={`${tableClasses.head} text-right`}>工具费用</th>
                    <th className={`${tableClasses.head} text-right`}>总费用</th>
                    <th className={tableClasses.headEnd}>状态</th>
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {usageEvents.map((item) => (
                    <tr key={item.id}>
                      <td
                        className={`${tableClasses.cellStart} whitespace-nowrap text-xs text-muted-foreground`}
                      >
                        {formatDateTime(item.created_at, { includeYear: false })}
                      </td>
                      <td
                        className={`${tableClasses.cell} truncate font-medium`}
                        title={item.upstream_model}
                      >
                        {item.upstream_model}
                      </td>
                      <td
                        className={`${tableClasses.cell} whitespace-nowrap text-right font-mono text-muted-foreground`}
                      >
                        <BillingTokenUsage usage={item} />
                      </td>
                      <td
                        className={`${tableClasses.cell} whitespace-nowrap text-right font-mono text-muted-foreground`}
                      >
                        <BillingToolUsage usage={item} />
                      </td>
                      <td
                        className={`${tableClasses.cell} whitespace-nowrap text-right font-mono text-muted-foreground`}
                      >
                        {toolUsageAmount(item)}
                      </td>
                      <td className={`${tableClasses.cell} whitespace-nowrap text-right font-mono`}>
                        {usageAmount(item)}
                      </td>
                      <td className={tableClasses.cellEnd}>
                        <Badge variant={item.status === "failed" ? "destructive" : "secondary"}>
                          {item.status === "completed" ? "已计费" : "失败"}
                        </Badge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CursorTableScroll>
          </>
        ) : (
          <EmptyState
            icon={Activity}
            title="暂无用量明细"
            className="min-h-40"
            titleClassName="mt-2 font-normal text-muted-foreground"
          />
        )}

        {error ? <p className="mt-3 text-sm text-destructive">{error}</p> : null}
      </section>

      <Dialog
        open={redemptionOpen}
        onOpenChange={(open) => {
          setRedemptionOpen(open);
          if (!open && !isRedeeming) {
            setRedemptionCode("");
            setRedemptionError("");
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>兑换余额</DialogTitle>
            <DialogDescription>输入兑换码，金额将立即计入当前账户余额。</DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={redeem}>
            <FormField
              label="兑换码"
              htmlFor="billing-redemption-code"
              error={redemptionError}
              errorId="billing-redemption-error"
            >
              <Input
                id="billing-redemption-code"
                autoCapitalize="none"
                autoComplete="off"
                spellCheck={false}
                className="font-mono"
                placeholder="48 位小写十六进制兑换码"
                aria-invalid={redemptionError ? true : undefined}
                aria-describedby={redemptionError ? "billing-redemption-error" : undefined}
                value={redemptionCode}
                onChange={(event) => {
                  setRedemptionCode(event.target.value);
                  setRedemptionError("");
                }}
              />
            </FormField>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                disabled={isRedeeming}
                onClick={() => setRedemptionOpen(false)}
              >
                取消
              </Button>
              <Button type="submit" disabled={isRedeeming || !redemptionCode.trim()}>
                {isRedeeming ? <Spinner /> : null}
                确认兑换
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </SettingsSection>
  );
}
