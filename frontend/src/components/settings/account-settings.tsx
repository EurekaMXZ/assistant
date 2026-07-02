"use client";

import { Check, Copy, UserRound } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { User } from "@/lib/types";

function formatDate(value?: string | null) {
  if (!value) return "暂无记录";
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

function roleLabel(role: User["role"]) {
  return role === "admin" ? "管理员" : role === "system" ? "系统" : "用户";
}

export function AccountSettings({ user }: { user: User }) {
  const copyUserID = async () => {
    await navigator.clipboard.writeText(user.id);
    toast.success("账户 ID 已复制");
  };

  return (
    <div className="space-y-8">
      <header>
        <h2 className="text-xl font-semibold">账户</h2>
      </header>

      <section className="flex items-center gap-4 border-b pb-7">
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
        <dl className="divide-y border-y">
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
            <dd><Badge variant="outline">{roleLabel(user.role)}</Badge></dd>
          </div>
          <div className="grid gap-1 py-4 sm:grid-cols-[160px_1fr] sm:items-center">
            <dt className="text-sm text-muted-foreground">账户 ID</dt>
            <dd className="flex min-w-0 items-center gap-2">
              <code className="truncate font-mono text-xs text-muted-foreground">{user.id}</code>
              <Tooltip>
                <TooltipTrigger
                  render={<Button type="button" variant="ghost" size="icon-xs" onClick={copyUserID} />}
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

      <Separator />

      <section>
        <h3 className="mb-4 text-sm font-medium">时间记录</h3>
        <div className="grid gap-5 sm:grid-cols-2">
          <div>
            <p className="text-xs text-muted-foreground">注册时间</p>
            <p className="mt-1 text-sm font-medium">{formatDate(user.created_at)}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">最近登录</p>
            <p className="mt-1 text-sm font-medium">{formatDate(user.last_login_at)}</p>
          </div>
        </div>
      </section>
    </div>
  );
}
