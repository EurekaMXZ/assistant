"use client";

import { useEffect, useEffectEvent, useRef, useState } from "react";
import { LocateFixed, Loader2, MapPinned, Search } from "lucide-react";
import { load as loadAMap } from "@amap/amap-jsapi-loader";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { UserLocationInput, UserLocationSource } from "@/lib/types";

declare global {
  interface Window {
    __ASSISTANT_RUNTIME_CONFIG__?: {
      amapJsKey?: string;
      amapServiceHost?: string;
    };
    _AMapSecurityConfig?: { serviceHost: string };
  }
}

interface AMapLngLat {
  getLng(): number;
  getLat(): number;
}

interface AMapMap {
  add(overlay: AMapMarker): void;
  destroy(): void;
  off(event: "click", handler: (event: { lnglat: AMapLngLat }) => void): void;
  on(event: "click", handler: (event: { lnglat: AMapLngLat }) => void): void;
  remove(overlay: AMapMarker): void;
  setCenter(position: [number, number]): void;
  setZoom(zoom: number): void;
}

interface AMapMarker {
  setPosition(position: [number, number]): void;
}

interface AutocompleteTip {
  id?: string;
  name?: string;
  district?: string;
  adcode?: string;
  location?: unknown;
}

interface AutocompleteResult {
  tips?: AutocompleteTip[];
}

interface GeocoderResult {
  info?: string;
  regeocode?: {
    formattedAddress?: string;
    addressComponent?: {
      province?: string;
      city?: string | string[];
      district?: string;
      adcode?: string;
    };
    pois?: Array<{ id?: string; name?: string }>;
  };
}

interface GeolocationResult {
  position?: unknown;
}

interface AMapAutocomplete {
  search(
    keyword: string,
    callback: (status: string, result: AutocompleteResult | string) => void,
  ): void;
}

interface AMapGeocoder {
  getAddress(
    position: [number, number],
    callback: (status: string, result: GeocoderResult) => void,
  ): void;
}

interface AMapGeolocation {
  getCurrentPosition(callback: (status: string, result: GeolocationResult | string) => void): void;
}

interface AMapNamespace {
  Map: new (
    container: HTMLElement,
    options: { center: [number, number]; resizeEnable: boolean; zoom: number },
  ) => AMapMap;
  Marker: new (options: { position: [number, number] }) => AMapMarker;
  AutoComplete: new (options: { city: string }) => AMapAutocomplete;
  Geocoder: new (options: { city: string }) => AMapGeocoder;
  Geolocation: new (options: {
    convert: boolean;
    enableHighAccuracy: boolean;
    timeout: number;
  }) => AMapGeolocation;
}

interface SearchResult extends AutocompleteTip {
  latitude: number;
  longitude: number;
}

interface AMapLocationPickerProps {
  value: UserLocationInput | null;
  disabled: boolean;
  onChange: (location: UserLocationInput) => void;
  onSelectionComplete: (location: UserLocationInput) => void;
  onResolvingChange: (resolving: boolean) => void;
}

const defaultCenter: [number, number] = [116.397428, 39.90923];

function amapConfig() {
  const runtime = typeof window === "undefined" ? undefined : window.__ASSISTANT_RUNTIME_CONFIG__;
  return {
    key: runtime?.amapJsKey?.trim() || process.env.NEXT_PUBLIC_AMAP_JS_KEY?.trim() || "",
    serviceHost:
      runtime?.amapServiceHost?.trim() || process.env.NEXT_PUBLIC_AMAP_SERVICE_HOST?.trim() || "",
  };
}

function readCoordinates(value: unknown): { latitude: number; longitude: number } | null {
  if (Array.isArray(value) && value.length >= 2) {
    const longitude = Number(value[0]);
    const latitude = Number(value[1]);
    return Number.isFinite(latitude) && Number.isFinite(longitude) ? { latitude, longitude } : null;
  }
  if (typeof value === "string") {
    return readCoordinates(value.split(","));
  }
  if (value && typeof value === "object" && "getLng" in value && "getLat" in value) {
    const lngLat = value as AMapLngLat;
    const longitude = lngLat.getLng();
    const latitude = lngLat.getLat();
    return Number.isFinite(latitude) && Number.isFinite(longitude) ? { latitude, longitude } : null;
  }
  return null;
}

function cityName(value?: string | string[]) {
  return Array.isArray(value) ? (value[0] ?? "") : (value ?? "");
}

export function AMapLocationPicker({
  value,
  disabled,
  onChange,
  onSelectionComplete,
  onResolvingChange,
}: AMapLocationPickerProps) {
  const { key: amapKey, serviceHost: configuredAMapServiceHost } = amapConfig();
  const containerRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<AMapMap | null>(null);
  const markerRef = useRef<AMapMarker | null>(null);
  const amapRef = useRef<AMapNamespace | null>(null);
  const autocompleteRef = useRef<AMapAutocomplete | null>(null);
  const geolocationRef = useRef<AMapGeolocation | null>(null);
  const mountedRef = useRef(false);
  const disabledRef = useRef(disabled);
  const reverseRequestRef = useRef(0);
  const searchRequestRef = useRef(0);
  const geolocationRequestRef = useRef(0);
  const skipNextSearchRef = useRef(false);
  const reverseTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const selectCoordinatesRef = useRef<
    | ((
        latitude: number,
        longitude: number,
        source: UserLocationSource,
        poi?: AutocompleteTip,
      ) => void)
    | null
  >(null);
  const initialValueRef = useRef(value);
  const emitChange = useEffectEvent(onChange);
  const emitSelectionComplete = useEffectEvent(onSelectionComplete);
  const emitResolvingChange = useEffectEvent(onResolvingChange);
  const [loading, setLoading] = useState(Boolean(amapKey));
  const [loadError, setLoadError] = useState("");
  const [operationError, setOperationError] = useState("");
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResult[]>([]);
  const [searching, setSearching] = useState(false);
  const [locating, setLocating] = useState(false);

  useEffect(() => {
    if (!amapKey || !containerRef.current) return;

    mountedRef.current = true;
    let disposed = false;
    let clickHandler: ((event: { lnglat: AMapLngLat }) => void) | null = null;
    const initialize = async () => {
      setLoading(true);
      setLoadError("");
      try {
        window._AMapSecurityConfig = {
          serviceHost: configuredAMapServiceHost || `${window.location.origin}/_AMapService`,
        };
        const loaded: unknown = await loadAMap({
          key: amapKey,
          version: "2.0",
          plugins: ["AMap.AutoComplete", "AMap.Geocoder", "AMap.Geolocation"],
        });
        if (disposed || !containerRef.current) return;

        const AMap = loaded as AMapNamespace;
        const initial = initialValueRef.current;
        const center: [number, number] = initial
          ? [initial.longitude, initial.latitude]
          : defaultCenter;
        const map = new AMap.Map(containerRef.current, {
          center,
          resizeEnable: true,
          zoom: initial ? 15 : 11,
        });
        const geocoder = new AMap.Geocoder({ city: "全国" });
        amapRef.current = AMap;
        mapRef.current = map;
        autocompleteRef.current = new AMap.AutoComplete({ city: "全国" });
        geolocationRef.current = new AMap.Geolocation({
          convert: true,
          enableHighAccuracy: true,
          timeout: 10000,
        });

        const showMarker = (latitude: number, longitude: number) => {
          const position: [number, number] = [longitude, latitude];
          if (markerRef.current) {
            markerRef.current.setPosition(position);
          } else {
            markerRef.current = new AMap.Marker({ position });
            map.add(markerRef.current);
          }
          map.setCenter(position);
          map.setZoom(15);
        };

        selectCoordinatesRef.current = (latitude, longitude, source, poi) => {
          if (disposed || disabledRef.current) return;
          const requestID = ++reverseRequestRef.current;
          if (reverseTimeoutRef.current) clearTimeout(reverseTimeoutRef.current);
          showMarker(latitude, longitude);
          setOperationError("");
          emitResolvingChange(true);
          const coordinateDraft: UserLocationInput = {
            latitude,
            longitude,
            coordinate_system: "gcj02",
            formatted_address: [poi?.district, poi?.name].filter(Boolean).join(""),
            province: "",
            city: "",
            district: "",
            adcode: poi?.adcode?.trim() || "",
            poi_id: poi?.id?.trim() || "",
            poi_name: poi?.name?.trim() || "",
            source,
          };
          emitChange(coordinateDraft);

          const finishWithoutRegion = () => {
            if (disposed || requestID !== reverseRequestRef.current) return;
            reverseRequestRef.current += 1;
            reverseTimeoutRef.current = null;
            setOperationError("未能解析区县信息；仍可保存并使用坐标");
            emitSelectionComplete(coordinateDraft);
            emitResolvingChange(false);
          };
          reverseTimeoutRef.current = setTimeout(finishWithoutRegion, 12_000);
          geocoder.getAddress([longitude, latitude], (status, result) => {
            if (disposed || requestID !== reverseRequestRef.current) return;
            if (reverseTimeoutRef.current) clearTimeout(reverseTimeoutRef.current);
            reverseTimeoutRef.current = null;
            const regeocode = status === "complete" ? result.regeocode : undefined;
            const component = regeocode?.addressComponent;
            const nearestPOI = regeocode?.pois?.[0];
            const location: UserLocationInput = {
              ...coordinateDraft,
              formatted_address:
                regeocode?.formattedAddress?.trim() || coordinateDraft.formatted_address,
              province: component?.province?.trim() ?? "",
              city: cityName(component?.city).trim(),
              district: component?.district?.trim() ?? "",
              adcode: component?.adcode?.trim() || coordinateDraft.adcode,
              poi_id: coordinateDraft.poi_id || nearestPOI?.id?.trim() || "",
              poi_name: coordinateDraft.poi_name || nearestPOI?.name?.trim() || "",
            };
            if (!location.province && !location.city && !location.district) {
              setOperationError("未能解析区县信息；仍可保存并使用坐标");
            }
            emitChange(location);
            emitSelectionComplete(location);
            emitResolvingChange(false);
          });
        };

        if (initial) showMarker(initial.latitude, initial.longitude);
        clickHandler = (event) => {
          selectCoordinatesRef.current?.(event.lnglat.getLat(), event.lnglat.getLng(), "map");
        };
        map.on("click", clickHandler);
      } catch (error) {
        if (!disposed) {
          setLoadError(error instanceof Error ? error.message : "高德地图加载失败");
        }
      } finally {
        if (!disposed) setLoading(false);
      }
    };

    void initialize();
    return () => {
      disposed = true;
      mountedRef.current = false;
      reverseRequestRef.current += 1;
      searchRequestRef.current += 1;
      geolocationRequestRef.current += 1;
      if (reverseTimeoutRef.current) clearTimeout(reverseTimeoutRef.current);
      reverseTimeoutRef.current = null;
      selectCoordinatesRef.current = null;
      if (clickHandler && mapRef.current) mapRef.current.off("click", clickHandler);
      mapRef.current?.destroy();
      mapRef.current = null;
      markerRef.current = null;
      amapRef.current = null;
      autocompleteRef.current = null;
      geolocationRef.current = null;
    };
  }, [amapKey, configuredAMapServiceHost]);

  useEffect(() => {
    disabledRef.current = disabled;
    if (!disabled) return;
    searchRequestRef.current += 1;
    geolocationRequestRef.current += 1;
    setSearching(false);
    setLocating(false);
  }, [disabled]);

  useEffect(() => {
    const keyword = query.trim();
    const requestID = ++searchRequestRef.current;
    if (skipNextSearchRef.current) {
      skipNextSearchRef.current = false;
      setSearching(false);
      return;
    }
    if (!keyword || !autocompleteRef.current || disabled || loading || loadError) {
      setSearching(false);
      setResults([]);
      return;
    }

    setResults([]);
    const timer = window.setTimeout(() => {
      if (
        !mountedRef.current ||
        requestID !== searchRequestRef.current ||
        disabledRef.current ||
        !autocompleteRef.current
      ) {
        return;
      }
      setSearching(true);
      setOperationError("");
      autocompleteRef.current.search(keyword, (status, result) => {
        if (!mountedRef.current || requestID !== searchRequestRef.current || disabledRef.current) {
          return;
        }
        setSearching(false);
        if (status !== "complete" || typeof result === "string") {
          setResults([]);
          setOperationError("未找到匹配的地址");
          return;
        }
        const nextResults = (result.tips ?? []).flatMap((tip) => {
          const coordinates = readCoordinates(tip.location);
          return coordinates && tip.name ? [{ ...tip, ...coordinates }] : [];
        });
        setResults(nextResults);
        if (!nextResults.length) setOperationError("未找到可定位的地址");
      });
    }, 300);

    return () => window.clearTimeout(timer);
  }, [disabled, loadError, loading, query]);

  useEffect(() => {
    if (!mapRef.current || !amapRef.current) return;
    if (!value) {
      if (markerRef.current) mapRef.current.remove(markerRef.current);
      markerRef.current = null;
      return;
    }
    const position: [number, number] = [value.longitude, value.latitude];
    if (markerRef.current) {
      markerRef.current.setPosition(position);
    } else {
      markerRef.current = new amapRef.current.Marker({ position });
      mapRef.current.add(markerRef.current);
    }
    mapRef.current.setCenter(position);
  }, [value]);

  const useCurrentLocation = () => {
    if (!geolocationRef.current) return;
    const requestID = ++geolocationRequestRef.current;
    setLocating(true);
    setOperationError("");
    geolocationRef.current.getCurrentPosition((status, result) => {
      if (
        !mountedRef.current ||
        requestID !== geolocationRequestRef.current ||
        disabledRef.current
      ) {
        return;
      }
      setLocating(false);
      if (status !== "complete" || typeof result === "string") {
        setOperationError("无法获取当前位置，请检查浏览器定位权限");
        return;
      }
      const coordinates = readCoordinates(result.position);
      if (!coordinates) {
        setOperationError("当前位置坐标无效");
        return;
      }
      selectCoordinatesRef.current?.(coordinates.latitude, coordinates.longitude, "geolocation");
    });
  };

  if (!amapKey) {
    return (
      <div className="flex h-80 flex-col items-center justify-center border-y bg-muted/20 px-6 text-center sm:h-[360px]">
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
            onChange={(event) => {
              skipNextSearchRef.current = false;
              setQuery(event.target.value);
              setResults([]);
              setOperationError("");
            }}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                searchRequestRef.current += 1;
                setSearching(false);
                setResults([]);
              }
            }}
            placeholder="搜索地址或地点"
            maxLength={200}
            disabled={disabled || loading || Boolean(loadError)}
            aria-expanded={results.length > 0}
            aria-controls="amap-address-results"
            className="pl-9 pr-9"
          />
          {searching ? (
            <Loader2 className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 animate-spin text-muted-foreground" />
          ) : null}
          {results.length ? (
            <ul
              id="amap-address-results"
              aria-label="地址搜索结果"
              className="absolute inset-x-0 top-full z-30 mt-1 max-h-56 divide-y overflow-y-auto rounded-md border bg-popover text-popover-foreground shadow-lg"
            >
              {results.map((result, index) => (
                <li key={`${result.id ?? result.name}-${index}`}>
                  <button
                    type="button"
                    className="w-full px-3 py-2.5 text-left first:rounded-t-md last:rounded-b-md hover:bg-muted focus-visible:bg-muted focus-visible:outline-none"
                    disabled={disabled}
                    onClick={() => {
                      skipNextSearchRef.current = true;
                      searchRequestRef.current += 1;
                      setQuery(result.name ?? "");
                      setResults([]);
                      selectCoordinatesRef.current?.(
                        result.latitude,
                        result.longitude,
                        "search",
                        result,
                      );
                    }}
                  >
                    <span className="block truncate text-sm font-medium">{result.name}</span>
                    <span className="mt-0.5 block truncate text-xs text-muted-foreground">
                      {result.district || "地点详情暂缺"}
                    </span>
                  </button>
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
          onClick={useCurrentLocation}
          disabled={disabled || locating || loading || Boolean(loadError)}
        >
          {locating ? <Loader2 className="animate-spin" /> : <LocateFixed />}
          定位
        </Button>
      </div>

      <div className="relative h-80 overflow-hidden border-y bg-muted/20 sm:h-[360px]">
        <div ref={containerRef} className="size-full" aria-label="高德地图位置选择器" />
        {loading ? (
          <div className="absolute inset-0 flex items-center justify-center bg-background/80 text-sm text-muted-foreground">
            <Loader2 className="mr-2 size-4 animate-spin" />
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
