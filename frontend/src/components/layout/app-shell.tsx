"use client";

import { useEffect, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { RefreshCw } from "lucide-react";
import { AssistantLogo } from "@/components/assistant-logo";
import { AuthDialog } from "@/components/auth/auth-dialog";
import {
  MobileHeaderContext,
  type MobileHeaderAction,
  type MobileHeaderStatus,
  type MobileHeaderTitleAction,
} from "@/components/layout/mobile-header-context";
import { MobileHeaderTitle } from "@/components/layout/mobile-header-title";
import { Sidebar } from "@/components/layout/sidebar";
import { SettingsDialog } from "@/components/settings/settings-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTrigger } from "@/components/ui/dialog";
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
  const [mobileHeaderTitle, setMobileHeaderTitle] = useState("Assistant");
  const [mobileHeaderAction, setMobileHeaderAction] = useState<MobileHeaderAction | null>(null);
  const [mobileHeaderStatus, setMobileHeaderStatus] = useState<MobileHeaderStatus | null>(null);
  const [mobileHeaderTitleAction, setMobileHeaderTitleAction] =
    useState<MobileHeaderTitleAction | null>(null);
  const [authMode, setAuthMode] = useState<AuthDialogMode | null>(null);
  const [settingsSection, setSettingsSection] = useState<SettingsSection | null>(null);
  const pathname = usePathname();
  const router = useRouter();
  const isAuthRoute = pathname.startsWith("/auth/");
  const isConversationShareRoute = pathname.startsWith("/share/");
  const isProtectedRoute = pathname !== "/" && !isAuthRoute && !isConversationShareRoute;
  const currentConversationId = extractConversationId(pathname);
  const mobileHeaderActionIsCurrent =
    !mobileHeaderAction?.conversationId ||
    mobileHeaderAction.conversationId === currentConversationId;
  const mobileHeaderTitleActionIsCurrent =
    !mobileHeaderTitleAction?.conversationId ||
    mobileHeaderTitleAction.conversationId === currentConversationId;

  useEffect(() => {
    setMobileHeaderTitle(
      currentConversationId
        ? "新会话"
        : pathname === "/mcp"
          ? "MCP 服务器"
          : pathname === "/storage"
            ? "存储空间"
            : "Assistant",
    );
  }, [currentConversationId, pathname]);

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

  if (authStatus === "error" && !isConversationShareRoute) {
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

  if (isConversationShareRoute) {
    return (
      <MobileHeaderContext.Provider
        value={{
          setAction: setMobileHeaderAction,
          setStatus: setMobileHeaderStatus,
          setTitle: setMobileHeaderTitle,
          setTitleAction: setMobileHeaderTitleAction,
        }}
      >
        <div className="flex h-dvh w-full flex-col overflow-hidden bg-background text-foreground">
          <header className="grid h-[calc(3.5rem+env(safe-area-inset-top))] shrink-0 grid-cols-[minmax(0,1fr)_auto] items-center border-b px-4 pt-[env(safe-area-inset-top)] md:hidden">
            <MobileHeaderTitle title={mobileHeaderTitle} />
            {mobileHeaderStatus ? (
              <span className="flex items-center gap-1.5 pl-3 text-xs text-muted-foreground">
                {mobileHeaderStatus.icon}
                {mobileHeaderStatus.label}
              </span>
            ) : null}
          </header>
          <main className="flex min-h-0 flex-1 flex-col overflow-hidden">{children}</main>
        </div>
      </MobileHeaderContext.Provider>
    );
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
    <MobileHeaderContext.Provider
      value={{
        setAction: setMobileHeaderAction,
        setStatus: setMobileHeaderStatus,
        setTitle: setMobileHeaderTitle,
        setTitleAction: setMobileHeaderTitleAction,
      }}
    >
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
            mcpActive={pathname === "/mcp"}
            storageActive={pathname === "/storage"}
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
          <header className="grid h-[calc(3.5rem+env(safe-area-inset-top))] shrink-0 grid-cols-[2.25rem_minmax(0,1fr)_auto] items-center border-b px-2 pt-[env(safe-area-inset-top)] md:hidden">
            <Dialog open={sidebarOpen} onOpenChange={setSidebarOpen}>
              <DialogTrigger
                render={<Button variant="ghost" size="icon-sm" className="rounded-lg" />}
              >
                <AssistantLogo className="size-5" />
                <span className="sr-only">打开侧栏</span>
              </DialogTrigger>
              <DialogContent side="left" className="w-[260px] gap-0 p-0" showCloseButton={false}>
                <Sidebar
                  authLoading={isLoading}
                  currentConversationId={currentConversationId}
                  mcpActive={pathname === "/mcp"}
                  storageActive={pathname === "/storage"}
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
              </DialogContent>
            </Dialog>

            <MobileHeaderTitle
              actionLabel={mobileHeaderTitleAction?.label}
              onLongPress={
                mobileHeaderTitleAction && mobileHeaderTitleActionIsCurrent
                  ? mobileHeaderTitleAction.onLongPress
                  : undefined
              }
              title={mobileHeaderTitle}
            />

            {mobileHeaderStatus ? (
              <span className="flex items-center justify-end gap-1.5 pr-1 text-xs text-muted-foreground">
                {mobileHeaderStatus.icon}
                {mobileHeaderStatus.label}
              </span>
            ) : !user && !isLoading ? (
              <div className="flex items-center justify-end gap-1">
                <Button variant="ghost" size="xs" onClick={() => openAuthDialog("login")}>
                  登录
                </Button>
                <Button size="xs" onClick={() => openAuthDialog("register")}>
                  注册
                </Button>
                {mobileHeaderAction && mobileHeaderActionIsCurrent ? (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="rounded-lg"
                    aria-busy={mobileHeaderAction.busy}
                    disabled={mobileHeaderAction.disabled}
                    onClick={mobileHeaderAction.onClick}
                  >
                    {mobileHeaderAction.icon}
                    <span className="sr-only">{mobileHeaderAction.label}</span>
                  </Button>
                ) : null}
              </div>
            ) : mobileHeaderAction ? (
              <Button
                variant="ghost"
                size="icon-sm"
                className="rounded-lg"
                aria-busy={mobileHeaderAction.busy}
                disabled={!mobileHeaderActionIsCurrent || mobileHeaderAction.disabled}
                onClick={() => {
                  if (mobileHeaderActionIsCurrent) mobileHeaderAction.onClick();
                }}
              >
                {mobileHeaderAction.icon}
                <span className="sr-only">{mobileHeaderAction.label}</span>
              </Button>
            ) : (
              <span aria-hidden="true" />
            )}
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
    </MobileHeaderContext.Provider>
  );
}
