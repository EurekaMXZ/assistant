"use client";

import { Check, Copy, UserRound } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { SettingsSection } from "@/components/shared/settings-section";
import type { User } from "@/lib/types";
import { useCopyToClipboard } from "@/hooks/use-copy-to-clipboard";
import { formatDateTime } from "@/lib/format";

function roleLabel(role: User["role"]) {
  return role === "admin" ? "管理员" : role === "system" ? "系统" : "用户";
}

export function AccountSettings({ user }: { user: User }) {
  const { copyToClipboard } = useCopyToClipboard({
    successMessage: "账户 ID 已复制",
    errorMessage: "复制账户 ID 失败",
  });
  const copyUserID = async () => {
    await copyToClipboard(user.id);
  };

  return (
    <SettingsSection title="账户" className="space-y-8">
      <section className="flex items-center gap-4">
        <div className="flex size-14 shrink-0 items-center justify-center rounded-lg bg-foreground text-lg font-semibold text-background">
          {user.username.slice(0, 1).toUpperCase() || <UserRound className="size-5" />}
        </div>
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <p className="truncate text-lg font-medium">{user.username}</p>
            <Badge variant={user.status === "active" ? "secondary" : "destructive"}>
              {user.status === "active" ? <Check data-icon="inline-start" /> : null}
              {user.status === "active" ? "正常" : "已停用"}
            </Badge>
          </div>
          <p className="mt-1 truncate text-sm text-muted-foreground">{user.email}</p>
        </div>
      </section>

      <section>
        <h3 className="mb-4 text-sm font-medium">账户信息</h3>
        <dl className="divide-y">
          <div className="grid gap-1 py-4 sm:grid-cols-[160px_1fr] sm:items-center">
            <dt className="text-sm text-muted-foreground">用户名</dt>
            <dd className="text-sm font-medium">{user.username}</dd>
          </div>
          <div className="grid gap-1 py-4 sm:grid-cols-[160px_1fr] sm:items-center">
            <dt className="text-sm text-muted-foreground">邮箱</dt>
            <dd className="break-all text-sm font-medium">{user.email}</dd>
          </div>
          <div className="grid gap-1 py-4 sm:grid-cols-[160px_1fr] sm:items-center">
            <dt className="text-sm text-muted-foreground">角色</dt>
            <dd>
              <Badge variant="outline">{roleLabel(user.role)}</Badge>
            </dd>
          </div>
          <div className="grid gap-1 py-4 sm:grid-cols-[160px_1fr] sm:items-center">
            <dt className="text-sm text-muted-foreground">账户 ID</dt>
            <dd className="flex min-w-0 items-center gap-2">
              <code className="truncate font-mono text-xs text-muted-foreground">{user.id}</code>
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button type="button" variant="ghost" size="icon-xs" onClick={copyUserID} />
                  }
                >
                  <Copy />
                  <span className="sr-only">复制账户 ID</span>
                </TooltipTrigger>
                <TooltipContent>复制账户 ID</TooltipContent>
              </Tooltip>
            </dd>
          </div>
        </dl>
      </section>

      <section>
        <h3 className="mb-4 text-sm font-medium">时间记录</h3>
        <div className="grid gap-5 sm:grid-cols-2">
          <div>
            <p className="text-xs text-muted-foreground">注册时间</p>
            <p className="mt-1 text-sm font-medium">
              {formatDateTime(user.created_at, { fallback: "暂无记录" })}
            </p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">最近登录</p>
            <p className="mt-1 text-sm font-medium">
              {formatDateTime(user.last_login_at, { fallback: "暂无记录" })}
            </p>
          </div>
        </div>
      </section>
    </SettingsSection>
  );
}
