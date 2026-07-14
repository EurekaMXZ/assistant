export type UserRole = "system" | "admin" | "user";
type UserStatus = "active" | "disabled";

export interface User {
  id: string;
  email: string;
  username: string;
  role: UserRole;
  status: UserStatus;
  email_verified_at?: string;
  last_login_at?: string;
  created_at: string;
  updated_at: string;
}

export interface Session {
  access_token: string;
  token_type: string;
  expires_at: string;
  user: User;
}

export interface RegistrationResult {
  verification_required: boolean;
  email_sent: boolean;
}

export interface MailSettings {
  enabled: boolean;
  host: string;
  port: number;
  security: string;
  username: string;
  from_email: string;
  from_name: string;
  password_configured: boolean;
  updated_at: string;
}

type BillingAccountStatus = "active" | "frozen";

export interface BillingAccount {
  id: string;
  user_id: string;
  currency: string;
  status: BillingAccountStatus;
  balance_nanos: number;
  balance: string;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface BillingTransaction {
  id: string;
  account_id: string;
  user_id: string;
  currency: string;
  account_sequence: number;
  kind: "manual_topup" | "manual_refund" | "model_usage_charge" | "redemption_credit";
  direction: "credit" | "debit";
  amount_nanos: number;
  amount: string;
  balance_after_nanos: number;
  balance_after: string;
  actor_user_id?: string;
  reason: string;
  reference: string;
  metadata?: Record<string, unknown>;
  created_at: string;
}

export interface BillingRedemptionCode {
  id: string;
  code_hint: string;
  currency: string;
  amount_nanos: number;
  amount: string;
  status: "active" | "disabled" | "expired" | "redeemed";
  created_by_user_id: string;
  redeemed_by_user_id?: string;
  billing_transaction_id?: string;
  disabled_by_user_id?: string;
  expires_at?: string;
  redeemed_at?: string;
  disabled_at?: string;
  created_at: string;
}

export interface BillingRedemptionResult {
  account: BillingAccount;
  transaction: BillingTransaction;
  replayed: boolean;
}

export interface BillingRedemptionCodeIssue {
  redemption_code: BillingRedemptionCode;
  code: string;
}

export type BillingToolKey =
  "sandbox.create" | "image_generation" | "tavily.search" | "tavily.extract";

export interface BillingToolPrice {
  tool_key: BillingToolKey;
  currency: string;
  price_per_call_nanos: number;
  price_per_call: string;
  enabled: boolean;
  version: number;
  updated_by_user_id?: string;
  created_at: string;
  updated_at: string;
}

export interface BillingUsageEvent {
  id: string;
  request_key: string;
  workflow: "turn" | "compaction";
  owner_user_id?: string;
  provider: string;
  model_id?: string;
  upstream_model: string;
  status: "completed" | "failed";
  currency?: string;
  amount_nanos?: number | null;
  input_tokens: number;
  cache_read_input_tokens: number;
  cache_creation_input_tokens: number;
  output_tokens: number;
  reasoning_output_tokens: number;
  total_tokens: number;
  tool_amount_nanos: number;
  tool_amount: string;
  tool_usage: Partial<Record<BillingToolKey, number>>;
  tool_pricing_snapshot: Record<string, unknown>;
  billing_transaction_id?: string;
  error_code?: string;
  created_at: string;
}

export interface CursorPage {
  next_cursor?: string;
  has_more: boolean;
}

export interface CursorPageResponse<T> {
  data: T[];
  page: CursorPage;
}

export type ReasoningEffort = "low" | "medium" | "high" | "xhigh";

export interface Model {
  id: string;
  provider: string;
  credential_id?: string;
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
  status: "enabled" | "disabled";
  is_default?: boolean;
  revision: number;
}

export interface ProviderCredential {
  id: string;
  provider: string;
  name: string;
  base_url: string;
  masked_key: string;
  status: "enabled" | "disabled" | "revoked";
  last_validated_at?: string;
  last_validation_error?: string;
  created_at: string;
  updated_at: string;
}

export interface ModelSettings {
  default_chat_model_id?: string;
  compaction_model_id?: string;
  updated_at: string;
}

export interface ModelPriceVersion {
  id: string;
  model_id: string;
  version: number;
  currency: string;
  input_per_million_nanos: number;
  cache_read_input_per_million_nanos: number;
  cache_creation_input_per_million_nanos: number;
  output_per_million_nanos: number;
  image_input_per_million_nanos?: number;
  image_output_per_image_nanos?: number;
  status: "draft" | "published" | "archived";
  effective_from?: string;
  created_at: string;
}

export interface AuditEvent {
  id: string;
  actor_user_id?: string;
  actor_role?: string;
  subject_user_id?: string;
  action: string;
  resource_type?: string;
  resource_id?: string;
  outcome: string;
  request_id?: string;
  client_ip?: string;
  reason?: string;
  visible_to_subject: boolean;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface AdminOverview {
  users: number;
  enabled_models?: number;
  credentials?: number;
  active_accounts: number;
  audit_events: number;
  audit: AuditEvent[];
}

export interface Conversation {
  id: string;
  owner_user_id?: string;
  title?: string;
  status: string;
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  archived_at?: string;
}

export interface Attachment {
  id: string;
  conversation_id: string;
  uploaded_by_user_id: string;
  filename: string;
  content_type: string;
  category: "image" | "text" | "document" | "binary" | string;
  size_bytes: number;
  sha256: string;
  object_key?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

type MessageRole = "system" | "developer" | "user" | "assistant" | "tool";

export interface Message {
  id: string;
  conversation_id: string;
  turn_id?: string;
  seq: number;
  role: MessageRole;
  content_text?: string;
  token_count?: number;
  metadata: Record<string, unknown>;
  created_at: string;
}

export type TurnStatus = "accepted" | "context_ready" | "processing" | "completed" | "failed";

export interface Turn {
  id: string;
  conversation_id: string;
  seq: number;
  status: TurnStatus;
  request_blob_key?: string;
  response_blob_key?: string;
  stream_blob_key?: string;
  openai_response_id?: string;
  error_code?: string;
  error_message?: string;
  metadata: Record<string, unknown>;
  started_at?: string;
  completed_at?: string;
  failed_at?: string;
  created_at: string;
  updated_at: string;
}

export interface TimelineItem {
  id: string;
  type: string;
  title?: string;
  status?: string;
  content_text?: string;
  summary?: string;
  details?: string[];
  input_label?: string;
  input_text?: string;
  links?: TimelineLink[];
  command?: string;
  working_directory?: string;
  command_output?: string;
  exit_code?: number;
  timed_out?: boolean;
  metadata?: Record<string, unknown>;
  created_at: string;
}

interface TimelineLink {
  url: string;
  label: string;
}

export interface Timeline {
  turn_id: string;
  conversation_id: string;
  status: TurnStatus;
  items: TimelineItem[];
}

export interface TurnStreamSnapshot {
  turn_id: string;
  conversation_id: string;
  status: TurnStatus;
  items: TimelineItem[];
  started_at?: string;
  completed_at?: string;
  failed_at?: string;
}

export interface TurnStreamItemDelta {
  item_id: string;
  item_type: string;
  delta: string;
  sequence_number?: number;
  created_at: string;
}

export interface TurnStreamDone {
  turn_id: string;
  conversation_id?: string;
  status: TurnStatus;
  error_code?: string;
  error?: string;
}

export interface SseFrame<T = unknown> {
  event: string;
  data: T;
}
