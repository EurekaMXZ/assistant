"use client";

import { useEffect, useState } from "react";
import {
  ArrowDownLeft,
  ArrowUpRight,
  CreditCard,
  MoreHorizontal,
  ReceiptText,
  WalletCards,
} from "lucide-react";
import { toast } from "sonner";
import { AdminRedemptionCodes } from "@/components/admin/admin-redemption-codes";
import {
  AdminEmpty,
  AdminError,
  AdminLoading,
  AdminPageHeader,
  SavingIcon,
  adminSelectClass,
  formatAdminDate,
} from "@/components/admin/admin-shared";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  applyAdminBillingAdjustment,
  listAdminBillingAccounts,
  listAdminBillingTransactions,
  listAdminBillingUsageEvents,
  listAdminUsers,
  updateAdminBillingAccount,
} from "@/lib/api";
import { parseDecimalNanos } from "@/lib/decimal-nanos";
import { createIdempotencyKey } from "@/lib/idempotency-key";
import type { BillingAccount, BillingTransaction, BillingUsageEvent, User } from "@/lib/types";

type BillingView = "accounts" | "transactions" | "usage" | "codes";

export function AdminBilling() {
  const [view, setView] = useState<BillingView>("accounts");
  const [accounts, setAccounts] = useState<BillingAccount[]>([]);
  const [transactions, setTransactions] = useState<BillingTransaction[]>([]);
  const [usage, setUsage] = useState<BillingUsageEvent[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<BillingAccount | null>(null);
  const [adjusting, setAdjusting] = useState<{
    account: BillingAccount;
    kind: "topups" | "refunds";
  } | null>(null);
  const [status, setStatus] = useState<BillingAccount["status"]>("active");
  const [amount, setAmount] = useState("");
  const [reason, setReason] = useState("");
  const [reference, setReference] = useState("");
  const [adjustmentKey, setAdjustmentKey] = useState("");
  const [saving, setSaving] = useState(false);

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      const [nextAccounts, nextTransactions, nextUsage, nextUsers] = await Promise.all([
        listAdminBillingAccounts(),
        listAdminBillingTransactions(),
        listAdminBillingUsageEvents(),
        listAdminUsers(),
      ]);
      setAccounts(nextAccounts);
      setTransactions(nextTransactions);
      setUsage(nextUsage);
      setUsers(nextUsers);
    } catch (err) {
      setError(err instanceof Error ? err.message : "计费数据加载失败");
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => {
    void load();
  }, []);
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
        className="mt-5 inline-flex rounded-md bg-muted p-1"
        role="tablist"
        aria-label="计费视图"
      >
        {(["accounts", "transactions", "usage", "codes"] as const).map((item) => (
          <Button
            key={item}
            type="button"
            role="tab"
            aria-selected={view === item}
            size="sm"
            variant={view === item ? "secondary" : "ghost"}
            className="h-7 min-h-7 rounded px-2.5 text-xs aria-selected:bg-background aria-selected:shadow-xs"
            onClick={() => setView(item)}
          >
            {item === "accounts"
              ? "账户"
              : item === "transactions"
                ? "资金流水"
                : item === "usage"
                  ? "模型用量"
                  : "兑换码"}
          </Button>
        ))}
      </div>
      {loading && view !== "codes" ? <AdminLoading /> : null}
      {!loading && error && view !== "codes" ? <AdminError message={error} onRetry={load} /> : null}
      {!loading && !error && view === "accounts" ? (
        accounts.length ? (
          <div className="mt-5 overflow-x-auto border-y">
            <table className="w-full min-w-[820px] text-left text-sm">
              <thead className="text-xs text-muted-foreground">
                <tr className="border-b">
                  <th className="py-3 pr-4 font-medium">用户</th>
                  <th className="px-4 py-3 font-medium">模式</th>
                  <th className="px-4 py-3 text-right font-medium">余额</th>
                  <th className="px-4 py-3 font-medium">状态</th>
                  <th className="px-4 py-3 font-medium">更新时间</th>
                  <th className="py-3 pl-4 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {accounts.map((account) => {
                  const user = userMap.get(account.user_id);
                  return (
                    <tr key={account.id}>
                      <td className="py-3 pr-4">
                        <p className="font-medium">
                          {user?.username || account.user_id.slice(0, 8)}
                        </p>
                        <p className="mt-0.5 text-xs text-muted-foreground">
                          {user?.email || account.user_id}
                        </p>
                      </td>
                      <td className="px-4 py-3">
                        <Badge variant="outline">预付费</Badge>
                      </td>
                      <td className="whitespace-nowrap px-4 py-3 text-right font-mono">
                        {account.currency} {account.balance}
                      </td>
                      <td className="px-4 py-3">
                        <Badge variant={account.status === "active" ? "secondary" : "destructive"}>
                          {account.status === "active" ? "正常" : "冻结"}
                        </Badge>
                      </td>
                      <td className="whitespace-nowrap px-4 py-3 text-xs text-muted-foreground">
                        {formatAdminDate(account.updated_at)}
                      </td>
                      <td className="py-3 pl-4 text-right">
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
          </div>
        ) : (
          <AdminEmpty icon={WalletCards} title="暂无计费账户" />
        )
      ) : null}

      {!loading && !error && view === "transactions" ? (
        transactions.length ? (
          <div className="mt-5 overflow-x-auto border-y">
            <table className="w-full min-w-[860px] text-left text-sm">
              <thead className="text-xs text-muted-foreground">
                <tr className="border-b">
                  <th className="py-3 pr-4 font-medium">时间</th>
                  <th className="px-4 py-3 font-medium">用户</th>
                  <th className="px-4 py-3 font-medium">类型</th>
                  <th className="px-4 py-3 text-right font-medium">金额</th>
                  <th className="px-4 py-3 text-right font-medium">余额</th>
                  <th className="py-3 pl-4 font-medium">原因</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {transactions.map((item) => (
                  <tr key={item.id}>
                    <td className="whitespace-nowrap py-3 pr-4 text-xs text-muted-foreground">
                      {formatAdminDate(item.created_at)}
                    </td>
                    <td className="px-4 py-3">
                      {userMap.get(item.user_id)?.username || item.user_id.slice(0, 8)}
                    </td>
                    <td className="px-4 py-3">
                      <span className="inline-flex items-center gap-2">
                        {item.direction === "credit" ? (
                          <ArrowDownLeft className="size-3.5 text-emerald-600" />
                        ) : (
                          <ArrowUpRight className="size-3.5 text-amber-600" />
                        )}
                        {transactionName(item.kind)}
                      </span>
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right font-mono">
                      {item.direction === "credit" ? "+" : "-"}
                      {item.currency} {item.amount}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right font-mono text-muted-foreground">
                      {item.balance_after}
                    </td>
                    <td className="max-w-52 truncate py-3 pl-4 text-muted-foreground">
                      {item.reason || "-"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <AdminEmpty icon={ReceiptText} title="暂无资金流水" />
        )
      ) : null}

      {!loading && !error && view === "usage" ? (
        usage.length ? (
          <div className="mt-5 overflow-x-auto border-y">
            <table className="w-full min-w-[900px] text-left text-sm">
              <thead className="text-xs text-muted-foreground">
                <tr className="border-b">
                  <th className="py-3 pr-4 font-medium">时间</th>
                  <th className="px-4 py-3 font-medium">用户</th>
                  <th className="px-4 py-3 font-medium">模型</th>
                  <th className="px-4 py-3 text-right font-medium">Tokens</th>
                  <th className="px-4 py-3 text-right font-medium">费用</th>
                  <th className="py-3 pl-4 text-right font-medium">状态</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {usage.map((item) => (
                  <tr key={item.id}>
                    <td className="whitespace-nowrap py-3 pr-4 text-xs text-muted-foreground">
                      {formatAdminDate(item.created_at)}
                    </td>
                    <td className="px-4 py-3">
                      {item.owner_user_id
                        ? userMap.get(item.owner_user_id)?.username ||
                          item.owner_user_id.slice(0, 8)
                        : "-"}
                    </td>
                    <td className="px-4 py-3 font-medium">{item.upstream_model}</td>
                    <td className="px-4 py-3 text-right font-mono">
                      {item.total_tokens.toLocaleString("zh-CN")}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right font-mono">
                      {item.currency && item.amount_nanos != null
                        ? `${item.currency} ${(item.amount_nanos / 1_000_000_000).toLocaleString("zh-CN", { maximumFractionDigits: 9 })}`
                        : "未计价"}
                    </td>
                    <td className="py-3 pl-4 text-right">
                      <Badge variant={item.status === "completed" ? "secondary" : "destructive"}>
                        {item.status}
                      </Badge>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <AdminEmpty icon={CreditCard} title="暂无模型用量" />
        )
      ) : null}

      {view === "codes" ? <AdminRedemptionCodes users={users} /> : null}

      <Dialog open={!!editing} onOpenChange={(open) => !open && setEditing(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>账户设置</DialogTitle>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="billing-status">状态</Label>
            <select
              id="billing-status"
              className={adminSelectClass}
              value={status}
              onChange={(event) => setStatus(event.target.value as BillingAccount["status"])}
            >
              <option value="active">正常</option>
              <option value="frozen">冻结</option>
            </select>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditing(null)}>
              取消
            </Button>
            <Button disabled={saving} onClick={() => void saveAccount()}>
              <SavingIcon saving={saving} />
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!adjusting}
        onOpenChange={(open) => {
          if (!open) {
            setAdjusting(null);
            setAdjustmentKey("");
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{adjusting?.kind === "topups" ? "账户充值" : "退款扣减"}</DialogTitle>
            <DialogDescription>金额将以独立账本记录写入，操作不可直接删除。</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="billing-amount">金额 ({adjusting?.account.currency})</Label>
              <Input
                id="billing-amount"
                inputMode="decimal"
                value={amount}
                onChange={(event) => setAmount(event.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="billing-reason">原因</Label>
              <Textarea
                id="billing-reason"
                value={reason}
                onChange={(event) => setReason(event.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="billing-reference">外部参考</Label>
              <Input
                id="billing-reference"
                value={reference}
                onChange={(event) => setReference(event.target.value)}
              />
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setAdjusting(null);
                setAdjustmentKey("");
              }}
            >
              取消
            </Button>
            <Button
              variant={adjusting?.kind === "refunds" ? "destructive" : "default"}
              disabled={saving || !amount || !reason.trim()}
              onClick={() => void applyAdjustment()}
            >
              <SavingIcon saving={saving} />
              确认入账
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function transactionName(kind: BillingTransaction["kind"]) {
  return {
    manual_topup: "充值",
    manual_refund: "退款扣减",
    model_usage_charge: "模型用量",
    redemption_credit: "兑换码充值",
  }[kind];
}
