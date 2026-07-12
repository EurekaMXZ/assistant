"use client";

import { useEffect, useState } from "react";
import { KeyRound, MoreHorizontal, Plus, RotateCw, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import {
  AdminEmpty,
  AdminError,
  AdminLoading,
  AdminPageHeader,
  SavingIcon,
  formatAdminDate,
} from "@/components/admin/admin-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  createAdminCredential,
  listAdminCredentials,
  revokeAdminCredential,
  rotateAdminCredential,
  runAdminCredentialAction,
  updateAdminCredential,
} from "@/lib/api";
import type { ProviderCredential } from "@/lib/types";

export function AdminCredentials() {
  const [items, setItems] = useState<ProviderCredential[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editor, setEditor] = useState<ProviderCredential | "create" | null>(null);
  const [rotateItem, setRotateItem] = useState<ProviderCredential | null>(null);
  const [revokeItem, setRevokeItem] = useState<ProviderCredential | null>(null);
  const [name, setName] = useState("");
  const [baseUrl, setBaseUrl] = useState("https://api.openai.com/v1");
  const [apiKey, setApiKey] = useState("");
  const [saving, setSaving] = useState(false);
  const [actingId, setActingId] = useState("");

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      setItems(await listAdminCredentials());
    } catch (err) {
      setError(err instanceof Error ? err.message : "凭据加载失败");
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => {
    void load();
  }, []);

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
      {loading ? <AdminLoading /> : null}
      {!loading && error ? <AdminError message={error} onRetry={load} /> : null}
      {!loading && !error && !items.length ? <AdminEmpty icon={KeyRound} title="暂无凭据" /> : null}
      {!loading && !error && items.length ? (
        <div className="mt-6 overflow-x-auto border-y">
          <table className="w-full min-w-[840px] text-left text-sm">
            <thead className="text-xs text-muted-foreground">
              <tr className="border-b">
                <th className="py-3 pr-4 font-medium">凭据</th>
                <th className="px-4 py-3 font-medium">地址</th>
                <th className="px-4 py-3 font-medium">最近验证</th>
                <th className="px-4 py-3 font-medium">状态</th>
                <th className="py-3 pl-4 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {items.map((item) => (
                <tr key={item.id}>
                  <td className="py-3 pr-4">
                    <div className="flex items-center gap-3">
                      <span className="grid size-8 place-items-center rounded-md bg-muted">
                        <KeyRound className="size-4" />
                      </span>
                      <div>
                        <p className="font-medium">{item.name}</p>
                        <p className="mt-0.5 font-mono text-xs text-muted-foreground">
                          {item.masked_key}
                        </p>
                      </div>
                    </div>
                  </td>
                  <td className="max-w-72 truncate px-4 py-3 font-mono text-xs text-muted-foreground">
                    {item.base_url}
                  </td>
                  <td className="px-4 py-3">
                    <p className="text-xs">{formatAdminDate(item.last_validated_at)}</p>
                    {item.last_validation_error ? (
                      <p className="mt-1 max-w-56 truncate text-xs text-destructive">
                        {item.last_validation_error}
                      </p>
                    ) : null}
                  </td>
                  <td className="px-4 py-3">
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
                  <td className="py-3 pl-4 text-right">
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
        </div>
      ) : null}

      <Dialog open={editor !== null} onOpenChange={(open) => !open && setEditor(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editor === "create" ? "添加凭据" : "编辑凭据"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="credential-name">名称</Label>
              <Input
                id="credential-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="primary-openai"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="credential-url">Base URL</Label>
              <Input
                id="credential-url"
                value={baseUrl}
                onChange={(event) => setBaseUrl(event.target.value)}
              />
            </div>
            {editor === "create" ? (
              <div className="space-y-2">
                <Label htmlFor="credential-key">API Key</Label>
                <Input
                  id="credential-key"
                  type="password"
                  value={apiKey}
                  onChange={(event) => setApiKey(event.target.value)}
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
                saving || !name.trim() || !baseUrl.trim() || (editor === "create" && !apiKey.trim())
              }
              onClick={() => void save()}
            >
              <SavingIcon saving={saving} />
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!rotateItem} onOpenChange={(open) => !open && setRotateItem(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>轮换 API 密钥</DialogTitle>
            <DialogDescription>轮换后旧密钥立即停止用于新请求，凭据会重新启用。</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="rotate-key">新 API Key</Label>
            <Input
              id="rotate-key"
              type="password"
              value={apiKey}
              onChange={(event) => setApiKey(event.target.value)}
              autoComplete="new-password"
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRotateItem(null)}>
              取消
            </Button>
            <Button disabled={saving || !apiKey.trim()} onClick={() => void rotate()}>
              <SavingIcon saving={saving} />
              轮换密钥
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
