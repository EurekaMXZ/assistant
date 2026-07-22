"use client";

import { useEffect, useState } from "react";
import {
  Activity,
  ArrowDownLeft,
  ArrowUpRight,
  BadgeDollarSign,
  CreditCard,
  Gift,
  MoreHorizontal,
  ReceiptText,
  WalletCards,
} from "lucide-react";
import { toast } from "sonner";
import { AdminRedemptionCodes } from "@/components/admin/admin-redemption-codes";
import { AdminToolPrices } from "@/components/admin/admin-tool-prices";
import {
  BillingAccountDialog,
  BillingAdjustmentDialog,
  type BillingAdjustment,
} from "@/components/admin/admin-billing-dialogs";
import { BillingTokenUsage, BillingToolUsage } from "@/components/billing/billing-usage-tooltip";
import { AdminPageHeader } from "@/components/admin/admin-shared";
import { AdminListPage } from "@/components/admin/admin-list-page";
import { tableClasses, tableHeadClass } from "@/components/shared/table-styles";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  applyAdminBillingAdjustment,
  listAdminBillingAccountsPage,
  listAdminBillingTransactionsPage,
  listAdminBillingUsageEventsPage,
  listAdminUsersPage,
  updateAdminBillingAccount,
} from "@/lib/api";
import { parseDecimalNanos } from "@/lib/decimal-nanos";
import { formatDateTime } from "@/lib/format";
import { createIdempotencyKey } from "@/lib/idempotency-key";
import type { BillingAccount, BillingTransaction, BillingUsageEvent, User } from "@/lib/types";
import { useCursorPagination } from "@/lib/use-cursor-pagination";

type BillingView = "accounts" | "transactions" | "usage" | "tool-prices" | "codes";

const billingViews = [
  { id: "accounts", label: "账户", icon: WalletCards },
  { id: "transactions", label: "资金流水", icon: ReceiptText },
  { id: "usage", label: "用量明细", icon: Activity },
  { id: "tool-prices", label: "工具计费", icon: BadgeDollarSign },
  { id: "codes", label: "兑换码", icon: Gift },
] as const satisfies ReadonlyArray<{ id: BillingView; label: string; icon: typeof WalletCards }>;

export function AdminBilling() {
  const [view, setView] = useState<BillingView>("accounts");
  const accountState = useCursorPagination<BillingAccount>(
    listAdminBillingAccountsPage,
    "计费账户加载失败",
  );
  const transactionState = useCursorPagination<BillingTransaction>(
    listAdminBillingTransactionsPage,
    "资金流水加载失败",
  );
  const usageState = useCursorPagination<BillingUsageEvent>(
    listAdminBillingUsageEventsPage,
    "用量明细加载失败",
  );
  const { items: accounts, setItems: setAccounts } = accountState;
  const { items: transactions, setItems: setTransactions } = transactionState;
  const { items: usage } = usageState;
  const [users, setUsers] = useState<User[]>([]);
  const [editing, setEditing] = useState<BillingAccount | null>(null);
  const [adjusting, setAdjusting] = useState<BillingAdjustment | null>(null);
  const [status, setStatus] = useState<BillingAccount["status"]>("active");
  const [amount, setAmount] = useState("");
  const [reason, setReason] = useState("");
  const [reference, setReference] = useState("");
  const [adjustmentKey, setAdjustmentKey] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    void listAdminUsersPage()
      .then((result) => setUsers(result.data))
      .catch((err: unknown) =>
        toast.error(err instanceof Error ? err.message : "用户信息加载失败"),
      );
  }, []);
  const activeState =
    view === "accounts" ? accountState : view === "transactions" ? transactionState : usageState;
  const loading = activeState.loading;
  const error = activeState.error;
  const load = activeState.reload;
  const userMap = new Map(users.map((user) => [user.id, user]));

  const openEdit = (account: BillingAccount) => {
    setEditing(account);
    setStatus(account.status);
  };

  const saveAccount = async () => {
    if (!editing) return;
    setSaving(true);
    try {
      const saved = await updateAdminBillingAccount(editing.user_id, { status });
      setAccounts((items) => items.map((item) => (item.id === saved.id ? saved : item)));
      setEditing(null);
      toast.success("计费账户已更新");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "计费账户更新失败");
    } finally {
      setSaving(false);
    }
  };

  const openAdjustment = (account: BillingAccount, kind: "topups" | "refunds") => {
    setAdjusting({ account, kind });
    setAmount("");
    setReason("");
    setReference("");
    setAdjustmentKey(createIdempotencyKey());
  };

  const applyAdjustment = async () => {
    if (!adjusting || !adjustmentKey) return;
    setSaving(true);
    try {
      if (parseDecimalNanos(amount) <= 0) throw new Error("金额必须大于 0");
      const transaction = await applyAdminBillingAdjustment(
        adjusting.account.user_id,
        adjusting.kind,
        {
          amount,
          currency: adjusting.account.currency,
          reason: reason.trim(),
          reference: reference.trim(),
        },
        adjustmentKey,
      );
      setTransactions((items) => [transaction, ...items]);
      setAccounts((items) =>
        items.map((item) =>
          item.id === adjusting.account.id
            ? {
                ...item,
                balance: transaction.balance_after,
                balance_nanos: transaction.balance_after_nanos,
              }
            : item,
        ),
      );
      setAdjusting(null);
      setAdjustmentKey("");
      toast.success(adjusting.kind === "topups" ? "充值已入账" : "退款扣减已入账");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "账户调整失败");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <AdminPageHeader title="计费" />
      <div
        className="mt-5 flex max-w-full overflow-x-auto rounded-md bg-muted p-1 sm:w-fit"
        role="tablist"
        aria-label="计费视图"
      >
        {billingViews.map((item) => {
          const Icon = item.icon;
          return (
            <Button
              key={item.id}
              type="button"
              role="tab"
              aria-selected={view === item.id}
              size="sm"
              variant={view === item.id ? "secondary" : "ghost"}
              className="h-7 min-h-7 shrink-0 rounded px-2.5 text-xs text-muted-foreground hover:bg-background hover:text-foreground aria-selected:bg-background aria-selected:text-foreground aria-selected:shadow-xs"
              onClick={() => setView(item.id)}
            >
              <Icon className="size-3.5" />
              {item.label}
            </Button>
          );
        })}
      </div>
      {view === "accounts" ? (
        <AdminListPage
          ariaLabel="计费账户"
          className="mt-5"
          emptyIcon={WalletCards}
          emptyTitle="暂无计费账户"
          error={error}
          hasItems={accounts.length > 0}
          hasMore={accountState.page.has_more}
          loading={loading}
          loadingMore={accountState.loadingMore}
          loadMoreError={accountState.loadMoreError}
          onLoadMore={accountState.loadMore}
          onRetry={load}
        >
          <table className="admin-responsive-table w-[72rem] min-w-full table-fixed text-left text-sm">
            <colgroup>
              <col className="w-[24rem]" />
              <col className="w-[8rem]" />
              <col className="w-[12rem]" />
              <col className="w-[8rem]" />
              <col className="w-[14rem]" />
              <col className="w-[6rem]" />
            </colgroup>
            <thead className={tableHeadClass}>
              <tr className="border-b">
                <th className={tableClasses.headStart}>用户</th>
                <th className={tableClasses.head}>模式</th>
                <th className={`${tableClasses.head} text-right`}>余额</th>
                <th className={tableClasses.head}>状态</th>
                <th className={tableClasses.head}>更新时间</th>
                <th className={tableClasses.headEnd}>操作</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {accounts.map((account) => {
                const user = userMap.get(account.user_id);
                return (
                  <tr key={account.id}>
                    <td className={tableClasses.cellStart} data-primary>
                      <p className="truncate font-medium" title={user?.username || account.user_id}>
                        {user?.username || account.user_id.slice(0, 8)}
                      </p>
                      <p
                        className="mt-0.5 truncate text-xs text-muted-foreground"
                        title={user?.email || account.user_id}
                      >
                        {user?.email || account.user_id}
                      </p>
                    </td>
                    <td className={tableClasses.cell} data-label="模式">
                      <Badge variant="outline">预付费</Badge>
                    </td>
                    <td
                      className={`${tableClasses.cell} whitespace-nowrap text-right font-mono`}
                      data-label="余额"
                    >
                      {account.currency} {account.balance}
                    </td>
                    <td className={tableClasses.cell} data-label="状态">
                      <Badge variant={account.status === "active" ? "secondary" : "destructive"}>
                        {account.status === "active" ? "正常" : "冻结"}
                      </Badge>
                    </td>
                    <td
                      className={`${tableClasses.cell} whitespace-nowrap text-xs text-muted-foreground`}
                      data-label="更新时间"
                    >
                      {formatDateTime(account.updated_at)}
                    </td>
                    <td className={tableClasses.cellEnd} data-actions>
                      <DropdownMenu>
                        <DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" />}>
                          <MoreHorizontal />
                          <span className="sr-only">计费账户操作</span>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end" className="w-40">
                          <DropdownMenuGroup>
                            <DropdownMenuItem onClick={() => openEdit(account)}>
                              账户设置
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => openAdjustment(account, "topups")}>
                              <ArrowDownLeft />
                              充值
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => openAdjustment(account, "refunds")}>
                              <ArrowUpRight />
                              退款扣减
                            </DropdownMenuItem>
                          </DropdownMenuGroup>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </AdminListPage>
      ) : null}

      {view === "transactions" ? (
        <AdminListPage
          ariaLabel="资金流水"
          className="mt-5"
          emptyIcon={ReceiptText}
          emptyTitle="暂无资金流水"
          error={error}
          hasItems={transactions.length > 0}
          hasMore={transactionState.page.has_more}
          loading={loading}
          loadingMore={transactionState.loadingMore}
          loadMoreError={transactionState.loadMoreError}
          onLoadMore={transactionState.loadMore}
          onRetry={load}
        >
          <table className="admin-responsive-table w-[78rem] min-w-full table-fixed text-left text-sm">
            <colgroup>
              <col className="w-[11rem]" />
              <col className="w-[18rem]" />
              <col className="w-[14rem]" />
              <col className="w-[12rem]" />
              <col className="w-[10rem]" />
              <col className="w-[13rem]" />
            </colgroup>
            <thead className={tableHeadClass}>
              <tr className="border-b">
                <th className={tableClasses.headStart}>时间</th>
                <th className={tableClasses.head}>用户</th>
                <th className={tableClasses.head}>类型</th>
                <th className={`${tableClasses.head} text-right`}>金额</th>
                <th className={`${tableClasses.head} text-right`}>余额</th>
                <th className={`${tableClasses.headEnd} text-left`}>原因</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {transactions.map((item) => (
                <tr key={item.id}>
                  <td
                    className={`${tableClasses.cellStart} whitespace-nowrap text-xs text-muted-foreground`}
                    data-label="时间"
                  >
                    {formatDateTime(item.created_at)}
                  </td>
                  <td
                    className={`${tableClasses.cell} truncate`}
                    title={userMap.get(item.user_id)?.username || item.user_id}
                    data-primary
                  >
                    {userMap.get(item.user_id)?.username || item.user_id.slice(0, 8)}
                  </td>
                  <td className={`${tableClasses.cell} whitespace-nowrap`} data-label="类型">
                    <span className="inline-flex items-center gap-2 whitespace-nowrap">
                      {item.direction === "credit" ? (
                        <ArrowDownLeft className="size-4 shrink-0 stroke-[1.75] text-credit" />
                      ) : (
                        <ArrowUpRight className="size-4 shrink-0 stroke-[1.75] text-debit" />
                      )}
                      {transactionName(item.kind)}
                    </span>
                  </td>
                  <td
                    className={`${tableClasses.cell} whitespace-nowrap text-right font-mono`}
                    data-label="金额"
                  >
                    {item.direction === "credit" ? "+" : "-"}
                    {item.currency} {item.amount}
                  </td>
                  <td
                    className={`${tableClasses.cell} whitespace-nowrap text-right font-mono text-muted-foreground`}
                    data-label="余额"
                  >
                    {item.balance_after}
                  </td>
                  <td
                    className={`${tableClasses.cellEnd} truncate text-left text-muted-foreground`}
                    title={item.reason || ""}
                    data-label="原因"
                  >
                    {item.reason || "-"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </AdminListPage>
      ) : null}

      {view === "usage" ? (
        <AdminListPage
          ariaLabel="用量明细"
          className="mt-5"
          emptyIcon={CreditCard}
          emptyTitle="暂无模型用量"
          error={error}
          hasItems={usage.length > 0}
          hasMore={usageState.page.has_more}
          loading={loading}
          loadingMore={usageState.loadingMore}
          loadMoreError={usageState.loadMoreError}
          onLoadMore={usageState.loadMore}
          onRetry={load}
        >
          <table className="admin-responsive-table w-[80rem] min-w-full table-fixed text-left text-sm">
            <colgroup>
              <col className="w-[11rem]" />
              <col className="w-[16rem]" />
              <col className="w-[18rem]" />
              <col className="w-[9rem]" />
              <col className="w-[7rem]" />
              <col className="w-[12rem]" />
              <col className="w-[7rem]" />
            </colgroup>
            <thead className={tableHeadClass}>
              <tr className="border-b">
                <th className={tableClasses.headStart}>时间</th>
                <th className={tableClasses.head}>用户</th>
                <th className={tableClasses.head}>模型</th>
                <th className={`${tableClasses.head} text-right`}>Tokens</th>
                <th className={`${tableClasses.head} text-right`}>工具</th>
                <th className={`${tableClasses.head} text-right`}>费用</th>
                <th className={tableClasses.headEnd}>状态</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {usage.map((item) => (
                <tr key={item.id}>
                  <td
                    className={`${tableClasses.cellStart} whitespace-nowrap text-xs text-muted-foreground`}
                    data-label="时间"
                  >
                    {formatDateTime(item.created_at)}
                  </td>
                  <td
                    className={`${tableClasses.cell} truncate`}
                    title={
                      item.owner_user_id
                        ? userMap.get(item.owner_user_id)?.username || item.owner_user_id
                        : ""
                    }
                    data-label="用户"
                  >
                    {item.owner_user_id
                      ? userMap.get(item.owner_user_id)?.username || item.owner_user_id.slice(0, 8)
                      : "-"}
                  </td>
                  <td
                    className={`${tableClasses.cell} truncate font-medium`}
                    title={item.upstream_model}
                    data-primary
                  >
                    {item.upstream_model}
                  </td>
                  <td className={`${tableClasses.cell} text-right font-mono`} data-label="Tokens">
                    <BillingTokenUsage usage={item} />
                  </td>
                  <td className={`${tableClasses.cell} text-right font-mono`} data-label="工具">
                    <BillingToolUsage usage={item} />
                  </td>
                  <td
                    className={`${tableClasses.cell} whitespace-nowrap text-right font-mono`}
                    data-label="费用"
                  >
                    {item.currency && item.amount_nanos != null
                      ? `${item.currency} ${(item.amount_nanos / 1_000_000_000).toLocaleString("zh-CN", { maximumFractionDigits: 9 })}`
                      : "未计价"}
                  </td>
                  <td className={tableClasses.cellEnd} data-label="状态">
                    <Badge variant={item.status === "completed" ? "secondary" : "destructive"}>
                      {item.status}
                    </Badge>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </AdminListPage>
      ) : null}

      {view === "codes" ? <AdminRedemptionCodes users={users} /> : null}
      {view === "tool-prices" ? <AdminToolPrices /> : null}

      <BillingAccountDialog
        account={editing}
        status={status}
        saving={saving}
        onClose={() => setEditing(null)}
        onSave={() => void saveAccount()}
        onStatusChange={setStatus}
      />
      <BillingAdjustmentDialog
        adjustment={adjusting}
        amount={amount}
        reason={reason}
        reference={reference}
        saving={saving}
        onAmountChange={setAmount}
        onClose={() => {
          setAdjusting(null);
          setAdjustmentKey("");
        }}
        onReasonChange={setReason}
        onReferenceChange={setReference}
        onSave={() => void applyAdjustment()}
      />
    </div>
  );
}

function transactionName(kind: BillingTransaction["kind"]) {
  return {
    manual_topup: "充值",
    manual_refund: "退款扣减",
    model_usage_charge: "模型与工具用量",
    redemption_credit: "兑换码充值",
  }[kind];
}
