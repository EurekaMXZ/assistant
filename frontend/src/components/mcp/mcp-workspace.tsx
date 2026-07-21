"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  ArrowLeft,
  CircleAlert,
  CircleDashed,
  FlaskConical,
  Loader2,
  Plus,
  RefreshCw,
  Save,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import {
  createMCPServer,
  deleteMCPServer,
  getMCPServer,
  listMCPServers,
  testMCPServer,
  updateMCPServer,
} from "@/lib/api";
import type {
  CreateMCPSecretInput,
  MCPSecret,
  UpdateMCPSecretInput,
  UserMCPServer,
} from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

type EditorTarget = { kind: "new" } | { kind: "server"; id: string } | null;
type MobilePane = "list" | "editor";
type BusyOperation = "saving" | "testing" | "deleting" | null;
type SecretKind = "parameter" | "header";

interface SecretDraft {
  id: string;
  name: string;
  value: string;
  configured: boolean;
  keyHint?: string;
  originalName?: string;
}

interface MCPServerForm {
  name: string;
  slug: string;
  endpointURL: string;
  enabled: boolean;
  parameters: SecretDraft[];
  headers: SecretDraft[];
  enabledTools: string[];
}

const parameterNamePattern = /^[A-Za-z0-9._~-]+$/;
const headerNamePattern = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;
const slugPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const managedHeaders = new Set([
  "accept",
  "connection",
  "content-length",
  "content-type",
  "host",
  "keep-alive",
  "mcp-protocol-version",
  "mcp-session-id",
  "proxy-authorization",
  "proxy-connection",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
]);

let secretDraftSequence = 0;

function nextSecretDraftID(kind: SecretKind) {
  secretDraftSequence += 1;
  return `${kind}-${secretDraftSequence}`;
}

function emptyForm(): MCPServerForm {
  return {
    name: "",
    slug: "",
    endpointURL: "",
    enabled: true,
    parameters: [],
    headers: [],
    enabledTools: [],
  };
}

function secretDrafts(secrets: MCPSecret[], kind: SecretKind): SecretDraft[] {
  return secrets.map((secret) => ({
    id: nextSecretDraftID(kind),
    name: secret.name,
    value: "",
    configured: secret.configured,
    keyHint: secret.key_hint,
    originalName: secret.name,
  }));
}

function formFromServer(server: UserMCPServer): MCPServerForm {
  return {
    name: server.name,
    slug: server.slug,
    endpointURL: server.endpoint_url,
    enabled: server.enabled,
    parameters: secretDrafts(server.parameters, "parameter"),
    headers: secretDrafts(server.headers, "header"),
    enabledTools: server.tools.filter((tool) => tool.enabled).map((tool) => tool.name),
  };
}

function formFingerprint(form: MCPServerForm) {
  return JSON.stringify(form);
}

function validationMessage(form: MCPServerForm, creating: boolean): string | null {
  const name = form.name.trim();
  if (!name || Array.from(name).length > 100) return "名称应为 1 至 100 个字符";

  const slug = form.slug.trim();
  if (!slugPattern.test(slug) || slug.length > 64) {
    return "标识只能包含小写字母、数字和单个连字符";
  }

  const endpointError = validateEndpointURL(form.endpointURL);
  if (endpointError) return endpointError;

  return (
    validateSecretDrafts(form.parameters, "parameter", creating) ||
    validateSecretDrafts(form.headers, "header", creating)
  );
}

function validateEndpointURL(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed || trimmed.length > 2048) return "请输入有效的服务器 URL";
  try {
    const url = new URL(trimmed);
    if (url.protocol !== "http:" && url.protocol !== "https:") {
      return "服务器 URL 必须使用 HTTP 或 HTTPS";
    }
    if (url.username || url.password || url.search || url.hash) {
      return "服务器 URL 不能包含账户信息、查询参数或片段";
    }
  } catch {
    return "请输入有效的服务器 URL";
  }
  return null;
}

function validateSecretDrafts(
  drafts: SecretDraft[],
  kind: SecretKind,
  creating: boolean,
): string | null {
  const label = kind === "parameter" ? "查询参数" : "请求头";
  if (drafts.length > 32) return `${label}最多可添加 32 项`;

  const names = new Set<string>();
  for (const draft of drafts) {
    const name = draft.name.trim();
    if (!name || name.length > 128) return `${label}名称应为 1 至 128 个字符`;
    if (kind === "parameter" && !parameterNamePattern.test(name)) {
      return "查询参数名称包含不支持的字符";
    }
    if (kind === "header" && !headerNamePattern.test(name)) {
      return "请求头名称包含不支持的字符";
    }
    if (kind === "header" && managedHeaders.has(name.toLowerCase())) {
      return `请求头 ${name} 由系统管理，不能手动设置`;
    }

    const lookupName = kind === "header" ? name.toLowerCase() : name;
    if (names.has(lookupName)) return `${label}名称不能重复`;
    names.add(lookupName);

    if (draft.value.length > 8192) return `${label}值不能超过 8192 个字符`;
    const canKeepExisting =
      !creating &&
      draft.configured &&
      sameSecretName(draft.originalName, name, kind) &&
      draft.value === "";
    if (!draft.value && !canKeepExisting) return `请填写 ${name} 的值`;
  }
  return null;
}

function sameSecretName(originalName: string | undefined, name: string, kind: SecretKind) {
  if (!originalName) return false;
  return kind === "header"
    ? originalName.toLowerCase() === name.toLowerCase()
    : originalName === name;
}

function createSecretPayload(drafts: SecretDraft[]): CreateMCPSecretInput[] {
  return drafts.map((draft) => ({ name: draft.name.trim(), value: draft.value }));
}

function updateSecretPayload(drafts: SecretDraft[]): UpdateMCPSecretInput[] {
  return drafts.map((draft) => ({
    name: draft.name.trim(),
    ...(draft.value ? { value: draft.value } : {}),
  }));
}

function localizedError(error: unknown, fallback: string) {
  if (!(error instanceof Error)) return fallback;
  const knownMessages: Record<string, string> = {
    "MCP server slug already exists": "该服务器标识已被使用",
    "endpoint_url host is not allowed": "该服务器地址不允许访问",
    "unable to connect to MCP server": "无法连接到 MCP 服务器",
    "MCP server tools/list failed": "无法读取 MCP 工具清单",
    "MCP server validation failed": "MCP 服务器验证失败",
    "secret value is required for a new entry": "新增或改名的凭据必须填写值",
  };
  return knownMessages[error.message] || fallback;
}

export function MCPWorkspace() {
  const [servers, setServers] = useState<UserMCPServer[]>([]);
  const [target, setTargetState] = useState<EditorTarget>(null);
  const [activeServer, setActiveServer] = useState<UserMCPServer | null>(null);
  const [form, setForm] = useState<MCPServerForm>(emptyForm);
  const [baseline, setBaseline] = useState<string | null>(null);
  const [mobilePane, setMobilePane] = useState<MobilePane>("list");
  const [listLoading, setListLoading] = useState(true);
  const [listError, setListError] = useState("");
  const [listReload, setListReload] = useState(0);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");
  const [detailReload, setDetailReload] = useState(0);
  const [busyOperation, setBusyOperation] = useState<BusyOperation>(null);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const targetRef = useRef<EditorTarget>(null);
  const listGenerationRef = useRef(0);
  const detailGenerationRef = useRef(0);

  const setTarget = useCallback((next: EditorTarget) => {
    detailGenerationRef.current += 1;
    targetRef.current = next;
    setTargetState(next);
    if (next?.kind === "server") {
      setActiveServer(null);
      setDetailError("");
      setDetailLoading(true);
    }
  }, []);

  const showNewEditor = useCallback(() => {
    const nextForm = emptyForm();
    setTarget({ kind: "new" });
    setActiveServer(null);
    setDetailError("");
    setDetailLoading(false);
    setForm(nextForm);
    setBaseline(formFingerprint(nextForm));
  }, [setTarget]);

  const loadServerIntoEditor = useCallback((server: UserMCPServer) => {
    const nextForm = formFromServer(server);
    setActiveServer(server);
    setDetailLoading(false);
    setForm(nextForm);
    setBaseline(formFingerprint(nextForm));
    setServers((current) => current.map((item) => (item.id === server.id ? server : item)));
  }, []);

  useEffect(() => {
    const generation = ++listGenerationRef.current;
    const controller = new AbortController();
    setListLoading(true);
    setListError("");

    void listMCPServers(controller.signal)
      .then((items) => {
        if (controller.signal.aborted || listGenerationRef.current !== generation) return;
        setServers(items);

        const currentTarget = targetRef.current;
        if (
          currentTarget?.kind === "server" &&
          items.some((server) => server.id === currentTarget.id)
        ) {
          return;
        }
        if (currentTarget?.kind === "new") return;

        if (items[0]) {
          setTarget({ kind: "server", id: items[0].id });
        } else {
          showNewEditor();
          setMobilePane("editor");
        }
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted || listGenerationRef.current !== generation) return;
        setListError(localizedError(error, "服务器列表加载失败"));
      })
      .finally(() => {
        if (!controller.signal.aborted && listGenerationRef.current === generation) {
          setListLoading(false);
        }
      });

    return () => controller.abort();
  }, [listReload, setTarget, showNewEditor]);

  const selectedServerID = target?.kind === "server" ? target.id : null;

  useEffect(() => {
    if (!selectedServerID || activeServer?.id === selectedServerID) return;
    const generation = ++detailGenerationRef.current;
    const controller = new AbortController();
    setDetailLoading(true);
    setDetailError("");
    setActiveServer(null);

    void getMCPServer(selectedServerID, controller.signal)
      .then((server) => {
        if (controller.signal.aborted || detailGenerationRef.current !== generation) return;
        loadServerIntoEditor(server);
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted || detailGenerationRef.current !== generation) return;
        setDetailError(localizedError(error, "服务器详情加载失败"));
      })
      .finally(() => {
        if (!controller.signal.aborted && detailGenerationRef.current === generation) {
          setDetailLoading(false);
        }
      });

    return () => controller.abort();
  }, [activeServer?.id, detailReload, loadServerIntoEditor, selectedServerID]);

  const isDirty = baseline !== null && formFingerprint(form) !== baseline;
  const isBusy = busyOperation !== null;
  const creating = target?.kind === "new";

  const invalidateListRequest = () => {
    listGenerationRef.current += 1;
    setListLoading(false);
  };

  const guardUnsavedChanges = () => {
    if (!isDirty) return false;
    toast.error("请先保存或撤销当前更改");
    return true;
  };

  const selectServer = (serverID: string) => {
    if (isBusy) return;
    if (target?.kind === "server" && target.id === serverID) {
      setMobilePane("editor");
      return;
    }
    if (guardUnsavedChanges()) return;
    setTarget({ kind: "server", id: serverID });
    setMobilePane("editor");
  };

  const startCreating = () => {
    if (isBusy || guardUnsavedChanges()) return;
    showNewEditor();
    setMobilePane("editor");
  };

  const resetChanges = () => {
    if (creating) {
      showNewEditor();
      return;
    }
    if (activeServer) loadServerIntoEditor(activeServer);
  };

  const save = async () => {
    if (!target || isBusy) return;
    const error = validationMessage(form, creating);
    if (error) {
      toast.error(error);
      return;
    }

    invalidateListRequest();
    setBusyOperation("saving");
    try {
      const server = creating
        ? await createMCPServer({
            name: form.name,
            slug: form.slug,
            endpoint_url: form.endpointURL,
            enabled: form.enabled,
            parameters: createSecretPayload(form.parameters),
            headers: createSecretPayload(form.headers),
          })
        : await updateMCPServer(target.id, {
            name: form.name,
            slug: form.slug,
            endpoint_url: form.endpointURL,
            enabled: form.enabled,
            parameters: updateSecretPayload(form.parameters),
            headers: updateSecretPayload(form.headers),
            enabled_tools: form.enabledTools,
          });

      if (creating) {
        setServers((current) => [server, ...current]);
        setTarget({ kind: "server", id: server.id });
      }
      loadServerIntoEditor(server);
      toast.success(creating ? "MCP 服务器已创建" : "MCP 服务器已保存");
    } catch (error) {
      toast.error(localizedError(error, creating ? "服务器创建失败" : "服务器保存失败"));
    } finally {
      setBusyOperation(null);
    }
  };

  const testConnection = async () => {
    if (target?.kind !== "server" || isBusy || isDirty) return;
    const serverID = target.id;
    invalidateListRequest();
    setBusyOperation("testing");
    try {
      const server = await testMCPServer(serverID);
      if (targetRef.current?.kind === "server" && targetRef.current.id === serverID) {
        loadServerIntoEditor(server);
      }
      if (server.last_validation_status === "valid") toast.success("连接测试通过");
      else toast.error(validationErrorText(server.last_validation_error));
    } catch (error) {
      toast.error(localizedError(error, "连接测试失败"));
    } finally {
      setBusyOperation(null);
    }
  };

  const removeServer = async () => {
    if (target?.kind !== "server" || isBusy) return;
    const serverID = target.id;
    invalidateListRequest();
    setBusyOperation("deleting");
    try {
      await deleteMCPServer(serverID);
      const remaining = servers.filter((server) => server.id !== serverID);
      setServers(remaining);
      if (remaining[0]) {
        setTarget({ kind: "server", id: remaining[0].id });
        setMobilePane("list");
      } else {
        showNewEditor();
        setMobilePane("editor");
      }
      toast.success("MCP 服务器已删除");
    } catch (error) {
      toast.error(localizedError(error, "服务器删除失败"));
    } finally {
      setBusyOperation(null);
    }
  };

  const returnToList = () => {
    if (isBusy || guardUnsavedChanges()) return;
    setMobilePane("list");
  };

  return (
    <div className="grid h-full min-h-0 grid-cols-1 md:grid-cols-[minmax(15rem,20rem)_minmax(0,1fr)]">
      <aside
        className={cn(
          "min-h-0 flex-col border-r bg-sidebar/40 md:flex",
          mobilePane === "list" ? "flex" : "hidden",
        )}
      >
        <div className="flex h-16 shrink-0 items-center justify-between gap-3 border-b px-4">
          <div className="min-w-0">
            <h1 className="truncate text-sm font-semibold">MCP 服务器</h1>
            <p className="mt-0.5 text-xs text-muted-foreground">{servers.length} 个服务器</p>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <Button type="button" size="sm" disabled={isBusy} onClick={startCreating}>
              <Plus className="size-4" />
              新增
            </Button>
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto">
          {listLoading && servers.length === 0 ? (
            <div className="space-y-1 p-2">
              <Skeleton className="h-16 w-full" />
              <Skeleton className="h-16 w-full" />
              <Skeleton className="h-16 w-full" />
            </div>
          ) : listError && servers.length === 0 ? (
            <div className="flex min-h-56 flex-col items-center justify-center px-5 text-center">
              <CircleAlert className="size-5 text-muted-foreground" />
              <p className="mt-3 text-sm font-medium">{listError}</p>
              <Button
                className="mt-4"
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setListReload((value) => value + 1)}
              >
                <RefreshCw className="size-4" />
                重新加载
              </Button>
            </div>
          ) : servers.length === 0 ? (
            <div className="flex min-h-64 flex-col items-center justify-center px-6 text-center">
              <CircleDashed className="size-5 text-muted-foreground" />
              <p className="mt-3 text-sm font-medium">还没有 MCP 服务器</p>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">
                添加一个 HTTP 端点以发现可用工具。
              </p>
              <Button className="mt-4" type="button" size="sm" onClick={startCreating}>
                <Plus className="size-4" />
                新建服务器
              </Button>
            </div>
          ) : (
            <div className="space-y-1 p-2">
              {servers.map((server) => (
                <ServerListItem
                  key={server.id}
                  server={server}
                  active={target?.kind === "server" && target.id === server.id}
                  disabled={isBusy}
                  onClick={() => selectServer(server.id)}
                />
              ))}
            </div>
          )}
        </div>
      </aside>

      <main
        className={cn(
          "min-h-0 min-w-0 flex-col bg-background md:flex",
          mobilePane === "editor" ? "flex" : "hidden",
        )}
      >
        {detailLoading || !target ? (
          <EditorLoading />
        ) : detailError ? (
          <div className="flex min-h-72 flex-1 flex-col items-center justify-center px-6 text-center">
            <CircleAlert className="size-6 text-muted-foreground" />
            <p className="mt-3 text-sm font-medium">{detailError}</p>
            <Button
              className="mt-4"
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setDetailReload((value) => value + 1)}
            >
              <RefreshCw className="size-4" />
              重新加载
            </Button>
          </div>
        ) : (
          <>
            <EditorHeader
              creating={creating}
              dirty={isDirty}
              busyOperation={busyOperation}
              enabled={form.enabled}
              name={form.name}
              onBack={returnToList}
              onDelete={() => setDeleteConfirmOpen(true)}
              onReset={resetChanges}
              onSave={() => void save()}
              onTest={() => void testConnection()}
              onEnabledChange={(enabled) =>
                setForm((current) => ({ ...current, enabled }))
              }
            />

            <div className="min-h-0 flex-1 overflow-y-auto">
              <div className="mx-auto w-full max-w-4xl px-4 py-6 sm:px-7 lg:px-10 lg:py-8">
                <section className="border-b pb-8">
                  <div className="grid gap-5 sm:grid-cols-2">
                    <div className="space-y-2">
                      <Label htmlFor="mcp-name">名称</Label>
                      <Input
                        id="mcp-name"
                        value={form.name}
                        maxLength={100}
                        disabled={isBusy}
                        placeholder="例如：内部知识库"
                        onChange={(event) =>
                          setForm((current) => ({ ...current, name: event.target.value }))
                        }
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="mcp-slug">标识</Label>
                      <Input
                        id="mcp-slug"
                        value={form.slug}
                        maxLength={64}
                        disabled={isBusy}
                        spellCheck={false}
                        placeholder="internal-knowledge"
                        className="font-mono text-sm"
                        onChange={(event) =>
                          setForm((current) => ({ ...current, slug: event.target.value }))
                        }
                      />
                    </div>
                    <div className="space-y-2 sm:col-span-2">
                      <Label htmlFor="mcp-endpoint">服务器 URL</Label>
                      <Input
                        id="mcp-endpoint"
                        type="url"
                        value={form.endpointURL}
                        maxLength={2048}
                        disabled={isBusy}
                        spellCheck={false}
                        placeholder="https://mcp.example.com/mcp"
                        className="font-mono text-sm"
                        onChange={(event) =>
                          setForm((current) => ({ ...current, endpointURL: event.target.value }))
                        }
                      />
                    </div>
                  </div>
                </section>

                <SecretEditor
                  title="查询参数"
                  kind="parameter"
                  rows={form.parameters}
                  disabled={isBusy}
                  onChange={(parameters) => setForm((current) => ({ ...current, parameters }))}
                />

                <SecretEditor
                  title="请求头"
                  kind="header"
                  rows={form.headers}
                  disabled={isBusy}
                  onChange={(headers) => setForm((current) => ({ ...current, headers }))}
                />

                {!creating && activeServer ? (
                  <ToolsSection
                    server={activeServer}
                    enabledTools={form.enabledTools}
                    disabled={isBusy}
                    onChange={(enabledTools) =>
                      setForm((current) => ({ ...current, enabledTools }))
                    }
                  />
                ) : null}
              </div>
            </div>
          </>
        )}
      </main>

      <ConfirmDialog
        open={deleteConfirmOpen}
        onOpenChange={(open) => !isBusy && setDeleteConfirmOpen(open)}
        title="删除 MCP 服务器"
        description={`确认删除“${activeServer?.name || "此服务器"}”吗？已保存的凭据和工具配置也会被删除，且无法恢复。`}
        confirmText="删除"
        destructive
        onConfirm={() => void removeServer()}
      />
    </div>
  );
}

function ServerListItem({
  server,
  active,
  disabled,
  onClick,
}: {
  server: UserMCPServer;
  active: boolean;
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      disabled={disabled}
      aria-current={active ? "page" : undefined}
      onClick={onClick}
      className={cn(
        "h-auto min-h-12 w-full min-w-0 justify-start rounded-lg px-2 py-2 text-left text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
        active && "bg-sidebar-accent text-sidebar-accent-foreground",
      )}
    >
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{server.name}</span>
        <span className="mt-0.5 block truncate font-mono text-xs text-muted-foreground">
          {server.slug}
        </span>
      </span>
      {!server.enabled ? (
        <span className="shrink-0 text-xs text-muted-foreground">已停用</span>
      ) : null}
    </Button>
  );
}

function EditorLoading() {
  return (
    <div className="min-h-0 flex-1 overflow-hidden">
      <div className="flex h-16 items-center justify-between border-b px-4 sm:px-7">
        <Skeleton className="h-5 w-36" />
        <Skeleton className="h-8 w-44" />
      </div>
      <div className="mx-auto max-w-4xl space-y-7 px-4 py-7 sm:px-7 lg:px-10">
        <Skeleton className="h-5 w-24" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-36 w-full" />
        <Skeleton className="h-32 w-full" />
      </div>
    </div>
  );
}

function EditorHeader({
  creating,
  dirty,
  busyOperation,
  enabled,
  name,
  onBack,
  onDelete,
  onReset,
  onSave,
  onTest,
  onEnabledChange,
}: {
  creating: boolean;
  dirty: boolean;
  busyOperation: BusyOperation;
  enabled: boolean;
  name: string;
  onBack: () => void;
  onDelete: () => void;
  onReset: () => void;
  onSave: () => void;
  onTest: () => void;
  onEnabledChange: (enabled: boolean) => void;
}) {
  const busy = busyOperation !== null;
  return (
    <header className="z-10 flex min-h-16 shrink-0 flex-wrap items-center justify-between gap-2 border-b bg-background px-3 py-2 sm:px-5 lg:px-7">
      <div className="flex min-w-0 items-center gap-2">
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          className="md:hidden"
          disabled={busy}
          onClick={onBack}
        >
          <ArrowLeft className="size-4" />
          <span className="sr-only">返回服务器列表</span>
        </Button>
        <div className="min-w-0">
          <h2 className="truncate text-sm font-semibold">{creating ? "新建服务器" : name}</h2>
          {dirty ? <p className="mt-0.5 text-xs text-muted-foreground">有未保存的更改</p> : null}
        </div>
      </div>
      <div className="ml-auto flex min-w-0 items-center gap-1 sm:gap-2">
        <label className="flex min-h-8 shrink-0 cursor-pointer items-center gap-2 rounded-md px-2 text-sm hover:bg-muted">
          <input
            type="checkbox"
            checked={enabled}
            disabled={busy}
            onChange={(event) => onEnabledChange(event.target.checked)}
            className="size-4 shrink-0 accent-foreground"
          />
          <span>启用</span>
        </label>
        {dirty ? (
          <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={onReset}>
            撤销
          </Button>
        ) : null}
        {!creating ? (
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={busy || dirty}
                  onClick={onTest}
                />
              }
            >
              {busyOperation === "testing" ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <FlaskConical className="size-4" />
              )}
              <span className="hidden sm:inline">测试连接</span>
              <span className="sr-only sm:hidden">测试连接</span>
            </TooltipTrigger>
            <TooltipContent>{dirty ? "保存更改后再测试连接" : "测试连接"}</TooltipContent>
          </Tooltip>
        ) : null}
        <Button type="button" size="sm" disabled={busy || !dirty} onClick={onSave}>
          {busyOperation === "saving" ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <Save className="size-4" />
          )}
          保存
        </Button>
        {!creating ? (
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground hover:text-destructive"
                  disabled={busy}
                  onClick={onDelete}
                />
              }
            >
              {busyOperation === "deleting" ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Trash2 className="size-4" />
              )}
              <span className="sr-only">删除服务器</span>
            </TooltipTrigger>
            <TooltipContent>删除服务器</TooltipContent>
          </Tooltip>
        ) : null}
      </div>
    </header>
  );
}

function SectionHeading({ title }: { title: string }) {
  return <h3 className="text-sm font-medium">{title}</h3>;
}

function validationErrorText(message?: string) {
  if (!message) return "连接测试未通过";
  return localizedError(new Error(message), "连接测试未通过");
}

function SecretEditor({
  title,
  kind,
  rows,
  disabled,
  onChange,
}: {
  title: string;
  kind: SecretKind;
  rows: SecretDraft[];
  disabled: boolean;
  onChange: (rows: SecretDraft[]) => void;
}) {
  const addRow = () => {
    if (rows.length >= 32) return;
    onChange([
      ...rows,
      {
        id: nextSecretDraftID(kind),
        name: "",
        value: "",
        configured: false,
      },
    ]);
  };
  const updateRow = (id: string, changes: Partial<SecretDraft>) => {
    onChange(rows.map((row) => (row.id === id ? { ...row, ...changes } : row)));
  };

  return (
    <section className="border-b py-6">
      <div className="flex min-h-8 items-center justify-between gap-4">
        <SectionHeading title={title} />
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled || rows.length >= 32}
          onClick={addRow}
        >
          <Plus className="size-4" />
          添加
        </Button>
      </div>

      {rows.length === 0 ? (
        <div className="mt-3 border-y py-5 text-center text-sm text-muted-foreground">暂无配置</div>
      ) : (
        <div className="mt-3 border-y">
          <div className="hidden grid-cols-[minmax(9rem,0.8fr)_minmax(12rem,1.2fr)_2rem] gap-3 border-b px-0 py-2 text-xs font-medium text-muted-foreground sm:grid">
            <span>名称</span>
            <span>值</span>
            <span className="sr-only">操作</span>
          </div>
          <div className="divide-y">
            {rows.map((row) => {
              const nameID = `${row.id}-name`;
              const valueID = `${row.id}-value`;
              const renamed =
                row.configured && !sameSecretName(row.originalName, row.name.trim(), kind);
              return (
                <div
                  key={row.id}
                  className="grid min-w-0 grid-cols-[minmax(0,1fr)_2rem] gap-3 py-3 sm:grid-cols-[minmax(9rem,0.8fr)_minmax(12rem,1.2fr)_2rem]"
                >
                  <div className="min-w-0 space-y-2">
                    <Label htmlFor={nameID} className="sm:sr-only">
                      名称
                    </Label>
                    <Input
                      id={nameID}
                      value={row.name}
                      maxLength={128}
                      disabled={disabled}
                      spellCheck={false}
                      placeholder={kind === "parameter" ? "api_key" : "Authorization"}
                      className="font-mono text-sm"
                      onChange={(event) => updateRow(row.id, { name: event.target.value })}
                    />
                  </div>
                  <div className="col-start-1 min-w-0 space-y-2 sm:col-start-2 sm:row-start-1">
                    <Label htmlFor={valueID} className="sm:sr-only">
                      值
                    </Label>
                    <Input
                      id={valueID}
                      type="password"
                      value={row.value}
                      maxLength={8192}
                      disabled={disabled}
                      autoComplete="new-password"
                      placeholder={
                        row.configured && !renamed ? "留空以保留当前值" : "输入凭据值"
                      }
                      onChange={(event) => updateRow(row.id, { value: event.target.value })}
                    />
                    {row.configured ? (
                      <p
                        className={cn(
                          "break-words text-xs leading-5 text-muted-foreground",
                          renamed && !row.value && "text-destructive",
                        )}
                      >
                        {renamed && !row.value
                          ? "名称已更改，请重新输入值"
                          : `已配置${row.keyHint ? ` · ${row.keyHint}` : ""}`}
                      </p>
                    ) : (
                      <p className="text-xs leading-5 text-muted-foreground">新增项必须填写值</p>
                    )}
                  </div>
                  <div className="col-start-2 row-start-1 pt-6 sm:col-start-3 sm:pt-0">
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-sm"
                            className="text-muted-foreground hover:text-destructive"
                            disabled={disabled}
                            onClick={() => onChange(rows.filter((item) => item.id !== row.id))}
                          />
                        }
                      >
                        <Trash2 className="size-4" />
                        <span className="sr-only">删除此项</span>
                      </TooltipTrigger>
                      <TooltipContent>删除此项</TooltipContent>
                    </Tooltip>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </section>
  );
}

function ToolsSection({
  server,
  enabledTools,
  disabled,
  onChange,
}: {
  server: UserMCPServer;
  enabledTools: string[];
  disabled: boolean;
  onChange: (names: string[]) => void;
}) {
  const enabledNames = new Set(enabledTools);
  const toggle = (name: string, enabled: boolean) => {
    onChange(
      enabled
        ? [...enabledTools.filter((item) => item !== name), name]
        : enabledTools.filter((item) => item !== name),
    );
  };

  return (
    <section className="pt-8 pb-4">
      <div className="flex items-start justify-between gap-4">
        <SectionHeading title="工具能力" />
        <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
          {enabledTools.length}/{server.tools.length} 已启用
        </span>
      </div>
      {server.tools.length === 0 ? (
        <div className="mt-5 border-y py-8 text-center">
          <p className="text-sm text-muted-foreground">暂无工具</p>
        </div>
      ) : (
        <div className="mt-5 divide-y border-y">
          {server.tools.map((tool) => (
            <label key={tool.name} className="flex min-w-0 items-start gap-3 py-4">
              <input
                type="checkbox"
                checked={enabledNames.has(tool.name)}
                disabled={disabled}
                onChange={(event) => toggle(tool.name, event.target.checked)}
                className="mt-0.5 size-4 shrink-0 accent-foreground"
              />
              <span className="min-w-0">
                <span className="block break-all font-mono text-sm font-medium">{tool.name}</span>
                <span className="mt-1 block break-words text-xs leading-5 text-muted-foreground">
                  {tool.description || "此工具未提供说明"}
                </span>
              </span>
            </label>
          ))}
        </div>
      )}
    </section>
  );
}
