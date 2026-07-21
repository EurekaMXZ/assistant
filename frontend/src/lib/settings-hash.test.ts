import { describe, expect, it } from "vitest";
import { buildSettingsUrl, parseSettingsHash } from "./settings-hash";

describe("settings hash", () => {
  it("round-trips the personalization section", () => {
    expect(buildSettingsUrl("user/personalization")).toBe("/#settings/user/personalization");
    expect(parseSettingsHash("#settings/user/personalization")).toBe("user/personalization");
  });
});
