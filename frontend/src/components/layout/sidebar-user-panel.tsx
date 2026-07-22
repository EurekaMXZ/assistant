"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { User } from "@/lib/types";
import type { BillingAccount } from "@/lib/types";
import { getBillingAccount, isSessionUnauthorizedError } from "@/lib/api";
import { subscribeBillingAccountUpdated } from "@/lib/billing-account-events";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { LogOut, Settings, Shield, User as UserIcon } from "lucide-react";
import { cn } from "@/lib/utils";

interface SidebarUserPanelProps {
  authLoading: boolean;
  collapsed?: boolean;
  showAdmin: boolean;
  user: User | null;
  onOpenAdmin: () => void;
  onLogout: () => void;
  onOpenLogin: () => void;
  onOpenRegister: () => void;
  onOpenSettings: () => void;
}

export function SidebarUserPanel({
  authLoading,
  collapsed = false,
  showAdmin,
  user,
  onOpenAdmin,
  onLogout,
  onOpenLogin,
  onOpenRegister,
  onOpenSettings,
}: SidebarUserPanelProps) {
  const [billingAccount, setBillingAccount] = useState<BillingAccount | null>(null);
  const activeUserIDRef = useRef(user?.id);

  useEffect(() => {
    activeUserIDRef.current = user?.id;
  }, [user?.id]);

  const applyBillingAccount = useCallback((account: BillingAccount) => {
    if (account.user_id !== activeUserIDRef.current) return;
    setBillingAccount((current) => {
      if (current?.user_id === account.user_id && current.version > account.version) return current;
      return account;
    });
  }, []);

  const refreshBalance = async () => {
    if (!user) return;
    try {
      applyBillingAccount(await getBillingAccount());
    } catch (error) {
      if (!isSessionUnauthorizedError(error)) setBillingAccount(null);
    }
  };

  useEffect(() => {
    if (!user) {
      setBillingAccount(null);
      return;
    }
    let cancelled = false;
    void getBillingAccount()
      .then((account) => {
        if (!cancelled) applyBillingAccount(account);
      })
      .catch((error) => {
        if (!cancelled && !isSessionUnauthorizedError(error)) setBillingAccount(null);
      });
    return () => {
      cancelled = true;
    };
  }, [applyBillingAccount, user]);

  useEffect(() => subscribeBillingAccountUpdated(applyBillingAccount), [applyBillingAccount]);

  const balanceLabel = billingAccount
    ? `${billingAccount.currency} ${billingAccount.balance}`
    : "余额加载中";

  return (
    <div className="mt-auto shrink-0 px-2 py-2">
      {authLoading ? (
        <Skeleton
          className={cn(collapsed ? "mx-auto size-8 rounded-lg" : "h-20 w-full rounded-xl")}
        />
      ) : user ? (
        <DropdownMenu onOpenChange={(open) => open && void refreshBalance()}>
          <DropdownMenuTrigger
            render={
              <Button
                variant="nav"
                size={collapsed ? "icon-sm" : "sm"}
                className={cn(collapsed ? "mx-auto" : "min-h-12 w-full justify-start px-2 py-2")}
              />
            }
          >
            <UserIcon className={cn("size-4", !collapsed && "mr-2")} />
            {!collapsed ? (
              <span className="min-w-0 text-left leading-tight">
                <span className="block truncate text-sm">{user.username}</span>
                <span className="mt-0.5 block truncate text-xs font-normal text-muted-foreground">
                  {balanceLabel}
                </span>
              </span>
            ) : null}
            {collapsed ? <span className="sr-only">用户菜单</span> : null}
          </DropdownMenuTrigger>
          <DropdownMenuContent
            align={collapsed ? "start" : "end"}
            side={collapsed ? "right" : undefined}
            className="w-56"
          >
            <DropdownMenuItem disabled>
              <span className="text-xs text-muted-foreground">{user.email}</span>
            </DropdownMenuItem>
            <DropdownMenuItem onClick={onOpenSettings}>
              <Settings className="mr-2 size-4" />
              设置
            </DropdownMenuItem>
            {showAdmin ? (
              <DropdownMenuItem onClick={onOpenAdmin}>
                <Shield className="mr-2 size-4" />
                管理员
              </DropdownMenuItem>
            ) : null}
            <DropdownMenuItem onClick={onLogout}>
              <LogOut className="mr-2 size-4" />
              退出登录
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      ) : collapsed ? (
        <Button variant="nav" size="icon-sm" className="mx-auto" onClick={onOpenLogin}>
          <UserIcon className="size-4" />
          <span className="sr-only">登录</span>
        </Button>
      ) : (
        <Card className="rounded-xl bg-background/80 p-3 backdrop-blur-sm">
          <p className="font-medium text-foreground">登录后继续</p>
          <p className="mt-1 text-xs leading-5 text-muted-foreground">
            发送消息、搜索会话和查看历史都需要登录。
          </p>
          <div className="mt-3 flex items-center gap-2">
            <Button size="sm" className="min-h-10 flex-1 py-2 md:min-h-9" onClick={onOpenLogin}>
              登录
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="min-h-10 flex-1 py-2 md:min-h-9"
              onClick={onOpenRegister}
            >
              注册
            </Button>
          </div>
        </Card>
      )}
    </div>
  );
}
