"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { CircleAlert, CircleDashed, Plus, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import {
  createMCPServer,
  deleteMCPServer,
  getMCPServer,
  listMCPServers,
  testMCPServer,
  updateMCPServer,
} from "@/lib/api";
import type { UserMCPServer } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { Input } from "@/components/ui/input";
import { FormField } from "@/components/ui/form-field";
import { Skeleton } from "@/components/ui/skeleton";
import { SecretEditor } from "./mcp-secret-editor";
import {
  createSecretPayload,
  emptyForm,
  formFingerprint,
  formFromServer,
  localizedError,
  updateSecretPayload,
  validationMessage,
  type MCPServerForm,
} from "./mcp-server-form";
import {
  EditorHeader,
  EditorLoading,
  ServerListItem,
  ToolsSection,
} from "./mcp-workspace-sections";

type EditorTarget = { kind: "new" } | { kind: "server"; id: string } | null;
type MobilePane = "list" | "editor";
type BusyOperation = "saving" | "testing" | "deleting" | null;

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
    <div className="grid h-full min-h-0 w-full min-w-0 grid-cols-1 overflow-hidden lg:grid-cols-[minmax(15rem,20rem)_minmax(0,1fr)]">
      <aside
        className={cn(
          "min-h-0 min-w-0 flex-col bg-sidebar/40 lg:flex lg:border-r",
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
          "min-h-0 min-w-0 flex-col bg-background lg:flex",
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
              onEnabledChange={(enabled) => setForm((current) => ({ ...current, enabled }))}
            />

            <div className="min-h-0 flex-1 overflow-y-auto">
              <div className="mx-auto w-full max-w-4xl px-4 py-6 sm:px-7 lg:px-10 lg:py-8">
                <section className="border-b pb-8">
                  <div className="grid gap-5 sm:grid-cols-2">
                    <FormField label="名称" htmlFor="mcp-name">
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
                    </FormField>
                    <FormField label="标识" htmlFor="mcp-slug">
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
                    </FormField>
                    <FormField label="服务器 URL" htmlFor="mcp-endpoint" className="sm:col-span-2">
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
                    </FormField>
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

function validationErrorText(message?: string) {
  if (!message) return "连接测试未通过";
  return localizedError(new Error(message), "连接测试未通过");
}
