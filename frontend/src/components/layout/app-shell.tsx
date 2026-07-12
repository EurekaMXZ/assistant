"use client";

import { useEffect, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Menu, RefreshCw } from "lucide-react";
import { AuthDialog } from "@/components/auth/auth-dialog";
import { Sidebar } from "@/components/layout/sidebar";
import { SettingsDialog } from "@/components/settings/settings-dialog";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetTrigger } from "@/components/ui/sheet";
import { useAuth } from "@/hooks/use-auth";
import { openAuthDialog, subscribeAuthDialog, type AuthDialogMode } from "@/lib/auth-dialog-events";
import { buildSettingsUrl, parseSettingsHash, type SettingsSection } from "@/lib/settings-hash";
import { cn } from "@/lib/utils";

function extractConversationId(pathname: string) {
  const match = pathname.match(/^\/c\/([^/]+)$/);
  return match?.[1] || null;
}

export function AppShell({ children }: { children: React.ReactNode }) {
  const { user, logout, isLoading, error: authError, refresh, status: authStatus } = useAuth();
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [desktopSidebarCollapsed, setDesktopSidebarCollapsed] = useState(false);
  const [authMode, setAuthMode] = useState<AuthDialogMode | null>(null);
  const [settingsSection, setSettingsSection] = useState<SettingsSection | null>(null);
  const pathname = usePathname();
  const router = useRouter();
  const isAuthRoute = pathname.startsWith("/auth/");
  const isProtectedRoute = pathname !== "/" && !isAuthRoute;
  const currentConversationId = extractConversationId(pathname);

  useEffect(() => {
    if (authStatus === "unauthenticated" && isProtectedRoute) {
      router.replace("/");
      openAuthDialog("login");
    }
  }, [authStatus, isProtectedRoute, router]);

  useEffect(() => {
    return subscribeAuthDialog((mode) => {
      setAuthMode(mode);
    });
  }, []);

  useEffect(() => {
    if (user) {
      setAuthMode(null);
    }
  }, [user]);

  useEffect(() => {
    if (typeof window === "undefined") {
      return;
    }

    const syncFromHash = () => {
      if (pathname !== "/") {
        setSettingsSection(null);
        return;
      }

      const nextSection = parseSettingsHash(window.location.hash);
      if (!nextSection) {
        setSettingsSection(null);
        return;
      }

      if (!user) {
        setSettingsSection(null);
        window.history.replaceState(null, "", "/");
        openAuthDialog("login");
        return;
      }

      setSettingsSection(nextSection);
    };

    syncFromHash();
    window.addEventListener("hashchange", syncFromHash);
    return () => window.removeEventListener("hashchange", syncFromHash);
  }, [pathname, user]);

  const closeSettings = () => {
    if (typeof window !== "undefined" && parseSettingsHash(window.location.hash)) {
      window.history.replaceState(null, "", "/");
    }
    setSettingsSection(null);
  };

  const openSettings = (section: SettingsSection = "user/profile") => {
    setSidebarOpen(false);
    setSettingsSection(section);
    router.push(buildSettingsUrl(section));
  };

  const handleSettingsSectionChange = (section: SettingsSection) => {
    setSettingsSection(section);
    router.push(buildSettingsUrl(section));
  };

  if (isLoading && isProtectedRoute) {
    return null;
  }

  if (authStatus === "error") {
    return (
      <main className="flex h-dvh items-center justify-center px-6">
        <div className="max-w-sm text-center">
          <p className="font-medium">账户状态暂时无法加载</p>
          <p className="mt-2 text-sm text-muted-foreground">
            {authError || "请检查网络连接后重试。"}
          </p>
          <Button className="mt-4" variant="outline" onClick={() => void refresh()}>
            <RefreshCw className="size-4" />
            重试
          </Button>
        </div>
      </main>
    );
  }

  if (authStatus === "unauthenticated" && isProtectedRoute) {
    return null;
  }

  if (pathname.startsWith("/admin")) {
    return <main className="h-dvh min-h-0 overflow-hidden">{children}</main>;
  }

  if (isAuthRoute) {
    return (
      <>
        <main className="h-dvh overflow-y-auto">{children}</main>
        <AuthDialog mode={authMode} onModeChange={setAuthMode} />
      </>
    );
  }

  return (
    <div className="flex h-dvh w-full overflow-hidden">
      <aside
        className={cn(
          "hidden h-full shrink-0 border-r bg-sidebar transition-[width] duration-200 ease-in-out md:block",
          desktopSidebarCollapsed ? "w-[52px]" : "w-[260px]",
        )}
      >
        <Sidebar
          authLoading={isLoading}
          collapsed={desktopSidebarCollapsed}
          currentConversationId={currentConversationId}
          user={user}
          onLogout={logout}
          onNavigate={() => setSidebarOpen(false)}
          onToggleCollapse={() => setDesktopSidebarCollapsed((collapsed) => !collapsed)}
          onOpenLogin={() => openAuthDialog("login")}
          onOpenRegister={() => openAuthDialog("register")}
          onOpenSettings={openSettings}
        />
      </aside>

      <div className="flex min-h-0 flex-1 flex-col">
        <header className="flex items-center justify-between border-b px-4 py-3 md:hidden">
          <span className="font-semibold">Assistant</span>
          <Sheet open={sidebarOpen} onOpenChange={setSidebarOpen}>
            <SheetTrigger
              render={
                <Button variant="ghost" size="icon">
                  <Menu className="h-5 w-5" />
                  <span className="sr-only">打开菜单</span>
                </Button>
              }
            />
            <SheetContent side="left" className="w-[260px] p-0">
              <Sidebar
                authLoading={isLoading}
                currentConversationId={currentConversationId}
                user={user}
                onLogout={() => {
                  setSidebarOpen(false);
                  logout();
                }}
                onNavigate={() => setSidebarOpen(false)}
                onOpenLogin={() => {
                  setSidebarOpen(false);
                  openAuthDialog("login");
                }}
                onOpenRegister={() => {
                  setSidebarOpen(false);
                  openAuthDialog("register");
                }}
                onOpenSettings={openSettings}
              />
            </SheetContent>
          </Sheet>
        </header>

        <main className="flex min-h-0 flex-1 flex-col overflow-hidden">{children}</main>
      </div>

      <SettingsDialog
        open={settingsSection !== null}
        section={settingsSection || "user/profile"}
        user={user}
        onOpenChange={(open) => {
          if (!open) {
            closeSettings();
          }
        }}
        onSectionChange={handleSettingsSectionChange}
      />

      <AuthDialog mode={authMode} onModeChange={setAuthMode} />
    </div>
  );
}
