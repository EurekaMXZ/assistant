"use client";

import { useRef } from "react";
import { CreditCard, ShieldCheck, UserRound } from "lucide-react";
import type { User } from "@/lib/types";
import { AccountSettings } from "@/components/settings/account-settings";
import { ExpensesSettings } from "@/components/settings/expenses-settings";
import { SecuritySettings } from "@/components/settings/security-settings";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { SettingsSection } from "@/lib/settings-hash";

interface SettingsDialogProps {
  open: boolean;
  section: SettingsSection;
  user: User | null;
  onOpenChange: (open: boolean) => void;
  onSectionChange: (section: SettingsSection) => void;
}

const sections = [
  {
    id: "user/profile",
    label: "账户",
    icon: UserRound,
  },
  {
    id: "user/security",
    label: "安全",
    icon: ShieldCheck,
  },
  {
    id: "user/expenses",
    label: "费用",
    icon: CreditCard,
  },
] satisfies Array<{ id: SettingsSection; label: string; icon: typeof UserRound }>;

export function SettingsDialog({
  open,
  section,
  user,
  onOpenChange,
  onSectionChange,
}: SettingsDialogProps) {
  const titleRef = useRef<HTMLHeadingElement>(null);

  if (!user) {
    return null;
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="h-[min(720px,calc(100dvh-2rem))] overflow-hidden p-0 sm:max-w-[900px]"
        initialFocus={titleRef}
        showCloseButton
      >
        <DialogHeader className="sr-only">
          <DialogTitle ref={titleRef} tabIndex={-1}>
            个人设置
          </DialogTitle>
          <DialogDescription>管理账户、安全和费用信息</DialogDescription>
        </DialogHeader>

        <div className="grid min-h-0 flex-1 grid-rows-[auto_minmax(0,1fr)] sm:grid-cols-[210px_minmax(0,1fr)] sm:grid-rows-1">
          <aside className="border-b bg-muted/35 px-3 py-3 sm:border-r sm:border-b-0 sm:px-4 sm:py-6">
            <div className="hidden px-2 sm:block">
              <p className="text-base font-semibold">设置</p>
              <p className="mt-1 truncate text-xs text-muted-foreground">{user.email}</p>
            </div>
            <nav className="flex gap-1 overflow-x-auto sm:mt-7 sm:flex-col" aria-label="设置导航">
              {sections.map((item) => {
                const Icon = item.icon;
                const active = item.id === section;
                return (
                  <Button
                    key={item.id}
                    type="button"
                    size="sm"
                    variant={active ? "secondary" : "ghost"}
                    className="h-9 shrink-0 justify-start rounded-lg px-3 text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground data-[active=true]:bg-sidebar-accent data-[active=true]:text-sidebar-accent-foreground"
                    data-active={active}
                    onClick={() => onSectionChange(item.id)}
                  >
                    <Icon className="size-4" />
                    {item.label}
                  </Button>
                );
              })}
            </nav>
          </aside>

          <div className="min-h-0 overflow-y-auto">
            <div className="mx-auto w-full max-w-[680px] px-5 py-6 sm:px-8 sm:py-8">
              {section === "user/profile" ? <AccountSettings user={user} /> : null}
              {section === "user/security" ? <SecuritySettings user={user} /> : null}
              {section === "user/expenses" ? <ExpensesSettings /> : null}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
