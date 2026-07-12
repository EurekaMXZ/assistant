"use client";

import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import { AssistantLogo } from "@/components/assistant-logo";
import { cn } from "@/lib/utils";
import type { User } from "@/lib/types";
import { adminSectionsForRole, type AdminSection } from "./admin-sections";
export type { AdminSection } from "./admin-sections";

export function AdminNavigation({
  section,
  user,
  onSelect,
}: {
  section: AdminSection;
  user: User;
  onSelect: (section: AdminSection) => void;
}) {
  const visibleSections = adminSectionsForRole(user.role);

  return (
    <div className="flex h-full min-h-0 flex-col bg-sidebar text-sidebar-foreground">
      <div className="flex h-16 shrink-0 items-center gap-3 border-b border-sidebar-border px-5">
        <AssistantLogo className="size-5" />
        <p className="text-base font-semibold">Assistant Admin</p>
      </div>

      <nav className="min-h-0 flex-1 space-y-1 p-3 pt-5" aria-label="管理导航">
        {visibleSections.map((item) => {
          const Icon = item.icon;
          const active = section === item.id;
          return (
            <Button
              key={item.id}
              type="button"
              variant="ghost"
              size="sm"
              data-active={active}
              className={cn(
                "h-9 w-full justify-start rounded-md px-2.5 text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                "data-[active=true]:bg-sidebar-accent data-[active=true]:font-medium data-[active=true]:text-sidebar-accent-foreground",
              )}
              onClick={() => onSelect(item.id)}
            >
              <Icon className="size-4" />
              {item.label}
            </Button>
          );
        })}
      </nav>

      <div className="shrink-0 border-t border-sidebar-border p-3">
        <div className="mb-2 min-w-0 px-2 py-1">
          <p className="truncate text-sm font-medium">{user.username}</p>
          <p className="truncate text-xs text-muted-foreground">{user.email}</p>
        </div>
        <Button
          nativeButton={false}
          render={<Link href="/" />}
          variant="ghost"
          size="sm"
          className="w-full justify-start px-2.5 text-muted-foreground"
        >
          <ArrowLeft className="size-4" />
          返回 Assistant
        </Button>
      </div>
    </div>
  );
}
