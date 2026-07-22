"use client";

import { useState } from "react";
import { KeyRound, MoreHorizontal, Plus, RotateCw, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { AdminPageHeader } from "@/components/admin/admin-shared";
import { AdminListPage } from "@/components/admin/admin-list-page";
import { tableClasses, tableHeadClass } from "@/components/shared/table-styles";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { FormDialog } from "@/components/shared/form-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { formatDateTime } from "@/lib/format";
import { FormField } from "@/components/ui/form-field";
import {
  createAdminCredential,
  listAdminCredentialsPage,
  revokeAdminCredential,
  rotateAdminCredential,
  runAdminCredentialAction,
  updateAdminCredential,
} from "@/lib/api";
import type { ProviderCredential } from "@/lib/types";
import { useCursorPagination } from "@/lib/use-cursor-pagination";

export function AdminCredentials() {
  const { items, setItems, page, loading, loadingMore, error, loadMoreError, loadMore, reload } =
    useCursorPagination<ProviderCredential>(listAdminCredentialsPage, "凭据加载失败");
  const [editor, setEditor] = useState<ProviderCredential | "create" | null>(null);
  const [rotateItem, setRotateItem] = useState<ProviderCredential | null>(null);
  const [revokeItem, setRevokeItem] = useState<ProviderCredential | null>(null);
  const [name, setName] = useState("");
  const [baseUrl, setBaseUrl] = useState("https://api.openai.com/v1");
  const [apiKey, setApiKey] = useState("");
  const [saving, setSaving] = useState(false);
  const [actingId, setActingId] = useState("");

  const openEditor = (item: ProviderCredential | "create") => {
    setEditor(item);
    setName(item === "create" ? "" : item.name);
    setBaseUrl(item === "create" ? "https://api.openai.com/v1" : item.base_url);
    setApiKey("");
  };

  const save = async () => {
    setSaving(true);
    try {
      const saved =
        editor === "create"
          ? await createAdminCredential({
              provider: "openai",
              name: name.trim(),
              base_url: baseUrl.trim(),
              api_key: apiKey.trim(),
            })
          : editor
            ? await updateAdminCredential(editor.id, {
                name: name.trim(),
                base_url: baseUrl.trim(),
              })
            : null;
      if (!saved) return;
      setItems((current) =>
        editor === "create"
          ? [saved, ...current]
          : current.map((item) => (item.id === saved.id ? saved : item)),
      );
      setEditor(null);
      toast.success(editor === "create" ? "凭据已创建" : "凭据已更新");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "凭据保存失败");
    } finally {
      setSaving(false);
    }
  };

  const rotate = async () => {
    if (!rotateItem) return;
    setSaving(true);
    try {
      const saved = await rotateAdminCredential(rotateItem.id, apiKey.trim());
      setItems((current) => current.map((item) => (item.id === saved.id ? saved : item)));
      setRotateItem(null);
      toast.success("API 密钥已轮换");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "密钥轮换失败");
    } finally {
      setSaving(false);
    }
  };

  const runAction = async (item: ProviderCredential, action: "validate" | "enable" | "disable") => {
    setActingId(item.id);
    try {
      const saved = await runAdminCredentialAction(item.id, action);
      setItems((current) => current.map((entry) => (entry.id === saved.id ? saved : entry)));
      toast.success(
        action === "validate" ? "凭据验证完成" : action === "enable" ? "凭据已启用" : "凭据已停用",
      );
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "凭据操作失败");
    } finally {
      setActingId("");
    }
  };

  const revoke = async () => {
    if (!revokeItem) return;
    try {
      const saved = await revokeAdminCredential(revokeItem.id);
      setItems((current) => current.map((item) => (item.id === saved.id ? saved : item)));
      toast.success("凭据已撤销");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "凭据撤销失败");
    } finally {
      setRevokeItem(null);
    }
  };

  return (
    <div>
      <AdminPageHeader
        title="提供方凭据"
        action={
          <Button size="sm" onClick={() => openEditor("create")}>
            <Plus />
            添加凭据
          </Button>
        }
      />
      <AdminListPage
        ariaLabel="提供方凭据"
        className="mt-6"
        emptyIcon={KeyRound}
        emptyTitle="暂无凭据"
        error={error}
        hasItems={items.length > 0}
        hasMore={page.has_more}
        loading={loading}
        loadingMore={loadingMore}
        loadMoreError={loadMoreError}
        onLoadMore={loadMore}
        onRetry={reload}
      >
        <table className="admin-responsive-table w-[72rem] min-w-full table-fixed text-left text-sm">
          <colgroup>
            <col className="w-[22rem]" />
            <col className="w-[22rem]" />
            <col className="w-[14rem]" />
            <col className="w-[8rem]" />
            <col className="w-[6rem]" />
          </colgroup>
          <thead className={tableHeadClass}>
            <tr className="border-b">
              <th className={tableClasses.headStart}>凭据</th>
              <th className={tableClasses.head}>地址</th>
              <th className={tableClasses.head}>最近验证</th>
              <th className={tableClasses.head}>状态</th>
              <th className={tableClasses.headEnd}>操作</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {items.map((item) => (
              <tr key={item.id}>
                <td className={tableClasses.cellStart} data-primary>
                  <div className="flex min-w-0 items-center gap-3">
                    <span className="grid size-8 shrink-0 place-items-center rounded-md bg-muted">
                      <KeyRound className="size-4" />
                    </span>
                    <div className="min-w-0">
                      <p className="truncate font-medium" title={item.name}>
                        {item.name}
                      </p>
                      <p
                        className="mt-0.5 truncate font-mono text-xs text-muted-foreground"
                        title={item.masked_key}
                      >
                        {item.masked_key}
                      </p>
                    </div>
                  </div>
                </td>
                <td
                  className={`${tableClasses.cell} truncate font-mono text-xs text-muted-foreground`}
                  title={item.base_url}
                  data-label="地址"
                >
                  {item.base_url}
                </td>
                <td className={tableClasses.cell} data-label="最近验证">
                  <p className="text-xs">{formatDateTime(item.last_validated_at)}</p>
                  {item.last_validation_error ? (
                    <p
                      className="mt-1 truncate text-xs text-destructive"
                      title={item.last_validation_error}
                    >
                      {item.last_validation_error}
                    </p>
                  ) : null}
                </td>
                <td className={tableClasses.cell} data-label="状态">
                  <Badge
                    variant={
                      item.status === "enabled"
                        ? "secondary"
                        : item.status === "revoked"
                          ? "destructive"
                          : "outline"
                    }
                  >
                    {item.status === "enabled"
                      ? "已启用"
                      : item.status === "disabled"
                        ? "已停用"
                        : "已撤销"}
                  </Badge>
                </td>
                <td className={tableClasses.cellEnd} data-actions>
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button variant="ghost" size="icon-sm" disabled={actingId === item.id} />
                      }
                    >
                      <MoreHorizontal />
                      <span className="sr-only">凭据操作</span>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="w-44">
                      <DropdownMenuGroup>
                        <DropdownMenuItem
                          disabled={item.status === "revoked"}
                          onClick={() => openEditor(item)}
                        >
                          编辑
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          disabled={item.status === "revoked"}
                          onClick={() => void runAction(item, "validate")}
                        >
                          <ShieldCheck />
                          验证连接
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          disabled={item.status === "revoked"}
                          onClick={() => {
                            setRotateItem(item);
                            setApiKey("");
                          }}
                        >
                          <RotateCw />
                          轮换密钥
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          disabled={item.status === "revoked"}
                          onClick={() =>
                            void runAction(item, item.status === "enabled" ? "disable" : "enable")
                          }
                        >
                          {item.status === "enabled" ? "停用凭据" : "启用凭据"}
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem
                          variant="destructive"
                          disabled={item.status === "revoked"}
                          onClick={() => setRevokeItem(item)}
                        >
                          撤销凭据
                        </DropdownMenuItem>
                      </DropdownMenuGroup>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </AdminListPage>

      <FormDialog
        open={editor !== null}
        onOpenChange={(open) => !open && setEditor(null)}
        title={editor === "create" ? "添加凭据" : "编辑凭据"}
        saving={saving}
        submitDisabled={!name.trim() || !baseUrl.trim() || (editor === "create" && !apiKey.trim())}
        onSubmit={save}
      >
        <FormField label="名称" htmlFor="credential-name">
          <Input
            id="credential-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="primary-openai"
          />
        </FormField>
        <FormField label="Base URL" htmlFor="credential-url">
          <Input
            id="credential-url"
            value={baseUrl}
            onChange={(event) => setBaseUrl(event.target.value)}
          />
        </FormField>
        {editor === "create" ? (
          <FormField label="API Key" htmlFor="credential-key">
            <Input
              id="credential-key"
              type="password"
              value={apiKey}
              onChange={(event) => setApiKey(event.target.value)}
              autoComplete="new-password"
            />
          </FormField>
        ) : null}
      </FormDialog>

      <FormDialog
        open={!!rotateItem}
        onOpenChange={(open) => !open && setRotateItem(null)}
        title="轮换 API 密钥"
        description="轮换后旧密钥立即停止用于新请求，凭据会重新启用。"
        saving={saving}
        submitDisabled={!apiKey.trim()}
        submitText="轮换密钥"
        onSubmit={rotate}
      >
        <FormField label="新 API Key" htmlFor="rotate-key">
          <Input
            id="rotate-key"
            type="password"
            value={apiKey}
            onChange={(event) => setApiKey(event.target.value)}
            autoComplete="new-password"
          />
        </FormField>
      </FormDialog>

      <ConfirmDialog
        open={!!revokeItem}
        onOpenChange={(open) => !open && setRevokeItem(null)}
        title="撤销提供方凭据？"
        description="撤销不可恢复，仍引用该凭据的模型将无法执行新请求。"
        confirmText="撤销凭据"
        destructive
        onConfirm={() => void revoke()}
      />
    </div>
  );
}
