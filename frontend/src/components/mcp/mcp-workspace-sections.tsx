"use client";

import { ArrowLeft, FlaskConical, Save, Trash2 } from "lucide-react";
import { Spinner } from "@/components/shared/spinner";
import { EmptyState } from "@/components/shared/empty-state";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { UserMCPServer } from "@/lib/types";
import { cn } from "@/lib/utils";

export function ServerListItem({
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
      variant="nav"
      size="sm"
      disabled={disabled}
      aria-current={active ? "page" : undefined}
      onClick={onClick}
      className={cn(
        "h-auto min-h-12 w-full min-w-0 justify-start px-2 py-2 text-left",
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

export function EditorLoading() {
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

export function EditorHeader({
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
  busyOperation: "saving" | "testing" | "deleting" | null;
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
        <label className="flex min-h-10 shrink-0 cursor-pointer items-center gap-2 rounded-md px-2 text-sm hover:bg-muted md:min-h-8">
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
              {busyOperation === "testing" ? <Spinner /> : <FlaskConical className="size-4" />}
              <span className="hidden sm:inline">测试连接</span>
              <span className="sr-only sm:hidden">测试连接</span>
            </TooltipTrigger>
            <TooltipContent>{dirty ? "保存更改后再测试连接" : "测试连接"}</TooltipContent>
          </Tooltip>
        ) : null}
        <Button type="button" size="sm" disabled={busy || !dirty} onClick={onSave}>
          {busyOperation === "saving" ? <Spinner /> : <Save className="size-4" />}
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
              {busyOperation === "deleting" ? <Spinner /> : <Trash2 className="size-4" />}
              <span className="sr-only">删除服务器</span>
            </TooltipTrigger>
            <TooltipContent>删除服务器</TooltipContent>
          </Tooltip>
        ) : null}
      </div>
    </header>
  );
}

export function ToolsSection({
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
        <h3 className="text-sm font-medium">工具能力</h3>
        <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
          {enabledTools.length}/{server.tools.length} 已启用
        </span>
      </div>
      {server.tools.length === 0 ? (
        <EmptyState
          className="mt-5 min-h-32 px-0"
          title="暂无工具"
          titleClassName="mt-0 font-normal text-muted-foreground"
        />
      ) : (
        <div className="mt-5 divide-y">
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
