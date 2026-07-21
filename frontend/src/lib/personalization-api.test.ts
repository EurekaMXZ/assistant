import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  getPersonalization,
  getProfileLocation,
  updatePersonalization,
  updateProfileLocation,
} from "./api";
import { userLocationInputSchema } from "./api-schemas";

const timestamp = "2026-07-21T12:00:00Z";

beforeEach(() => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
    clear: () => values.clear(),
    key: (index: number) => [...values.keys()][index] ?? null,
    get length() {
      return values.size;
    },
  } satisfies Storage;
  vi.stubGlobal("localStorage", storage);
  Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
});

describe("profile personalization API", () => {
  it("loads personalization defaults through the typed envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      Response.json({
        personalization: {
          user_id: "user-1",
          preferences_text: "",
          location_enabled_for_model: false,
          version: 0,
          created_at: timestamp,
          updated_at: timestamp,
        },
      }),
    );

    const result = await getPersonalization();

    expect(result.version).toBe(0);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/v1/profile/personalization");
  });

  it("maps a no-content location response to null", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));

    await expect(getProfileLocation()).resolves.toBeNull();
  });

  it("sends only validated personalization and GCJ-02 location fields", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(
        Response.json({
          personalization: {
            user_id: "user-1",
            preferences_text: "回答简洁",
            location_enabled_for_model: true,
            version: 1,
            created_at: timestamp,
            updated_at: timestamp,
          },
        }),
      )
      .mockResolvedValueOnce(
        Response.json({
          location: {
            user_id: "user-1",
            latitude: 31.2304,
            longitude: 121.4737,
            coordinate_system: "gcj02",
            formatted_address: "上海市黄浦区",
            province: "上海市",
            city: "上海市",
            district: "黄浦区",
            adcode: "310101",
            poi_id: "",
            poi_name: "",
            source: "map",
            created_at: timestamp,
            updated_at: timestamp,
          },
        }),
      );

    await updatePersonalization({
      preferences_text: "回答简洁",
      location_enabled_for_model: true,
      expected_version: 0,
    });
    await updateProfileLocation({
      latitude: 31.2304,
      longitude: 121.4737,
      coordinate_system: "gcj02",
      formatted_address: "上海市黄浦区",
      province: "上海市",
      city: "上海市",
      district: "黄浦区",
      adcode: "310101",
      poi_id: "",
      poi_name: "",
      source: "map",
    });

    expect(fetchMock.mock.calls.map((call) => [call[0], call[1]?.method])).toEqual([
      ["/api/v1/profile/personalization", "PUT"],
      ["/api/v1/profile/location", "PUT"],
    ]);
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
      preferences_text: "回答简洁",
      location_enabled_for_model: true,
      expected_version: 0,
    });
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).not.toHaveProperty("user_id");
  });

  it("rejects unsupported coordinate systems, sources, and adcodes", () => {
    const invalid = {
      latitude: 31.2,
      longitude: 121.4,
      coordinate_system: "wgs84",
      formatted_address: "",
      province: "",
      city: "",
      district: "",
      adcode: "31A101",
      poi_id: "",
      poi_name: "",
      source: "manual",
    };
    expect(userLocationInputSchema.safeParse(invalid).success).toBe(false);
  });
});
