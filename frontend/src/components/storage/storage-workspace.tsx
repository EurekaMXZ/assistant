"use client";

import { useCallback, useEffect, useState } from "react";
import { Download, File, HardDrive, Loader2, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  deleteStorageAttachment,
  getConversationAttachmentUrl,
  getStorageOverview,
} from "@/lib/api";
import type { StorageAttachment, StorageUsage } from "@/lib/types";
import { formatStorageBytes } from "@/lib/storage";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Skeleton } from "@/components/ui/skeleton";

export function StorageWorkspace() {
  const [usage, setUsage] = useState<StorageUsage | null>(null);
  const [items, setItems] = useState<StorageAttachment[]>([]);
  const [nextCursor, setNextCursor] = useState<string | undefined>();
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loadMoreError, setLoadMoreError] = useState("");
  const [deleteItem, setDeleteItem] = useState<StorageAttachment | null>(null);
  const [deletingID, setDeletingID] = useState<string | null>(null);

  const load = useCallback(async (cursor?: string) => {
    if (cursor) {
      setLoadingMore(true);
      setLoadMoreError("");
    } else {
      setLoading(true);
      setError(null);
    }

    try {
      const result = await getStorageOverview(cursor);
      setUsage(result.storage);
      setItems((current) => (cursor ? [...current, ...result.data] : result.data));
      setNextCursor(result.page.next_cursor);
    } catch (err) {
      const message = err instanceof Error ? err.message : "存储空间加载失败";
      if (cursor) setLoadMoreError(message);
      else setError(message);
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const remove = async () => {
    if (!deleteItem) return;
    const item = deleteItem;
    setDeletingID(item.id);
    try {
      await deleteStorageAttachment(item.id);
      setItems((current) => current.filter((attachment) => attachment.id !== item.id));
      setUsage((current) =>
        current
          ? {
              ...current,
              used_bytes: Math.max(0, current.used_bytes - item.size_bytes),
              available_bytes: Math.min(
                current.quota_bytes,
                current.available_bytes + item.size_bytes,
              ),
            }
          : current,
      );
      setDeleteItem(null);
      toast.success("附件已删除");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "附件删除失败");
    } finally {
      setDeletingID(null);
    }
  };

  const download = async (item: StorageAttachment) => {
    try {
      const url = await getConversationAttachmentUrl(item.conversation_id, item.id, true);
      window.open(url, "_blank", "noopener,noreferrer");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "附件下载失败");
    }
  };

  const percent = usage
    ? Math.min(100, usage.quota_bytes > 0 ? (usage.used_bytes / usage.quota_bytes) * 100 : 0)
    : 0;

  return (
    <div className="h-full min-h-0 overflow-y-auto">
      <div className="mx-auto min-w-0 w-full max-w-[1440px] px-4 py-6 sm:px-6 lg:px-10 lg:py-9">
        <header className="flex flex-col gap-4 border-b pb-5 sm:flex-row sm:items-center sm:justify-between">
          <h1 className="text-2xl font-semibold">存储空间</h1>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            title="刷新存储空间"
            aria-label="刷新存储空间"
            onClick={() => void load()}
            disabled={loading || loadingMore}
          >
            <RefreshCw className={loading ? "size-4 animate-spin" : "size-4"} />
          </Button>
        </header>

        {loading && !usage ? (
          <div className="space-y-7 pt-6">
            <Skeleton className="h-28 w-full" />
            <Skeleton className="h-9 w-24" />
            <div className="space-y-3">
              <Skeleton className="h-12 w-full" />
              <Skeleton className="h-12 w-full" />
              <Skeleton className="h-12 w-full" />
            </div>
          </div>
        ) : error && !usage ? (
          <div className="flex min-h-72 flex-col items-center justify-center text-center">
            <HardDrive className="size-6 text-muted-foreground" />
            <p className="mt-3 text-sm font-medium">{error}</p>
            <Button className="mt-4" variant="outline" size="sm" onClick={() => void load()}>
              <RefreshCw className="size-4" />
              重新加载
            </Button>
          </div>
        ) : (
          <div className="space-y-7 pt-7">
            <section
              className="grid gap-5 border-b pb-5 sm:grid-cols-[minmax(0,1fr)_minmax(18rem,24rem)] sm:items-end"
              aria-label="存储用量"
            >
              <div>
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <HardDrive className="size-4" />
                  存储用量
                </div>
                <p className="mt-2 font-mono text-3xl font-semibold leading-none">
                  {formatStorageBytes(usage?.used_bytes || 0)}
                  <span className="ml-1 text-sm font-normal text-muted-foreground">
                    / {formatStorageBytes(usage?.quota_bytes || 0)}
                  </span>
                </p>
              </div>
              <div>
                <div className="flex items-center justify-between gap-3 text-sm">
                  <span className="text-muted-foreground">可用</span>
                  <span className="font-mono">
                    {formatStorageBytes(usage?.available_bytes || 0)}
                  </span>
                </div>
                <div className="mt-3 h-2 overflow-hidden rounded-full bg-muted">
                  <div
                    className={percent >= 90 ? "h-full bg-destructive" : "h-full bg-foreground"}
                    style={{ width: `${percent}%` }}
                  />
                </div>
                <p className="mt-1 text-right text-xs text-muted-foreground">
                  {Math.round(percent)}% 已使用
                </p>
              </div>
            </section>

            <section aria-label="附件">
              {items.length === 0 ? (
                <div className="flex min-h-40 flex-col items-center justify-center border-b text-center">
                  <File className="size-5 text-muted-foreground" />
                  <p className="mt-2 text-sm text-muted-foreground">暂无附件</p>
                </div>
              ) : (
                <div className="overflow-x-auto border-b" aria-label="附件列表" tabIndex={0}>
                  <div className="divide-y sm:hidden">
                    {items.map((item) => (
                      <StorageAttachmentRow
                        key={item.id}
                        item={item}
                        deleting={deletingID === item.id}
                        onDownload={download}
                        onDelete={() => setDeleteItem(item)}
                      />
                    ))}
                  </div>
                  <table className="hidden w-full min-w-[52rem] table-fixed text-left text-sm sm:table">
                    <colgroup>
                      <col className="w-[35%]" />
                      <col className="w-[35%]" />
                      <col className="w-[15%]" />
                      <col className="w-[15%]" />
                    </colgroup>
                    <thead className="text-xs text-muted-foreground">
                      <tr className="border-b">
                        <th className="py-3 pr-4 font-medium">文件</th>
                        <th className="px-4 py-3 font-medium">对话</th>
                        <th className="px-4 py-3 text-right font-medium">大小</th>
                        <th className="py-3 pl-4 text-right font-medium">操作</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y">
                      {items.map((item) => (
                        <tr key={item.id}>
                          <td className="py-3 pr-4">
                            <p className="truncate font-medium" title={item.filename}>
                              {item.filename}
                            </p>
                            <p className="mt-1 text-xs text-muted-foreground">
                              {formatAttachmentDate(item.created_at)}
                            </p>
                          </td>
                          <td
                            className="truncate px-4 py-3 text-muted-foreground"
                            title={item.conversation_title || "新会话"}
                          >
                            {item.conversation_title || "新会话"}
                          </td>
                          <td className="whitespace-nowrap px-4 py-3 text-right font-mono text-xs text-muted-foreground">
                            {formatStorageBytes(item.size_bytes)}
                          </td>
                          <td className="py-3 pl-4 text-right">
                            <StorageAttachmentActions
                              item={item}
                              deleting={deletingID === item.id}
                              onDownload={download}
                              onDelete={() => setDeleteItem(item)}
                            />
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}

              {nextCursor || loadMoreError ? (
                <div className="flex flex-col items-center gap-2 pt-5">
                  {loadMoreError ? (
                    <p className="text-sm text-destructive" role="alert">
                      {loadMoreError}
                    </p>
                  ) : null}
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={loadingMore}
                    onClick={() => nextCursor && void load(nextCursor)}
                  >
                    {loadingMore ? <Loader2 className="size-4 animate-spin" /> : null}
                    {loadMoreError ? "重试" : "加载更多"}
                  </Button>
                </div>
              ) : null}
            </section>
          </div>
        )}
      </div>

      <ConfirmDialog
        open={deleteItem !== null}
        onOpenChange={(open) => !open && setDeleteItem(null)}
        title="删除附件"
        description={`确认删除“${deleteItem?.filename || "此附件"}”吗？删除后无法恢复。`}
        confirmText="删除"
        destructive
        onConfirm={() => void remove()}
      />
    </div>
  );
}

function formatAttachmentDate(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

function StorageAttachmentRow({
  item,
  deleting,
  onDownload,
  onDelete,
}: {
  item: StorageAttachment;
  deleting: boolean;
  onDownload: (item: StorageAttachment) => void;
  onDelete: () => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4 py-3">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium" title={item.filename}>
          {item.filename}
        </p>
        <p className="mt-1 truncate text-xs text-muted-foreground">
          {item.conversation_title || "新会话"} · {formatStorageBytes(item.size_bytes)}
        </p>
      </div>
      <StorageAttachmentActions
        item={item}
        deleting={deleting}
        onDownload={onDownload}
        onDelete={onDelete}
      />
    </div>
  );
}

function StorageAttachmentActions({
  item,
  deleting,
  onDownload,
  onDelete,
}: {
  item: StorageAttachment;
  deleting: boolean;
  onDownload: (item: StorageAttachment) => void;
  onDelete: () => void;
}) {
  return (
    <span className="inline-flex shrink-0 items-center gap-1">
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        title={`下载 ${item.filename}`}
        aria-label={`下载 ${item.filename}`}
        onClick={() => onDownload(item)}
      >
        <Download className="size-4" />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        title={`删除 ${item.filename}`}
        aria-label={`删除 ${item.filename}`}
        disabled={deleting}
        className="text-muted-foreground hover:text-destructive"
        onClick={onDelete}
      >
        {deleting ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
      </Button>
    </span>
  );
}
