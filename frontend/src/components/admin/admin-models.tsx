"use client";

import { useEffect, useEffectEvent, useState } from "react";
import { Boxes, MoreHorizontal, Plus, Settings2, Sparkles, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { AdminPageHeader, SavingIcon } from "@/components/admin/admin-shared";
import { AdminListPage } from "@/components/admin/admin-list-page";
import { tableClasses, tableHeadClass } from "@/components/shared/table-styles";
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
import { FormField } from "@/components/ui/form-field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import {
  ModelEditorDialog as ExtractedModelEditorDialog,
  emptyModelForm,
  modelFormFrom,
  type ModelForm,
} from "@/components/admin/admin-model-editor-dialog";
import { ModelPriceDialog } from "@/components/admin/admin-model-price-dialog";
import {
  createAdminModel,
  deleteAdminModel,
  getAdminModelSettings,
  listAdminCredentials,
  listAdminCredentialsPage,
  listAdminModels,
  listAdminModelsPage,
  setAdminModelEnabled,
  updateAdminModel,
  updateAdminModelSettings,
} from "@/lib/api";
import {
  buildAdminModelCreatePayload,
  buildAdminModelUpdatePayload,
} from "@/lib/admin-model-payloads";
import type { Model, ModelSettings, ProviderCredential } from "@/lib/types";
import { useCursorPagination } from "@/lib/use-cursor-pagination";

export function AdminModels() {
  const {
    items: models,
    setItems: setModels,
    page,
    loading: modelsLoading,
    loadingMore,
    error: modelsError,
    loadMoreError,
    loadMore,
    reload: reloadModels,
  } = useCursorPagination<Model>(listAdminModelsPage, "模型目录加载失败");
  const [credentials, setCredentials] = useState<ProviderCredential[]>([]);
  const [settings, setSettings] = useState<ModelSettings | null>(null);
  const [metadataLoading, setMetadataLoading] = useState(true);
  const [metadataError, setMetadataError] = useState("");
  const [settingsModels, setSettingsModels] = useState<Model[]>([]);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<Model | null>(null);
  const [form, setForm] = useState<ModelForm>(emptyModelForm);
  const [saving, setSaving] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [defaultChatModelId, setDefaultChatModelId] = useState("");
  const [compactionModelId, setCompactionModelId] = useState("");
  const [priceModel, setPriceModel] = useState<Model | null>(null);
  const [deleteModel, setDeleteModel] = useState<Model | null>(null);

  const loadMetadata = async () => {
    setMetadataLoading(true);
    setMetadataError("");
    try {
      const [nextCredentials, nextSettings] = await Promise.all([
        listAdminCredentialsPage(),
        getAdminModelSettings(),
      ]);
      setCredentials(nextCredentials.data);
      setSettings(nextSettings);
      setDefaultChatModelId(nextSettings.default_chat_model_id || "");
      setCompactionModelId(nextSettings.compaction_model_id || "");
    } catch (err) {
      setMetadataError(err instanceof Error ? err.message : "模型配置加载失败");
    } finally {
      setMetadataLoading(false);
    }
  };

  const loadMetadataEffect = useEffectEvent(loadMetadata);
  useEffect(() => {
    void loadMetadataEffect();
  }, []);

  const loading = modelsLoading || metadataLoading;
  const error = modelsError || metadataError;
  const retry = () => {
    void reloadModels();
    void loadMetadata();
  };

  const openSettings = async () => {
    setSettingsModels(models);
    setSettingsOpen(true);
    try {
      setSettingsModels(await listAdminModels());
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "模型选项加载失败");
    }
  };

  const loadCredentialOptions = async () => {
    try {
      setCredentials(await listAdminCredentials());
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "凭据选项加载失败");
    }
  };

  const openCreate = () => {
    setEditing(null);
    setForm({
      ...emptyModelForm,
      credentialId: credentials.find((item) => item.status === "enabled")?.id || "",
    });
    setEditorOpen(true);
    void loadCredentialOptions();
  };

  const openEdit = (item: Model) => {
    setEditing(item);
    setForm(modelFormFrom(item));
    setEditorOpen(true);
    void loadCredentialOptions();
  };

  const saveModel = async () => {
    setSaving(true);
    try {
      const saved = editing
        ? await updateAdminModel(editing.id, buildAdminModelUpdatePayload(form, editing))
        : await createAdminModel(
            buildAdminModelCreatePayload(
              form,
              credentials.find((credential) => credential.id === form.credentialId),
            ),
          );
      setModels((items) =>
        editing ? items.map((item) => (item.id === saved.id ? saved : item)) : [saved, ...items],
      );
      setEditorOpen(false);
      toast.success(editing ? "模型已更新" : "模型已创建");
    } catch (err) {
      toast.error(
        err instanceof SyntaxError
          ? "默认参数不是有效的 JSON"
          : err instanceof Error
            ? err.message
            : "模型保存失败",
      );
    } finally {
      setSaving(false);
    }
  };

  const toggleModel = async (item: Model) => {
    try {
      const saved = await setAdminModelEnabled(item.id, item.status !== "enabled");
      setModels((items) => items.map((model) => (model.id === saved.id ? saved : model)));
      toast.success(saved.status === "enabled" ? "模型已启用" : "模型已停用");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "模型状态更新失败");
    }
  };

  const removeModel = async () => {
    if (!deleteModel) return;
    try {
      await deleteAdminModel(deleteModel.id);
      setModels((items) => items.filter((item) => item.id !== deleteModel.id));
      setSettingsModels((items) => items.filter((item) => item.id !== deleteModel.id));
      setDefaultChatModelId((current) => (current === deleteModel.id ? "" : current));
      setCompactionModelId((current) => (current === deleteModel.id ? "" : current));
      setDeleteModel(null);
      toast.success("模型已删除");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "模型删除失败");
    }
  };

  const saveSettings = async () => {
    setSaving(true);
    try {
      const saved = await updateAdminModelSettings({
        default_chat_model_id: defaultChatModelId,
        compaction_model_id: compactionModelId,
      });
      setSettings(saved);
      setSettingsOpen(false);
      toast.success("默认模型已更新");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "默认模型更新失败");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <AdminPageHeader
        title="模型"
        action={
          <>
            <Button variant="outline" size="sm" onClick={() => void openSettings()}>
              <Settings2 /> 默认模型
            </Button>
            <Button size="sm" onClick={openCreate}>
              <Plus /> 添加模型
            </Button>
          </>
        }
      />

      <AdminListPage
        ariaLabel="模型列表"
        className="mt-6"
        emptyIcon={Boxes}
        emptyTitle="暂无模型"
        error={error}
        hasItems={models.length > 0}
        hasMore={page.has_more}
        loading={loading}
        loadingMore={loadingMore}
        loadMoreError={loadMoreError}
        onLoadMore={loadMore}
        onRetry={retry}
      >
        <table className="admin-responsive-table w-[76rem] min-w-full table-fixed text-left text-sm">
          <colgroup>
            <col className="w-[28rem]" />
            <col className="w-[16rem]" />
            <col className="w-[10rem]" />
            <col className="w-[7rem]" />
            <col className="w-[9rem]" />
            <col className="w-[6rem]" />
          </colgroup>
          <thead className={tableHeadClass}>
            <tr className="border-b">
              <th className={tableClasses.headStart}>模型</th>
              <th className={tableClasses.head}>推理档位</th>
              <th className={tableClasses.head}>上下文</th>
              <th className={tableClasses.head}>版本</th>
              <th className={tableClasses.head}>状态</th>
              <th className={tableClasses.headEnd}>操作</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {models.map((item) => (
              <tr key={item.id} className="group">
                <td className={tableClasses.cellStart} data-primary>
                  <div className="flex items-center gap-3">
                    <span className="grid size-8 shrink-0 place-items-center rounded-md bg-muted">
                      <Sparkles className="size-4" />
                    </span>
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <p className="truncate font-medium" title={item.display_name}>
                          {item.display_name}
                        </p>
                        {settings?.default_chat_model_id === item.id ? (
                          <Badge variant="secondary">默认</Badge>
                        ) : null}
                      </div>
                      <p
                        className="mt-0.5 truncate font-mono text-xs text-muted-foreground"
                        title={item.upstream_model}
                      >
                        {item.upstream_model}
                      </p>
                    </div>
                  </div>
                </td>
                <td className={tableClasses.cell} data-label="推理档位">
                  <div className="flex flex-wrap gap-1">
                    {item.supported_reasoning_efforts.length ? (
                      item.supported_reasoning_efforts.map((effort) => (
                        <Badge key={effort} variant="outline">
                          {effort}
                        </Badge>
                      ))
                    ) : (
                      <span className="text-xs text-muted-foreground">关闭</span>
                    )}
                  </div>
                </td>
                <td
                  className={`${tableClasses.cell} whitespace-nowrap font-mono text-xs`}
                  data-label="上下文"
                >
                  {item.context_window_tokens.toLocaleString("zh-CN")}
                </td>
                <td
                  className={`${tableClasses.cell} font-mono text-xs text-muted-foreground`}
                  data-label="版本"
                >
                  r{item.revision}
                </td>
                <td className={tableClasses.cell} data-label="状态">
                  <Badge variant={item.status === "enabled" ? "secondary" : "outline"}>
                    {item.status === "enabled" ? "已启用" : "已停用"}
                  </Badge>
                </td>
                <td className={tableClasses.cellEnd} data-actions>
                  <DropdownMenu>
                    <DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" />}>
                      <MoreHorizontal />
                      <span className="sr-only">模型操作</span>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="w-40">
                      <DropdownMenuGroup>
                        <DropdownMenuItem onClick={() => openEdit(item)}>编辑配置</DropdownMenuItem>
                        <DropdownMenuItem onClick={() => setPriceModel(item)}>
                          价格版本
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={() => void toggleModel(item)}>
                          {item.status === "enabled" ? "停用模型" : "启用模型"}
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          variant="destructive"
                          onClick={() => setDeleteModel(item)}
                        >
                          <Trash2 />
                          删除模型
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

      <ExtractedModelEditorDialog
        open={editorOpen}
        onOpenChange={setEditorOpen}
        editing={editing}
        form={form}
        setForm={setForm}
        credentials={credentials}
        saving={saving}
        onSave={saveModel}
      />

      <Dialog open={settingsOpen} onOpenChange={setSettingsOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>默认模型</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <FormField label="默认对话模型" htmlFor="default-model">
              <Select
                items={(settingsModels.length ? settingsModels : models)
                  .filter((item) => item.status === "enabled")
                  .map((item) => ({ value: item.id, label: item.display_name }))}
                value={defaultChatModelId}
                onValueChange={(value) => value && setDefaultChatModelId(value)}
              >
                <SelectTrigger id="default-model">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(settingsModels.length ? settingsModels : models)
                    .filter((item) => item.status === "enabled")
                    .map((item) => (
                      <SelectItem key={item.id} value={item.id}>
                        {item.display_name}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </FormField>
            <FormField label="上下文压缩模型" htmlFor="compaction-model">
              <Select
                items={(settingsModels.length ? settingsModels : models)
                  .filter((item) => item.status === "enabled")
                  .map((item) => ({ value: item.id, label: item.display_name }))}
                value={compactionModelId}
                onValueChange={(value) => value && setCompactionModelId(value)}
              >
                <SelectTrigger id="compaction-model">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(settingsModels.length ? settingsModels : models)
                    .filter((item) => item.status === "enabled")
                    .map((item) => (
                      <SelectItem key={item.id} value={item.id}>
                        {item.display_name}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </FormField>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setSettingsOpen(false)}>
              取消
            </Button>
            <Button
              disabled={saving || !defaultChatModelId || !compactionModelId}
              onClick={() => void saveSettings()}
            >
              <SavingIcon saving={saving} />
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={deleteModel !== null}
        onOpenChange={(open) => !open && setDeleteModel(null)}
        title="删除模型"
        description={`确认删除“${deleteModel?.display_name || "此模型"}”吗？模型将从目录中移除，历史记录会保留。`}
        confirmText="删除"
        destructive
        onConfirm={() => void removeModel()}
      />

      <ModelPriceDialog model={priceModel} onOpenChange={(open) => !open && setPriceModel(null)} />
    </div>
  );
}
