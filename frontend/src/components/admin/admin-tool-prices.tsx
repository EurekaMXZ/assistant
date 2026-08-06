"use client";

import { useEffect, useState } from "react";
import {
  Box,
  CircleHelp,
  Clock3,
  Download,
  FileText,
  ImageIcon,
  Pencil,
  Plug,
  Search,
  Terminal,
  Trash2,
  Upload,
} from "lucide-react";
import { toast } from "sonner";
import { SavingIcon } from "./admin-shared";
import { AdminListPage } from "@/components/admin/admin-list-page";
import { tableClasses, tableHeadClass } from "@/components/shared/table-styles";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { listAdminBillingToolPrices, updateAdminBillingToolPrices } from "@/lib/api";
import { parseDecimalNanos } from "@/lib/decimal-nanos";
import { formatDateTime } from "@/lib/format";
import type { BillingToolKey, BillingToolPrice } from "@/lib/types";

const toolMeta = {
  "sandbox.create": { name: "Sandbox Create", icon: Box },
  image_generation: { name: "Image Generation", icon: ImageIcon },
  "tavily.search": { name: "Tavily Search", icon: Search },
  "tavily.extract": { name: "Tavily Extract", icon: FileText },
  "conversation.rename_title": { name: "Rename Conversation", icon: Pencil },
  "conversation.export_text": { name: "Export Text", icon: Download },
  "sandbox.destroy": { name: "Destroy Sandbox", icon: Trash2 },
  "sandbox.shell_create": { name: "Create Shell", icon: Terminal },
  "sandbox.shell_connect": { name: "Run Shell Command", icon: Terminal },
  "sandbox.shell_destroy": { name: "Close Shell", icon: Plug },
  "sandbox.write_file": { name: "Write Sandbox File", icon: Upload },
  "sandbox.edit_file": { name: "Edit Sandbox File", icon: Pencil },
  "sandbox.read_file": { name: "Read Sandbox File", icon: FileText },
  "sandbox.import_attachment": { name: "Import Attachment", icon: Upload },
  "sandbox.export_file": { name: "Export Sandbox File", icon: Download },
  "time.now": { name: "Current Time", icon: Clock3 },
  ask_user: { name: "Ask User", icon: CircleHelp },
} satisfies Record<BillingToolKey, { name: string; icon: typeof Box }>;

interface ToolPriceDraft {
  amount: string;
  billingEnabled: boolean;
  toolEnabled: boolean;
}

function priceDrafts(items: BillingToolPrice[]) {
  return Object.fromEntries(
    items.map((item) => [
      item.tool_key,
      {
        amount: item.price_per_call,
        billingEnabled: item.enabled,
        toolEnabled: item.tool_enabled,
      },
    ]),
  );
}

export function AdminToolPrices() {
  const [prices, setPrices] = useState<BillingToolPrice[]>([]);
  const [drafts, setDrafts] = useState<Partial<Record<BillingToolKey, ToolPriceDraft>>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [loadAttempt, setLoadAttempt] = useState(0);

  useEffect(() => {
    const load = async () => {
      setLoading(true);
      setError("");
      try {
        const items = await listAdminBillingToolPrices();
        setPrices(items);
        setDrafts(priceDrafts(items));
      } catch (err) {
        setError(err instanceof Error ? err.message : "工具价格加载失败");
      } finally {
        setLoading(false);
      }
    };
    void load();
  }, [loadAttempt]);

  const updateDraft = (key: BillingToolKey, patch: Partial<ToolPriceDraft>) => {
    setDrafts((current) => ({
      ...current,
      [key]: { amount: "", billingEnabled: false, toolEnabled: false, ...current[key], ...patch },
    }));
  };

  const save = async (event: React.FormEvent) => {
    event.preventDefault();
    setSaving(true);
    try {
      const payload = prices.map((price) => {
        const draft = drafts[price.tool_key];
        if (!draft) throw new Error(`缺少 ${toolMeta[price.tool_key].name} 配置`);
        const pricePerCall = parseDecimalNanos(draft.amount);
        if (draft.billingEnabled && pricePerCall <= 0)
          throw new Error("启用计费时单次价格必须大于 0");
        return {
          tool_key: price.tool_key,
          enabled: draft.billingEnabled,
          tool_enabled: draft.toolEnabled,
          price_per_call_nanos: pricePerCall,
          version: price.version,
        };
      });
      const items = await updateAdminBillingToolPrices(payload);
      setPrices(items);
      setDrafts(priceDrafts(items));
      toast.success("工具计费方案已保存");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "工具计费方案保存失败");
    } finally {
      setSaving(false);
    }
  };

  return (
    <form className="mt-5" onSubmit={save}>
      <AdminListPage
        ariaLabel="工具计费"
        emptyIcon={Box}
        emptyTitle="暂无工具计费配置"
        error={error}
        hasItems={prices.length > 0}
        loading={loading}
        onRetry={() => setLoadAttempt((value) => value + 1)}
      >
        <table className="admin-responsive-table w-[62rem] min-w-full table-fixed text-left text-sm">
          <colgroup>
            <col className="w-[18rem]" />
            <col className="w-[16rem]" />
            <col className="w-[9rem]" />
            <col className="w-[9rem]" />
            <col className="w-[12rem]" />
            <col className="w-[8rem]" />
          </colgroup>
          <thead className={tableHeadClass}>
            <tr className="border-b">
              <th className={tableClasses.headStart}>工具</th>
              <th className={tableClasses.head}>计费键</th>
              <th className={tableClasses.head}>工具启用</th>
              <th className={tableClasses.head}>计费启用</th>
              <th className={`${tableClasses.head} text-right`}>
                单次价格 ({prices[0]?.currency || "-"})
              </th>
              <th className={tableClasses.headEnd}>版本</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {prices.map((price) => {
              const meta = toolMeta[price.tool_key];
              const Icon = meta.icon;
              const draft = drafts[price.tool_key];
              return (
                <tr key={price.tool_key}>
                  <td className={tableClasses.cellStart} data-primary>
                    <span className="inline-flex items-center gap-2 font-medium">
                      <Icon className="size-4 text-muted-foreground" />
                      {meta.name}
                    </span>
                  </td>
                  <td
                    className={`${tableClasses.cell} font-mono text-xs text-muted-foreground`}
                    data-label="计费键"
                  >
                    {price.tool_key}
                  </td>
                  <td className={tableClasses.cell} data-label="工具启用">
                    <label className="inline-flex items-center gap-2">
                      <input
                        type="checkbox"
                        className="size-4 accent-foreground"
                        disabled={saving}
                        checked={draft?.toolEnabled || false}
                        onChange={(event) =>
                          updateDraft(price.tool_key, { toolEnabled: event.target.checked })
                        }
                      />
                      <span>{draft?.toolEnabled ? "可用" : "停用"}</span>
                    </label>
                  </td>
                  <td className={tableClasses.cell} data-label="计费启用">
                    <label className="inline-flex items-center gap-2">
                      <input
                        type="checkbox"
                        className="size-4 accent-foreground"
                        disabled={saving}
                        checked={draft?.billingEnabled || false}
                        onChange={(event) =>
                          updateDraft(price.tool_key, { billingEnabled: event.target.checked })
                        }
                      />
                      <span>{draft?.billingEnabled ? "计费" : "免费"}</span>
                    </label>
                  </td>
                  <td className={tableClasses.cell} data-label="单次价格">
                    <Input
                      className="ml-auto h-8 w-40 text-right font-mono"
                      inputMode="decimal"
                      aria-label={`${meta.name} 单次价格`}
                      disabled={saving}
                      value={draft?.amount || ""}
                      onChange={(event) =>
                        updateDraft(price.tool_key, { amount: event.target.value })
                      }
                    />
                  </td>
                  <td
                    className={`${tableClasses.cellEnd} text-xs text-muted-foreground`}
                    data-label="版本"
                  >
                    <p>v{price.version}</p>
                    <p className="mt-0.5">{formatDateTime(price.updated_at)}</p>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </AdminListPage>
      {!loading && !error ? (
        <div className="mt-4 flex justify-end">
          <Button type="submit" disabled={saving || prices.length === 0}>
            <SavingIcon saving={saving} />
            保存方案
          </Button>
        </div>
      ) : null}
    </form>
  );
}
