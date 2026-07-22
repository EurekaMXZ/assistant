"use client";

import { useEffect, useState } from "react";
import { Ban, Check, Copy, Gift, Plus, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { SavingIcon } from "./admin-shared";
import { AdminListPage } from "@/components/admin/admin-list-page";
import { tableClasses, tableHeadClass } from "@/components/shared/table-styles";
import { Spinner } from "@/components/shared/spinner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { FormField } from "@/components/ui/form-field";
import { Textarea } from "@/components/ui/textarea";
import {
  disableAdminBillingRedemptionCode,
  issueAdminBillingRedemptionCodes,
  listAdminBillingRedemptionCodes,
} from "@/lib/api";
import { parseDecimalNanos } from "@/lib/decimal-nanos";
import { formatDateTime } from "@/lib/format";
import { useCopyToClipboard } from "@/hooks/use-copy-to-clipboard";
import type {
  BillingRedemptionCode,
  BillingRedemptionCodeIssue,
  CursorPage,
  User,
} from "@/lib/types";

interface AdminRedemptionCodesProps {
  users: User[];
}

export function AdminRedemptionCodes({ users }: AdminRedemptionCodesProps) {
  const [codes, setCodes] = useState<BillingRedemptionCode[]>([]);
  const [page, setPage] = useState<CursorPage>({ has_more: false });
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadMoreError, setLoadMoreError] = useState("");
  const [error, setError] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [amount, setAmount] = useState("");
  const [quantity, setQuantity] = useState("1");
  const [expiresAt, setExpiresAt] = useState("");
  const [saving, setSaving] = useState(false);
  const [issued, setIssued] = useState<BillingRedemptionCodeIssue[]>([]);
  const { copied, copyToClipboard, resetCopied } = useCopyToClipboard({
    successMessage: "兑换码已复制",
    errorMessage: "无法自动复制，请手动选择兑换码",
    resetAfter: false,
  });
  const [confirmedSaved, setConfirmedSaved] = useState(false);
  const [disableTarget, setDisableTarget] = useState<BillingRedemptionCode | null>(null);
  const [disabling, setDisabling] = useState("");
  const userMap = new Map(users.map((user) => [user.id, user]));

  const load = async () => {
    setLoading(true);
    setError("");
    setLoadMoreError("");
    try {
      const result = await listAdminBillingRedemptionCodes();
      setCodes(result.data);
      setPage(result.page);
    } catch (err) {
      setError(err instanceof Error ? err.message : "兑换码加载失败");
    } finally {
      setLoading(false);
    }
  };

  const loadMore = async () => {
    if (!page.next_cursor || loadingMore) return;
    setLoadingMore(true);
    setLoadMoreError("");
    try {
      const result = await listAdminBillingRedemptionCodes(page.next_cursor);
      setCodes((items) => [...items, ...result.data]);
      setPage(result.page);
    } catch (err) {
      setLoadMoreError(err instanceof Error ? err.message : "更多兑换码加载失败");
    } finally {
      setLoadingMore(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const openCreate = () => {
    setAmount("");
    setQuantity("1");
    setExpiresAt("");
    setCreateOpen(true);
  };

  const create = async (event: React.FormEvent) => {
    event.preventDefault();
    if (saving) return;
    setSaving(true);
    try {
      if (parseDecimalNanos(amount) <= 0) throw new Error("金额必须大于 0");
      const parsedQuantity = Number(quantity);
      if (!Number.isInteger(parsedQuantity) || parsedQuantity < 1 || parsedQuantity > 100) {
        throw new Error("数量必须是 1 到 100 之间的整数");
      }
      let expires_at: string | undefined;
      if (expiresAt) {
        const expiration = new Date(expiresAt);
        if (!Number.isFinite(expiration.getTime()) || expiration.getTime() <= Date.now()) {
          throw new Error("过期时间必须晚于当前时间");
        }
        expires_at = expiration.toISOString();
      }
      const result = await issueAdminBillingRedemptionCodes({
        amount,
        quantity: parsedQuantity,
        expires_at,
      });
      setCodes((items) => [...result.map((item) => item.redemption_code), ...items]);
      setCreateOpen(false);
      resetCopied();
      setConfirmedSaved(false);
      setIssued(result);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "兑换码生成失败");
    } finally {
      setSaving(false);
    }
  };

  const copyIssuedCode = async () => {
    if (!issued.length) return;
    const plaintext = issued.map((item) => item.code).join("\n");
    if (!(await copyToClipboard(plaintext))) return;
    setConfirmedSaved(true);
  };

  const disableCode = async () => {
    const target = disableTarget;
    if (!target || disabling) return;
    setDisabling(target.id);
    try {
      const updated = await disableAdminBillingRedemptionCode(target.id);
      setCodes((items) => items.map((item) => (item.id === updated.id ? updated : item)));
      toast.success("兑换码已禁用");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "兑换码禁用失败");
    } finally {
      setDisabling("");
    }
  };

  return (
    <div className="mt-5">
      <div className="mb-4 flex justify-end gap-2">
        <Button variant="outline" size="sm" onClick={() => void load()}>
          <RefreshCw className="size-4" />
          刷新
        </Button>
        <Button size="sm" onClick={openCreate}>
          <Plus className="size-4" />
          生成兑换码
        </Button>
      </div>

      <AdminListPage
        ariaLabel="兑换码"
        emptyIcon={Gift}
        emptyTitle="暂无兑换码"
        error={error}
        hasItems={codes.length > 0}
        hasMore={page.has_more}
        loading={loading}
        loadingMore={loadingMore}
        loadMoreError={loadMoreError}
        onLoadMore={loadMore}
        onRetry={load}
      >
        <table className="admin-responsive-table w-[87rem] min-w-full table-fixed text-left text-sm">
          <colgroup>
            <col className="w-[18rem]" />
            <col className="w-[12rem]" />
            <col className="w-[8rem]" />
            <col className="w-[14rem]" />
            <col className="w-[14rem]" />
            <col className="w-[14rem]" />
            <col className="w-[7rem]" />
          </colgroup>
          <thead className={tableHeadClass}>
            <tr className="border-b">
              <th className={tableClasses.headStart}>兑换码</th>
              <th className={`${tableClasses.head} text-right`}>金额</th>
              <th className={tableClasses.head}>状态</th>
              <th className={tableClasses.head}>兑换用户</th>
              <th className={tableClasses.head}>过期时间</th>
              <th className={tableClasses.head}>生成时间</th>
              <th className={tableClasses.headEnd}>操作</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {codes.map((item) => {
              const status = item.status;
              return (
                <tr key={item.id}>
                  <td className={`${tableClasses.cellStart} font-mono text-xs`} data-primary>
                    {item.code_hint}
                  </td>
                  <td
                    className={`${tableClasses.cell} whitespace-nowrap text-right font-mono`}
                    data-label="金额"
                  >
                    {item.currency} {item.amount}
                  </td>
                  <td className={tableClasses.cell} data-label="状态">
                    <Badge
                      variant={
                        status === "active"
                          ? "secondary"
                          : status === "disabled"
                            ? "destructive"
                            : "outline"
                      }
                    >
                      {status === "active"
                        ? "可兑换"
                        : status === "disabled"
                          ? "已禁用"
                          : status === "expired"
                            ? "已过期"
                            : "已兑换"}
                    </Badge>
                  </td>
                  <td
                    className={`${tableClasses.cell} truncate`}
                    data-label="兑换用户"
                    title={
                      item.redeemed_by_user_id
                        ? userMap.get(item.redeemed_by_user_id)?.username ||
                          item.redeemed_by_user_id
                        : ""
                    }
                  >
                    {item.redeemed_by_user_id
                      ? userMap.get(item.redeemed_by_user_id)?.username ||
                        item.redeemed_by_user_id.slice(0, 8)
                      : "-"}
                  </td>
                  <td
                    className={`${tableClasses.cell} whitespace-nowrap text-xs text-muted-foreground`}
                    data-label="过期时间"
                  >
                    {formatDateTime(item.expires_at)}
                  </td>
                  <td
                    className={`${tableClasses.cell} whitespace-nowrap text-xs text-muted-foreground`}
                    data-label="生成时间"
                  >
                    {formatDateTime(item.created_at)}
                  </td>
                  <td className={tableClasses.cellEnd} data-actions>
                    {status === "active" ? (
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="text-destructive hover:text-destructive"
                        disabled={disabling === item.id}
                        onClick={() => setDisableTarget(item)}
                      >
                        {disabling === item.id ? <Spinner /> : <Ban className="size-4" />}
                        禁用
                      </Button>
                    ) : (
                      "-"
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </AdminListPage>

      <Dialog open={createOpen} onOpenChange={(open) => !saving && setCreateOpen(open)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>生成兑换码</DialogTitle>
            <DialogDescription>
              兑换码只能使用一次，金额和过期时间生成后不可修改。
            </DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={create}>
            <FormField label="单个面额" htmlFor="redemption-amount">
              <Input
                id="redemption-amount"
                inputMode="decimal"
                placeholder="10.00"
                value={amount}
                onChange={(event) => setAmount(event.target.value)}
              />
            </FormField>
            <FormField label="数量" htmlFor="redemption-quantity">
              <Input
                id="redemption-quantity"
                type="number"
                inputMode="numeric"
                min={1}
                max={100}
                step={1}
                value={quantity}
                onChange={(event) => setQuantity(event.target.value)}
              />
            </FormField>
            <FormField label="过期时间（可选）" htmlFor="redemption-expires-at">
              <Input
                id="redemption-expires-at"
                type="datetime-local"
                value={expiresAt}
                onChange={(event) => setExpiresAt(event.target.value)}
              />
            </FormField>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                disabled={saving}
                onClick={() => setCreateOpen(false)}
              >
                取消
              </Button>
              <Button type="submit" disabled={saving || !amount || !quantity}>
                <SavingIcon saving={saving} />
                生成
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog
        open={issued.length > 0}
        onOpenChange={(open) => {
          if (!open && confirmedSaved) {
            setIssued([]);
            resetCopied();
            setConfirmedSaved(false);
          }
        }}
      >
        <DialogContent showCloseButton={false}>
          <DialogHeader>
            <DialogTitle>兑换码已生成</DialogTitle>
            <DialogDescription>
              已生成 {issued.length} 个兑换码。完整内容仅在此处显示一次，请立即复制并妥善保存。
            </DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2">
            <Label htmlFor="issued-redemption-code" className="sr-only">
              新生成的兑换码
            </Label>
            <Textarea
              id="issued-redemption-code"
              readOnly
              className="max-h-64 min-h-28 resize-none font-mono text-xs"
              value={issued.map((item) => item.code).join("\n")}
            />
            <Button
              type="button"
              variant="outline"
              size="icon"
              aria-label="复制兑换码"
              onClick={() => void copyIssuedCode()}
            >
              {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
            </Button>
          </div>
          <label className="flex items-start gap-2 text-sm">
            <input
              type="checkbox"
              className="mt-0.5 size-4 accent-primary"
              checked={confirmedSaved}
              onChange={(event) => setConfirmedSaved(event.target.checked)}
            />
            <span>我已保存兑换码，关闭后无法再次查看完整内容</span>
          </label>
          <DialogFooter>
            <Button disabled={!confirmedSaved} onClick={() => setIssued([])}>
              完成
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={disableTarget !== null}
        onOpenChange={(open) => !open && setDisableTarget(null)}
        title="禁用兑换码"
        description={`禁用后该兑换码将无法使用，且不能恢复。${disableTarget ? ` 金额：${disableTarget.currency} ${disableTarget.amount}` : ""}`}
        confirmText="确认禁用"
        destructive
        onConfirm={() => void disableCode()}
      />
    </div>
  );
}
