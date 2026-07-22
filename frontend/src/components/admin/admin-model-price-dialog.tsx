"use client";

import { useEffect, useRef, useState } from "react";
import { CircleDollarSign, Plus } from "lucide-react";
import { toast } from "sonner";
import { AdminLoading, SavingIcon } from "@/components/admin/admin-shared";
import { CursorTableScroll } from "@/components/shared/cursor-table-scroll";
import { EmptyState } from "@/components/shared/empty-state";
import { tableClasses, tableHeadClass } from "@/components/shared/table-styles";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { FormField } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";
import { createAdminModelPrice, listAdminModelPrices, setAdminModelPriceStatus } from "@/lib/api";
import { formatNanos, parseDecimalNanos } from "@/lib/decimal-nanos";
import { formatDateTime } from "@/lib/format";
import type { CursorPage, Model, ModelPriceVersion } from "@/lib/types";

export function ModelPriceDialog({
  model,
  onOpenChange,
}: {
  model: Model | null;
  onOpenChange: (open: boolean) => void;
}) {
  const [prices, setPrices] = useState<ModelPriceVersion[]>([]);
  const [page, setPage] = useState<CursorPage>({ has_more: false });
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadMoreError, setLoadMoreError] = useState("");
  const [saving, setSaving] = useState(false);
  const [showForm, setShowForm] = useState(false);
  const [currency, setCurrency] = useState("USD");
  const [input, setInput] = useState("");
  const [cacheRead, setCacheRead] = useState("");
  const [cacheCreation, setCacheCreation] = useState("");
  const [output, setOutput] = useState("");
  const priceRequestIDRef = useRef(0);

  useEffect(() => {
    if (!model) return;
    const requestID = ++priceRequestIDRef.current;
    let cancelled = false;
    setLoading(true);
    setLoadingMore(false);
    setLoadMoreError("");
    setPrices([]);
    setPage({ has_more: false });
    void listAdminModelPrices(model.id)
      .then((result) => {
        if (!cancelled && requestID === priceRequestIDRef.current) {
          setPrices(result.data);
          setPage(result.page);
        }
      })
      .catch((error) => {
        if (!cancelled && requestID === priceRequestIDRef.current) {
          toast.error(error instanceof Error ? error.message : "价格加载失败");
        }
      })
      .finally(() => {
        if (!cancelled && requestID === priceRequestIDRef.current) setLoading(false);
      });
    return () => {
      cancelled = true;
      if (requestID === priceRequestIDRef.current) priceRequestIDRef.current += 1;
    };
  }, [model]);

  const loadMorePrices = async () => {
    if (!model || !page.next_cursor || loadingMore) return;
    const requestID = priceRequestIDRef.current;
    setLoadingMore(true);
    setLoadMoreError("");
    try {
      const result = await listAdminModelPrices(model.id, page.next_cursor);
      if (requestID !== priceRequestIDRef.current) return;
      setPrices((items) => [...items, ...result.data]);
      setPage(result.page);
    } catch (error) {
      if (requestID !== priceRequestIDRef.current) return;
      setLoadMoreError(error instanceof Error ? error.message : "更多价格版本加载失败");
    } finally {
      if (requestID === priceRequestIDRef.current) setLoadingMore(false);
    }
  };

  const createPrice = async () => {
    if (!model) return;
    setSaving(true);
    try {
      const created = await createAdminModelPrice(model.id, {
        currency: currency.toUpperCase(),
        input_per_million_nanos: parseDecimalNanos(input),
        cache_read_input_per_million_nanos: parseDecimalNanos(cacheRead),
        cache_creation_input_per_million_nanos: parseDecimalNanos(cacheCreation),
        output_per_million_nanos: parseDecimalNanos(output),
      });
      setPrices((items) => [created, ...items]);
      setShowForm(false);
      toast.success("价格草稿已创建");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "价格创建失败");
    } finally {
      setSaving(false);
    }
  };

  const changeStatus = async (price: ModelPriceVersion, action: "publish" | "archive") => {
    if (!model) return;
    try {
      const saved = await setAdminModelPriceStatus(model.id, price.id, action);
      setPrices((items) => items.map((item) => (item.id === saved.id ? saved : item)));
      toast.success(action === "publish" ? "价格已发布" : "价格已归档");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "价格状态更新失败");
    }
  };

  return (
    <Dialog open={!!model} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{model?.display_name} · 价格版本</DialogTitle>
          <DialogDescription>
            价格版本发布后不可修改，新的请求会快照当前生效版本。
          </DialogDescription>
        </DialogHeader>
        <div className="flex justify-end">
          <Button size="sm" variant="outline" onClick={() => setShowForm((value) => !value)}>
            <Plus />
            新建价格
          </Button>
        </div>
        {showForm ? (
          <div className="grid gap-3 py-2 sm:grid-cols-2">
            <FormField label="币种">
              <Input
                value={currency}
                maxLength={3}
                onChange={(event) => setCurrency(event.target.value)}
              />
            </FormField>
            <FormField label="输入 / 1M">
              <Input
                inputMode="decimal"
                value={input}
                onChange={(event) => setInput(event.target.value)}
              />
            </FormField>
            <FormField label="输出 / 1M">
              <Input
                inputMode="decimal"
                value={output}
                onChange={(event) => setOutput(event.target.value)}
              />
            </FormField>
            <FormField label="缓存读取 / 1M">
              <Input
                inputMode="decimal"
                value={cacheRead}
                onChange={(event) => setCacheRead(event.target.value)}
              />
            </FormField>
            <FormField label="缓存创建 / 1M">
              <Input
                inputMode="decimal"
                value={cacheCreation}
                onChange={(event) => setCacheCreation(event.target.value)}
              />
            </FormField>
            <div className="flex items-end">
              <Button
                className="w-full"
                disabled={
                  saving || !currency.trim() || !input || !output || !cacheRead || !cacheCreation
                }
                onClick={() => void createPrice()}
              >
                <SavingIcon saving={saving} />
                创建草稿
              </Button>
            </div>
          </div>
        ) : null}
        {loading ? (
          <AdminLoading />
        ) : prices.length ? (
          <CursorTableScroll
            className="max-h-[min(45vh,28rem)] overflow-auto border-y"
            hasMore={page.has_more}
            loadingMore={loadingMore}
            loadMoreError={loadMoreError}
            onLoadMore={loadMorePrices}
            aria-label="模型价格版本"
          >
            <table className="admin-responsive-table w-[60rem] min-w-full table-fixed text-left text-sm">
              <colgroup>
                <col className="w-[9rem]" />
                <col className="w-[9rem]" />
                <col className="w-[9rem]" />
                <col className="w-[9rem]" />
                <col className="w-[9rem]" />
                <col className="w-[8rem]" />
                <col className="w-[7rem]" />
              </colgroup>
              <thead className={tableHeadClass}>
                <tr className="border-b">
                  <th className={tableClasses.headStart}>版本</th>
                  <th className={`${tableClasses.head} text-right`}>输入</th>
                  <th className={`${tableClasses.head} text-right`}>输出</th>
                  <th className={`${tableClasses.head} text-right`}>缓存读取</th>
                  <th className={`${tableClasses.head} text-right`}>缓存创建</th>
                  <th className={tableClasses.head}>状态</th>
                  <th className={tableClasses.headEnd}>操作</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {prices.map((price) => (
                  <tr key={price.id}>
                    <td className={tableClasses.cellStart} data-primary>
                      <p className="font-mono">v{price.version}</p>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {formatDateTime(price.created_at)}
                      </p>
                    </td>
                    <td className={`${tableClasses.cell} text-right font-mono`} data-label="输入">
                      {price.currency} {formatNanos(price.input_per_million_nanos)}
                    </td>
                    <td className={`${tableClasses.cell} text-right font-mono`} data-label="输出">
                      {price.currency} {formatNanos(price.output_per_million_nanos)}
                    </td>
                    <td
                      className={`${tableClasses.cell} text-right font-mono`}
                      data-label="缓存读取"
                    >
                      {price.currency} {formatNanos(price.cache_read_input_per_million_nanos)}
                    </td>
                    <td
                      className={`${tableClasses.cell} text-right font-mono`}
                      data-label="缓存创建"
                    >
                      {price.currency} {formatNanos(price.cache_creation_input_per_million_nanos)}
                    </td>
                    <td className={tableClasses.cell} data-label="状态">
                      <Badge variant={price.status === "published" ? "secondary" : "outline"}>
                        {price.status}
                      </Badge>
                    </td>
                    <td className={tableClasses.cellEnd} data-actions>
                      {price.status === "draft" ? (
                        <Button
                          size="xs"
                          variant="outline"
                          onClick={() => void changeStatus(price, "publish")}
                        >
                          发布
                        </Button>
                      ) : price.status === "published" ? (
                        <Button
                          size="xs"
                          variant="ghost"
                          onClick={() => void changeStatus(price, "archive")}
                        >
                          归档
                        </Button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CursorTableScroll>
        ) : (
          <EmptyState icon={CircleDollarSign} title="暂无价格版本" />
        )}
      </DialogContent>
    </Dialog>
  );
}
