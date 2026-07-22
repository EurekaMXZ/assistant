"use client";

import { Plus, Trash2 } from "lucide-react";
import { EmptyState } from "@/components/shared/empty-state";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import {
  nextSecretDraftID,
  sameSecretName,
  type SecretDraft,
  type SecretKind,
} from "./mcp-secret-drafts";

export function SecretEditor({
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
        <h3 className="text-sm font-medium">{title}</h3>
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
        <EmptyState
          className="mt-3 min-h-28 px-0"
          title="暂无配置"
          titleClassName="mt-0 font-normal text-muted-foreground"
        />
      ) : (
        <div className="mt-3">
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
                      placeholder={row.configured && !renamed ? "留空以保留当前值" : "输入凭据值"}
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
