"use client";

import { useState } from "react";
import { KeyRound, MoreHorizontal, Plus, Trash2, UserRound, Users } from "lucide-react";
import { toast } from "sonner";
import {
  AdminEmpty,
  AdminError,
  AdminLoading,
  AdminPageHeader,
  SavingIcon,
  adminSelectClass,
  adminTableHeadClass,
  adminTableScrollClass,
  formatAdminDate,
} from "@/components/admin/admin-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { CursorTableScroll } from "@/components/ui/cursor-table-scroll";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import {
  createAdminUser,
  deleteAdminUser,
  listAdminUsersPage,
  resetAdminUserPassword,
  updateAdminUser,
} from "@/lib/api";
import { canManageUser, manageableUserRoles } from "@/lib/permissions";
import type { User, UserRole } from "@/lib/types";
import { useCursorPagination } from "@/lib/use-cursor-pagination";
import { formatStorageBytes } from "@/lib/storage";

const roleLabels: Record<UserRole, string> = {
  system: "系统",
  admin: "管理员",
  user: "用户",
};

export function AdminUsers({ actor }: { actor: User }) {
  const {
    items: users,
    setItems: setUsers,
    page,
    loading,
    loadingMore,
    error,
    loadMoreError,
    loadMore,
    reload,
  } = useCursorPagination<User>(listAdminUsersPage, "用户加载失败");
  const [editor, setEditor] = useState<User | "create" | null>(null);
  const [resetUser, setResetUser] = useState<User | null>(null);
  const [deleteUser, setDeleteUser] = useState<User | null>(null);
  const [email, setEmail] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<Exclude<UserRole, "system">>("user");
  const [storageQuotaMB, setStorageQuotaMB] = useState("512");
  const [saving, setSaving] = useState(false);
  const manageableRoles = manageableUserRoles(actor);

  const openEditor = (item: User | "create") => {
    setEditor(item);
    setEmail(item === "create" ? "" : item.email);
    setUsername(item === "create" ? "" : item.username);
    setRole(item === "create" || item.role === "system" ? "user" : item.role);
    setStorageQuotaMB(
      item === "create" ? "512" : String(Math.round(item.storage_quota_bytes / (1024 * 1024))),
    );
    setPassword("");
  };

  const save = async () => {
    const quotaMB = Number(storageQuotaMB);
    if (editor !== "create" && (!Number.isFinite(quotaMB) || quotaMB < 0)) {
      toast.error("存储配额必须是非负数");
      return;
    }
    setSaving(true);
    try {
      const saved =
        editor === "create"
          ? await createAdminUser({
              email: email.trim(),
              username: username.trim(),
              password,
              role,
              status: "active",
            })
          : editor
            ? await updateAdminUser(editor.id, {
                email: email.trim(),
                username: username.trim(),
                role,
                storage_quota_bytes: Math.round(quotaMB * 1024 * 1024),
              })
            : null;
      if (!saved) return;
      setUsers((items) =>
        editor === "create"
          ? [saved, ...items]
          : items.map((item) => (item.id === saved.id ? saved : item)),
      );
      setEditor(null);
      toast.success(editor === "create" ? "用户已创建" : "用户已更新");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "用户保存失败");
    } finally {
      setSaving(false);
    }
  };

  const toggleStatus = async (item: User) => {
    try {
      const saved = await updateAdminUser(item.id, {
        status: item.status === "active" ? "disabled" : "active",
      });
      setUsers((items) => items.map((user) => (user.id === saved.id ? saved : user)));
      toast.success(saved.status === "active" ? "用户已启用" : "用户已停用");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "用户状态更新失败");
    }
  };

  const resetPassword = async () => {
    if (!resetUser) return;
    setSaving(true);
    try {
      await resetAdminUserPassword(resetUser.id, password);
      setResetUser(null);
      toast.success("密码已重置");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "密码重置失败");
    } finally {
      setSaving(false);
    }
  };

  const removeUser = async () => {
    if (!deleteUser) return;
    try {
      await deleteAdminUser(deleteUser.id);
      setUsers((items) => items.filter((item) => item.id !== deleteUser.id));
      setDeleteUser(null);
      toast.success("用户已删除");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "用户删除失败");
    }
  };

  return (
    <div>
      <AdminPageHeader
        title="用户"
        action={
          <Button size="sm" onClick={() => openEditor("create")}>
            <Plus />
            创建用户
          </Button>
        }
      />
      {loading ? <AdminLoading /> : null}
      {!loading && error ? <AdminError message={error} onRetry={reload} /> : null}
      {!loading && !error && !users.length ? <AdminEmpty icon={Users} title="暂无用户" /> : null}
      {!loading && !error && users.length ? (
        <CursorTableScroll
          className={`${adminTableScrollClass} mt-6`}
          hasMore={page.has_more}
          loadingMore={loadingMore}
          loadMoreError={loadMoreError}
          onLoadMore={loadMore}
          aria-label="用户列表"
        >
          <table className="w-[72rem] min-w-full table-fixed text-left text-sm">
            <colgroup>
              <col className="w-[26rem]" />
              <col className="w-[9rem]" />
              <col className="w-[11rem]" />
              <col className="w-[13rem]" />
              <col className="w-[9rem]" />
              <col className="w-[7rem]" />
            </colgroup>
            <thead className={adminTableHeadClass}>
              <tr className="border-b">
                <th className="py-3 pr-4 font-medium">用户</th>
                <th className="px-4 py-3 font-medium">角色</th>
                <th className="px-4 py-3 font-medium">存储空间</th>
                <th className="px-4 py-3 font-medium">最近登录</th>
                <th className="px-4 py-3 font-medium">状态</th>
                <th className="py-3 pl-4 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {users.map((item) => {
                const manageable = canManageUser(actor, item);
                return (
                  <tr key={item.id}>
                    <td className="py-3 pr-4">
                      <div className="flex min-w-0 items-center gap-3">
                        <span className="grid size-8 shrink-0 place-items-center rounded-md bg-muted">
                          <UserRound className="size-4" />
                        </span>
                        <div className="min-w-0">
                          <p className="truncate font-medium" title={item.username}>
                            {item.username}
                          </p>
                          <p
                            className="mt-0.5 truncate text-xs text-muted-foreground"
                            title={item.email}
                          >
                            {item.email}
                          </p>
                        </div>
                      </div>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">{roleLabels[item.role]}</td>
                    <td className="whitespace-nowrap px-4 py-3 text-xs text-muted-foreground">
                      {formatStorageBytes(item.storage_used_bytes)} /{" "}
                      {formatStorageBytes(item.storage_quota_bytes)}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-xs text-muted-foreground">
                      {formatAdminDate(item.last_login_at)}
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant={item.status === "active" ? "secondary" : "outline"}>
                        {item.status === "active" ? "正常" : "已停用"}
                      </Badge>
                    </td>
                    <td className="py-3 pl-4 text-right">
                      {manageable ? (
                        <DropdownMenu>
                          <DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" />}>
                            <MoreHorizontal />
                            <span className="sr-only">用户操作</span>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end" className="w-40">
                            <DropdownMenuGroup>
                              <DropdownMenuItem onClick={() => openEditor(item)}>
                                编辑资料
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                onClick={() => {
                                  setResetUser(item);
                                  setPassword("");
                                }}
                              >
                                <KeyRound />
                                重置密码
                              </DropdownMenuItem>
                              <DropdownMenuItem onClick={() => void toggleStatus(item)}>
                                {item.status === "active" ? "停用用户" : "启用用户"}
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                variant="destructive"
                                onClick={() => setDeleteUser(item)}
                              >
                                <Trash2 />
                                删除用户
                              </DropdownMenuItem>
                            </DropdownMenuGroup>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      ) : (
                        <span className="text-muted-foreground">-</span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </CursorTableScroll>
      ) : null}

      <Dialog open={editor !== null} onOpenChange={(open) => !open && setEditor(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editor === "create" ? "创建用户" : "编辑用户"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="admin-user-email">邮箱</Label>
              <Input
                id="admin-user-email"
                type="email"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="admin-username">用户名</Label>
              <Input
                id="admin-username"
                value={username}
                onChange={(event) => setUsername(event.target.value)}
              />
            </div>
            {actor.role === "system" ? (
              <div className="space-y-2">
                <Label htmlFor="admin-user-role">角色</Label>
                <select
                  id="admin-user-role"
                  className={adminSelectClass}
                  value={role}
                  onChange={(event) => setRole(event.target.value as Exclude<UserRole, "system">)}
                >
                  {manageableRoles.map((item) => (
                    <option key={item} value={item}>
                      {roleLabels[item]}
                    </option>
                  ))}
                </select>
              </div>
            ) : null}
            {editor !== "create" ? (
              <div className="space-y-2">
                <Label htmlFor="admin-user-storage-quota">存储配额（MB）</Label>
                <Input
                  id="admin-user-storage-quota"
                  type="number"
                  min="0"
                  step="1"
                  value={storageQuotaMB}
                  onChange={(event) => setStorageQuotaMB(event.target.value)}
                />
              </div>
            ) : null}
            {editor === "create" ? (
              <div className="space-y-2">
                <Label htmlFor="admin-user-password">初始密码</Label>
                <Input
                  id="admin-user-password"
                  type="password"
                  minLength={8}
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  autoComplete="new-password"
                />
              </div>
            ) : null}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditor(null)}>
              取消
            </Button>
            <Button
              disabled={
                saving ||
                !email.trim() ||
                !username.trim() ||
                (editor !== "create" &&
                  (!Number.isFinite(Number(storageQuotaMB)) || Number(storageQuotaMB) < 0)) ||
                (editor === "create" && password.length < 8)
              }
              onClick={() => void save()}
            >
              <SavingIcon saving={saving} />
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!resetUser} onOpenChange={(open) => !open && setResetUser(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>重置 {resetUser?.username} 的密码</DialogTitle>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="reset-user-password">新密码</Label>
            <Input
              id="reset-user-password"
              type="password"
              minLength={8}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="new-password"
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setResetUser(null)}>
              取消
            </Button>
            <Button disabled={saving || password.length < 8} onClick={() => void resetPassword()}>
              <SavingIcon saving={saving} />
              重置密码
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={deleteUser !== null}
        onOpenChange={(open) => !open && setDeleteUser(null)}
        title="删除用户"
        description={`确认删除“${deleteUser?.username || "此用户"}”吗？该用户将无法登录，操作不可恢复。`}
        confirmText="删除"
        destructive
        onConfirm={() => void removeUser()}
      />
    </div>
  );
}
