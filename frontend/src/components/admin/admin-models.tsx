"use client";

import { useEffect, useEffectEvent, useRef, useState } from "react";
import {
  Boxes,
  CircleDollarSign,
  MoreHorizontal,
  Plus,
  Settings2,
  Sparkles,
  Trash2,
} from "lucide-react";
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
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { CursorTableScroll } from "@/components/ui/cursor-table-scroll";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import {
  createAdminModel,
  createAdminModelPrice,
  deleteAdminModel,
  getAdminModelSettings,
  listAdminCredentials,
  listAdminCredentialsPage,
  listAdminModelPrices,
  listAdminModels,
  listAdminModelsPage,
  setAdminModelEnabled,
  setAdminModelPriceStatus,
  updateAdminModel,
  updateAdminModelSettings,
} from "@/lib/api";
import {
  buildAdminModelCreatePayload,
  buildAdminModelUpdatePayload,
} from "@/lib/admin-model-payloads";
import { formatNanos, parseDecimalNanos } from "@/lib/decimal-nanos";
import type {
  Model,
  ModelPriceVersion,
  ModelSettings,
  ProviderCredential,
  ReasoningEffort,
  CursorPage,
} from "@/lib/types";
import { useCursorPagination } from "@/lib/use-cursor-pagination";

const efforts: ReasoningEffort[] = ["low", "medium", "high", "xhigh"];

type ModelForm = {
  credentialId: string;
  slug: string;
  upstreamModel: string;
  displayName: string;
  description: string;
  contextWindow: string;
  maxOutput: string;
  supportsTools: boolean;
  supportsParallelTools: boolean;
  reasoningEfforts: ReasoningEffort[];
  defaultParameters: string;
};

const emptyModelForm: ModelForm = {
  credentialId: "",
  slug: "",
  upstreamModel: "",
  displayName: "",
  description: "",
  contextWindow: "128000",
  maxOutput: "8192",
  supportsTools: true,
  supportsParallelTools: true,
  reasoningEfforts: [],
  defaultParameters: "{}",
};

function modelFormFrom(item: Model): ModelForm {
  return {
    credentialId: item.credential_id || "",
    slug: item.slug,
    upstreamModel: item.upstream_model,
    displayName: item.display_name,
    description: item.description,
    contextWindow: String(item.context_window_tokens),
    maxOutput: String(item.max_output_tokens),
    supportsTools: item.supports_tools,
    supportsParallelTools: item.supports_parallel_tools,
    reasoningEfforts: item.supported_reasoning_efforts,
    defaultParameters: JSON.stringify(item.default_parameters || {}, null, 2),
  };
}

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

      {loading ? <AdminLoading /> : null}
      {!loading && error ? <AdminError message={error} onRetry={retry} /> : null}
      {!loading && !error && !models.length ? <AdminEmpty icon={Boxes} title="暂无模型" /> : null}
      {!loading && !error && models.length ? (
        <CursorTableScroll
          className={`${adminTableScrollClass} mt-6`}
          hasMore={page.has_more}
          loadingMore={loadingMore}
          loadMoreError={loadMoreError}
          onLoadMore={loadMore}
          aria-label="模型列表"
        >
          <table className="w-[76rem] min-w-full table-fixed text-left text-sm">
            <colgroup>
              <col className="w-[28rem]" />
              <col className="w-[16rem]" />
              <col className="w-[10rem]" />
              <col className="w-[7rem]" />
              <col className="w-[9rem]" />
              <col className="w-[6rem]" />
            </colgroup>
            <thead className={adminTableHeadClass}>
              <tr className="border-b">
                <th className="py-3 pr-4 font-medium">模型</th>
                <th className="px-4 py-3 font-medium">推理档位</th>
                <th className="px-4 py-3 font-medium">上下文</th>
                <th className="px-4 py-3 font-medium">版本</th>
                <th className="px-4 py-3 font-medium">状态</th>
                <th className="py-3 pl-4 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {models.map((item) => (
                <tr key={item.id} className="group">
                  <td className="py-3 pr-4">
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
                  <td className="px-4 py-3">
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
                  <td className="whitespace-nowrap px-4 py-3 font-mono text-xs">
                    {item.context_window_tokens.toLocaleString("zh-CN")}
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
                    r{item.revision}
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={item.status === "enabled" ? "secondary" : "outline"}>
                      {item.status === "enabled" ? "已启用" : "已停用"}
                    </Badge>
                  </td>
                  <td className="py-3 pl-4 text-right">
                    <DropdownMenu>
                      <DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" />}>
                        <MoreHorizontal />
                        <span className="sr-only">模型操作</span>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" className="w-40">
                        <DropdownMenuGroup>
                          <DropdownMenuItem onClick={() => openEdit(item)}>
                            编辑配置
                          </DropdownMenuItem>
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
        </CursorTableScroll>
      ) : null}

      <ModelEditorDialog
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
            <div className="space-y-2">
              <Label htmlFor="default-model">默认对话模型</Label>
              <select
                id="default-model"
                className={adminSelectClass}
                value={defaultChatModelId}
                onChange={(event) => setDefaultChatModelId(event.target.value)}
              >
                {(settingsModels.length ? settingsModels : models)
                  .filter((item) => item.status === "enabled")
                  .map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.display_name}
                    </option>
                  ))}
              </select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="compaction-model">上下文压缩模型</Label>
              <select
                id="compaction-model"
                className={adminSelectClass}
                value={compactionModelId}
                onChange={(event) => setCompactionModelId(event.target.value)}
              >
                {(settingsModels.length ? settingsModels : models)
                  .filter((item) => item.status === "enabled")
                  .map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.display_name}
                    </option>
                  ))}
              </select>
            </div>
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

      <PriceDialog model={priceModel} onOpenChange={(open) => !open && setPriceModel(null)} />
    </div>
  );
}

function ModelEditorDialog({
  open,
  onOpenChange,
  editing,
  form,
  setForm,
  credentials,
  saving,
  onSave,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editing: Model | null;
  form: ModelForm;
  setForm: React.Dispatch<React.SetStateAction<ModelForm>>;
  credentials: ProviderCredential[];
  saving: boolean;
  onSave: () => void;
}) {
  const update = <K extends keyof ModelForm>(key: K, value: ModelForm[K]) =>
    setForm((current) => ({ ...current, [key]: value }));
  const toggleEffort = (effort: ReasoningEffort) =>
    update(
      "reasoningEfforts",
      form.reasoningEfforts.includes(effort)
        ? form.reasoningEfforts.filter((item) => item !== effort)
        : [...form.reasoningEfforts, effort],
    );
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{editing ? "编辑模型" : "添加模型"}</DialogTitle>
        </DialogHeader>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2 sm:col-span-2">
            <Label htmlFor="model-credential">提供方凭据</Label>
            <select
              id="model-credential"
              className={adminSelectClass}
              value={form.credentialId}
              onChange={(event) => update("credentialId", event.target.value)}
            >
              {credentials
                .filter((item) => item.status === "enabled")
                .map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name} · {item.masked_key}
                  </option>
                ))}
            </select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="model-slug">标识</Label>
            <Input
              id="model-slug"
              disabled={!!editing}
              value={form.slug}
              onChange={(event) => update("slug", event.target.value)}
              placeholder="gpt-primary"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="upstream-model">上游模型</Label>
            <Input
              id="upstream-model"
              disabled={!!editing}
              value={form.upstreamModel}
              onChange={(event) => update("upstreamModel", event.target.value)}
              placeholder="gpt-5.5"
            />
          </div>
          <div className="space-y-2 sm:col-span-2">
            <Label htmlFor="model-name">显示名称</Label>
            <Input
              id="model-name"
              value={form.displayName}
              onChange={(event) => update("displayName", event.target.value)}
              placeholder="GPT 5.5"
            />
          </div>
          <div className="space-y-2 sm:col-span-2">
            <Label htmlFor="model-description">说明</Label>
            <Input
              id="model-description"
              value={form.description}
              onChange={(event) => update("description", event.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="context-window">上下文 Tokens</Label>
            <Input
              id="context-window"
              type="number"
              value={form.contextWindow}
              onChange={(event) => update("contextWindow", event.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="max-output">最大输出 Tokens</Label>
            <Input
              id="max-output"
              type="number"
              value={form.maxOutput}
              onChange={(event) => update("maxOutput", event.target.value)}
            />
          </div>
          <div className="space-y-2 sm:col-span-2">
            <Label>工具能力</Label>
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                size="sm"
                variant={form.supportsTools ? "secondary" : "outline"}
                onClick={() => update("supportsTools", !form.supportsTools)}
              >
                工具调用
              </Button>
              <Button
                type="button"
                size="sm"
                variant={form.supportsParallelTools ? "secondary" : "outline"}
                onClick={() => update("supportsParallelTools", !form.supportsParallelTools)}
              >
                并行工具
              </Button>
            </div>
          </div>
          <div className="space-y-2 sm:col-span-2">
            <Label>支持的推理档位</Label>
            <div className="flex flex-wrap gap-2">
              {efforts.map((effort) => (
                <Button
                  key={effort}
                  type="button"
                  size="sm"
                  variant={form.reasoningEfforts.includes(effort) ? "secondary" : "outline"}
                  onClick={() => toggleEffort(effort)}
                >
                  {effort}
                </Button>
              ))}
            </div>
          </div>
          <div className="space-y-2 sm:col-span-2">
            <Label htmlFor="default-parameters">默认上游参数</Label>
            <Textarea
              id="default-parameters"
              className="min-h-32 font-mono text-xs"
              value={form.defaultParameters}
              onChange={(event) => update("defaultParameters", event.target.value)}
              spellCheck={false}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button
            disabled={
              saving ||
              !form.credentialId ||
              !form.slug.trim() ||
              !form.upstreamModel.trim() ||
              !form.displayName.trim()
            }
            onClick={onSave}
          >
            <SavingIcon saving={saving} />
            {editing ? "保存更改" : "创建模型"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function PriceDialog({
  model,
  onOpenChange,
}: {
  model: Model | null;
  onOpenChange: (open: boolean) => void;
}) {
  const [prices, setPrices] = useState<ModelPriceVersion[]>([]);
  const [page, setPage] = useState<CursorPage>({ has_more: false });
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadMoreError, setLoadMoreError] = useState("");
  const [saving, setSaving] = useState(false);
  const [showForm, setShowForm] = useState(false);
  const [currency, setCurrency] = useState("USD");
  const [input, setInput] = useState("");
  const [cacheRead, setCacheRead] = useState("");
  const [cacheCreation, setCacheCreation] = useState("");
  const [output, setOutput] = useState("");
  const priceRequestIDRef = useRef(0);

  useEffect(() => {
    if (!model) return;
    const requestID = ++priceRequestIDRef.current;
    let cancelled = false;
    setLoading(true);
    setLoadingMore(false);
    setLoadMoreError("");
    setPrices([]);
    setPage({ has_more: false });
    void listAdminModelPrices(model.id)
      .then((result) => {
        if (!cancelled && requestID === priceRequestIDRef.current) {
          setPrices(result.data);
          setPage(result.page);
        }
      })
      .catch((err) => {
        if (!cancelled && requestID === priceRequestIDRef.current) {
          toast.error(err instanceof Error ? err.message : "价格加载失败");
        }
      })
      .finally(() => {
        if (!cancelled && requestID === priceRequestIDRef.current) setLoading(false);
      });
    return () => {
      cancelled = true;
      if (requestID === priceRequestIDRef.current) priceRequestIDRef.current += 1;
    };
  }, [model]);
  const loadMorePrices = async () => {
    if (!model || !page.next_cursor || loadingMore) return;
    const requestID = priceRequestIDRef.current;
    setLoadingMore(true);
    setLoadMoreError("");
    try {
      const result = await listAdminModelPrices(model.id, page.next_cursor);
      if (requestID !== priceRequestIDRef.current) return;
      setPrices((items) => [...items, ...result.data]);
      setPage(result.page);
    } catch (err) {
      if (requestID !== priceRequestIDRef.current) return;
      setLoadMoreError(err instanceof Error ? err.message : "更多价格版本加载失败");
    } finally {
      if (requestID === priceRequestIDRef.current) setLoadingMore(false);
    }
  };
  const createPrice = async () => {
    if (!model) return;
    setSaving(true);
    try {
      const created = await createAdminModelPrice(model.id, {
        currency: currency.toUpperCase(),
        input_per_million_nanos: parseDecimalNanos(input),
        cache_read_input_per_million_nanos: parseDecimalNanos(cacheRead),
        cache_creation_input_per_million_nanos: parseDecimalNanos(cacheCreation),
        output_per_million_nanos: parseDecimalNanos(output),
      });
      setPrices((items) => [created, ...items]);
      setShowForm(false);
      toast.success("价格草稿已创建");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "价格创建失败");
    } finally {
      setSaving(false);
    }
  };
  const changeStatus = async (price: ModelPriceVersion, action: "publish" | "archive") => {
    if (!model) return;
    try {
      const saved = await setAdminModelPriceStatus(model.id, price.id, action);
      setPrices((items) => items.map((item) => (item.id === saved.id ? saved : item)));
      toast.success(action === "publish" ? "价格已发布" : "价格已归档");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "价格状态更新失败");
    }
  };
  const displayRate = (nanos: number) => formatNanos(nanos);
  return (
    <Dialog open={!!model} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{model?.display_name} · 价格版本</DialogTitle>
          <DialogDescription>
            价格版本发布后不可修改，新的请求会快照当前生效版本。
          </DialogDescription>
        </DialogHeader>
        <div className="flex justify-end">
          <Button size="sm" variant="outline" onClick={() => setShowForm((value) => !value)}>
            <Plus />
            新建价格
          </Button>
        </div>
        {showForm ? (
          <div className="grid gap-3 border-y py-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label>币种</Label>
              <Input
                value={currency}
                maxLength={3}
                onChange={(event) => setCurrency(event.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label>输入 / 1M</Label>
              <Input
                inputMode="decimal"
                value={input}
                onChange={(event) => setInput(event.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label>输出 / 1M</Label>
              <Input
                inputMode="decimal"
                value={output}
                onChange={(event) => setOutput(event.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label>缓存读取 / 1M</Label>
              <Input
                inputMode="decimal"
                value={cacheRead}
                onChange={(event) => setCacheRead(event.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label>缓存创建 / 1M</Label>
              <Input
                inputMode="decimal"
                value={cacheCreation}
                onChange={(event) => setCacheCreation(event.target.value)}
              />
            </div>
            <div className="flex items-end">
              <Button
                className="w-full"
                disabled={
                  saving || !currency.trim() || !input || !output || !cacheRead || !cacheCreation
                }
                onClick={() => void createPrice()}
              >
                <SavingIcon saving={saving} />
                创建草稿
              </Button>
            </div>
          </div>
        ) : null}
        {loading ? (
          <AdminLoading />
        ) : prices.length ? (
          <CursorTableScroll
            className="max-h-[min(45vh,28rem)] overflow-auto border-y"
            hasMore={page.has_more}
            loadingMore={loadingMore}
            loadMoreError={loadMoreError}
            onLoadMore={loadMorePrices}
            aria-label="模型价格版本"
          >
            <table className="w-[60rem] min-w-full table-fixed text-left text-sm">
              <colgroup>
                <col className="w-[9rem]" />
                <col className="w-[9rem]" />
                <col className="w-[9rem]" />
                <col className="w-[9rem]" />
                <col className="w-[9rem]" />
                <col className="w-[8rem]" />
                <col className="w-[7rem]" />
              </colgroup>
              <thead className={adminTableHeadClass}>
                <tr className="border-b">
                  <th className="py-3 pr-3 font-medium">版本</th>
                  <th className="px-3 py-3 text-right font-medium">输入</th>
                  <th className="px-3 py-3 text-right font-medium">输出</th>
                  <th className="px-3 py-3 text-right font-medium">缓存读取</th>
                  <th className="px-3 py-3 text-right font-medium">缓存创建</th>
                  <th className="px-3 py-3 font-medium">状态</th>
                  <th className="py-3 pl-3 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {prices.map((price) => (
                  <tr key={price.id}>
                    <td className="py-3 pr-3">
                      <p className="font-mono">v{price.version}</p>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {formatAdminDate(price.created_at)}
                      </p>
                    </td>
                    <td className="px-3 py-3 text-right font-mono">
                      {price.currency} {displayRate(price.input_per_million_nanos)}
                    </td>
                    <td className="px-3 py-3 text-right font-mono">
                      {price.currency} {displayRate(price.output_per_million_nanos)}
                    </td>
                    <td className="px-3 py-3 text-right font-mono">
                      {price.currency} {displayRate(price.cache_read_input_per_million_nanos)}
                    </td>
                    <td className="px-3 py-3 text-right font-mono">
                      {price.currency} {displayRate(price.cache_creation_input_per_million_nanos)}
                    </td>
                    <td className="px-3 py-3">
                      <Badge variant={price.status === "published" ? "secondary" : "outline"}>
                        {price.status}
                      </Badge>
                    </td>
                    <td className="py-3 pl-3 text-right">
                      {price.status === "draft" ? (
                        <Button
                          size="xs"
                          variant="outline"
                          onClick={() => void changeStatus(price, "publish")}
                        >
                          发布
                        </Button>
                      ) : price.status === "published" ? (
                        <Button
                          size="xs"
                          variant="ghost"
                          onClick={() => void changeStatus(price, "archive")}
                        >
                          归档
                        </Button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CursorTableScroll>
        ) : (
          <AdminEmpty icon={CircleDollarSign} title="暂无价格版本" />
        )}
      </DialogContent>
    </Dialog>
  );
}
