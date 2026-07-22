import { describe, expect, it } from "vitest";
import { formatMessageDateTime } from "./format";

describe("message date formatting", () => {
  const now = new Date(2026, 6, 23, 18, 30);

  it("uses relative calendar labels during the last week", () => {
    expect(formatMessageDateTime(new Date(2026, 6, 23, 9, 5), now)).toBe("今天 09:05");
    expect(formatMessageDateTime(new Date(2026, 6, 22, 9, 5), now)).toBe("昨天 09:05");
    expect(formatMessageDateTime(new Date(2026, 6, 21, 9, 5), now)).toBe("前天 09:05");
    expect(formatMessageDateTime(new Date(2026, 6, 20, 9, 5), now)).toBe("周一 09:05");
  });

  it("uses a full date and time outside the last week", () => {
    const formatted = formatMessageDateTime(new Date(2026, 6, 16, 9, 5), now);
    expect(formatted).toContain("2026");
    expect(formatted).toContain("07");
    expect(formatted).toContain("09:05");
  });

  it("returns the fallback for invalid values", () => {
    expect(formatMessageDateTime("not-a-date", now)).toBe("-");
  });
});
