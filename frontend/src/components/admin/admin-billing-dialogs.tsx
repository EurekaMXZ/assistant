"use client";

import { SavingIcon } from "@/components/admin/admin-shared";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { FormField } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import type { BillingAccount } from "@/lib/types";

export type BillingAdjustment = {
  account: BillingAccount;
  kind: "topups" | "refunds";
};

export function BillingAccountDialog({
  account,
  status,
  saving,
  onClose,
  onSave,
  onStatusChange,
}: {
  account: BillingAccount | null;
  status: BillingAccount["status"];
  saving: boolean;
  onClose: () => void;
  onSave: () => void;
  onStatusChange: (status: BillingAccount["status"]) => void;
}) {
  return (
    <Dialog open={!!account} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>账户设置</DialogTitle>
        </DialogHeader>
        <FormField label="状态" htmlFor="billing-status">
          <Select
            items={[
              { value: "active", label: "正常" },
              { value: "frozen", label: "冻结" },
            ]}
            value={status}
            onValueChange={(value) => value && onStatusChange(value)}
          >
            <SelectTrigger id="billing-status">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="active">正常</SelectItem>
              <SelectItem value="frozen">冻结</SelectItem>
            </SelectContent>
          </Select>
        </FormField>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button disabled={saving} onClick={onSave}>
            <SavingIcon saving={saving} />
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function BillingAdjustmentDialog({
  adjustment,
  amount,
  reason,
  reference,
  saving,
  onAmountChange,
  onClose,
  onReasonChange,
  onReferenceChange,
  onSave,
}: {
  adjustment: BillingAdjustment | null;
  amount: string;
  reason: string;
  reference: string;
  saving: boolean;
  onAmountChange: (amount: string) => void;
  onClose: () => void;
  onReasonChange: (reason: string) => void;
  onReferenceChange: (reference: string) => void;
  onSave: () => void;
}) {
  return (
    <Dialog open={!!adjustment} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{adjustment?.kind === "topups" ? "账户充值" : "退款扣减"}</DialogTitle>
          <DialogDescription>金额将以独立账本记录写入，操作不可直接删除。</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <FormField label={`金额 (${adjustment?.account.currency})`} htmlFor="billing-amount">
            <Input
              id="billing-amount"
              inputMode="decimal"
              value={amount}
              onChange={(event) => onAmountChange(event.target.value)}
            />
          </FormField>
          <FormField label="原因" htmlFor="billing-reason">
            <Textarea
              id="billing-reason"
              value={reason}
              onChange={(event) => onReasonChange(event.target.value)}
            />
          </FormField>
          <FormField label="外部参考" htmlFor="billing-reference">
            <Input
              id="billing-reference"
              value={reference}
              onChange={(event) => onReferenceChange(event.target.value)}
            />
          </FormField>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button
            variant={adjustment?.kind === "refunds" ? "destructive" : "default"}
            disabled={saving || !amount || !reason.trim()}
            onClick={onSave}
          >
            <SavingIcon saving={saving} />
            确认入账
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
