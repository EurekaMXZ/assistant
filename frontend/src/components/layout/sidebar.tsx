"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  deleteConversation,
  isSessionUnauthorizedError,
  listConversations,
  patchConversation,
} from "@/lib/api";
import { emitConversationUpdated, subscribeConversationUpdated } from "@/lib/conversation-events";
import type { Conversation, User } from "@/lib/types";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { FormField } from "@/components/ui/form-field";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { SidebarConversationList } from "@/components/layout/sidebar-conversation-list";
import { SidebarSearchDialog } from "@/components/layout/sidebar-search-dialog";
import { SidebarUserPanel } from "@/components/layout/sidebar-user-panel";
import { HardDrive, PanelLeft, Plug, Search, StickyNotePlus } from "lucide-react";
import { toast } from "sonner";
import type { SettingsSection } from "@/lib/settings-hash";
import { cn } from "@/lib/utils";
import { canAccessAdmin } from "@/lib/permissions";
import { AssistantLogo } from "@/components/assistant-logo";

interface SidebarProps {
  authLoading?: boolean;
  collapsed?: boolean;
  currentConversationId?: string | null;
  mcpActive?: boolean;
  storageActive?: boolean;
  user: User | null;
  onNavigate?: () => void;
  onLogout: () => void;
  onOpenLogin: () => void;
  onOpenRegister: () => void;
  onOpenSettings: (section?: SettingsSection) => void;
  onToggleCollapse?: () => void;
}

export function Sidebar({
  authLoading = false,
  collapsed = false,
  currentConversationId,
  mcpActive = false,
  storageActive = false,
  user,
  onNavigate,
  onLogout,
  onOpenLogin,
  onOpenRegister,
  onOpenSettings,
  onToggleCollapse,
}: SidebarProps) {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [renameConversation, setRenameConversation] = useState<Conversation | null>(null);
  const [archiveConversation, setArchiveConversation] = useState<Conversation | null>(null);
  const [deleteConversationTarget, setDeleteConversationTarget] = useState<Conversation | null>(
    null,
  );
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [newTitle, setNewTitle] = useState("");
  const listRequestRef = useRef<{ controller: AbortController; generation: number } | null>(null);
  const generationRef = useRef(0);
  const router = useRouter();

  const visibleConversations = useMemo(
    () => conversations.filter((conversation) => !conversation.archived_at),
    [conversations],
  );
  const normalizedSearchQuery = searchQuery.trim().toLowerCase();
  const searchResults = useMemo(() => {
    if (!normalizedSearchQuery) {
      return [];
    }

    return conversations.filter((conversation) => {
      const title = (conversation.title || "新会话").trim().toLowerCase();
      return title.includes(normalizedSearchQuery);
    });
  }, [conversations, normalizedSearchQuery]);

  const load = useCallback(async () => {
    if (authLoading) {
      return;
    }

    if (!user) {
      listRequestRef.current?.controller.abort();
      setConversations([]);
      setIsLoading(false);
      return;
    }

    const generation = ++generationRef.current;
    listRequestRef.current?.controller.abort();
    const controller = new AbortController();
    listRequestRef.current = { controller, generation };
    const requestedUserId = user.id;
    try {
      const data = await listConversations(200, controller.signal);
      if (
        controller.signal.aborted ||
        listRequestRef.current?.generation !== generation ||
        user.id !== requestedUserId
      )
        return;
      setConversations(data);
    } catch (err) {
      if ((err as Error).name === "AbortError") return;
      if (isSessionUnauthorizedError(err)) {
        return;
      }
      toast.error(err instanceof Error ? err.message : "加载会话失败");
    } finally {
      if (listRequestRef.current?.generation === generation) {
        listRequestRef.current = null;
        setIsLoading(false);
      }
    }
  }, [authLoading, user]);

  useEffect(() => () => listRequestRef.current?.controller.abort(), []);

  useEffect(() => {
    if (authLoading) {
      setIsLoading(true);
      return;
    }

    setIsLoading(!!user);
    void load();
  }, [authLoading, load, user]);

  useEffect(() => {
    if (authLoading || !user) {
      return;
    }

    const onVisible = () => {
      if (document.visibilityState === "visible") {
        void load();
      }
    };

    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, [authLoading, load, user]);

  useEffect(() => {
    return subscribeConversationUpdated((event) => {
      const createdConversation = event.conversation;
      if (createdConversation) {
        generationRef.current += 1;
        listRequestRef.current?.controller.abort();
        listRequestRef.current = null;
        setIsLoading(false);
        setConversations((prev) => {
          const existingIndex = prev.findIndex(
            (conversation) => conversation.id === createdConversation.id,
          );
          if (existingIndex === -1) {
            return [createdConversation, ...prev];
          }
          return prev.map((conversation, index) =>
            index === existingIndex ? createdConversation : conversation,
          );
        });
        return;
      }

      setConversations((prev) => {
        let changed = false;
        const next = prev.map((conversation) => {
          if (conversation.id !== event.id) {
            return conversation;
          }

          changed = true;
          return {
            ...conversation,
            ...(typeof event.title !== "undefined" ? { title: event.title ?? undefined } : {}),
            ...(typeof event.archived_at !== "undefined"
              ? { archived_at: event.archived_at ?? undefined }
              : {}),
          };
        });

        return changed ? next : prev;
      });
    });
  }, []);

  const handleCreate = () => {
    if (!user) {
      onOpenLogin();
      return;
    }

    onNavigate?.();
    router.push("/");
  };

  const handleSearch = () => {
    if (!user) {
      onOpenLogin();
      return;
    }

    setSearchOpen(true);
  };

  const handleSelectConversation = (conversationId: string) => {
    if (!user) {
      onOpenLogin();
      return;
    }

    setSearchOpen(false);
    setSearchQuery("");
    onNavigate?.();
    router.push(`/c/${conversationId}`);
  };

  const handleRename = async () => {
    if (!renameConversation) {
      return;
    }

    try {
      const updated = await patchConversation(renameConversation.id, {
        title: newTitle,
      });
      setConversations((prev) =>
        prev.map((conversation) => (conversation.id === updated.id ? updated : conversation)),
      );
      emitConversationUpdated({ id: updated.id, title: updated.title });
      setRenameConversation(null);
    } catch (err) {
      if (isSessionUnauthorizedError(err)) {
        return;
      }
      toast.error(err instanceof Error ? err.message : "重命名失败");
    }
  };

  const handleArchive = async () => {
    if (!archiveConversation) {
      return;
    }

    try {
      const updated = await patchConversation(archiveConversation.id, {
        archived: true,
      });
      setConversations((prev) =>
        prev.map((conversation) => (conversation.id === updated.id ? updated : conversation)),
      );
      emitConversationUpdated({
        id: updated.id,
        title: updated.title,
        archived_at: updated.archived_at,
      });
      setArchiveConversation(null);
    } catch (err) {
      if (isSessionUnauthorizedError(err)) {
        return;
      }
      toast.error(err instanceof Error ? err.message : "归档失败");
    }
  };

  const handleDelete = async () => {
    if (!deleteConversationTarget) return;
    const target = deleteConversationTarget;
    try {
      await deleteConversation(target.id);
      setConversations((prev) => prev.filter((conversation) => conversation.id !== target.id));
      setDeleteConversationTarget(null);
      if (currentConversationId === target.id) {
        onNavigate?.();
        router.push("/");
      }
      toast.success("会话已删除");
    } catch (err) {
      if (isSessionUnauthorizedError(err)) return;
      toast.error(err instanceof Error ? err.message : "会话删除失败");
    }
  };

  const handleOpenStorage = () => {
    if (!user) {
      onOpenLogin();
      return;
    }
    onNavigate?.();
    router.push("/storage");
  };

  const handleOpenMCP = () => {
    if (!user) {
      onOpenLogin();
      return;
    }
    onNavigate?.();
    router.push("/mcp");
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div
        className={cn(
          "flex h-14 shrink-0 items-center px-2 py-2",
          collapsed ? "justify-center" : "gap-1",
        )}
      >
        {collapsed && onToggleCollapse ? (
          <Button
            variant="nav"
            size="icon-sm"
            className="group/sidebar-toggle relative shrink-0"
            onClick={() => {
              setSearchOpen(false);
              setSearchQuery("");
              onToggleCollapse();
            }}
          >
            <AssistantLogo className="size-5 transition-opacity group-hover/sidebar-toggle:opacity-0 group-focus-visible/sidebar-toggle:opacity-0" />
            <PanelLeft className="absolute size-4 opacity-0 transition-opacity group-hover/sidebar-toggle:opacity-100 group-focus-visible/sidebar-toggle:opacity-100" />
            <span className="sr-only">展开侧栏</span>
          </Button>
        ) : (
          <Link
            href="/"
            onClick={() => onNavigate?.()}
            className={cn(
              "flex h-full items-center gap-2 px-2 text-base font-semibold text-sidebar-foreground transition-colors hover:text-sidebar-foreground/80",
              onToggleCollapse ? "min-w-0 flex-1" : "w-full",
            )}
          >
            <AssistantLogo className="size-5" />
            <span>Assistant</span>
          </Link>
        )}

        {onToggleCollapse && !collapsed ? (
          <Button
            variant="nav"
            size="icon-sm"
            className="shrink-0"
            onClick={() => {
              setSearchOpen(false);
              setSearchQuery("");
              onToggleCollapse();
            }}
          >
            <PanelLeft className="size-4" />
            <span className="sr-only">{collapsed ? "展开侧栏" : "折叠侧栏"}</span>
          </Button>
        ) : null}
      </div>

      <div className={cn("px-2 py-2", collapsed && "space-y-1")}>
        <Button
          variant="nav"
          size={collapsed ? "icon-sm" : "sm"}
          className={cn(
            collapsed ? "mx-auto" : "min-h-10 w-full justify-start px-2 py-2 md:min-h-9",
          )}
          disabled={authLoading}
          onClick={handleCreate}
        >
          <StickyNotePlus className="size-4" />
          {!collapsed ? "新建会话" : <span className="sr-only">新建会话</span>}
        </Button>

        <Button
          variant="nav"
          size={collapsed ? "icon-sm" : "sm"}
          className={cn(
            collapsed ? "mx-auto" : "min-h-10 w-full justify-start px-2 py-2 md:min-h-9",
          )}
          disabled={authLoading}
          onClick={handleSearch}
        >
          <Search className="size-4" />
          {!collapsed ? "搜索会话" : <span className="sr-only">搜索会话</span>}
        </Button>

        <Button
          variant="nav"
          size={collapsed ? "icon-sm" : "sm"}
          aria-pressed={storageActive}
          className={cn(
            collapsed ? "mx-auto" : "min-h-10 w-full justify-start px-2 py-2 md:min-h-9",
          )}
          disabled={authLoading}
          onClick={handleOpenStorage}
        >
          <HardDrive className="size-4" />
          {!collapsed ? "存储空间" : <span className="sr-only">存储空间</span>}
        </Button>

        <Button
          variant="nav"
          size={collapsed ? "icon-sm" : "sm"}
          aria-pressed={mcpActive}
          className={cn(
            collapsed ? "mx-auto" : "min-h-10 w-full justify-start px-2 py-2 md:min-h-9",
          )}
          disabled={authLoading}
          onClick={handleOpenMCP}
        >
          <Plug className="size-4" />
          {!collapsed ? "MCP 服务器" : <span className="sr-only">MCP 服务器</span>}
        </Button>
      </div>

      {!collapsed ? (
        <>
          <SidebarConversationList
            authLoading={authLoading}
            conversations={visibleConversations}
            currentConversationId={currentConversationId}
            isLoading={isLoading}
            isSignedIn={!!user}
            onSelectConversation={handleSelectConversation}
            onOpenArchive={setArchiveConversation}
            onOpenDelete={setDeleteConversationTarget}
            onOpenRename={(conversation) => {
              setRenameConversation(conversation);
              setNewTitle(conversation.title || "");
            }}
          />
        </>
      ) : null}

      <SidebarUserPanel
        authLoading={authLoading}
        collapsed={collapsed}
        showAdmin={canAccessAdmin(user?.role)}
        user={user}
        onLogout={onLogout}
        onOpenLogin={onOpenLogin}
        onOpenRegister={onOpenRegister}
        onOpenAdmin={() => {
          onNavigate?.();
          router.push("/admin");
        }}
        onOpenSettings={() => onOpenSettings("user/profile")}
      />

      <Dialog
        open={!!renameConversation}
        onOpenChange={(open) => !open && setRenameConversation(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>重命名会话</DialogTitle>
          </DialogHeader>
          <FormField label="标题" htmlFor="title" className="py-4">
            <Input
              id="title"
              value={newTitle}
              onChange={(event) => setNewTitle(event.target.value)}
              onKeyDown={(event) => event.key === "Enter" && void handleRename()}
            />
          </FormField>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRenameConversation(null)}>
              取消
            </Button>
            <Button onClick={() => void handleRename()}>保存</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <SidebarSearchDialog
        open={searchOpen}
        query={searchQuery}
        results={searchResults}
        onOpenChange={(open) => {
          setSearchOpen(open);
          if (!open) {
            setSearchQuery("");
          }
        }}
        onQueryChange={setSearchQuery}
        onSelectConversation={handleSelectConversation}
      />

      <ConfirmDialog
        open={!!archiveConversation}
        onOpenChange={(open) => !open && setArchiveConversation(null)}
        title="归档会话"
        description={`确认归档 "${archiveConversation?.title || "新会话"}" 吗？归档后将从侧边栏隐藏。`}
        confirmText="归档"
        onConfirm={() => void handleArchive()}
      />

      <ConfirmDialog
        open={!!deleteConversationTarget}
        onOpenChange={(open) => !open && setDeleteConversationTarget(null)}
        title="删除会话"
        description={`确认删除“${deleteConversationTarget?.title || "新会话"}”吗？删除后无法恢复。`}
        confirmText="删除"
        destructive
        onConfirm={() => void handleDelete()}
      />
    </div>
  );
}
