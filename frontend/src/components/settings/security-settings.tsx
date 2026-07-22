"use client";

import { LogOut, MonitorCheck } from "lucide-react";
import { ChangePasswordForm } from "@/components/auth/change-password-form";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { SettingsSection } from "@/components/shared/settings-section";
import { useAuth } from "@/hooks/use-auth";
import type { User } from "@/lib/types";

export function SecuritySettings({ user }: { user: User }) {
  const { logout } = useAuth();

  return (
    <SettingsSection title="安全">
      <section>
        <div className="mb-6">
          <h3 className="text-sm font-medium">登录密码</h3>
        </div>
        <ChangePasswordForm />
      </section>

      <section>
        <h3 className="mb-4 text-sm font-medium">当前会话</h3>
        <div className="flex flex-col gap-4 py-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex min-w-0 items-center gap-3">
            <div className="flex size-9 shrink-0 items-center justify-center rounded-md bg-secondary">
              <MonitorCheck className="size-4" />
            </div>
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <p className="text-sm font-medium">当前设备</p>
                <Badge variant="secondary">已登录</Badge>
              </div>
              <p className="mt-1 truncate text-xs text-muted-foreground">{user.email}</p>
            </div>
          </div>
          <Button type="button" variant="outline" size="sm" className="w-fit" onClick={logout}>
            <LogOut data-icon="inline-start" />
            退出登录
          </Button>
        </div>
      </section>
    </SettingsSection>
  );
}
