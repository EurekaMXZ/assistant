"use client";

import dynamic from "next/dynamic";
import { useEffect, useRef, useState } from "react";
import { Loader2, MapPin, RefreshCw, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import {
  ApiError,
  deleteProfileLocation,
  getPersonalization,
  getProfileLocation,
  isSessionUnauthorizedError,
  updatePersonalization,
  updateProfileLocation,
} from "@/lib/api";
import type { UserLocation, UserLocationInput } from "@/lib/types";

const AMapLocationPicker = dynamic(
  () =>
    import("@/components/settings/amap-location-picker").then(
      (module) => module.AMapLocationPicker,
    ),
  {
    ssr: false,
    loading: () => <Skeleton className="h-80 w-full rounded-none sm:h-[360px]" />,
  },
);

type LocationMutation = "idle" | "resolving" | "saving" | "clearing";

function locationTitle(location: UserLocationInput) {
  return (
    location.formatted_address ||
    location.poi_name ||
    `${location.latitude.toFixed(6)}, ${location.longitude.toFixed(6)}`
  );
}

export function PersonalizationSettings() {
  const mountedRef = useRef(false);
  const locationRequestRef = useRef(0);
  const savingPreferencesRef = useRef(false);
  const [preferencesText, setPreferencesText] = useState("");
  const [locationEnabled, setLocationEnabled] = useState(false);
  const [preferencesVersion, setPreferencesVersion] = useState(0);
  const [savedLocation, setSavedLocation] = useState<UserLocation | null>(null);
  const [draftLocation, setDraftLocation] = useState<UserLocationInput | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [savingPreferences, setSavingPreferences] = useState(false);
  const [locationMutation, setLocationMutation] = useState<LocationMutation>("idle");
  const preferencesLength = Array.from(preferencesText).length;
  const locationAPIBusy = locationMutation === "saving" || locationMutation === "clearing";

  const load = async () => {
    setLoading(true);
    setLoadError("");
    try {
      const [personalization, location] = await Promise.all([
        getPersonalization(),
        getProfileLocation(),
      ]);
      if (!mountedRef.current) return;
      setPreferencesText(personalization.preferences_text);
      setLocationEnabled(personalization.location_enabled_for_model);
      setPreferencesVersion(personalization.version);
      setSavedLocation(location);
      setDraftLocation(location);
    } catch (error) {
      if (mountedRef.current && !isSessionUnauthorizedError(error)) {
        setLoadError(error instanceof Error ? error.message : "个性化设置加载失败");
      }
    } finally {
      if (mountedRef.current) setLoading(false);
    }
  };

  useEffect(() => {
    mountedRef.current = true;
    void load();
    return () => {
      mountedRef.current = false;
      locationRequestRef.current += 1;
    };
  }, []);

  const savePreferences = async (event: React.FormEvent) => {
    event.preventDefault();
    if (savingPreferencesRef.current) return;
    savingPreferencesRef.current = true;
    setSavingPreferences(true);
    const payload = {
      preferences_text: preferencesText,
      location_enabled_for_model: locationEnabled,
    };
    try {
      let saved;
      try {
        saved = await updatePersonalization({
          ...payload,
          expected_version: preferencesVersion,
        });
      } catch (error) {
        if (!(error instanceof ApiError) || error.status !== 409) throw error;
        const latest = await getPersonalization();
        if (!mountedRef.current) return;
        saved = await updatePersonalization({
          ...payload,
          expected_version: latest.version,
        });
      }
      if (!mountedRef.current) return;
      setPreferencesText(saved.preferences_text);
      setLocationEnabled(saved.location_enabled_for_model);
      setPreferencesVersion(saved.version);
      toast.success("个性化偏好已保存");
    } catch (error) {
      if (mountedRef.current && !isSessionUnauthorizedError(error)) {
        toast.error(
          error instanceof ApiError && error.status === 409
            ? "设置已在其他窗口更新，请重试"
            : error instanceof Error
              ? error.message
              : "偏好保存失败",
        );
      }
    } finally {
      savingPreferencesRef.current = false;
      if (mountedRef.current) setSavingPreferences(false);
    }
  };

  const saveLocation = async (location: UserLocationInput) => {
    if (locationMutation === "saving" || locationMutation === "clearing") return;
    const requestID = ++locationRequestRef.current;
    setLocationMutation("saving");
    try {
      const saved = await updateProfileLocation(location);
      if (!mountedRef.current || requestID !== locationRequestRef.current) return;
      setSavedLocation(saved);
      setDraftLocation(saved);
      toast.success("位置已保存");
    } catch (error) {
      if (
        mountedRef.current &&
        requestID === locationRequestRef.current &&
        !isSessionUnauthorizedError(error)
      ) {
        setDraftLocation(savedLocation);
        toast.error(error instanceof Error ? error.message : "位置保存失败");
      }
    } finally {
      if (mountedRef.current && requestID === locationRequestRef.current) {
        setLocationMutation("idle");
      }
    }
  };

  const clearLocation = async () => {
    if (!savedLocation || locationMutation !== "idle") return;
    const requestID = ++locationRequestRef.current;
    setLocationMutation("clearing");
    try {
      await deleteProfileLocation();
      if (!mountedRef.current || requestID !== locationRequestRef.current) return;
      setSavedLocation(null);
      setDraftLocation(null);
      toast.success("位置已清除");
    } catch (error) {
      if (
        mountedRef.current &&
        requestID === locationRequestRef.current &&
        !isSessionUnauthorizedError(error)
      ) {
        toast.error(error instanceof Error ? error.message : "位置清除失败");
      }
    } finally {
      if (mountedRef.current && requestID === locationRequestRef.current) {
        setLocationMutation("idle");
      }
    }
  };

  if (loading) {
    return (
      <div className="space-y-7">
        <Skeleton className="h-7 w-24" />
        <Skeleton className="h-44 w-full" />
        <Skeleton className="h-48 w-full rounded-none" />
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="flex min-h-72 flex-col items-center justify-center text-center">
        <MapPin className="size-6 text-muted-foreground" />
        <p className="mt-3 text-sm font-medium">{loadError}</p>
        <Button type="button" variant="outline" size="sm" className="mt-4" onClick={load}>
          <RefreshCw />
          重新加载
        </Button>
      </div>
    );
  }

  return (
    <div className="space-y-9">
      <header>
        <h2 className="text-xl font-semibold">个性化</h2>
      </header>

      <form onSubmit={savePreferences} className="space-y-5 border-b pb-9">
        <div className="space-y-2">
          <div className="flex items-center justify-between gap-4">
            <Label htmlFor="preferences-text">回复偏好</Label>
            <span className="text-xs tabular-nums text-muted-foreground">
              {preferencesLength}/8000
            </span>
          </div>
          <Textarea
            id="preferences-text"
            value={preferencesText}
            onChange={(event) => {
              if (Array.from(event.target.value).length <= 8000) {
                setPreferencesText(event.target.value);
              }
            }}
            rows={6}
            className="min-h-36 resize-y text-sm"
            placeholder="例如：使用简洁中文回答；代码示例优先使用 Go。"
          />
        </div>

        <label className="flex items-start gap-3 border-y py-4">
          <input
            type="checkbox"
            checked={locationEnabled}
            onChange={(event) => setLocationEnabled(event.target.checked)}
            className="mt-0.5 size-4 shrink-0 accent-foreground"
          />
          <span>
            <span className="block text-sm font-medium">允许模型使用已保存的位置</span>
            <span className="mt-1 block text-xs leading-5 text-muted-foreground">
              向模型提供已保存的地址、地点名称、GCJ-02 经纬度及省市区信息，用于理解当前位置并查找附近地点。关闭后位置仍保存在账户中。
            </span>
          </span>
        </label>

        <Button type="submit" size="sm" disabled={savingPreferences}>
          {savingPreferences ? <Loader2 className="animate-spin" /> : <Save />}
          保存偏好
        </Button>
      </form>

      <section className="space-y-5">
        <div className="flex min-w-0 items-start justify-between gap-4 border-y py-4">
          <div className="flex min-w-0 items-start gap-3">
            <MapPin className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0">
              <h3 className="text-sm font-medium">我的位置</h3>
              <p className="mt-1 truncate text-xs text-muted-foreground">
                {locationMutation === "saving"
                  ? "正在保存…"
                  : savedLocation
                    ? locationTitle(savedLocation)
                    : "未设置"}
              </p>
            </div>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="删除我的位置"
            title="删除我的位置"
            onClick={clearLocation}
            disabled={!savedLocation || locationMutation !== "idle"}
          >
            {locationMutation === "clearing" ? (
              <Loader2 className="animate-spin" />
            ) : (
              <Trash2 />
            )}
          </Button>
        </div>

        <AMapLocationPicker
          value={draftLocation}
          disabled={locationAPIBusy}
          onChange={(location) => {
            if (!locationAPIBusy) setDraftLocation(location);
          }}
          onSelectionComplete={(location) => {
            void saveLocation(location);
          }}
          onResolvingChange={(resolving) => {
            setLocationMutation((current) => {
              if (resolving && current === "idle") return "resolving";
              if (!resolving && current === "resolving") return "idle";
              return current;
            });
          }}
        />

      </section>
    </div>
  );
}
