import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { User } from "@/lib/types";
import { SidebarUserPanel } from "./sidebar-user-panel";

vi.mock("@/lib/api", () => ({
  getBillingAccount: vi.fn(async () => ({
    id: "account-1",
    user_id: "user-1",
    currency: "USD",
    status: "active",
    balance_nanos: 0,
    balance: "0.00",
    version: 1,
    created_at: "2026-07-23T00:00:00Z",
    updated_at: "2026-07-23T00:00:00Z",
  })),
  isSessionUnauthorizedError: vi.fn(() => false),
}));

vi.mock("@/lib/billing-account-events", () => ({
  subscribeBillingAccountUpdated: vi.fn(() => () => undefined),
}));

const user: User = {
  id: "user-1",
  email: "user@example.com",
  username: "Test User",
  role: "user",
  status: "active",
  created_at: "2026-07-23T00:00:00Z",
  updated_at: "2026-07-23T00:00:00Z",
  storage_quota_bytes: 1024,
  storage_used_bytes: 0,
  sandbox_quota: 3,
};

describe("sidebar user panel", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("exposes repository help links from a submenu", async () => {
    await act(async () => {
      root.render(
        <SidebarUserPanel
          authLoading={false}
          showAdmin={false}
          user={user}
          onOpenAdmin={vi.fn()}
          onLogout={vi.fn()}
          onOpenLogin={vi.fn()}
          onOpenRegister={vi.fn()}
          onOpenSettings={vi.fn()}
        />,
      );
    });

    const userMenu = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("Test User"),
    );
    await act(async () => userMenu?.click());
    const helpMenu = Array.from(
      document.body.querySelectorAll<HTMLElement>("[role=menuitem]"),
    ).find((item) => item.textContent?.includes("帮助"));
    await act(async () => helpMenu?.click());

    const links = Object.fromEntries(
      Array.from(document.body.querySelectorAll<HTMLAnchorElement>("a[target=_blank]")).map(
        (link) => [link.textContent?.trim(), link.href],
      ),
    );
    expect(links).toMatchObject({
      关于: "https://github.com/EurekaMXZ/assistant",
      报告错误: "https://github.com/EurekaMXZ/assistant/issues",
      帮助中心: "https://github.com/EurekaMXZ/assistant/discussions",
      开源协议: "https://www.apache.org/licenses/LICENSE-2.0",
    });
  });
});
