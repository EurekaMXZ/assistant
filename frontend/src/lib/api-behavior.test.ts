import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  ApiResponseValidationError,
  answerToolCall,
  applyAdminBillingAdjustment,
  createConversationShare,
  deleteAdminModel,
  deleteAdminUser,
  deleteConversation,
  deleteStorageAttachment,
  disableAdminBillingRedemptionCode,
  getAdminOverview,
  getConversationShare,
  getStorageOverview,
  getStreamUrl,
  handleSessionUnauthorized,
  issueAdminBillingRedemptionCodes,
  isSessionUnauthorizedError,
  listAdminUsersPage,
  redeemBillingCode,
  updateAdminBillingToolPrices,
} from "./api";

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
  storage.setItem("assistant_access_token", "token");
});

describe("authentication error handling", () => {
  it("clears credentials only for actual session unauthorized responses", () => {
    expect(handleSessionUnauthorized(503, "temporarily unavailable")).toBe(false);
    expect(window.localStorage.getItem("assistant_access_token")).toBe("token");
    expect(isSessionUnauthorizedError(new ApiError("temporarily unavailable", 503))).toBe(false);
    expect(handleSessionUnauthorized(401, "invalid access token")).toBe(true);
    expect(window.localStorage.getItem("assistant_access_token")).toBeNull();
  });
});

describe("billing idempotency", () => {
  it("uses the caller-provided operation key", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({
          transaction: {
            id: "transaction-1",
            account_id: "account-1",
            user_id: "user-1",
            currency: "USD",
            account_sequence: 1,
            kind: "manual_topup",
            direction: "credit",
            amount_nanos: 1_000_000_000,
            amount: "1.00",
            balance_after_nanos: 1_000_000_000,
            balance_after: "1.00",
            reason: "test",
            reference: "ticket",
            created_at: "2026-01-01T00:00:00Z",
          },
        }),
        { status: 201, headers: { "Content-Type": "application/json" } },
      ),
    );
    await applyAdminBillingAdjustment(
      "user-1",
      "topups",
      {
        amount: "1.00",
        currency: "USD",
        reason: "test",
        reference: "ticket",
      },
      "billing-operation-1",
    );
    expect(new Headers(fetchMock.mock.calls[0][1]?.headers).get("Idempotency-Key")).toBe(
      "billing-operation-1",
    );
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/admin/billing/accounts/user-1/topups");
  });
});

describe("backend API routing", () => {
  it("routes backend stream paths directly to the public API", () => {
    expect(getStreamUrl("/api/v1/turns/turn-1/stream")).toBe("/api/v1/turns/turn-1/stream");
    expect(getStreamUrl("/turns/turn-1/stream")).toBe("/api/v1/turns/turn-1/stream");
  });
});

describe("ask user answers", () => {
  const response = {
    interaction: {
      id: "ask-user:tool-1",
      tool_call_id: "tool-1",
      prompt: "Continue?",
      kind: "single_choice",
      options: [
        { id: "yes", label: "Yes", tone: "primary" },
        { id: "cancel", label: "Cancel", tone: "neutral" },
      ],
      status: "completed",
      answer: {
        status: "answered",
        option_id: "yes",
        label: "Yes",
        user_reported: true,
      },
    },
    stream_path: "/api/v1/turns/turn-1/stream",
  };

  it("posts the option with the supplied idempotency key", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(Response.json(response));

    const result = await answerToolCall("turn/1", "tool/1", "yes", "answer-operation-1");

    expect(result.interaction.answer?.label).toBe("Yes");
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/turns/turn%2F1/tool-calls/tool%2F1/answer");
    expect(fetchMock.mock.calls[0][1]?.body).toBe(JSON.stringify({ option_id: "yes" }));
    expect(new Headers(fetchMock.mock.calls[0][1]?.headers).get("Idempotency-Key")).toBe(
      "answer-operation-1",
    );
  });

  it("rejects response fields outside the interaction contract", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      Response.json({ ...response, interaction: { ...response.interaction, unexpected: true } }),
    );

    await expect(
      answerToolCall("turn-1", "tool-1", "yes", "answer-operation-1"),
    ).rejects.toBeInstanceOf(ApiResponseValidationError);
  });
});

describe("storage and deletion APIs", () => {
  it("loads storage usage and attachment metadata", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      Response.json({
        storage: { quota_bytes: 512, used_bytes: 128, available_bytes: 384 },
        data: [
          {
            id: "attachment-1",
            conversation_id: "conversation-1",
            uploaded_by_user_id: "user-1",
            filename: "report.txt",
            content_type: "text/plain",
            category: "text",
            size_bytes: 128,
            sha256: "0".repeat(64),
            status: "ready",
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
            conversation_title: "Report",
          },
        ],
        page: { has_more: false },
      }),
    );

    const result = await getStorageOverview();

    expect(result.storage.available_bytes).toBe(384);
    expect(result.data[0]?.conversation_title).toBe("Report");
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/storage?limit=50");
  });

  it("uses no-content deletes for attachment and management resources", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response(null, { status: 204 }));

    await deleteStorageAttachment("attachment-1");
    await deleteConversation("conversation-1");
    await deleteAdminUser("user-1");
    await deleteAdminModel("model-1");

    expect(fetchMock.mock.calls.map((call) => [String(call[0]), call[1]?.method])).toEqual([
      ["/api/v1/storage/attachments/attachment-1", "DELETE"],
      ["/api/v1/conversations/conversation-1", "DELETE"],
      ["/api/v1/users/user-1", "DELETE"],
      ["/api/v1/admin/models/model-1", "DELETE"],
    ]);
  });
});

describe("conversation sharing", () => {
  it("creates a share with the caller-provided idempotency key", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      Response.json(
        {
          share: {
            id: "share-1",
            conversation_id: "conversation-1",
            created_by_user_id: "user-1",
            title: "Shared conversation",
            last_message_seq: 4,
            created_at: "2026-07-14T12:00:00Z",
          },
          replayed: false,
        },
        { status: 201 },
      ),
    );

    const result = await createConversationShare("conversation-1", "share-operation-1");

    expect(result.share.id).toBe("share-1");
    expect(result.share.last_message_seq).toBe(4);
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/conversations/conversation-1/shares");
    expect(fetchMock.mock.calls[0][1]?.method).toBe("POST");
    expect(new Headers(fetchMock.mock.calls[0][1]?.headers).get("Idempotency-Key")).toBe(
      "share-operation-1",
    );
  });

  it("loads a public conversation snapshot without requiring a share creation route", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      Response.json({
        share: {
          id: "share/1",
          title: "Shared conversation",
          last_message_seq: 1,
          created_at: "2026-07-14T12:00:00Z",
          messages: [
            {
              id: "message-1",
              conversation_id: "conversation-1",
              seq: 1,
              role: "user",
              content_text: "hello",
              metadata: {},
              created_at: "2026-07-14T12:00:00Z",
            },
          ],
        },
      }),
    );

    const result = await getConversationShare("share/1");

    expect(result.messages[0]?.content_text).toBe("hello");
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/conversation-shares/share%2F1");
  });
});

describe("cursor pagination", () => {
  it("requests only the specified admin user page", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      Response.json({
        data: [
          {
            id: "user-1",
            email: "user@example.com",
            username: "user",
            role: "user",
            status: "active",
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
        ],
        page: { next_cursor: "next-users", has_more: true },
      }),
    );

    const result = await listAdminUsersPage("current users");

    expect(result.data).toHaveLength(1);
    expect(result.page.next_cursor).toBe("next-users");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/users?limit=50&cursor=current%20users");
  });
});

describe("admin overview", () => {
  it("uses the aggregate endpoint instead of enumerating list pages", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      Response.json({
        users: 12,
        active_accounts: 8,
        audit_events: 34,
        audit: [],
      }),
    );

    const result = await getAdminOverview();

    expect(result.users).toBe(12);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/admin/overview");
  });
});

describe("billing redemptions", () => {
  it("redeems a code against the authenticated account", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      Response.json({
        account: {
          id: "account-1",
          user_id: "user-1",
          currency: "USD",
          status: "active",
          balance_nanos: 5_000_000_000,
          balance: "5.00",
          version: 1,
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
        transaction: {
          id: "transaction-1",
          account_id: "account-1",
          user_id: "user-1",
          currency: "USD",
          account_sequence: 1,
          kind: "redemption_credit",
          direction: "credit",
          amount_nanos: 5_000_000_000,
          amount: "5.00",
          balance_after_nanos: 5_000_000_000,
          balance_after: "5.00",
          actor_user_id: "user-1",
          reason: "Redemption code",
          reference: "***abcdef",
          created_at: "2026-01-01T00:00:00Z",
        },
        replayed: false,
      }),
    );

    const code = "0123456789abcdef0123456789abcdef0123456789abcdef";
    const result = await redeemBillingCode(code);

    expect(result.transaction.kind).toBe("redemption_credit");
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/billing/redemptions");
    expect(fetchMock.mock.calls[0][1]?.body).toBe(JSON.stringify({ code }));
  });

  it("issues a code from the admin billing API", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      Response.json({
        redemption_codes: [
          {
            redemption_code: {
              id: "code-1",
              code_hint: "***abcdef",
              currency: "USD",
              amount_nanos: 5_000_000_000,
              amount: "5.00",
              status: "active",
              created_by_user_id: "admin-1",
              created_at: "2026-01-01T00:00:00Z",
            },
            code: "0123456789abcdef0123456789abcdef0123456789abcdef",
          },
        ],
      }),
    );

    const result = await issueAdminBillingRedemptionCodes({ amount: "5.00", quantity: 1 });

    expect(result[0].code).toBe("0123456789abcdef0123456789abcdef0123456789abcdef");
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/admin/billing/redemption-codes");
    expect(fetchMock.mock.calls[0][1]?.body).toBe(JSON.stringify({ amount: "5.00", quantity: 1 }));
  });

  it("disables an active code from the admin billing API", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      Response.json({
        redemption_code: {
          id: "code-1",
          code_hint: "***abcdef",
          currency: "USD",
          amount_nanos: 5_000_000_000,
          amount: "5.00",
          status: "disabled",
          created_by_user_id: "admin-1",
          disabled_by_user_id: "admin-1",
          disabled_at: "2026-01-01T00:01:00Z",
          created_at: "2026-01-01T00:00:00Z",
        },
      }),
    );

    const result = await disableAdminBillingRedemptionCode("code-1");

    expect(result.status).toBe("disabled");
    expect(fetchMock.mock.calls[0][0]).toBe(
      "/api/v1/admin/billing/redemption-codes/code-1/disable",
    );
    expect(fetchMock.mock.calls[0][1]?.method).toBe("POST");
  });
});

describe("tool pricing", () => {
  it("updates the complete tool pricing plan", async () => {
    const payload = [
      {
        tool_key: "sandbox.create" as const,
        price_per_call_nanos: 250_000_000,
        enabled: true,
        version: 1,
      },
      {
        tool_key: "image_generation" as const,
        price_per_call_nanos: 500_000_000,
        enabled: true,
        version: 1,
      },
      {
        tool_key: "tavily.search" as const,
        price_per_call_nanos: 5_000_000,
        enabled: true,
        version: 1,
      },
      {
        tool_key: "tavily.extract" as const,
        price_per_call_nanos: 10_000_000,
        enabled: false,
        version: 1,
      },
    ];
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      Response.json({
        tool_prices: payload.map((item, index) => ({
          ...item,
          currency: "USD",
          price_per_call: ["0.25", "0.50", "0.005", "0.01"][index],
          version: item.version + 1,
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        })),
      }),
    );

    const result = await updateAdminBillingToolPrices(payload);

    expect(result).toHaveLength(4);
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/admin/billing/tool-prices");
    expect(fetchMock.mock.calls[0][1]?.method).toBe("PUT");
    expect(fetchMock.mock.calls[0][1]?.body).toBe(JSON.stringify({ tool_prices: payload }));
  });
});
