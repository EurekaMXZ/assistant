"use client";

import { useState } from "react";
import { KeyRound, MoreHorizontal, Plus, Trash2, UserRound, Users } from "lucide-react";
import { toast } from "sonner";
import { AdminPageHeader } from "@/components/admin/admin-shared";
import { AdminListPage } from "@/components/admin/admin-list-page";
import { tableClasses, tableHeadClass } from "@/components/shared/table-styles";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { FormDialog } from "@/components/shared/form-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { FormField } from "@/components/ui/form-field";
import { formatDateTime } from "@/lib/format";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
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
      <AdminListPage
        ariaLabel="用户列表"
        className="mt-6"
        emptyIcon={Users}
        emptyTitle="暂无用户"
        error={error}
        hasItems={users.length > 0}
        hasMore={page.has_more}
        loading={loading}
        loadingMore={loadingMore}
        loadMoreError={loadMoreError}
        onLoadMore={loadMore}
        onRetry={reload}
      >
        <table className="admin-responsive-table w-[72rem] min-w-full table-fixed text-left text-sm">
          <colgroup>
            <col className="w-[26rem]" />
            <col className="w-[9rem]" />
            <col className="w-[11rem]" />
            <col className="w-[13rem]" />
            <col className="w-[9rem]" />
            <col className="w-[7rem]" />
          </colgroup>
          <thead className={tableHeadClass}>
            <tr className="border-b">
              <th className={tableClasses.headStart}>用户</th>
              <th className={tableClasses.head}>角色</th>
              <th className={tableClasses.head}>存储空间</th>
              <th className={tableClasses.head}>最近登录</th>
              <th className={tableClasses.head}>状态</th>
              <th className={tableClasses.headEnd}>操作</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {users.map((item) => {
              const manageable = canManageUser(actor, item);
              return (
                <tr key={item.id}>
                  <td className={tableClasses.cellStart} data-primary>
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
                  <td className={`${tableClasses.cell} text-muted-foreground`} data-label="角色">
                    {roleLabels[item.role]}
                  </td>
                  <td
                    className={`${tableClasses.cell} whitespace-nowrap text-xs text-muted-foreground`}
                    data-label="存储空间"
                  >
                    {formatStorageBytes(item.storage_used_bytes)} /{" "}
                    {formatStorageBytes(item.storage_quota_bytes)}
                  </td>
                  <td
                    className={`${tableClasses.cell} whitespace-nowrap text-xs text-muted-foreground`}
                    data-label="最近登录"
                  >
                    {formatDateTime(item.last_login_at)}
                  </td>
                  <td className={tableClasses.cell} data-label="状态">
                    <Badge variant={item.status === "active" ? "secondary" : "outline"}>
                      {item.status === "active" ? "正常" : "已停用"}
                    </Badge>
                  </td>
                  <td className={tableClasses.cellEnd} data-actions>
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
                      <span className="sr-only">无可用操作</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </AdminListPage>

      <FormDialog
        open={editor !== null}
        onOpenChange={(open) => !open && setEditor(null)}
        title={editor === "create" ? "创建用户" : "编辑用户"}
        saving={saving}
        submitDisabled={
          !email.trim() ||
          !username.trim() ||
          (editor !== "create" &&
            (!Number.isFinite(Number(storageQuotaMB)) || Number(storageQuotaMB) < 0)) ||
          (editor === "create" && password.length < 8)
        }
        onSubmit={save}
      >
        <FormField label="邮箱" htmlFor="admin-user-email">
          <Input
            id="admin-user-email"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
        </FormField>
        <FormField label="用户名" htmlFor="admin-username">
          <Input
            id="admin-username"
            value={username}
            onChange={(event) => setUsername(event.target.value)}
          />
        </FormField>
        {actor.role === "system" ? (
          <FormField label="角色" htmlFor="admin-user-role">
            <Select
              items={manageableRoles.map((item) => ({ value: item, label: roleLabels[item] }))}
              value={role}
              onValueChange={(value) => value && setRole(value)}
            >
              <SelectTrigger id="admin-user-role">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {manageableRoles.map((item) => (
                  <SelectItem key={item} value={item}>
                    {roleLabels[item]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FormField>
        ) : null}
        {editor !== "create" ? (
          <FormField label="存储配额（MB）" htmlFor="admin-user-storage-quota">
            <Input
              id="admin-user-storage-quota"
              type="number"
              min="0"
              step="1"
              value={storageQuotaMB}
              onChange={(event) => setStorageQuotaMB(event.target.value)}
            />
          </FormField>
        ) : null}
        {editor === "create" ? (
          <FormField label="初始密码" htmlFor="admin-user-password">
            <Input
              id="admin-user-password"
              type="password"
              minLength={8}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="new-password"
            />
          </FormField>
        ) : null}
      </FormDialog>

      <FormDialog
        open={!!resetUser}
        onOpenChange={(open) => !open && setResetUser(null)}
        title={`重置 ${resetUser?.username || "用户"} 的密码`}
        saving={saving}
        submitDisabled={password.length < 8}
        submitText="重置密码"
        onSubmit={resetPassword}
      >
        <FormField label="新密码" htmlFor="reset-user-password">
          <Input
            id="reset-user-password"
            type="password"
            minLength={8}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            autoComplete="new-password"
          />
        </FormField>
      </FormDialog>

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
