"use client";

import { useEffect, useRef } from "react";
import { CreditCard, ShieldCheck, SlidersHorizontal, UserRound } from "lucide-react";
import type { User } from "@/lib/types";
import { AccountSettings } from "@/components/settings/account-settings";
import { ExpensesSettings } from "@/components/settings/expenses-settings";
import { PersonalizationSettings } from "@/components/settings/personalization-settings";
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
    id: "user/personalization",
    label: "个性化",
    icon: SlidersHorizontal,
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
  const activeSectionRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    activeSectionRef.current?.scrollIntoView({
      behavior: "smooth",
      block: "nearest",
      inline: "center",
    });
  }, [open, section]);

  if (!user) {
    return null;
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="h-[min(760px,calc(100dvh-2rem))] overflow-hidden p-0 md:max-w-[960px]"
        initialFocus={titleRef}
        showCloseButton
      >
        <DialogHeader className="sr-only">
          <DialogTitle ref={titleRef} tabIndex={-1}>
            个人设置
          </DialogTitle>
          <DialogDescription>管理账户、个性化、安全和费用信息</DialogDescription>
        </DialogHeader>

        <div className="grid min-h-0 min-w-0 flex-1 grid-rows-[auto_minmax(0,1fr)] md:grid-cols-[210px_minmax(0,1fr)] md:grid-rows-1">
          <aside className="min-w-0 border-b bg-muted/35 py-3 md:border-r md:border-b-0 md:px-4 md:py-6">
            <div className="hidden px-2 md:block">
              <p className="text-base font-semibold">设置</p>
              <p className="mt-1 truncate text-xs text-muted-foreground">{user.email}</p>
            </div>
            <nav
              className="flex max-w-full touch-pan-x snap-x snap-mandatory scroll-px-3 gap-1 overflow-x-auto overscroll-x-contain px-3 [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden md:mt-7 md:flex-col md:overflow-x-visible md:px-0"
              aria-label="设置导航"
            >
              {sections.map((item) => {
                const Icon = item.icon;
                const active = item.id === section;
                return (
                  <Button
                    key={item.id}
                    ref={active ? activeSectionRef : undefined}
                    type="button"
                    size="sm"
                    variant="nav"
                    className="h-9 shrink-0 snap-center justify-start px-3 text-muted-foreground data-[active=true]:bg-sidebar-accent data-[active=true]:text-sidebar-accent-foreground md:snap-none"
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
            <div className="mx-auto w-full max-w-[720px] px-5 py-6 sm:px-8 sm:py-8">
              {section === "user/profile" ? <AccountSettings user={user} /> : null}
              {section === "user/personalization" ? <PersonalizationSettings /> : null}
              {section === "user/security" ? <SecuritySettings user={user} /> : null}
              {section === "user/expenses" ? <ExpensesSettings /> : null}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
