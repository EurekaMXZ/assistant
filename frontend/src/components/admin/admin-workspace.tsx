"use client";

import { useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { Menu } from "lucide-react";
import { AdminNavigation } from "@/components/admin/admin-navigation";
import {
  adminSectionsForRole,
  type AdminSection,
  type AdminSectionDefinition,
} from "@/components/admin/admin-sections";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTrigger } from "@/components/ui/dialog";
import { useAuth } from "@/hooks/use-auth";
import { canAccessAdmin } from "@/lib/permissions";

function sectionFromHash(sections: readonly AdminSectionDefinition[]): AdminSection {
  if (typeof window === "undefined") return "overview";
  const value = window.location.hash.slice(1) as AdminSection;
  return sections.some((item) => item.id === value) ? value : "overview";
}

export function AdminWorkspace() {
  const { user } = useAuth();
  const router = useRouter();
  const [section, setSection] = useState<AdminSection>("overview");
  const [mobileOpen, setMobileOpen] = useState(false);
  const sections = useMemo(() => (user ? adminSectionsForRole(user.role) : []), [user]);

  useEffect(() => {
    if (user && !canAccessAdmin(user.role)) router.replace("/");
  }, [router, user]);

  useEffect(() => {
    if (!user || !canAccessAdmin(user.role)) return;
    const sync = () => setSection(sectionFromHash(sections));
    sync();
    window.addEventListener("hashchange", sync);
    return () => window.removeEventListener("hashchange", sync);
  }, [sections, user]);

  if (!user || !canAccessAdmin(user.role)) return null;

  const selectSection = (next: AdminSection) => {
    setSection(next);
    setMobileOpen(false);
    window.history.replaceState(null, "", `/admin#${next}`);
  };
  const current = sections.find((item) => item.id === section) || sections[0];

  return (
    <div className="flex h-full min-h-0 w-full overflow-hidden bg-background">
      <aside className="hidden h-full w-[236px] shrink-0 border-r border-sidebar-border md:block">
        <AdminNavigation
          section={section}
          sections={sections}
          user={user}
          onSelect={selectSection}
        />
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-[calc(3.5rem+env(safe-area-inset-top))] shrink-0 items-center gap-3 border-b px-4 pt-[env(safe-area-inset-top)] md:hidden">
          <Dialog open={mobileOpen} onOpenChange={setMobileOpen}>
            <DialogTrigger render={<Button variant="ghost" size="icon-sm" />}>
              <Menu className="size-4" />
              <span className="sr-only">打开管理导航</span>
            </DialogTrigger>
            <DialogContent side="left" className="w-[268px] p-0">
              <AdminNavigation
                section={section}
                sections={sections}
                user={user}
                onSelect={selectSection}
              />
            </DialogContent>
          </Dialog>
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
