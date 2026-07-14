import type {
  Attachment,
  AdminOverview,
  BillingAccount,
  BillingRedemptionCode,
  BillingRedemptionCodeIssue,
  BillingRedemptionResult,
  BillingTransaction,
  BillingToolPrice,
  BillingUsageEvent,
  Conversation,
  ConversationShareResult,
  CursorPageResponse,
  Message,
  Model,
  ModelPriceVersion,
  ModelSettings,
  MailSettings,
  ProviderCredential,
  ReasoningEffort,
  RegistrationResult,
  Session,
  Turn,
  User,
} from "./types";
import { z } from "zod";
import {
  attachmentSchema,
  auditEventSchema,
  billingAccountSchema,
  billingRedemptionCodeSchema,
  billingTransactionSchema,
  billingToolPriceSchema,
  billingUsageEventSchema,
  committedInitialTurnSchema,
  conversationShareSchema,
  conversationSchema,
  cursorPageSchema,
  initialTurnResultSchema,
  messageSchema,
  modelPriceVersionSchema,
  modelSchema,
  modelSettingsSchema,
  preparedInitialTurnSchema,
  providerCredentialSchema,
  registrationResultSchema,
  sessionSchema,
  turnSchema,
  userSchema,
} from "./api-schemas";
import { normalizeTurnRequest, requestMetadata, type TurnRequestDescriptor } from "./turn-request";
import { openAuthDialog } from "./auth-dialog-events";
import { emitAuthStateChange } from "./auth-state-events";

const API_BASE = (process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:8080/api/v1").replace(
  /\/$/,
  "",
);

const TOKEN_KEY = "assistant_access_token";

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(TOKEN_KEY);
}

function setToken(token: string) {
  if (typeof window !== "undefined") {
    localStorage.setItem(TOKEN_KEY, token);
  }
}

export function clearToken() {
  if (typeof window !== "undefined") {
    localStorage.removeItem(TOKEN_KEY);
  }
}

function buildUrl(path: string): string {
  if (path.startsWith("http")) return path;
  const base = API_BASE.replace(/\/$/, "");
  const cleanPath = path.startsWith("/") ? path : `/${path}`;
  return `${base}${cleanPath}`;
}

function stripApiBasePath(path: string): string {
  if (path.startsWith("http")) return path;

  const base = API_BASE.replace(/\/$/, "");
  const cleanPath = path.startsWith("/") ? path : `/${path}`;
  if (!base) return cleanPath;

  try {
    const basePath = new URL(base, window.location.origin).pathname.replace(/\/$/, "");
    if (!basePath || basePath === "/") return cleanPath;
    if (cleanPath === basePath) return "/";
    if (cleanPath.startsWith(`${basePath}/`)) {
      return cleanPath.slice(basePath.length);
    }
  } catch {
    // Fall through to the original path.
  }

  return cleanPath;
}

export class ApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
    this.name = "ApiError";
  }
}

export class ApiResponseValidationError extends Error {
  constructor(path: string, issues: z.ZodIssue[]) {
    super(
      `Invalid API response from ${path}: ${issues.map((issue) => issue.path.join(".") || issue.message).join(", ")}`,
    );
    this.name = "ApiResponseValidationError";
  }
}

function isUnauthorizedError(error: unknown): error is ApiError {
  return error instanceof ApiError && error.status === 401;
}

export function isEmailVerificationRequiredError(error: unknown): error is ApiError {
  return (
    error instanceof ApiError &&
    error.status === 403 &&
    error.message.trim().toLowerCase() === "email verification required"
  );
}

function isSessionUnauthorizedMessage(message: string) {
  const normalized = message.trim().toLowerCase();
  return (
    normalized === "authentication required" ||
    normalized === "invalid access token" ||
    normalized === "missing bearer token" ||
    normalized === "invalid authorization header" ||
    normalized === "unauthorized"
  );
}

export function isSessionUnauthorizedError(error: unknown): error is ApiError {
  return isUnauthorizedError(error) && isSessionUnauthorizedMessage(error.message);
}

export function handleSessionUnauthorized(status: number, message: string, enabled = true) {
  if (
    status !== 401 ||
    !enabled ||
    !isSessionUnauthorizedMessage(message) ||
    typeof window === "undefined"
  ) {
    return false;
  }

  clearToken();
  emitAuthStateChange({ reason: "unauthorized" });
  openAuthDialog("login");
  return true;
}

interface ApiFetchOptions extends RequestInit {
  handleUnauthorized?: boolean;
}

async function apiFetch<T>(
  path: string,
  init: ApiFetchOptions = {},
  schema?: z.ZodType<T>,
): Promise<T> {
  const { handleUnauthorized = true, ...requestInit } = init;
  const token = getToken();
  const headers: Record<string, string> = {
    Accept: "application/json",
    ...(requestInit.headers as Record<string, string>),
  };

  if (!(requestInit.body instanceof FormData)) {
    headers["Content-Type"] = "application/json";
  }

  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const res = await fetch(buildUrl(path), {
    ...requestInit,
    headers,
  });

  if (!res.ok) {
    let message = `Request failed (${res.status})`;
    try {
      const data = await res.json();
      if (data && typeof data.error === "string") {
        message = data.error;
      }
    } catch {
      // ignore
    }
    handleSessionUnauthorized(res.status, message, handleUnauthorized);
    throw new ApiError(message, res.status);
  }

  if (res.status === 204) {
    return undefined as T;
  }

  const data: unknown = await res.json();
  if (!schema) return data as T;
  const parsed = schema.safeParse(data);
  if (!parsed.success) throw new ApiResponseValidationError(path, parsed.error.issues);
  return parsed.data;
}

async function listCursorPage<T>(path: string, itemSchema: z.ZodType<T>, cursor?: string) {
  const separator = path.includes("?") ? "&" : "?";
  return apiFetch<CursorPageResponse<T>>(
    `${path}${cursor ? `${separator}cursor=${encodeURIComponent(cursor)}` : ""}`,
    {},
    cursorPageSchema(itemSchema),
  );
}

async function listAllCursorItems<T>(path: string, itemSchema: z.ZodType<T>) {
  const items: T[] = [];
  let cursor = "";
  for (let pageNumber = 0; pageNumber < 50; pageNumber += 1) {
    const page = await listCursorPage(path, itemSchema, cursor);
    items.push(...page.data);
    if (!page.page.has_more || !page.page.next_cursor) return items;
    cursor = page.page.next_cursor;
  }
  throw new Error(`Cursor pagination exceeded the safety limit for ${path}`);
}

// Auth
export async function register(email: string, username: string, password: string) {
  return apiFetch<RegistrationResult>(
    "/auth/register",
    {
      method: "POST",
      handleUnauthorized: false,
      body: JSON.stringify({ email, username, password }),
    },
    registrationResultSchema,
  );
}

export async function login(email: string, password: string) {
  const { session } = await apiFetch<{ session: Session }>(
    "/auth/login",
    {
      method: "POST",
      handleUnauthorized: false,
      body: JSON.stringify({ email, password }),
    },
    z.object({ session: sessionSchema }),
  );
  setToken(session.access_token);
  return session;
}

export async function me() {
  return apiFetch<{ user: User }>("/auth/me", {}, z.object({ user: userSchema })).then(
    (r) => r.user,
  );
}

export async function changePassword(currentPassword: string, newPassword: string) {
  return apiFetch<{ user: User }>(
    "/auth/change-password",
    {
      method: "POST",
      body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    },
    z.object({ user: userSchema }),
  ).then((r) => r.user);
}

export async function verifyEmail(token: string) {
  return apiFetch(
    "/auth/verify-email",
    {
      method: "POST",
      handleUnauthorized: false,
      body: JSON.stringify({ token }),
    },
    z.object({ verified: z.boolean() }),
  ).then(() => undefined);
}

export async function resendVerification(email: string) {
  return apiFetch(
    "/auth/resend-verification",
    {
      method: "POST",
      handleUnauthorized: false,
      body: JSON.stringify({ email }),
    },
    z.object({ message: z.string() }),
  ).then(() => undefined);
}

export async function forgotPassword(email: string) {
  return apiFetch(
    "/auth/forgot-password",
    {
      method: "POST",
      handleUnauthorized: false,
      body: JSON.stringify({ email }),
    },
    z.object({ message: z.string() }),
  ).then(() => undefined);
}

export async function resetPassword(token: string, newPassword: string) {
  return apiFetch(
    "/auth/reset-password",
    {
      method: "POST",
      handleUnauthorized: false,
      body: JSON.stringify({ token, new_password: newPassword }),
    },
    z.object({ password_reset: z.boolean() }),
  ).then(() => undefined);
}

// Billing
export async function getBillingAccount() {
  return apiFetch<{ account: BillingAccount }>(
    "/billing/account",
    {},
    z.object({ account: billingAccountSchema }),
  ).then((r) => r.account);
}

export async function redeemBillingCode(code: string) {
  return apiFetch<BillingRedemptionResult>(
    "/billing/redemptions",
    { method: "POST", body: JSON.stringify({ code }) },
    z.object({
      account: billingAccountSchema,
      transaction: billingTransactionSchema,
      replayed: z.boolean(),
    }),
  );
}

export async function listBillingTransactions(cursor?: string) {
  const params = new URLSearchParams({ limit: "20" });
  if (cursor) params.set("cursor", cursor);
  return apiFetch<CursorPageResponse<BillingTransaction>>(
    `/billing/transactions?${params.toString()}`,
    {},
    cursorPageSchema(billingTransactionSchema),
  );
}

export async function listBillingUsageEvents(cursor?: string) {
  const params = new URLSearchParams({ limit: "20" });
  if (cursor) params.set("cursor", cursor);
  return apiFetch<CursorPageResponse<BillingUsageEvent>>(
    `/billing/usage-events?${params.toString()}`,
    {},
    cursorPageSchema(billingUsageEventSchema),
  );
}

export async function listModels() {
  return listAllCursorItems("/models?limit=200", modelSchema);
}

// Admin
export async function listAdminUsersPage(cursor?: string) {
  return listCursorPage("/users?limit=50", userSchema, cursor);
}

export async function getAdminOverview() {
  return apiFetch<AdminOverview>(
    "/admin/overview",
    {},
    z.object({
      users: z.number(),
      enabled_models: z.number().optional(),
      credentials: z.number().optional(),
      active_accounts: z.number(),
      audit_events: z.number(),
      audit: z.array(auditEventSchema),
    }),
  );
}

export async function createAdminUser(payload: {
  email: string;
  username: string;
  password: string;
  role: User["role"];
  status: User["status"];
}) {
  return apiFetch<{ user: User }>("/users", { method: "POST", body: JSON.stringify(payload) }).then(
    (result) => result.user,
  );
}

export async function updateAdminUser(
  userId: string,
  payload: Partial<Pick<User, "email" | "username" | "role" | "status">>,
) {
  return apiFetch<{ user: User }>(`/users/${userId}`, {
    method: "PATCH",
    body: JSON.stringify(payload),
  }).then((result) => result.user);
}

export async function resetAdminUserPassword(userId: string, newPassword: string) {
  return apiFetch<{ user: User }>(`/users/${userId}/reset-password`, {
    method: "POST",
    body: JSON.stringify({ new_password: newPassword }),
  }).then((result) => result.user);
}

export async function listAdminModels() {
  return listAllCursorItems("/admin/models?limit=200", modelSchema);
}

export async function listAdminModelsPage(cursor?: string) {
  return listCursorPage("/admin/models?limit=50", modelSchema, cursor);
}

export type AdminModelCreatePayload = {
  provider: string;
  credential_id: string;
  slug: string;
  upstream_model: string;
  display_name: string;
  description: string;
  input_modalities: string[];
  output_modalities: string[];
  supports_tools: boolean;
  supports_parallel_tools: boolean;
  supported_reasoning_efforts: ReasoningEffort[];
  context_window_tokens: number;
  max_output_tokens: number;
  default_parameters: Record<string, unknown>;
};

export type AdminModelUpdatePayload = Partial<
  Omit<AdminModelCreatePayload, "provider" | "slug" | "upstream_model">
>;

export async function createAdminModel(payload: AdminModelCreatePayload) {
  return apiFetch<{ model: Model }>(
    "/admin/models",
    { method: "POST", body: JSON.stringify(payload) },
    z.object({ model: modelSchema }),
  ).then((result) => result.model);
}

export async function updateAdminModel(modelId: string, payload: AdminModelUpdatePayload) {
  return apiFetch<{ model: Model }>(
    `/admin/models/${modelId}`,
    { method: "PATCH", body: JSON.stringify(payload) },
    z.object({ model: modelSchema }),
  ).then((result) => result.model);
}

export async function setAdminModelEnabled(modelId: string, enabled: boolean) {
  return apiFetch<{ model: Model }>(
    `/admin/models/${modelId}/${enabled ? "enable" : "disable"}`,
    { method: "POST" },
    z.object({ model: modelSchema }),
  ).then((result) => result.model);
}

export async function getAdminModelSettings() {
  return apiFetch<{ settings: ModelSettings }>(
    "/admin/model-settings",
    {},
    z.object({ settings: modelSettingsSchema }),
  ).then((result) => result.settings);
}

export async function updateAdminModelSettings(
  payload: Partial<Pick<ModelSettings, "default_chat_model_id" | "compaction_model_id">>,
) {
  return apiFetch<{ settings: ModelSettings }>(
    "/admin/model-settings",
    { method: "PATCH", body: JSON.stringify(payload) },
    z.object({ settings: modelSettingsSchema }),
  ).then((result) => result.settings);
}

export async function listAdminModelPrices(modelId: string, cursor?: string) {
  return listCursorPage(
    `/admin/models/${modelId}/prices?limit=50`,
    modelPriceVersionSchema,
    cursor,
  );
}

export async function createAdminModelPrice(
  modelId: string,
  payload: Omit<
    ModelPriceVersion,
    "id" | "model_id" | "version" | "status" | "effective_from" | "created_at"
  >,
) {
  return apiFetch<{ price: ModelPriceVersion }>(
    `/admin/models/${modelId}/prices`,
    { method: "POST", body: JSON.stringify(payload) },
    z.object({ price: modelPriceVersionSchema }),
  ).then((result) => result.price);
}

export async function setAdminModelPriceStatus(
  modelId: string,
  priceId: string,
  action: "publish" | "archive",
) {
  return apiFetch<{ price: ModelPriceVersion }>(
    `/admin/models/${modelId}/prices/${priceId}/${action}`,
    { method: "POST", body: action === "publish" ? "{}" : undefined },
    z.object({ price: modelPriceVersionSchema }),
  ).then((result) => result.price);
}

export async function listAdminCredentials() {
  return listAllCursorItems("/admin/provider-credentials?limit=200", providerCredentialSchema);
}

export async function listAdminCredentialsPage(cursor?: string) {
  return listCursorPage("/admin/provider-credentials?limit=50", providerCredentialSchema, cursor);
}

export async function createAdminCredential(payload: {
  provider: string;
  name: string;
  base_url: string;
  api_key: string;
}) {
  return apiFetch<{ credential: ProviderCredential }>("/admin/provider-credentials", {
    method: "POST",
    body: JSON.stringify(payload),
  }).then((result) => result.credential);
}

export async function updateAdminCredential(
  credentialId: string,
  payload: { name?: string; base_url?: string },
) {
  return apiFetch<{ credential: ProviderCredential }>(
    `/admin/provider-credentials/${credentialId}`,
    { method: "PATCH", body: JSON.stringify(payload) },
  ).then((result) => result.credential);
}

export async function rotateAdminCredential(credentialId: string, apiKey: string) {
  return apiFetch<{ credential: ProviderCredential }>(
    `/admin/provider-credentials/${credentialId}/rotate`,
    { method: "POST", body: JSON.stringify({ api_key: apiKey }) },
  ).then((result) => result.credential);
}

export async function runAdminCredentialAction(
  credentialId: string,
  action: "validate" | "enable" | "disable",
) {
  return apiFetch<{ credential: ProviderCredential }>(
    `/admin/provider-credentials/${credentialId}/${action}`,
    { method: "POST" },
  ).then((result) => result.credential);
}

export async function revokeAdminCredential(credentialId: string) {
  return apiFetch<{ credential: ProviderCredential }>(
    `/admin/provider-credentials/${credentialId}`,
    { method: "DELETE" },
  ).then((result) => result.credential);
}

export async function listAdminBillingAccountsPage(cursor?: string) {
  return listCursorPage("/admin/billing/accounts?limit=50", billingAccountSchema, cursor);
}

export async function listAdminBillingToolPrices() {
  return apiFetch<{ tool_prices: BillingToolPrice[] }>(
    "/admin/billing/tool-prices",
    {},
    z.object({ tool_prices: z.array(billingToolPriceSchema) }),
  ).then((result) => result.tool_prices);
}

export async function updateAdminBillingToolPrices(
  toolPrices: Pick<BillingToolPrice, "tool_key" | "price_per_call_nanos" | "enabled" | "version">[],
) {
  return apiFetch<{ tool_prices: BillingToolPrice[] }>(
    "/admin/billing/tool-prices",
    { method: "PUT", body: JSON.stringify({ tool_prices: toolPrices }) },
    z.object({ tool_prices: z.array(billingToolPriceSchema) }),
  ).then((result) => result.tool_prices);
}

export async function listAdminBillingRedemptionCodes(cursor?: string) {
  const params = new URLSearchParams({ limit: "50" });
  if (cursor) params.set("cursor", cursor);
  return apiFetch<CursorPageResponse<BillingRedemptionCode>>(
    `/admin/billing/redemption-codes?${params.toString()}`,
    {},
    cursorPageSchema(billingRedemptionCodeSchema),
  );
}

export async function issueAdminBillingRedemptionCodes(payload: {
  amount: string;
  quantity: number;
  expires_at?: string;
}) {
  return apiFetch<{ redemption_codes: BillingRedemptionCodeIssue[] }>(
    "/admin/billing/redemption-codes",
    { method: "POST", body: JSON.stringify(payload) },
    z.object({
      redemption_codes: z.array(
        z.object({ redemption_code: billingRedemptionCodeSchema, code: z.string() }),
      ),
    }),
  ).then((result) => result.redemption_codes);
}

export async function disableAdminBillingRedemptionCode(codeId: string) {
  return apiFetch<{ redemption_code: BillingRedemptionCode }>(
    `/admin/billing/redemption-codes/${codeId}/disable`,
    { method: "POST" },
    z.object({ redemption_code: billingRedemptionCodeSchema }),
  ).then((result) => result.redemption_code);
}

export async function updateAdminBillingAccount(
  userId: string,
  payload: Partial<Pick<BillingAccount, "status">>,
) {
  return apiFetch<{ account: BillingAccount }>(
    `/admin/billing/accounts/${userId}`,
    { method: "PATCH", body: JSON.stringify(payload) },
    z.object({ account: billingAccountSchema }),
  ).then((result) => result.account);
}

export async function applyAdminBillingAdjustment(
  userId: string,
  kind: "topups" | "refunds",
  payload: { amount: string; currency: string; reason: string; reference: string },
  idempotencyKey: string,
) {
  return apiFetch<{ transaction: BillingTransaction }>(
    `/admin/billing/accounts/${userId}/${kind}`,
    {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
      body: JSON.stringify(payload),
    },
    z.object({ transaction: billingTransactionSchema }),
  ).then((result) => result.transaction);
}

export async function listAdminBillingTransactionsPage(cursor?: string) {
  return listCursorPage("/admin/billing/transactions?limit=50", billingTransactionSchema, cursor);
}

export async function listAdminBillingUsageEventsPage(cursor?: string) {
  return listCursorPage("/admin/billing/usage-events?limit=50", billingUsageEventSchema, cursor);
}

export async function listAdminAuditEventsPage(cursor?: string) {
  return listCursorPage("/admin/audit-events?limit=50", auditEventSchema, cursor);
}

export async function getAdminMailSettings() {
  return apiFetch<{ settings: MailSettings }>("/admin/mail-settings").then(
    (result) => result.settings,
  );
}

export async function updateAdminMailSettings(
  payload: Pick<
    MailSettings,
    "enabled" | "host" | "port" | "security" | "username" | "from_email" | "from_name"
  > & { password?: string },
) {
  return apiFetch<{ settings: MailSettings }>("/admin/mail-settings", {
    method: "PATCH",
    body: JSON.stringify(payload),
  }).then((result) => result.settings);
}

export async function testAdminMailSettings(recipient: string) {
  return apiFetch<void>("/admin/mail-settings/test", {
    method: "POST",
    body: JSON.stringify({ recipient }),
  });
}

// Conversations
export async function listConversations(limit?: number, signal?: AbortSignal) {
  const qs = limit ? `?limit=${limit}` : "";
  return apiFetch<{ conversations: Conversation[] }>(
    `/conversations${qs}`,
    { signal },
    z.object({ conversations: z.array(conversationSchema) }),
  ).then((r) => r.conversations);
}

export async function createConversation(
  payload?: {
    title?: string;
    metadata?: Record<string, unknown>;
  },
  idempotencyKey?: string,
) {
  return apiFetch<{ conversation: Conversation }>(
    "/conversations",
    {
      method: "POST",
      headers: idempotencyKey ? { "Idempotency-Key": idempotencyKey } : undefined,
      body: JSON.stringify(payload || {}),
    },
    z.object({ conversation: conversationSchema }),
  ).then((r) => r.conversation);
}

export async function getConversation(id: string) {
  return apiFetch<{ conversation: Conversation }>(
    `/conversations/${id}`,
    {},
    z.object({ conversation: conversationSchema }),
  ).then((r) => r.conversation);
}

export async function patchConversation(
  id: string,
  payload: { title?: string; archived?: boolean },
) {
  return apiFetch<{ conversation: Conversation }>(`/conversations/${id}`, {
    method: "PATCH",
    body: JSON.stringify(payload),
  }).then((r) => r.conversation);
}

export async function createConversationShare(conversationId: string, idempotencyKey: string) {
  return apiFetch<ConversationShareResult>(
    `/conversations/${conversationId}/shares`,
    {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
    },
    z.object({ share: conversationShareSchema, replayed: z.boolean() }),
  );
}

// Attachments
export async function uploadConversationAttachment(
  conversationId: string,
  file: File,
  idempotencyKey?: string,
) {
  const body = new FormData();
  body.append("file", file);

  return apiFetch<{ attachment: Attachment }>(
    `/conversations/${conversationId}/attachments`,
    {
      method: "POST",
      headers: idempotencyKey ? { "Idempotency-Key": idempotencyKey } : undefined,
      body,
    },
    z.object({ attachment: attachmentSchema }),
  ).then((r) => r.attachment);
}

export async function getConversationAttachmentBlob(conversationId: string, attachmentId: string) {
  const token = getToken();
  const headers: Record<string, string> = {
    Accept: "*/*",
  };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const res = await fetch(
    buildUrl(`/conversations/${conversationId}/attachments/${attachmentId}`),
    { headers },
  );
  if (!res.ok) {
    let message = `Request failed (${res.status})`;
    try {
      const data = await res.json();
      if (data && typeof data.error === "string") {
        message = data.error;
      }
    } catch {
      // ignore
    }
    handleSessionUnauthorized(res.status, message);
    throw new ApiError(message, res.status);
  }

  return res.blob();
}

// Messages
export async function listMessages(conversationId: string) {
  return apiFetch<{ messages: Message[] }>(
    `/conversations/${conversationId}/messages`,
    {},
    z.object({ messages: z.array(messageSchema) }),
  ).then((r) => r.messages);
}

export interface InitialTurnResult {
  conversation_id: string;
  conversation?: Conversation;
  attachments?: Attachment[];
  message: Message;
  turn: Turn;
  stream_path: string;
}

export async function prepareInitialTurn(idempotencyKey: string) {
  return apiFetch(
    "/conversations/initial-turns",
    {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
      body: JSON.stringify({ action: "prepare", metadata: { source: "home" } }),
    },
    preparedInitialTurnSchema,
  );
}

export async function commitInitialTurn(
  idempotencyKey: string,
  conversationId: string,
  descriptor: TurnRequestDescriptor,
): Promise<InitialTurnResult> {
  const result = await apiFetch(
    "/conversations/initial-turns",
    {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
      body: JSON.stringify({
        action: "commit",
        conversation_id: conversationId,
        content: descriptor.content,
        attachment_ids: descriptor.attachment_ids,
        model_id: descriptor.model_id || "",
        reasoning_effort: descriptor.reasoning_effort || "",
        metadata: requestMetadata(descriptor),
      }),
    },
    committedInitialTurnSchema,
  );
  return {
    conversation_id: result.conversation.id,
    conversation: result.conversation,
    message: result.message,
    turn: result.turn,
    stream_path: result.stream_path,
  };
}

export async function createMessage(
  conversationId: string,
  input:
    | {
        content: string;
        metadata?: Record<string, unknown>;
        attachmentIds?: string[];
        modelId?: string;
        reasoningEffort?: ReasoningEffort;
      }
    | TurnRequestDescriptor,
  idempotencyKey?: string,
) {
  const descriptor = "attachment_ids" in input ? input : normalizeTurnRequest(input);
  return apiFetch<InitialTurnResult>(
    `/conversations/${conversationId}/messages`,
    {
      method: "POST",
      headers: idempotencyKey ? { "Idempotency-Key": idempotencyKey } : undefined,
      body: JSON.stringify({
        content: descriptor.content,
        attachment_ids: descriptor.attachment_ids,
        model_id: descriptor.model_id || "",
        reasoning_effort: descriptor.reasoning_effort || "",
        metadata: requestMetadata(descriptor),
      }),
    },
    initialTurnResultSchema,
  );
}

// Turns
export async function getTurn(id: string) {
  return apiFetch<{ turn: Turn }>(`/turns/${id}`, {}, z.object({ turn: turnSchema })).then(
    (r) => r.turn,
  );
}

export function getStreamUrl(streamPath: string): string {
  return buildUrl(stripApiBasePath(streamPath));
}

export { API_BASE };
