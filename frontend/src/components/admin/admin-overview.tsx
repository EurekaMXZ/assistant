"use client";

import { useEffect, useEffectEvent, useState } from "react";
import { Activity, ArrowRight } from "lucide-react";
import { AdminLoading, AdminPageHeader } from "@/components/admin/admin-shared";
import { AdminListPage } from "@/components/admin/admin-list-page";
import { ErrorState } from "@/components/shared/error-state";
import { tableClasses, tableHeadClass } from "@/components/shared/table-styles";
import type { AdminSection } from "@/components/admin/admin-sections";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { getAdminOverview } from "@/lib/api";
import { formatDateTime } from "@/lib/format";
import type { AdminOverview as OverviewData, User } from "@/lib/types";
import { cn } from "@/lib/utils";

export function AdminOverview({
  actor,
  onNavigate,
}: {
  actor: User;
  onNavigate: (section: AdminSection) => void;
}) {
  const [data, setData] = useState<OverviewData | null>(null);
  const [error, setError] = useState("");

  const load = async () => {
    setError("");
    try {
      setData(await getAdminOverview());
    } catch (err) {
      setError(err instanceof Error ? err.message : "管理数据加载失败");
    }
  };

  const loadOverview = useEffectEvent(load);
  useEffect(() => {
    void loadOverview();
  }, [actor.role]);

  const stats = data
    ? [
        { label: "用户", value: data.users },
        ...(actor.role === "system"
          ? [
              { label: "启用模型", value: data.enabled_models ?? 0 },
              { label: "有效凭据", value: data.credentials ?? 0 },
            ]
          : []),
        { label: "活跃账户", value: data.active_accounts },
        { label: "审计记录", value: data.audit_events },
      ]
    : [];

  return (
    <div>
      <AdminPageHeader title="运行概览" />

      {!data && !error ? <AdminLoading /> : null}
      {error && !data ? <ErrorState message={error} onRetry={load} /> : null}
      {data ? (
        <>
          <section
            className={cn(
              "mt-7 grid sm:grid-cols-2",
              actor.role === "system" ? "xl:grid-cols-5" : "xl:grid-cols-3",
            )}
          >
            {stats.map((item, index) => (
              <div
                key={item.label}
                className={cn(
                  "px-1 py-5 sm:px-5",
                  index > 0 && "border-t sm:border-l sm:border-t-0",
                )}
              >
                <p className="text-xs text-muted-foreground">{item.label}</p>
                <p className="mt-2 font-mono text-3xl font-semibold tabular-nums">{item.value}</p>
              </div>
            ))}
          </section>

          <div className="mt-9">
            <section>
              <div className="mb-3 flex items-center justify-between">
                <h2 className="text-sm font-semibold">最近管理操作</h2>
                <Button variant="ghost" size="sm" onClick={() => onNavigate("audit")}>
                  查看全部 <ArrowRight />
                </Button>
              </div>
              <AdminListPage
                ariaLabel="最近管理操作"
                emptyIcon={Activity}
                emptyTitle="暂无管理操作"
                hasItems={data.audit.length > 0}
                onRetry={load}
              >
                <table className="admin-responsive-table w-[50rem] min-w-full table-fixed text-left text-sm">
                  <colgroup>
                    <col className="w-[11rem]" />
                    <col className="w-[20rem]" />
                    <col className="w-[12rem]" />
                    <col className="w-[7rem]" />
                  </colgroup>
                  <thead className={tableHeadClass}>
                    <tr className="border-b">
                      <th className={tableClasses.headStart}>时间</th>
                      <th className={tableClasses.head}>操作</th>
                      <th className={tableClasses.head}>资源</th>
                      <th className={tableClasses.headEnd}>结果</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {data.audit.map((item) => (
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
                        <td
                          className={`${tableClasses.cell} truncate text-muted-foreground`}
                          title={item.resource_type || ""}
                          data-label="资源"
                        >
                          {item.resource_type || "-"}
                        </td>
                        <td className={tableClasses.cellEnd} data-label="结果">
                          <Badge
                            variant={item.outcome === "succeeded" ? "secondary" : "destructive"}
                          >
                            {item.outcome}
                          </Badge>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </AdminListPage>
            </section>
          </div>
        </>
      ) : null}
    </div>
  );
}
