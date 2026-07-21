import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  deleteProfileLocation: vi.fn(),
  loadAMap: vi.fn(),
  getPersonalization: vi.fn(),
  getProfileLocation: vi.fn(),
  updatePersonalization: vi.fn(),
  updateProfileLocation: vi.fn(),
}));

vi.mock("@amap/amap-jsapi-loader", () => ({ load: mocks.loadAMap }));
vi.mock("@/lib/api", () => ({
  ApiError: class ApiError extends Error {
    status: number;
    constructor(message: string, status: number) {
      super(message);
      this.status = status;
    }
  },
  deleteProfileLocation: mocks.deleteProfileLocation,
  getPersonalization: mocks.getPersonalization,
  getProfileLocation: mocks.getProfileLocation,
  isSessionUnauthorizedError: () => false,
  updatePersonalization: mocks.updatePersonalization,
  updateProfileLocation: mocks.updateProfileLocation,
}));

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT =
  true;

describe("personalization location settings", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubEnv("NEXT_PUBLIC_AMAP_JS_KEY", "public-test-key");
    mocks.getPersonalization.mockResolvedValue({
      user_id: "user-1",
      preferences_text: "",
      location_enabled_for_model: false,
      version: 0,
    });
    mocks.getProfileLocation.mockResolvedValue(null);
    mocks.updatePersonalization.mockImplementation(async (input) => ({
      user_id: "user-1",
      preferences_text: input.preferences_text,
      location_enabled_for_model: input.location_enabled_for_model,
      version: input.expected_version + 1,
      created_at: "2026-07-21T10:00:00Z",
      updated_at: "2026-07-21T10:00:00Z",
    }));
    mocks.deleteProfileLocation.mockResolvedValue(undefined);
    mocks.updateProfileLocation.mockImplementation(async (location) => ({
      ...location,
      user_id: "user-1",
      created_at: "2026-07-21T10:00:00Z",
      updated_at: "2026-07-21T10:00:00Z",
    }));
    mocks.loadAMap.mockResolvedValue({
      Map: class {
        add() {}
        destroy() {}
        off() {}
        on() {}
        remove() {}
        setCenter() {}
        setZoom() {}
      },
      Marker: class {
        setPosition() {}
      },
      AutoComplete: class {
        search(
          _keyword: string,
          callback: (status: string, result: Record<string, unknown>) => void,
        ) {
          callback("complete", {
            tips: [
              {
                id: "poi-1",
                name: "杭州东站",
                district: "浙江省杭州市上城区",
                adcode: "330102",
                location: "120.212605,30.290846",
              },
            ],
          });
        }
      },
      Geocoder: class {
        getAddress(
          _position: [number, number],
          callback: (status: string, result: Record<string, unknown>) => void,
        ) {
          callback("complete", {
            regeocode: {
              formattedAddress: "浙江省杭州市上城区杭州东站",
              addressComponent: {
                province: "浙江省",
                city: "杭州市",
                district: "上城区",
                adcode: "330102",
              },
            },
          });
        }
      },
      Geolocation: class {
        getCurrentPosition() {}
      },
    });
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllEnvs();
    vi.resetModules();
  });

  it("loads AMap by default", async () => {
    const { PersonalizationSettings } = await import("./personalization-settings");
    await act(async () => {
      root.render(<PersonalizationSettings />);
      await Promise.resolve();
    });

    await vi.waitFor(() => expect(mocks.loadAMap).toHaveBeenCalledTimes(1));
    expect(container.textContent).toContain("我的位置");
    expect(container.querySelector('[aria-label="删除我的位置"]')).not.toBeNull();
  });

  it("saves a search candidate immediately and deletes it from the header action", async () => {
    const { PersonalizationSettings } = await import("./personalization-settings");
    await act(async () => {
      root.render(<PersonalizationSettings />);
      await Promise.resolve();
    });
    await vi.waitFor(() => expect(mocks.loadAMap).toHaveBeenCalledTimes(1));

    const input = container.querySelector<HTMLInputElement>('input[placeholder="搜索地址或地点"]');
    expect(input).not.toBeNull();
    await act(async () => {
      const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
      setValue?.call(input, "杭州");
      input?.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await vi.waitFor(() => {
      expect(mocks.updateProfileLocation).not.toHaveBeenCalled();
      expect(container.textContent).toContain("杭州东站");
    });
    const candidate = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("杭州东站"),
    );
    expect(candidate).toBeDefined();
    const resultList = container.querySelector('[aria-label="地址搜索结果"]');
    expect(resultList?.className).toContain("absolute");
    expect(resultList?.parentElement?.querySelector('input[placeholder="搜索地址或地点"]')).not.toBeNull();
    await act(async () => {
      candidate?.click();
      await vi.waitFor(() => expect(mocks.updateProfileLocation).toHaveBeenCalledTimes(1));
    });
    expect(mocks.updateProfileLocation.mock.calls[0]?.[0]).toMatchObject({
      formatted_address: "浙江省杭州市上城区杭州东站",
      source: "search",
    });
    expect(container.textContent).toContain("浙江省杭州市上城区杭州东站");

    const deleteButton = container.querySelector<HTMLButtonElement>(
      '[aria-label="删除我的位置"]',
    );
    await act(async () => {
      deleteButton?.click();
      await vi.waitFor(() => expect(mocks.deleteProfileLocation).toHaveBeenCalledTimes(1));
    });
    expect(container.textContent).toContain("未设置");
  });

  it("refreshes the preference version and retries one conflict", async () => {
    const { ApiError } = await import("@/lib/api");
    mocks.getPersonalization
      .mockResolvedValueOnce({
        user_id: "user-1",
        preferences_text: "",
        location_enabled_for_model: false,
        version: 0,
      })
      .mockResolvedValueOnce({
        user_id: "user-1",
        preferences_text: "其他窗口的内容",
        location_enabled_for_model: true,
        version: 1,
      });
    mocks.updatePersonalization
      .mockRejectedValueOnce(new ApiError("conflict", 409))
      .mockResolvedValueOnce({
        user_id: "user-1",
        preferences_text: "保留当前输入",
        location_enabled_for_model: false,
        version: 2,
      });

    const { PersonalizationSettings } = await import("./personalization-settings");
    await act(async () => {
      root.render(<PersonalizationSettings />);
      await Promise.resolve();
    });

    const textarea = container.querySelector<HTMLTextAreaElement>("#preferences-text");
    await act(async () => {
      const setValue = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")?.set;
      setValue?.call(textarea, "保留当前输入");
      textarea?.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const saveButton = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "保存偏好",
    );
    await act(async () => {
      saveButton?.click();
      await vi.waitFor(() => expect(mocks.updatePersonalization).toHaveBeenCalledTimes(2));
    });

    expect(mocks.updatePersonalization.mock.calls).toEqual([
      [
        {
          preferences_text: "保留当前输入",
          location_enabled_for_model: false,
          expected_version: 0,
        },
      ],
      [
        {
          preferences_text: "保留当前输入",
          location_enabled_for_model: false,
          expected_version: 1,
        },
      ],
    ]);
  });
});
