"use client";

import type { RefObject } from "react";
import { LocateFixed, MapPinned, Search } from "lucide-react";
import { Spinner } from "@/components/shared/spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export interface AMapSearchResult {
  id?: string;
  name?: string;
  district?: string;
  adcode?: string;
  location?: unknown;
  latitude: number;
  longitude: number;
}

export function AMapLocationPickerView({
  configured,
  containerRef,
  disabled,
  loadError,
  loading,
  locating,
  operationError,
  query,
  results,
  searching,
  onEscapeSearch,
  onLocate,
  onQueryChange,
  onSelectResult,
}: {
  configured: boolean;
  containerRef: RefObject<HTMLDivElement | null>;
  disabled: boolean;
  loadError: string;
  loading: boolean;
  locating: boolean;
  operationError: string;
  query: string;
  results: AMapSearchResult[];
  searching: boolean;
  onEscapeSearch: () => void;
  onLocate: () => void;
  onQueryChange: (query: string) => void;
  onSelectResult: (result: AMapSearchResult) => void;
}) {
  if (!configured) {
    return (
      <div className="flex h-80 flex-col items-center justify-center bg-muted/20 px-6 text-center sm:h-[360px]">
        <MapPinned className="size-7 text-muted-foreground" />
        <p className="mt-3 text-sm font-medium">地图选点不可用</p>
        <p className="mt-1 max-w-sm text-xs leading-5 text-muted-foreground">
          未配置高德地图 Web Key。偏好设置仍可正常保存。
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex gap-2">
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 z-10 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            onKeyDown={(event) => event.key === "Escape" && onEscapeSearch()}
            placeholder="搜索地址或地点"
            maxLength={200}
            disabled={disabled || loading || Boolean(loadError)}
            aria-expanded={results.length > 0}
            aria-controls="amap-address-results"
            className="pl-9 pr-9"
          />
          {searching ? (
            <Spinner className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
          ) : null}
          {results.length ? (
            <ul
              id="amap-address-results"
              aria-label="地址搜索结果"
              className="absolute inset-x-0 top-full z-30 mt-1 max-h-56 divide-y overflow-y-auto rounded-md border bg-popover text-popover-foreground shadow-lg"
            >
              {results.map((result, index) => (
                <li key={`${result.id ?? result.name}-${index}`}>
                  <Button
                    type="button"
                    variant="ghost"
                    className="h-auto min-h-0 w-full justify-start rounded-none px-3 py-2.5 text-left whitespace-normal first:rounded-t-md last:rounded-b-md focus-visible:outline-none"
                    disabled={disabled}
                    onClick={() => onSelectResult(result)}
                  >
                    <span className="block truncate text-sm font-medium">{result.name}</span>
                    <span className="mt-0.5 block truncate text-xs text-muted-foreground">
                      {result.district || "地点详情暂缺"}
                    </span>
                  </Button>
                </li>
              ))}
            </ul>
          ) : null}
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="shrink-0"
          onClick={onLocate}
          disabled={disabled || locating || loading || Boolean(loadError)}
        >
          {locating ? <Spinner /> : <LocateFixed />}
          定位
        </Button>
      </div>

      <div className="relative h-80 overflow-hidden bg-muted/20 sm:h-[360px]">
        <div ref={containerRef} className="size-full" aria-label="高德地图位置选择器" />
        {loading ? (
          <div className="absolute inset-0 flex items-center justify-center bg-background/80 text-sm text-muted-foreground">
            <Spinner className="mr-2" />
            正在加载地图
          </div>
        ) : null}
        {loadError ? (
          <div className="absolute inset-0 flex items-center justify-center bg-background px-6 text-center text-sm text-destructive">
            地图加载失败：{loadError}
          </div>
        ) : null}
      </div>
      <p aria-live="polite" className="min-h-5 text-xs text-muted-foreground">
        {operationError || "点击地图、搜索地点或使用定位来选择位置。"}
      </p>
    </div>
  );
}
