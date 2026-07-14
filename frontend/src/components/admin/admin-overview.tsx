"use client";

import { useEffect, useEffectEvent, useState } from "react";
import { ArrowRight } from "lucide-react";
import {
  AdminError,
  AdminLoading,
  AdminPageHeader,
  adminTableHeadClass,
  adminTableScrollClass,
  formatAdminDate,
} from "@/components/admin/admin-shared";
import type { AdminSection } from "@/components/admin/admin-sections";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { getAdminOverview } from "@/lib/api";
import type { AdminOverview as OverviewData, User } from "@/lib/types";

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
      {error && !data ? <AdminError message={error} onRetry={load} /> : null}
      {data ? (
        <>
          <section
            className={`mt-7 grid border-y sm:grid-cols-2 ${actor.role === "system" ? "xl:grid-cols-5" : "xl:grid-cols-3"}`}
          >
            {stats.map((item, index) => (
              <div
                key={item.label}
                className={`px-1 py-5 sm:px-5 ${index > 0 ? "sm:border-l" : ""} ${index > 1 ? "border-t sm:border-t-0" : index === 1 ? "border-t sm:border-t-0" : ""}`}
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
              <div className={adminTableScrollClass}>
                <table className="w-[50rem] min-w-full table-fixed text-left text-sm">
                  <colgroup>
                    <col className="w-[11rem]" />
                    <col className="w-[20rem]" />
                    <col className="w-[12rem]" />
                    <col className="w-[7rem]" />
                  </colgroup>
                  <thead className={adminTableHeadClass}>
                    <tr className="border-b">
                      <th className="py-3 pr-4 font-medium">时间</th>
                      <th className="px-4 py-3 font-medium">操作</th>
                      <th className="px-4 py-3 font-medium">资源</th>
                      <th className="py-3 pl-4 text-right font-medium">结果</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {data.audit.map((item) => (
                      <tr key={item.id}>
                        <td className="whitespace-nowrap py-3 pr-4 text-xs text-muted-foreground">
                          {formatAdminDate(item.created_at)}
                        </td>
                        <td className="truncate px-4 py-3 font-medium" title={item.action}>
                          {item.action}
                        </td>
                        <td
                          className="truncate px-4 py-3 text-muted-foreground"
                          title={item.resource_type || ""}
                        >
                          {item.resource_type || "-"}
                        </td>
                        <td className="py-3 pl-4 text-right">
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
              </div>
            </section>
          </div>
        </>
      ) : null}
    </div>
  );
}
