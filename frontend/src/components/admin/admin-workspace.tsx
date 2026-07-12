"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Menu } from "lucide-react";
import { AdminNavigation } from "@/components/admin/admin-navigation";
import { adminSectionsForRole, type AdminSection } from "@/components/admin/admin-sections";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetTrigger } from "@/components/ui/sheet";
import { useAuth } from "@/hooks/use-auth";
import { canAccessAdmin } from "@/lib/permissions";
import type { UserRole } from "@/lib/types";

function sectionFromHash(role: UserRole): AdminSection {
  if (typeof window === "undefined") return "overview";
  const value = window.location.hash.slice(1) as AdminSection;
  return adminSectionsForRole(role).some((item) => item.id === value) ? value : "overview";
}

export function AdminWorkspace() {
  const { user } = useAuth();
  const router = useRouter();
  const [section, setSection] = useState<AdminSection>("overview");
  const [mobileOpen, setMobileOpen] = useState(false);

  useEffect(() => {
    if (user && !canAccessAdmin(user.role)) router.replace("/");
  }, [router, user]);

  useEffect(() => {
    if (!user || !canAccessAdmin(user.role)) return;
    const sync = () => setSection(sectionFromHash(user.role));
    sync();
    window.addEventListener("hashchange", sync);
    return () => window.removeEventListener("hashchange", sync);
  }, [user]);

  if (!user || !canAccessAdmin(user.role)) return null;

  const selectSection = (next: AdminSection) => {
    setSection(next);
    setMobileOpen(false);
    window.history.replaceState(null, "", `/admin#${next}`);
  };
  const current =
    adminSectionsForRole(user.role).find((item) => item.id === section) ||
    adminSectionsForRole(user.role)[0];

  return (
    <div className="flex h-full min-h-0 w-full overflow-hidden bg-background">
      <aside className="hidden h-full w-[236px] shrink-0 border-r border-sidebar-border md:block">
        <AdminNavigation section={section} user={user} onSelect={selectSection} />
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 shrink-0 items-center gap-3 border-b px-4 md:hidden">
          <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
            <SheetTrigger render={<Button variant="ghost" size="icon-sm" />}>
              <Menu className="size-4" />
              <span className="sr-only">打开管理导航</span>
            </SheetTrigger>
            <SheetContent side="left" className="w-[268px] p-0">
              <AdminNavigation section={section} user={user} onSelect={selectSection} />
            </SheetContent>
          </Sheet>
          <p className="text-sm font-semibold">{current.label}</p>
        </header>

        <main className="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden">
          <div className="mx-auto min-w-0 w-full max-w-[1440px] px-4 py-6 sm:px-6 lg:px-10 lg:py-9">
            {current.render({ actor: user, navigate: selectSection })}
          </div>
        </main>
      </div>
    </div>
  );
}
