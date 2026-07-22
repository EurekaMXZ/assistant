import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AMapLocationPickerView } from "./amap-location-picker-view";

const amapLoader = vi.hoisted(() => ({ load: vi.fn() }));

vi.mock("@amap/amap-jsapi-loader", () => amapLoader);

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT =
  true;

describe("AMap location picker", () => {
  let container: HTMLDivElement;
  let root: Root;
  let rootUnmounted: boolean;

  beforeEach(() => {
    vi.stubEnv("NEXT_PUBLIC_AMAP_JS_KEY", "public-test-key");
    vi.stubEnv("NEXT_PUBLIC_AMAP_SERVICE_HOST", "http://localhost:8081/_AMapService");
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    rootUnmounted = false;
  });

  afterEach(async () => {
    if (!rootUnmounted) await act(async () => root.unmount());
    container.remove();
    vi.unstubAllEnvs();
    vi.resetModules();
  });

  it("ignores stale reverse-geocode callbacks and callbacks after unmount", async () => {
    const reverseCallbacks: Array<(status: string, result: Record<string, unknown>) => void> = [];
    let clickHandler: ((event: { lnglat: { getLat(): number; getLng(): number } }) => void) | null =
      null;

    class FakeMap {
      add() {}
      destroy() {}
      off() {}
      on(_event: string, handler: typeof clickHandler) {
        clickHandler = handler;
      }
      remove() {}
      setCenter() {}
      setZoom() {}
    }
    class FakeMarker {
      setPosition() {}
    }
    class FakeAutocomplete {
      search() {}
    }
    class FakeGeocoder {
      getAddress(
        _position: [number, number],
        callback: (status: string, result: Record<string, unknown>) => void,
      ) {
        reverseCallbacks.push(callback);
      }
    }
    class FakeGeolocation {
      getCurrentPosition() {}
    }

    amapLoader.load.mockResolvedValue({
      Map: FakeMap,
      Marker: FakeMarker,
      AutoComplete: FakeAutocomplete,
      Geocoder: FakeGeocoder,
      Geolocation: FakeGeolocation,
    });
    const { AMapLocationPicker } = await import("./amap-location-picker");
    const onChange = vi.fn();
    const onSelectionComplete = vi.fn();
    const onResolvingChange = vi.fn();

    await act(async () => {
      root.render(
        <AMapLocationPicker
          value={null}
          disabled={false}
          onChange={onChange}
          onSelectionComplete={onSelectionComplete}
          onResolvingChange={onResolvingChange}
        />,
      );
      await Promise.resolve();
    });
    expect(clickHandler).not.toBeNull();
    expect(window._AMapSecurityConfig?.serviceHost).toBe("http://localhost:8081/_AMapService");

    await act(async () => {
      clickHandler?.({ lnglat: { getLat: () => 31.1, getLng: () => 121.1 } });
      clickHandler?.({ lnglat: { getLat: () => 31.2, getLng: () => 121.2 } });
    });
    expect(onChange).toHaveBeenCalledTimes(2);
    expect(onChange.mock.calls[1]?.[0]).toMatchObject({ latitude: 31.2, longitude: 121.2 });

    await act(async () => {
      reverseCallbacks[0]?.("complete", {
        regeocode: {
          addressComponent: { province: "旧省", city: "旧市", district: "旧区" },
        },
      });
    });
    expect(onChange).toHaveBeenCalledTimes(2);

    await act(async () => {
      reverseCallbacks[1]?.("complete", {
        regeocode: {
          addressComponent: { province: "新省", city: "新市", district: "新区" },
        },
      });
    });
    expect(onChange).toHaveBeenCalledTimes(3);
    expect(onChange.mock.calls[2]?.[0]).toMatchObject({
      latitude: 31.2,
      longitude: 121.2,
      province: "新省",
      city: "新市",
      district: "新区",
    });
    expect(onSelectionComplete).toHaveBeenCalledTimes(1);
    expect(onSelectionComplete.mock.calls[0]?.[0]).toMatchObject({
      latitude: 31.2,
      longitude: 121.2,
      province: "新省",
    });
    expect(onResolvingChange.mock.calls.at(-1)?.[0]).toBe(false);

    await act(async () => {
      clickHandler?.({ lnglat: { getLat: () => 31.3, getLng: () => 121.3 } });
    });
    const callCountBeforeUnmount = onChange.mock.calls.length;
    await act(async () => root.unmount());
    rootUnmounted = true;
    reverseCallbacks[2]?.("complete", {
      regeocode: { addressComponent: { province: "卸载后", city: "", district: "" } },
    });
    expect(onChange).toHaveBeenCalledTimes(callCountBeforeUnmount);
  });

  it("dismisses address results when clicking outside the search area", async () => {
    const onEscapeSearch = vi.fn();
    await act(async () => {
      root.render(
        <AMapLocationPickerView
          configured
          containerRef={{ current: null }}
          disabled={false}
          loadError=""
          loading={false}
          locating={false}
          operationError=""
          query="杭州"
          results={[
            {
              id: "result-1",
              name: "杭州电子科技大学",
              district: "钱塘区",
              latitude: 30.315,
              longitude: 120.343,
            },
          ]}
          searching={false}
          onEscapeSearch={onEscapeSearch}
          onLocate={vi.fn()}
          onQueryChange={vi.fn()}
          onSelectResult={vi.fn()}
        />,
      );
    });

    const input = container.querySelector<HTMLInputElement>('input[placeholder="搜索地址或地点"]');
    input?.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true }));
    expect(onEscapeSearch).not.toHaveBeenCalled();

    document.body.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true }));
    expect(onEscapeSearch).toHaveBeenCalledOnce();
  });
});
