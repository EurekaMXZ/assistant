import { z } from "zod";

const dateTime = z.string().min(1);
const metadata = z.record(z.unknown());

export const reasoningEffortSchema = z.enum(["low", "medium", "high", "xhigh"]);
export const turnStatusSchema = z.enum([
  "accepted",
  "context_ready",
  "processing",
  "completed",
  "failed",
]);

export const userSchema = z.object({
  id: z.string(),
  email: z.string(),
  username: z.string(),
  role: z.enum(["system", "admin", "user"]),
  status: z.enum(["active", "disabled"]),
  email_verified_at: dateTime.optional(),
  last_login_at: dateTime.optional(),
  created_at: dateTime,
  updated_at: dateTime,
});

export const sessionSchema = z.object({
  access_token: z.string().min(1),
  token_type: z.string(),
  expires_at: dateTime,
  user: userSchema,
});

export const registrationResultSchema = z.object({
  verification_required: z.boolean(),
  email_sent: z.boolean(),
});

export const conversationSchema = z.object({
  id: z.string(),
  owner_user_id: z.string().optional(),
  title: z.string().optional(),
  status: z.string(),
  metadata,
  created_at: dateTime,
  updated_at: dateTime,
  archived_at: dateTime.optional(),
});

export const conversationShareSchema = z.object({
  id: z.string(),
  conversation_id: z.string(),
  created_by_user_id: z.string(),
  title: z.string().optional(),
  last_message_seq: z.number().int().nonnegative(),
  created_at: dateTime,
});

export const attachmentSchema = z.object({
  id: z.string(),
  conversation_id: z.string(),
  uploaded_by_user_id: z.string(),
  filename: z.string(),
  content_type: z.string(),
  category: z.string(),
  size_bytes: z.number().int().nonnegative(),
  sha256: z.string(),
  object_key: z.string().optional(),
  metadata: metadata.optional(),
  created_at: dateTime,
  updated_at: dateTime,
});

export const messageSchema = z.object({
  id: z.string(),
  conversation_id: z.string(),
  turn_id: z.string().optional(),
  seq: z.number().int(),
  role: z.enum(["system", "developer", "user", "assistant", "tool"]),
  content_text: z.string().optional(),
  token_count: z.number().int().optional(),
  metadata,
  created_at: dateTime,
});

export const turnSchema = z.object({
  id: z.string(),
  conversation_id: z.string(),
  seq: z.number().int(),
  retry_of_turn_id: z.string().optional(),
  variant_index: z.number().int().positive().default(1),
  status: turnStatusSchema,
  request_blob_key: z.string().optional(),
  response_blob_key: z.string().optional(),
  stream_blob_key: z.string().optional(),
  openai_response_id: z.string().optional(),
  error_code: z.string().optional(),
  error_message: z.string().optional(),
  metadata,
  started_at: dateTime.optional(),
  completed_at: dateTime.optional(),
  failed_at: dateTime.optional(),
  created_at: dateTime,
  updated_at: dateTime,
});

export const modelSchema = z.object({
  id: z.string(),
  provider: z.string(),
  credential_id: z.string().optional(),
  slug: z.string(),
  upstream_model: z.string(),
  display_name: z.string(),
  description: z.string(),
  input_modalities: z.array(z.string()),
  output_modalities: z.array(z.string()),
  supports_tools: z.boolean(),
  supports_parallel_tools: z.boolean(),
  supported_reasoning_efforts: z.array(reasoningEffortSchema),
  context_window_tokens: z.number().int().positive(),
  max_output_tokens: z.number().int().positive(),
  default_parameters: metadata,
  status: z.enum(["enabled", "disabled"]),
  is_default: z.boolean().optional(),
  revision: z.number().int(),
});

export const modelSettingsSchema = z.object({
  default_chat_model_id: z.string().optional(),
  compaction_model_id: z.string().optional(),
  updated_at: dateTime,
});

export const modelPriceVersionSchema = z.object({
  id: z.string(),
  model_id: z.string(),
  version: z.number().int(),
  currency: z.string(),
  input_per_million_nanos: z.number().int().nonnegative(),
  cache_read_input_per_million_nanos: z.number().int().nonnegative(),
  cache_creation_input_per_million_nanos: z.number().int().nonnegative(),
  output_per_million_nanos: z.number().int().nonnegative(),
  image_input_per_million_nanos: z.number().int().nonnegative().optional(),
  image_output_per_image_nanos: z.number().int().nonnegative().optional(),
  status: z.enum(["draft", "published", "archived"]),
  effective_from: dateTime.optional(),
  created_at: dateTime,
});

export const providerCredentialSchema = z.object({
  id: z.string(),
  provider: z.string(),
  name: z.string(),
  base_url: z.string(),
  masked_key: z.string(),
  status: z.enum(["enabled", "disabled", "revoked"]),
  last_validated_at: dateTime.optional(),
  last_validation_error: z.string().optional(),
  created_at: dateTime,
  updated_at: dateTime,
});

export const auditEventSchema = z.object({
  id: z.string(),
  actor_user_id: z.string().optional(),
  actor_role: z.string().optional(),
  subject_user_id: z.string().optional(),
  action: z.string(),
  resource_type: z.string().optional(),
  resource_id: z.string().optional(),
  outcome: z.string(),
  request_id: z.string().optional(),
  client_ip: z.string().optional(),
  reason: z.string().optional(),
  visible_to_subject: z.boolean(),
  metadata,
  created_at: dateTime,
});

export const billingAccountSchema = z.object({
  id: z.string(),
  user_id: z.string(),
  currency: z.string(),
  status: z.enum(["active", "frozen"]),
  balance_nanos: z.number().int(),
  balance: z.string(),
  version: z.number().int(),
  created_at: dateTime,
  updated_at: dateTime,
});

export const billingTransactionSchema = z.object({
  id: z.string(),
  account_id: z.string(),
  user_id: z.string(),
  currency: z.string(),
  account_sequence: z.number().int(),
  kind: z.enum(["manual_topup", "manual_refund", "model_usage_charge", "redemption_credit"]),
  direction: z.enum(["credit", "debit"]),
  amount_nanos: z.number().int(),
  amount: z.string(),
  balance_after_nanos: z.number().int(),
  balance_after: z.string(),
  actor_user_id: z.string().optional(),
  reason: z.string(),
  reference: z.string(),
  metadata: metadata.optional(),
  created_at: dateTime,
});

export const billingRedemptionCodeSchema = z.object({
  id: z.string(),
  code_hint: z.string(),
  currency: z.string(),
  amount_nanos: z.number().int(),
  amount: z.string(),
  status: z.enum(["active", "disabled", "expired", "redeemed"]),
  created_by_user_id: z.string(),
  redeemed_by_user_id: z.string().optional(),
  billing_transaction_id: z.string().optional(),
  disabled_by_user_id: z.string().optional(),
  expires_at: dateTime.optional(),
  redeemed_at: dateTime.optional(),
  disabled_at: dateTime.optional(),
  created_at: dateTime,
});

export const billingToolPriceSchema = z.object({
  tool_key: z.enum(["sandbox.create", "image_generation", "tavily.search", "tavily.extract"]),
  currency: z.string(),
  price_per_call_nanos: z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER),
  price_per_call: z.string(),
  enabled: z.boolean(),
  version: z.number().int().positive(),
  updated_by_user_id: z.string().optional(),
  created_at: dateTime,
  updated_at: dateTime,
});

export const billingUsageEventSchema = z.object({
  id: z.string(),
  request_key: z.string(),
  owner_user_id: z.string().optional(),
  conversation_id: z.string().optional(),
  turn_id: z.string().optional(),
  turn_run_id: z.string().optional(),
  workflow: z.enum(["turn", "compaction"]),
  attempt: z.number().int().optional(),
  provider: z.string(),
  model_id: z.string().optional(),
  model_revision: z.number().int().optional(),
  model_price_id: z.string().optional(),
  upstream_model: z.string(),
  provider_response_id: z.string().optional(),
  status: z.enum(["completed", "failed"]),
  currency: z.string().optional(),
  amount_nanos: z.number().int().nullable().optional(),
  input_tokens: z.number().int(),
  cache_read_input_tokens: z.number().int(),
  cache_creation_input_tokens: z.number().int(),
  output_tokens: z.number().int(),
  reasoning_output_tokens: z.number().int(),
  total_tokens: z.number().int(),
  tool_amount_nanos: z.number().int().nonnegative(),
  tool_amount: z.string(),
  tool_usage: z.record(z.number().int().nonnegative()),
  tool_pricing_snapshot: metadata,
  billing_transaction_id: z.string().optional(),
  error_code: z.string().optional(),
  created_at: dateTime,
});

export const cursorPageSchema = <T extends z.ZodTypeAny>(item: T) =>
  z.object({
    data: z.array(item),
    page: z.object({
      next_cursor: z.string().optional(),
      has_more: z.boolean(),
    }),
  });

export const initialTurnResultSchema = z.object({
  conversation_id: z.string(),
  conversation: conversationSchema.optional(),
  attachments: z.array(attachmentSchema).optional(),
  message: messageSchema,
  turn: turnSchema,
  stream_path: z.string(),
});

export const preparedInitialTurnSchema = z.object({
  state: z.literal("draft"),
  replayed: z.boolean(),
  conversation: conversationSchema,
});

export const committedInitialTurnSchema = z.object({
  state: z.literal("committed"),
  replayed: z.boolean(),
  conversation: conversationSchema,
  message: messageSchema,
  turn: turnSchema,
  stream_path: z.string(),
});

export const timelineItemSchema = z.object({
  id: z.string(),
  type: z.string(),
  title: z.string().optional(),
  status: z.string().optional(),
  content_text: z.string().optional(),
  summary: z.string().optional(),
  details: z.array(z.string()).optional(),
  input_label: z.string().optional(),
  input_text: z.string().optional(),
  links: z.array(z.object({ url: z.string(), label: z.string() })).optional(),
  command: z.string().optional(),
  working_directory: z.string().optional(),
  command_output: z.string().optional(),
  exit_code: z.number().int().optional(),
  timed_out: z.boolean().optional(),
  metadata: metadata.optional(),
  created_at: dateTime,
});

export const turnStreamEventSchemas = {
  "turn.snapshot": z.object({
    turn_id: z.string(),
    conversation_id: z.string(),
    status: turnStatusSchema,
    items: z.array(timelineItemSchema),
    started_at: dateTime.optional(),
    completed_at: dateTime.optional(),
    failed_at: dateTime.optional(),
  }),
  "item.upsert": timelineItemSchema,
  "item.delta": z.object({
    item_id: z.string(),
    item_type: z.string(),
    delta: z.string(),
    sequence_number: z.number().int().optional(),
    created_at: dateTime,
  }),
  "item.done": timelineItemSchema,
  "turn.done": z.object({
    turn_id: z.string(),
    conversation_id: z.string().optional(),
    status: turnStatusSchema,
    error_code: z.string().optional(),
    error: z.string().optional(),
  }),
  "conversation.updated": z.object({
    conversation_id: z.string(),
    title: z.string().nullable().optional(),
  }),
} as const;

export type KnownTurnStreamEvent = keyof typeof turnStreamEventSchemas;

export function parseTurnStreamFrame(event: string, data: unknown) {
  const schema = turnStreamEventSchemas[event as KnownTurnStreamEvent];
  if (!schema) return null;
  const parsed = schema.safeParse(data);
  return parsed.success ? { event, data: parsed.data } : null;
}
