"use client";

import { useEffect, useState } from "react";
import { Box, FileText, ImageIcon, Search } from "lucide-react";
import { toast } from "sonner";
import { AdminError, AdminLoading, SavingIcon, formatAdminDate } from "./admin-shared";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { listAdminBillingToolPrices, updateAdminBillingToolPrices } from "@/lib/api";
import { parseDecimalNanos } from "@/lib/decimal-nanos";
import type { BillingToolKey, BillingToolPrice } from "@/lib/types";

const toolMeta = {
  "sandbox.create": { name: "Sandbox Create", icon: Box },
  image_generation: { name: "Image Generation", icon: ImageIcon },
  "tavily.search": { name: "Tavily Search", icon: Search },
  "tavily.extract": { name: "Tavily Extract", icon: FileText },
} satisfies Record<BillingToolKey, { name: string; icon: typeof Box }>;

interface ToolPriceDraft {
  amount: string;
  enabled: boolean;
}

function priceDrafts(items: BillingToolPrice[]) {
  return Object.fromEntries(
    items.map((item) => [item.tool_key, { amount: item.price_per_call, enabled: item.enabled }]),
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
      [key]: { amount: "", enabled: false, ...current[key], ...patch },
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
        if (draft.enabled && pricePerCall <= 0) throw new Error("启用计费时单次价格必须大于 0");
        return {
          tool_key: price.tool_key,
          enabled: draft.enabled,
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

  if (loading) return <AdminLoading />;
  if (error) {
    return <AdminError message={error} onRetry={() => setLoadAttempt((value) => value + 1)} />;
  }

  return (
    <form className="mt-5" onSubmit={save}>
      <div className="overflow-x-auto border-y">
        <table className="w-full min-w-[720px] text-left text-sm">
          <thead className="text-xs text-muted-foreground">
            <tr className="border-b">
              <th className="py-3 pr-4 font-medium">工具</th>
              <th className="px-4 py-3 font-medium">计费键</th>
              <th className="px-4 py-3 font-medium">启用</th>
              <th className="px-4 py-3 text-right font-medium">
                单次价格 ({prices[0]?.currency || "-"})
              </th>
              <th className="py-3 pl-4 text-right font-medium">版本</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {prices.map((price) => {
              const meta = toolMeta[price.tool_key];
              const Icon = meta.icon;
              const draft = drafts[price.tool_key];
              return (
                <tr key={price.tool_key}>
                  <td className="py-3 pr-4">
                    <span className="inline-flex items-center gap-2 font-medium">
                      <Icon className="size-4 text-muted-foreground" />
                      {meta.name}
                    </span>
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
                    {price.tool_key}
                  </td>
                  <td className="px-4 py-3">
                    <label className="inline-flex items-center gap-2">
                      <input
                        type="checkbox"
                        className="size-4 accent-foreground"
                        disabled={saving}
                        checked={draft?.enabled || false}
                        onChange={(event) =>
                          updateDraft(price.tool_key, { enabled: event.target.checked })
                        }
                      />
                      <span>{draft?.enabled ? "计费" : "停用"}</span>
                    </label>
                  </td>
                  <td className="px-4 py-3">
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
                  <td className="py-3 pl-4 text-right text-xs text-muted-foreground">
                    <p>v{price.version}</p>
                    <p className="mt-0.5">{formatAdminDate(price.updated_at)}</p>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <div className="mt-4 flex justify-end">
        <Button type="submit" disabled={saving || prices.length === 0}>
          <SavingIcon saving={saving} />
          保存方案
        </Button>
      </div>
    </form>
  );
}
