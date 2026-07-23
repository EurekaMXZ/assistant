import { z } from "zod";
import { parseSafeAskUserActionURL } from "./ask-user-action";

const dateTime = z.string().min(1);
const metadata = z.record(z.string(), z.unknown());
const textWithMaximumCharacters = (maximum: number) =>
  z.string().refine((value) => Array.from(value).length <= maximum, {
    message: `Must contain at most ${maximum} characters`,
  });

export const reasoningEffortSchema = z.enum(["low", "medium", "high", "xhigh"]);
export const turnStatusSchema = z.enum([
  "accepted",
  "context_ready",
  "processing",
  "awaiting_input",
  "cancel_requested",
  "completed",
  "failed",
  "cancelled",
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
  storage_quota_bytes: z
    .number()
    .int()
    .nonnegative()
    .default(512 * 1024 * 1024),
  storage_used_bytes: z.number().int().nonnegative().default(0),
  sandbox_quota: z.number().int().nonnegative().default(3),
  deleted_at: dateTime.optional(),
});

export const personalizationInputSchema = z.object({
  preferences_text: textWithMaximumCharacters(8000),
  location_enabled_for_model: z.boolean(),
});

export const personalizationUpdateInputSchema = personalizationInputSchema.extend({
  expected_version: z.number().int().nonnegative(),
});

export const userPersonalizationSchema = personalizationInputSchema.extend({
  user_id: z.string(),
  version: z.number().int().nonnegative(),
  created_at: dateTime.optional(),
  updated_at: dateTime.optional(),
});

export const userLocationInputSchema = z.object({
  latitude: z.number().finite().min(-90).max(90),
  longitude: z.number().finite().min(-180).max(180),
  coordinate_system: z.literal("gcj02"),
  formatted_address: textWithMaximumCharacters(500),
  province: textWithMaximumCharacters(100),
  city: textWithMaximumCharacters(100),
  district: textWithMaximumCharacters(100),
  adcode: z.union([z.literal(""), z.string().regex(/^\d{6}$/)]),
  poi_id: textWithMaximumCharacters(128),
  poi_name: textWithMaximumCharacters(200),
  source: z.enum(["map", "search", "geolocation"]),
});

export const userLocationSchema = userLocationInputSchema.extend({
  user_id: z.string(),
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
  deleted_at: dateTime.optional(),
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
  status: z.enum(["pending", "ready", "deleting"]),
  metadata: metadata.optional(),
  upload_completed_at: dateTime.optional(),
  created_at: dateTime,
  updated_at: dateTime,
});

export const presignedObjectUrlSchema = z.object({
  url: z.string().url(),
  method: z.string(),
  headers: z.record(z.string(), z.string()).optional(),
  expires_at: dateTime,
});

export const storageUsageSchema = z.object({
  quota_bytes: z.number().int().nonnegative(),
  used_bytes: z.number().int().nonnegative(),
  available_bytes: z.number().int().nonnegative(),
});

export const storageAttachmentSchema = attachmentSchema.extend({
  conversation_title: z.string().optional(),
});

const mcpServerNameSchema = z
  .string()
  .trim()
  .min(1)
  .refine((value) => Array.from(value).length <= 100);
const mcpServerSlugSchema = z
  .string()
  .trim()
  .min(1)
  .max(64)
  .regex(/^[a-z0-9]+(?:-[a-z0-9]+)*$/);
const mcpEndpointURLSchema = z
  .string()
  .trim()
  .min(1)
  .max(2048)
  .refine((value) => {
    try {
      const url = new URL(value);
      return (
        (url.protocol === "http:" || url.protocol === "https:") &&
        url.username === "" &&
        url.password === "" &&
        url.search === "" &&
        url.hash === ""
      );
    } catch {
      return false;
    }
  });
const mcpSecretNameSchema = z.string().trim().min(1).max(128);
const mcpSecretValueSchema = z.string().max(8192);

export const mcpSecretSchema = z.object({
  name: z.string(),
  configured: z.boolean(),
  key_hint: z.string().optional(),
});

export const userMCPToolSchema = z.object({
  name: z.string(),
  description: z.string(),
  input_schema: z.record(z.string(), z.unknown()),
  enabled: z.boolean(),
  created_at: dateTime,
  updated_at: dateTime,
});

export const userMCPServerSchema = z.object({
  id: z.string(),
  name: z.string(),
  slug: z.string(),
  endpoint_url: z.string(),
  enabled: z.boolean(),
  revision: z.number().int().positive(),
  parameters: z.array(mcpSecretSchema),
  headers: z.array(mcpSecretSchema),
  tools: z.array(userMCPToolSchema),
  last_validation_status: z.enum(["untested", "valid", "invalid"]),
  last_validation_error: z.string().optional(),
  last_validated_at: dateTime.optional(),
  created_at: dateTime,
  updated_at: dateTime,
});

export const createMCPSecretInputSchema = z.object({
  name: mcpSecretNameSchema,
  value: mcpSecretValueSchema,
});

export const updateMCPSecretInputSchema = z.object({
  name: mcpSecretNameSchema,
  value: mcpSecretValueSchema.nullable().optional(),
});

export const createMCPServerInputSchema = z.object({
  name: mcpServerNameSchema,
  slug: mcpServerSlugSchema,
  endpoint_url: mcpEndpointURLSchema,
  enabled: z.boolean().optional(),
  parameters: z.array(createMCPSecretInputSchema).max(32).optional(),
  headers: z.array(createMCPSecretInputSchema).max(32).optional(),
});

export const updateMCPServerInputSchema = z.object({
  name: mcpServerNameSchema.optional(),
  slug: mcpServerSlugSchema.optional(),
  endpoint_url: mcpEndpointURLSchema.optional(),
  enabled: z.boolean().optional(),
  parameters: z.array(updateMCPSecretInputSchema).max(32).optional(),
  headers: z.array(updateMCPSecretInputSchema).max(32).optional(),
  enabled_tools: z.array(z.string().min(1).max(255)).optional(),
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

export const conversationEventSchema = z.object({
  id: z.string(),
  conversation_id: z.string(),
  turn_id: z.string().optional(),
  turn_run_id: z.string().optional(),
  event_seq: z.string().regex(/^\d+$/),
  event_key: z.string(),
  schema_version: z.number().int().positive(),
  event_type: z.string(),
  payload: z.record(z.string(), z.unknown()),
  context_included: z.boolean(),
  created_at: dateTime,
});

export const conversationEventPageSchema = z.object({
  events: z.array(conversationEventSchema),
  next_before: z.string().optional(),
  next_after: z.string().optional(),
  has_more_before: z.boolean(),
  has_more_after: z.boolean(),
});

export const conversationShareSnapshotSchema = z.object({
  id: z.string(),
  title: z.string().optional(),
  last_message_seq: z.number().int().nonnegative(),
  created_at: dateTime,
  messages: z.array(messageSchema),
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
  deleted_at: dateTime.optional(),
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
  tool_usage: z.record(z.string(), z.number().int().nonnegative()),
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

export const askUserOptionSchema = z
  .object({
    id: z
      .string()
      .min(1)
      .max(64)
      .regex(/^[A-Za-z0-9_-]+$/),
    label: textWithMaximumCharacters(80).pipe(z.string().min(1)),
    tone: z.enum(["primary", "neutral", "danger"]),
  })
  .strict();

export const askUserActionSchema = z
  .object({
    label: textWithMaximumCharacters(80).pipe(z.string().min(1)),
    url: z
      .string()
      .min(1)
      .max(2048)
      .refine((value) => parseSafeAskUserActionURL(value) !== null),
  })
  .strict();

export const askUserAnswerSchema = z
  .object({
    status: z.enum(["answered", "cancelled"]),
    option_id: z.string().min(1).max(64),
    label: textWithMaximumCharacters(80).pipe(z.string().min(1)),
    user_reported: z.boolean(),
  })
  .strict();

export const askUserInteractionSchema = z
  .object({
    id: z.string().min(1),
    tool_call_id: z.string().min(1),
    prompt: textWithMaximumCharacters(500).pipe(z.string().min(1)),
    kind: z.enum(["single_choice", "external_action"]),
    options: z.array(askUserOptionSchema).min(2).max(6),
    action: askUserActionSchema.optional(),
    answer: askUserAnswerSchema.optional(),
    status: z.enum(["awaiting_input", "completed", "cancelled"]),
  })
  .strict()
  .superRefine((interaction, context) => {
    if (interaction.kind === "single_choice" && interaction.action) {
      context.addIssue({
        code: "custom",
        path: ["action"],
        message: "single_choice interaction must not include an action",
      });
    }
    if (interaction.kind === "external_action" && !interaction.action) {
      context.addIssue({
        code: "custom",
        path: ["action"],
        message: "external_action interaction requires an action",
      });
    }
    if (interaction.status === "awaiting_input" && interaction.answer) {
      context.addIssue({
        code: "custom",
        path: ["answer"],
        message: "awaiting_input interaction must not include an answer",
      });
    }
    if (
      (interaction.status === "completed" || interaction.status === "cancelled") &&
      !interaction.answer
    ) {
      context.addIssue({
        code: "custom",
        path: ["answer"],
        message: "completed interaction requires an answer",
      });
    }
  });

export const answerToolCallInputSchema = z
  .object({
    option_id: z
      .string()
      .min(1)
      .max(64)
      .regex(/^[A-Za-z0-9_-]+$/),
  })
  .strict();

export const answerToolCallResultSchema = z
  .object({
    interaction: askUserInteractionSchema,
    stream_path: z.string().min(1),
  })
  .strict();

export const timelineItemSchema = z
  .object({
    id: z.string(),
    type: z.string(),
    title: z.string().optional(),
    status: z.string().optional(),
    content_text: z.string().optional(),
    summary: z.string().optional(),
    details: z.array(z.string()).optional(),
    input_label: z.string().optional(),
    input_text: z.string().optional(),
    links: z.array(z.object({ url: z.string(), label: z.string() }).strict()).optional(),
    command: z.string().optional(),
    working_directory: z.string().optional(),
    command_output: z.string().optional(),
    exit_code: z.number().int().optional(),
    timed_out: z.boolean().optional(),
    tool_call_id: z.string().optional(),
    prompt: z.string().optional(),
    kind: z.enum(["single_choice", "external_action"]).optional(),
    options: z.array(askUserOptionSchema).optional(),
    action: askUserActionSchema.optional(),
    answer: askUserAnswerSchema.optional(),
    metadata: metadata.optional(),
    created_at: dateTime,
  })
  .strict()
  .superRefine((item, context) => {
    if (item.type !== "interaction") return;
    const parsed = askUserInteractionSchema.safeParse({
      id: item.id,
      tool_call_id: item.tool_call_id,
      prompt: item.prompt,
      kind: item.kind,
      options: item.options,
      action: item.action,
      answer: item.answer,
      status: item.status,
    });
    if (parsed.success) return;
    for (const issue of parsed.error.issues) {
      context.addIssue({ ...issue, path: issue.path });
    }
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

export type TurnStreamFrame = {
  [Event in KnownTurnStreamEvent]: {
    event: Event;
    data: z.infer<(typeof turnStreamEventSchemas)[Event]>;
  };
}[KnownTurnStreamEvent];

export function parseTurnStreamFrame(event: string, data: unknown): TurnStreamFrame | null {
  const schema = turnStreamEventSchemas[event as KnownTurnStreamEvent];
  if (!schema) return null;
  const parsed = schema.safeParse(data);
  return parsed.success ? ({ event, data: parsed.data } as TurnStreamFrame) : null;
}
