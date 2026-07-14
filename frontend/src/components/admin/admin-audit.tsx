"use client";

import { useDeferredValue, useState } from "react";
import { Activity, Eye, Search } from "lucide-react";
import {
  AdminEmpty,
  AdminError,
  AdminLoading,
  AdminPageHeader,
  adminTableHeadClass,
  adminTableScrollClass,
  formatAdminDate,
} from "@/components/admin/admin-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { CursorTableScroll } from "@/components/ui/cursor-table-scroll";
import { listAdminAuditEventsPage } from "@/lib/api";
import type { AuditEvent } from "@/lib/types";
import { useCursorPagination } from "@/lib/use-cursor-pagination";

export function AdminAudit() {
  const { items, page, loading, loadingMore, error, loadMoreError, loadMore, reload } =
    useCursorPagination<AuditEvent>(listAdminAuditEventsPage, "审计日志加载失败");
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<AuditEvent | null>(null);
  const deferredQuery = useDeferredValue(query.trim().toLowerCase());

  const filtered = deferredQuery
    ? items.filter((item) =>
        [
          item.action,
          item.resource_type,
          item.resource_id,
          item.actor_user_id,
          item.subject_user_id,
          item.outcome,
        ].some((value) => value?.toLowerCase().includes(deferredQuery)),
      )
    : items;

  return (
    <div>
      <AdminPageHeader title="审计日志" />
      <div className="mt-5 max-w-sm">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="搜索操作、资源或用户 ID"
            className="pl-9"
          />
        </div>
      </div>
      {loading ? <AdminLoading /> : null}
      {!loading && error ? <AdminError message={error} onRetry={reload} /> : null}
      {!loading && !error && !filtered.length ? (
        <AdminEmpty icon={Activity} title={query ? "没有匹配的审计记录" : "暂无审计记录"} />
      ) : null}
      {!loading && !error && filtered.length ? (
        <CursorTableScroll
          className={`${adminTableScrollClass} mt-5`}
          hasMore={page.has_more}
          loadingMore={loadingMore}
          loadMoreError={loadMoreError}
          onLoadMore={loadMore}
          aria-label="审计日志"
        >
          <table className="w-[86rem] min-w-full table-fixed text-left text-sm">
            <colgroup>
              <col className="w-[11rem]" />
              <col className="w-[18rem]" />
              <col className="w-[17rem]" />
              <col className="w-[14rem]" />
              <col className="w-[13rem]" />
              <col className="w-[8rem]" />
              <col className="w-[5rem]" />
            </colgroup>
            <thead className={adminTableHeadClass}>
              <tr className="border-b">
                <th className="py-3 pr-4 font-medium">时间</th>
                <th className="px-4 py-3 font-medium">操作</th>
                <th className="px-4 py-3 font-medium">资源</th>
                <th className="px-4 py-3 font-medium">操作者</th>
                <th className="px-4 py-3 font-medium">请求</th>
                <th className="px-4 py-3 font-medium">结果</th>
                <th className="py-3 pl-4 text-right font-medium">详情</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {filtered.map((item) => (
                <tr key={item.id}>
                  <td className="whitespace-nowrap py-3 pr-4 text-xs text-muted-foreground">
                    {formatAdminDate(item.created_at)}
                  </td>
                  <td className="truncate px-4 py-3 font-medium" title={item.action}>
                    {item.action}
                  </td>
                  <td className="px-4 py-3">
                    <p className="truncate" title={item.resource_type || ""}>
                      {item.resource_type || "-"}
                    </p>
                    <p
                      className="mt-0.5 truncate font-mono text-xs text-muted-foreground"
                      title={item.resource_id || ""}
                    >
                      {item.resource_id || ""}
                    </p>
                  </td>
                  <td className="px-4 py-3">
                    <p>{item.actor_role || "-"}</p>
                    <p
                      className="mt-0.5 truncate font-mono text-xs text-muted-foreground"
                      title={item.actor_user_id || ""}
                    >
                      {item.actor_user_id || ""}
                    </p>
                  </td>
                  <td className="px-4 py-3">
                    <p className="font-mono text-xs">
                      {item.request_id ? item.request_id.slice(0, 12) : "-"}
                    </p>
                    <p className="mt-0.5 text-xs text-muted-foreground">{item.client_ip || ""}</p>
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={item.outcome === "succeeded" ? "secondary" : "destructive"}>
                      {item.outcome}
                    </Badge>
                  </td>
                  <td className="py-3 pl-4 text-right">
                    <Button variant="ghost" size="icon-sm" onClick={() => setSelected(item)}>
                      <Eye />
                      <span className="sr-only">查看审计详情</span>
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </CursorTableScroll>
      ) : null}

      <Dialog open={!!selected} onOpenChange={(open) => !open && setSelected(null)}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{selected?.action}</DialogTitle>
          </DialogHeader>
          {selected ? (
            <dl className="grid gap-x-5 gap-y-3 text-sm sm:grid-cols-[120px_minmax(0,1fr)]">
              <dt className="text-muted-foreground">资源</dt>
              <dd className="break-all font-mono text-xs">
                {selected.resource_type || "-"} / {selected.resource_id || "-"}
              </dd>
              <dt className="text-muted-foreground">操作者</dt>
              <dd className="break-all font-mono text-xs">
                {selected.actor_role || "-"} / {selected.actor_user_id || "-"}
              </dd>
              <dt className="text-muted-foreground">目标用户</dt>
              <dd className="break-all font-mono text-xs">{selected.subject_user_id || "-"}</dd>
              <dt className="text-muted-foreground">请求 ID</dt>
              <dd className="break-all font-mono text-xs">{selected.request_id || "-"}</dd>
              <dt className="text-muted-foreground">来源 IP</dt>
              <dd>{selected.client_ip || "-"}</dd>
              <dt className="text-muted-foreground">原因</dt>
              <dd>{selected.reason || "-"}</dd>
              <dt className="text-muted-foreground">元数据</dt>
              <dd>
                <pre className="max-h-52 overflow-auto rounded-md bg-muted p-3 font-mono text-xs">
                  {JSON.stringify(selected.metadata || {}, null, 2)}
                </pre>
              </dd>
            </dl>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}
