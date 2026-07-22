"use client";

import { useDeferredValue, useState } from "react";
import { Activity, Eye, Search } from "lucide-react";
import { AdminPageHeader } from "@/components/admin/admin-shared";
import { AdminListPage } from "@/components/admin/admin-list-page";
import { tableClasses, tableHeadClass } from "@/components/shared/table-styles";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { listAdminAuditEventsPage } from "@/lib/api";
import { formatDateTime } from "@/lib/format";
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
      <AdminListPage
        ariaLabel="审计日志"
        className="mt-5"
        emptyIcon={Activity}
        emptyTitle={query ? "没有匹配的审计记录" : "暂无审计记录"}
        error={error}
        hasItems={filtered.length > 0}
        hasMore={page.has_more}
        loading={loading}
        loadingMore={loadingMore}
        loadMoreError={loadMoreError}
        onLoadMore={loadMore}
        onRetry={reload}
      >
        <table className="admin-responsive-table w-[86rem] min-w-full table-fixed text-left text-sm">
          <colgroup>
            <col className="w-[11rem]" />
            <col className="w-[18rem]" />
            <col className="w-[17rem]" />
            <col className="w-[14rem]" />
            <col className="w-[13rem]" />
            <col className="w-[8rem]" />
            <col className="w-[5rem]" />
          </colgroup>
          <thead className={tableHeadClass}>
            <tr className="border-b">
              <th className={tableClasses.headStart}>时间</th>
              <th className={tableClasses.head}>操作</th>
              <th className={tableClasses.head}>资源</th>
              <th className={tableClasses.head}>操作者</th>
              <th className={tableClasses.head}>请求</th>
              <th className={tableClasses.head}>结果</th>
              <th className={tableClasses.headEnd}>详情</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {filtered.map((item) => (
              <tr key={item.id}>
                <td
                  className={`${tableClasses.cellStart} whitespace-nowrap text-xs text-muted-foreground`}
                  data-label="时间"
                >
                  {formatDateTime(item.created_at)}
                </td>
                <td
                  className={`${tableClasses.cell} truncate font-medium`}
                  title={item.action}
                  data-primary
                >
                  {item.action}
                </td>
                <td className={tableClasses.cell} data-label="资源">
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
                <td className={tableClasses.cell} data-label="操作者">
                  <p>{item.actor_role || "-"}</p>
                  <p
                    className="mt-0.5 truncate font-mono text-xs text-muted-foreground"
                    title={item.actor_user_id || ""}
                  >
                    {item.actor_user_id || ""}
                  </p>
                </td>
                <td className={tableClasses.cell} data-label="请求">
                  <p className="font-mono text-xs">
                    {item.request_id ? item.request_id.slice(0, 12) : "-"}
                  </p>
                  <p className="mt-0.5 text-xs text-muted-foreground">{item.client_ip || ""}</p>
                </td>
                <td className={tableClasses.cell} data-label="结果">
                  <Badge variant={item.outcome === "succeeded" ? "secondary" : "destructive"}>
                    {item.outcome}
                  </Badge>
                </td>
                <td className={tableClasses.cellEnd} data-actions>
                  <Button variant="ghost" size="icon-sm" onClick={() => setSelected(item)}>
                    <Eye />
                    <span className="sr-only">查看审计详情</span>
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </AdminListPage>

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
