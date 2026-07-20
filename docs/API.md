# API Reference

This document describes the backend APIs that a frontend client can use today.
It is based on the current Go implementation under `internal/server/`.

Base path:

```text
/api/v1
```

## Conventions

### Authentication

Most endpoints require a bearer token:

```http
Authorization: Bearer <access_token>
```

The following auth routes are public:

- `POST /auth/register`
- `POST /auth/login`
- `POST /auth/verify-email`
- `POST /auth/resend-verification`
- `POST /auth/forgot-password`
- `POST /auth/reset-password`

### Content type

JSON requests should use:

```http
Content-Type: application/json
```

### Error format

All non-streaming errors use the same envelope:

```json
{
  "error": "human readable message",
  "request_id": "01-request-id"
}
```

Typical status codes:

- `400 Bad Request`: invalid request body or validation error
- `401 Unauthorized`: missing or invalid bearer token
- `403 Forbidden`: insufficient privileges or disabled user
- `404 Not Found`: resource not found
- `409 Conflict`: state conflict
- `402 Payment Required`: insufficient prepaid balance or refund balance
- `500 Internal Server Error`: unexpected backend error

Every response includes `X-Request-ID`. A client may supply an ID of at most 128 characters; otherwise the server generates one.

### Common query parameters

Some list endpoints accept `limit`:

- invalid or missing `limit` falls back to the endpoint default
- each endpoint applies its own max cap

Cursor-based endpoints also accept `cursor` and return:

```json
{
  "data": [],
  "page": {
    "next_cursor": "opaque-cursor",
    "has_more": true
  }
}
```

## Core entities

### Session

Returned by login:

```json
{
  "access_token": "jwt",
  "token_type": "Bearer",
  "expires_at": "2026-07-06T12:00:00Z",
  "user": {
    "id": "user_123",
    "email": "user@example.com",
    "username": "alice",
    "role": "user",
    "status": "active",
    "last_login_at": "2026-07-06T11:30:00Z",
    "created_at": "2026-07-01T09:00:00Z",
    "updated_at": "2026-07-06T11:30:00Z"
  }
}
```

### User

Fields:

- `id`
- `email`
- `username`
- `role`: `system | admin | user`
- `status`: `active | disabled`
- `email_verified_at`
- `last_login_at`
- `created_at`
- `updated_at`

### Conversation

Fields:

- `id`
- `owner_user_id`
- `title`
- `status`
- `metadata`
- `created_at`
- `updated_at`
- `archived_at`

### Message

Fields:

- `id`
- `conversation_id`
- `turn_id`
- `seq`
- `role`: `system | developer | user | assistant | tool`
- `content_text`
- `token_count`
- `metadata`
- `created_at`
- `updated_at`

### Turn

Fields:

- `id`
- `conversation_id`
- `seq`
- `status`: `accepted | context_ready | processing | completed | failed`
- `request_blob_key`
- `response_blob_key`
- `stream_blob_key`
- `openai_response_id`
- `error_code`
- `error_message`
- `metadata`
- `started_at`
- `completed_at`
- `failed_at`
- `created_at`
- `updated_at`

Known `error_code` values currently emitted by the workflow:

- `context_load_failed`
- `sandbox_scope_failed`
- `request_prepare_failed`
- `request_blob_failed`
- `model_stream_failed`
- `response_blob_failed`
- `model_context_blob_failed`
- `turn_finalize_failed`

### Conversation sandbox

Fields:

- `id`
- `conversation_id`
- `provider`: `firecracker | cubesandbox`
- `runtime_id`
- `status`: `active | stopped | releasing | destroyed` (`releasing` is a transient, retryable deletion state)
- `runtime_metadata`
- `last_activity_at`
- `created_at`
- `updated_at`
- `stopped_at`
- `destroyed_at`

### Sandbox command result

Fields:

- `runtime_id`
- `command`
- `args`
- `working_directory`
- `output`: stdout and stderr in their original execution order
- `exit_code`
- `timed_out`

## Auth APIs

### POST `/auth/register`

Create a new end-user account and send an email verification message when mail is enabled. Registration does not create a session; login remains unavailable until verification succeeds.

Request:

```json
{
  "email": "user@example.com",
  "username": "alice",
  "password": "secret"
}
```

Response: `201 Created`

```json
{
  "verification_required": true,
  "email_sent": true
}
```

### POST `/auth/verify-email`

Request:

```json
{
  "token": "verification-token"
}
```

Response: `200 OK`

```json
{
  "verified": true
}
```

### POST `/auth/resend-verification`

Request:

```json
{
  "email": "user@example.com"
}
```

Response: `200 OK`. The response does not disclose whether the account exists.

```json
{
  "message": "if the account exists, a verification email will be sent"
}
```

### POST `/auth/login`

Log in and return a session.

Request:

```json
{
  "email": "user@example.com",
  "password": "secret"
}
```

Response: `200 OK`

```json
{
  "session": {
    "access_token": "jwt",
    "token_type": "Bearer",
    "expires_at": "2026-07-06T12:00:00Z",
    "user": {}
  }
}
```

### POST `/auth/forgot-password`

Request:

```json
{
  "email": "user@example.com"
}
```

Response: `200 OK`. The response does not disclose whether the account exists.

```json
{
  "message": "if the account exists, a password reset email will be sent"
}
```

### POST `/auth/reset-password`

Request:

```json
{
  "token": "password-reset-token",
  "new_password": "new-secret"
}
```

Response: `200 OK`

```json
{
  "password_reset": true
}
```

### GET `/auth/me`

Return the authenticated user.

Response: `200 OK`

```json
{
  "user": {}
}
```

### POST `/auth/change-password`

Change the authenticated user's password.

Request:

```json
{
  "current_password": "old-secret",
  "new_password": "new-secret"
}
```

Response: `200 OK`

```json
{
  "user": {}
}
```

## Conversation APIs

### GET `/conversations`

List the current user's conversations.

Query params:

- `limit`: default `50`, max `200`
- `cursor`: opaque cursor returned by the previous page

Response: `200 OK`

```json
{
  "conversations": [
    {}
  ]
}
```

### POST `/conversations`

Create a conversation.

Request:

```json
{
  "title": "Quarterly planning",
  "metadata": {
    "source": "web"
  }
}
```

Both fields are optional.

Response: `201 Created`

```json
{
  "conversation": {}
}
```

Use this endpoint only when an empty conversation is intentional. For a new chat whose first user message should start a turn, use the resumable initial-turn workflow below.

### POST `/conversations/initial-turns`

Prepare and commit the first turn of a new conversation. Every request requires the same caller-generated `Idempotency-Key` header, with a non-empty value of at most 128 characters.

This is a two-stage workflow because attachment object uploads cannot participate in the database transaction that creates a message and turn.

#### Prepare

Request:

```http
Idempotency-Key: 550e8400-e29b-41d4-a716-446655440000
Content-Type: application/json
```

```json
{
  "action": "prepare",
  "title": "Quarterly planning",
  "metadata": {
    "source": "web"
  }
}
```

Response: `201 Created`

```json
{
  "state": "draft",
  "replayed": false,
  "conversation": {
    "id": "conversation_123"
  }
}
```

The server creates the conversation and its context head in one database transaction and records the idempotency key. Until commit succeeds, this conversation is a resumable draft: it is addressable through conversation and attachment endpoints but omitted from `GET /conversations`. Repeating prepare with the same key and payload returns the same conversation with `replayed: true`. Reusing the key with different prepare data returns `409 Conflict`.

After prepare, create an upload intent with `POST /conversations/:conversationID/attachments`, upload the bytes directly to its presigned S3 URL, then call the attachment completion endpoint. Send a stable, per-file `Idempotency-Key` when creating the intent. Retrying with the same key replays the same attachment and issues a fresh URL while its status is `pending`.

#### Commit

Use the same `Idempotency-Key` and the `conversation.id` returned by prepare:

```json
{
  "action": "commit",
  "conversation_id": "conversation_123",
  "content": "Summarize these files",
  "attachment_ids": [
    "attachment_123"
  ],
  "model_id": "018f-model-id",
  "reasoning_effort": "high",
  "metadata": {
    "source": "composer"
  }
}
```

The content, model, reasoning, attachment, ownership, and billing rules are the same as `POST /conversations/:conversationID/messages`.

Response: `202 Accepted`

```json
{
  "state": "committed",
  "replayed": false,
  "conversation": {},
  "message": {},
  "turn": {},
  "stream_path": "/api/v1/turns/turn_123/stream"
}
```

Commit creates the first user message, accepted turn, context-head update, outbox event, and idempotency completion record in one database transaction. If validation, model resolution, billing admission, or that transaction fails, the draft and uploaded attachments remain available. Retry commit with the same key, conversation ID, and attachment IDs; do not call prepare with a new key. A retry after a successful commit returns the original conversation/message/turn with `replayed: true` and does not enqueue another turn. A different commit payload after success, or a conversation ID that does not belong to the key, returns `409 Conflict`.

### GET `/conversations/:conversationID`

Get one conversation.

Response: `200 OK`

```json
{
  "conversation": {}
}
```

### PATCH `/conversations/:conversationID`

Update conversation title or archive state.

Request:

```json
{
  "title": "Renamed title",
  "archived": true
}
```

Both fields are optional.

Response: `200 OK`

```json
{
  "conversation": {}
}
```

### POST `/conversations/:conversationID/shares`

Create a share snapshot boundary for an owned conversation. A non-empty caller-generated `Idempotency-Key` header of at most 128 bytes is required.

The snapshot stores the conversation title and the highest message sequence visible at creation time. Messages added later are not part of this share.

Response: `201 Created`

```json
{
  "share": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "conversation_id": "conversation_123",
    "created_by_user_id": "user_123",
    "title": "Quarterly planning",
    "last_message_seq": 12,
    "created_at": "2026-07-14T12:00:00Z"
  },
  "replayed": false
}
```

Repeating the request with the same conversation, user, and idempotency key returns the original share with `200 OK` and `replayed: true`. A request for a conversation not owned by the caller returns `404 Not Found` without revealing whether it exists.

## Message APIs

### GET `/conversations/:conversationID/messages`

List messages in one conversation.

Query params:

- `limit`: default `100`, max `1000`

Response: `200 OK`

```json
{
  "messages": [
    {}
  ]
}
```

### POST `/conversations/:conversationID/messages`

Create a user message and enqueue a model turn.

Request:

```json
{
  "content": "Summarize the latest OpenAI news",
  "model_id": "018f-model-id",
  "reasoning_effort": "xhigh",
  "attachment_ids": [],
  "metadata": {
    "source": "composer"
  }
}
```

Rules:

- at least one of non-blank `content` or `attachment_ids` is required
- `model_id` is optional; the configured default chat model is used when omitted
- `reasoning_effort` is optional: `low | medium | high | xhigh`; omission uses the model default
- the selected model revision and active published price are snapshotted on the accepted turn
- `metadata` is optional
- a frozen account returns `403`; a prepaid account below the selected model's maximum snapshotted charge returns `402`

Response: `202 Accepted`

```json
{
  "conversation_id": "conv_123",
  "message": {},
  "turn": {},
  "stream_path": "/api/v1/turns/turn_123/stream"
}
```

Frontend usage:

1. append `message` to the local chat log immediately
2. keep `turn.id`
3. open the SSE stream at `stream_path`

## Attachment APIs

### POST `/conversations/:conversationID/attachments`

Create an upload intent for an attachment owned by the current user in an owned conversation. The API authenticates the caller, reserves the object key, and signs a direct S3 upload; it does not accept file bytes. Files must be non-empty and at most 128 MiB. A caller-generated `Idempotency-Key` of at most 128 characters is recommended for every file.

Request:

```json
{
  "filename": "report.pdf",
  "content_type": "application/pdf",
  "size_bytes": 48231,
  "sha256": "0000000000000000000000000000000000000000000000000000000000000000",
  "content_md5": "1B2M2Y8AsgTpgAmY7PhCfg=="
}
```

Response: `201 Created`

```json
{
  "attachment": {
    "id": "attachment_123",
    "conversation_id": "conversation_123",
    "filename": "report.pdf",
    "content_type": "application/pdf",
    "size_bytes": 48231,
    "sha256": "0000000000000000000000000000000000000000000000000000000000000000",
    "status": "pending"
  },
  "upload": {
    "url": "https://objects.example.com/bucket/attachments/...?X-Amz-Signature=...",
    "method": "PUT",
    "headers": {
      "Content-Type": "application/pdf",
      "Content-MD5": "1B2M2Y8AsgTpgAmY7PhCfg=="
    },
    "expires_at": "2026-07-20T12:15:00Z"
  }
}
```

Send the file directly to `upload.url` using the returned method and headers. Do not send the application bearer token to the object store. The expected content length, content type, and `Content-MD5` are part of the URL signature. S3 rejects bytes whose MD5 does not match, and any replay of the URL can only write the same content. A replay of an already completed intent returns the `ready` attachment without an `upload` field.

### POST `/conversations/:conversationID/attachments/:attachmentID/complete`

Confirm that the direct PUT completed. The API performs an S3 metadata HEAD, verifies the stored size and content type, then changes the attachment from `pending` to `ready`. S3 has already verified the signed `Content-MD5`; workers additionally verify the stored SHA-256 before sending an image to Responses or importing an object into a sandbox.

Request:

```json
{}
```

Response: `200 OK`

```json
{
  "attachment": {
    "id": "attachment_123",
    "status": "ready",
    "upload_completed_at": "2026-07-20T12:01:00Z"
  }
}
```

Only `ready` attachment IDs are accepted by message and initial-turn commit endpoints. Clients must keep send disabled while any selected attachment is still uploading or waiting for completion.

### GET `/conversations/:conversationID/attachments/:attachmentID`

Create a presigned S3 download for a `ready` attachment in an owned conversation. The API returns JSON and never proxies the object bytes. Use `?disposition=attachment` to request an attachment content disposition; the default is inline.

Response: `200 OK`

```json
{
  "attachment": {
    "id": "attachment_123",
    "status": "ready"
  },
  "download": {
    "url": "https://objects.example.com/bucket/attachments/...?X-Amz-Signature=...",
    "method": "GET",
    "expires_at": "2026-07-20T12:15:00Z"
  }
}
```

The S3 bucket CORS policy must allow the frontend origin to use `PUT`, `GET`, and `HEAD`, including the `Content-Type` and `Content-MD5` request headers.

Incomplete uploads expire after `S3_PENDING_UPLOAD_TTL`. A worker claims their database rows, removes their S3 objects, and then deletes the rows; failed object deletions remain claimed and are retried by the next reaper pass.

## Sandbox APIs

### GET `/conversations/:conversationID/sandbox`

Get the current active or stopped sandbox for the conversation.

Response: `200 OK`

```json
{
  "sandbox": {}
}
```

### POST `/conversations/:conversationID/sandbox`

Create a sandbox, or resume the existing sandbox when it is stopped.

Response: `201 Created`

```json
{
  "sandbox": {}
}
```

### DELETE `/conversations/:conversationID/sandbox`

Permanently release the active or stopped sandbox. Provider deletion is retried from the internal `releasing` state until it is confirmed.

Response: `200 OK`

```json
{
  "sandbox": {}
}
```

### POST `/conversations/:conversationID/sandbox/exec`

Execute one command inside the conversation sandbox, resuming it first when it is stopped.

Request:

```json
{
  "command": "python",
  "args": [
    "--version"
  ],
  "working_directory": "",
  "timeout_seconds": 30
}
```

Response: `200 OK`

```json
{
  "result": {}
}
```

## Turn APIs

### GET `/turns/:turnID`

Return the current turn object.

Response: `200 OK`

```json
{
  "turn": {}
}
```

### GET `/turns/:turnID/execution-trace`

Return a debug trace for one turn. This is frontend-usable, but it is best treated as a diagnostics view rather than the primary chat API.

Response: `200 OK`

```json
{
  "trace": {
    "turn_id": "turn_123",
    "conversation_id": "conv_123",
    "status": "completed",
    "openai_response_id": "resp_123",
    "stream_events": [],
    "runs": [
      {
        "id": "run_1",
        "step_index": 1,
        "attempt": 1,
        "provider": "openai.responses",
        "status": "completed",
        "response_id": "resp_123",
        "input_tokens": 100,
        "output_tokens": 200,
        "total_tokens": 300,
        "output_items": [],
        "tool_calls": [
          {
            "id": "tool_1",
            "call_id": "call_1",
            "tool_type": "function",
            "namespace": "internet",
            "tool_name": "search",
            "status": "completed",
            "summary": "Searched the web",
            "details": [
              "Query: latest OpenAI news",
              "Results: 5"
            ]
          }
        ]
      }
    ]
  }
}
```

Notes:

- encrypted reasoning content is redacted from trace artifacts
- tool calls are exposed as `summary` and `details`, not raw arguments/output
- each run represents exactly one upstream model request; `queued`, `running`, `completed`, and `failed` are valid run states
- a turn can contain any number of runs, and a stale worker retries only the current run attempt

## Stream API

### GET `/turns/:turnID/stream`

This is the only frontend-facing presentation API for one model turn. It serves both historical snapshots and live updates.

Headers:

```http
Accept: text/event-stream
Authorization: Bearer <access_token>
```

Server behavior:

- the server always sends one complete, filtered `turn.snapshot` first
- if the turn is completed or failed, it then sends `turn.done` and closes
- if the turn is still running, it continues with canonical item mutation events, then reconciles durable state and sends a final authoritative `turn.snapshot` before `turn.done`
- terminal snapshots include `started_at` and either `completed_at` or `failed_at`, so clients do not need a separate turn fetch to calculate elapsed time
- if live terminal delivery is missed, polling performs the same final `turn.snapshot` and `turn.done` reconciliation
- the server sends SSE keep-alive comments every 30 seconds
- raw provider events, tool arguments, tool output, encrypted reasoning, model instructions, tool definitions, and usage are never returned
- tool presentation fields are selected and sanitized by the backend before entering either the snapshot or live stream

### SSE frame format

Each frame uses standard SSE syntax:

```text
event: item.delta
data: {"item_id":"assistant:resp_123:0:0","item_type":"output_text","delta":"Hel","sequence_number":42,"created_at":"2026-07-06T12:00:01Z"}

```

Only registered presentation events are returned:

- `turn.snapshot`: complete filtered state at connection time or terminal reconciliation
- `item.upsert`: create or replace one display item
- `item.delta`: append text to one display item; consumers ignore a `sequence_number` that is not newer than the item's current sequence
- `item.done`: authoritative completed display item
- `turn.done`: terminal turn status
- `conversation.updated`: filtered conversation title update

Unknown internal events and unregistered fields are dropped by default.

### Presentation item fields

Presentation items can contain only these fields:

```json
{
  "id": "tool:tool_1",
  "type": "tool_call",
  "title": "Searching the Web",
  "status": "completed",
  "input_label": "Keywords",
  "input_text": "latest OpenAI news",
  "links": [
    {"url": "https://openai.com/news/", "label": "openai.com"}
  ],
  "created_at": "2026-07-06T12:00:01Z"
}
```

The backend maintains separate event and item registries. Only registered event types are processed, and each registered item type has an explicit field filter.

Assistant text is retained in snapshot and live frames as `type: "output_text"`. Frontend clients use these items to build the left-side assistant message and exclude them only from the right-side timeline.

Completed assistant text can include `metadata.phase` with the value `commentary` or `final_answer`. Because the provider's text-done event does not identify the phase, the server may first send `item.done` and then enrich the same item ID with an `item.upsert` after the complete model response arrives. Clients must merge that upsert into the existing item rather than creating a second message. Snapshots already contain the merged phase.

The active internet tools are search and extract. Their links are extracted only from URL-bearing argument/result fields. Before returning a link, the backend:

- accepts only `http` and `https`
- removes URL user information and fragments
- removes credential-like query parameters such as API keys, tokens, passwords, secrets, and signatures
- deduplicates links and limits each item to 24
- derives the display label from the hostname without trusting result titles or content

Non-Tavily tools continue to use the filtered `summary` and `details` fallback. The frontend renders canonical fields only and never parses raw tool arguments or results.

When a reasoning payload contains multiple standalone Markdown title paragraphs such as `**Title**`, each titled section is projected as a separate reasoning item. This preserves summary boundaries when the Responses API merges several reasoning summaries into one text value.

### Internal source envelope (not returned by this API)

The backend and provider use this richer envelope internally. It is archived for execution and diagnostics, but it is not sent through the presentation stream:

```json
{
  "type": "response.output_text.delta",
  "conversation_id": "conv_123",
  "turn_id": "turn_123",
  "response_id": "resp_123",
  "tool_name": "internet.search",
  "payload": "{\"status\":\"completed\"}",
  "delta": "Hel",
  "text": "Hello world",
  "error": "something failed"
}
```

Not every field is present on every internal event.

### Internal source event types (not returned by this API)

#### `response.started`

Sent when the backend is about to execute one scheduled model request. A multi-step turn can emit this event more than once.

Example:

```json
{
  "type": "response.started",
  "conversation_id": "conv_123",
  "turn_id": "turn_123"
}
```

#### `response.created`

Sent after the upstream model response is created and a `response_id` is known.

Example:

```json
{
  "type": "response.created",
  "conversation_id": "conv_123",
  "turn_id": "turn_123",
  "response_id": "resp_123"
}
```

#### `response.output_text.delta`

Incremental assistant text. Append `delta` to the current assistant message.

Example:

```json
{
  "type": "response.output_text.delta",
  "conversation_id": "conv_123",
  "turn_id": "turn_123",
  "response_id": "resp_123",
  "delta": "Hello"
}
```

#### `response.completed`

Success event for one scheduled model request. A multi-step turn can emit this event more than once, so it is not the frontend turn terminal. `text` contains that request's complete assistant text.

Example:

```json
{
  "type": "response.completed",
  "conversation_id": "conv_123",
  "turn_id": "turn_123",
  "response_id": "resp_123",
  "text": "Hello world"
}
```

The presentation layer uses the complete response to enrich canonical output items. Frontend clients terminate only on canonical `turn.done`.

#### `response.failed`

Terminal failure event.

Example:

```json
{
  "type": "response.failed",
  "conversation_id": "conv_123",
  "turn_id": "turn_123",
  "error": "openai streaming failed"
}
```

#### `reasoning.summary`

Persisted model reasoning summary for one model step.

The event carries a user-safe summary only. It never includes encrypted reasoning content.

Example:

```json
{
  "type": "reasoning.summary",
  "conversation_id": "conv_123",
  "turn_id": "turn_123",
  "response_id": "resp_123",
  "text": "Need to search before answering.",
  "payload": "{\"turn_run_id\":\"run_1\",\"response_id\":\"resp_123\",\"step_index\":1,\"summary\":\"Need to search before answering.\"}"
}
```

Nested payload schema:

```json
{
  "turn_run_id": "run_1",
  "response_id": "resp_123",
  "step_index": 1,
  "summary": "Need to search before answering."
}
```

### Internal tool lifecycle events

#### `tool.started`
#### `tool.completed`
#### `tool.failed`

These three archived events all use a nested JSON string in `payload`. Diagnostics consumers may parse that payload; frontend clients must use the canonical stream instead.

Nested payload schema:

```json
{
  "tool_call_record_id": "tool_1",
  "turn_run_id": "run_1",
  "call_id": "call_1",
  "tool_name": "internet.search",
  "tool_type": "function",
  "namespace": "internet",
  "server_label": "tavily",
  "status": "completed",
  "state": "optional state",
  "message": "optional message",
  "summary": "Searched the web",
  "details": [
    "Query: latest OpenAI news",
    "Results: 5"
  ],
  "error": "optional tool error"
}
```

Example `tool.started`:

```json
{
  "type": "tool.started",
  "conversation_id": "conv_123",
  "turn_id": "turn_123",
  "tool_name": "internet.search",
  "payload": "{\"tool_call_record_id\":\"tool_1\",\"turn_run_id\":\"run_1\",\"call_id\":\"call_1\",\"tool_name\":\"internet.search\",\"tool_type\":\"function\",\"namespace\":\"internet\",\"status\":\"started\",\"summary\":\"Searching the web\",\"details\":[\"Query: latest OpenAI news\"]}"
}
```

Example `tool.completed`:

```json
{
  "type": "tool.completed",
  "conversation_id": "conv_123",
  "turn_id": "turn_123",
  "tool_name": "internet.search",
  "payload": "{\"tool_call_record_id\":\"tool_1\",\"turn_run_id\":\"run_1\",\"call_id\":\"call_1\",\"tool_name\":\"internet.search\",\"tool_type\":\"function\",\"namespace\":\"internet\",\"status\":\"completed\",\"summary\":\"Searched the web\",\"details\":[\"Query: latest OpenAI news\",\"Results: 5\"]}"
}
```

Example `tool.failed`:

```json
{
  "type": "tool.failed",
  "conversation_id": "conv_123",
  "turn_id": "turn_123",
  "tool_name": "internet.search",
  "payload": "{\"tool_call_record_id\":\"tool_1\",\"turn_run_id\":\"run_1\",\"call_id\":\"call_1\",\"tool_name\":\"internet.search\",\"tool_type\":\"function\",\"namespace\":\"internet\",\"status\":\"failed\",\"summary\":\"Web search failed\",\"details\":[\"Query: latest OpenAI news\",\"Error: upstream failed\"],\"error\":\"upstream failed\"}",
  "error": "upstream failed"
}
```

These are internal lifecycle records. They are projected into filtered `item.upsert` or `item.done` frames before reaching frontend clients.

### Internal tool side-effect events

These are emitted when a local tool changes conversation state. Their `payload` is also a JSON string, but the shape depends on the event type.

#### `conversation.updated`

Payload:

```json
{
  "conversation_id": "conv_123",
  "title": "New title"
}
```

Example outer event:

```json
{
  "type": "conversation.updated",
  "conversation_id": "conv_123",
  "turn_id": "turn_123",
  "tool_name": "conversation.rename_title",
  "payload": "{\"conversation_id\":\"conv_123\",\"title\":\"New title\"}"
}
```

#### `sandbox.updated`

Payload:

```json
{
  "conversation_id": "conv_123",
  "sandbox": {
    "id": "sb_1",
    "conversation_id": "conv_123",
    "provider": "firecracker",
    "runtime_id": "runtime_1",
    "status": "active",
    "runtime_metadata": {},
    "created_at": "2026-07-06T11:00:00Z",
    "updated_at": "2026-07-06T11:00:00Z"
  }
}
```

### Typical internal event order for one turn

The exact sequence depends on whether tools are used, but a common order is:

1. `response.started`
2. `response.created`
3. optional `reasoning.summary`
4. zero or more `tool.started`
5. matching `tool.completed` or `tool.failed`
6. optional side-effect events such as `conversation.updated` or `sandbox.updated`
7. zero or more `response.output_text.delta`
8. one terminal event: `response.completed` or `response.failed`

Important notes:

- local tool side-effect events usually occur after `tool.completed`
- remote tool calls may emit only `tool.completed` or `tool.failed`
- `response.output_text.delta` can appear before or after tool events depending on the model's turn structure
- frontend code should not hard-code a strict event count

## Recommended frontend conversation flow

### Start a conversation with its first turn

1. generate and persist one idempotency key for the composer draft
2. when the user selects the first file, immediately call `POST /conversations/initial-turns` with `action: "prepare"`; do not wait for Send
3. create one upload intent per file, PUT each file directly to S3, call its completion endpoint, and retain only `ready` attachment IDs
4. keep Send disabled until every selected attachment is `ready`
5. when the user sends, call the initial-turn endpoint with the same key, `action: "commit"`, the prepared conversation ID, current message/model options, and ready attachment IDs
6. on any non-conflict failure, retry commit with the same key, conversation ID, and attachment IDs
7. after `202 Accepted`, append `message` locally and open SSE on `stream_path`

Do not implement a first message as `POST /conversations` followed by `POST /conversations/:conversationID/messages`; that older sequence can expose an intentionally empty active conversation when the second request fails. Keep the idempotency key and prepared conversation ID in recoverable client state until commit returns `202`.

### Open an existing conversation

1. call `GET /conversations`
2. call `GET /conversations/:conversationID/messages`
3. optionally call `GET /conversations/:conversationID/sandbox`

### Send one message

1. call `POST /conversations/:conversationID/messages`
2. append the returned `message` locally
3. open SSE on `stream_path`
4. merge stream events into chat state:
   - replace state from `turn.snapshot`
   - apply `item.upsert`, `item.delta`, and `item.done`
   - update title on `conversation.updated`
   - finalize on `turn.done`
5. if needed, refresh `GET /turns/:turnID` or `GET /conversations/:conversationID/messages`

### Resume after reconnect

If the frontend reconnects after a delay, it can reopen `GET /turns/:turnID/stream`.

- if the turn already completed, the endpoint sends `turn.snapshot`, then completed `turn.done`
- if the turn already failed, the endpoint sends `turn.snapshot`, then failed `turn.done`
- if the turn is running, `turn.snapshot` is followed by live item mutations

This makes the stream endpoint safe to use as both a live stream and a terminal-result fetch path.

When the frontend opens an older turn, it reuses `GET /turns/:turnID/stream`; terminal turns return their full filtered snapshot and close immediately.

## Model Catalog APIs

These endpoints require an authenticated user. Only enabled models are returned, and provider credential IDs are omitted.

### GET `/models`

Query params: `limit` (default `50`, max `200`) and `cursor`.

Response: cursor page whose `data` entries are models.

### GET `/models/:modelID`

Response:

```json
{
  "model": {
    "id": "018f-model-id",
    "provider": "openai",
    "slug": "gpt-primary",
    "upstream_model": "gpt-5.4",
    "display_name": "GPT Primary",
    "description": "",
    "input_modalities": ["text"],
    "output_modalities": ["text"],
    "supports_tools": true,
    "supports_parallel_tools": true,
    "supported_reasoning_efforts": ["low", "medium", "high", "xhigh"],
    "context_window_tokens": 128000,
    "max_output_tokens": 8192,
    "default_parameters": {},
    "status": "enabled",
    "revision": 1
  }
}
```

## Billing APIs

Money values use a three-letter currency and integer `*_nanos`, where one currency unit is `1,000,000,000` nanos. Human-readable amounts are also returned as decimal strings.

### GET `/billing/account`

Returns the authenticated user's prepaid account. Accounts are created lazily with a zero balance.

### POST `/billing/redemptions`

```json
{
  "code": "0123456789abcdef0123456789abcdef0123456789abcdef"
}
```

Atomically consumes a single-use redemption code and credits the authenticated user's account. Returns `201 Created` with `account` and the immutable `redemption_credit` transaction. Retrying the same code as the same user returns the original transaction; invalid, expired, or already consumed codes return the same generic validation error. Plaintext codes are never logged or stored.

### GET `/billing/transactions`

Query params: `kind`, `limit` (default `50`, max `200`), and `cursor`. Results are always scoped to the authenticated user.

### GET `/billing/transactions/:transactionID`

Returns one transaction only when it belongs to the authenticated user.

### GET `/billing/usage-events`

Query params: `status`, `limit`, and `cursor`. Usage includes turn and compaction requests, token counts, tool call counts, immutable model and tool pricing snapshots, rated amount, and linked billing transaction when one was captured. `tool_amount_nanos` is the tool portion of `amount_nanos`, `tool_amount` is its exact decimal string, and `tool_usage` maps billing tool keys to successful call counts.

### GET `/billing/usage-events/:usageEventID`

Returns one usage event only when it belongs to the authenticated user.

## Audit APIs

### GET `/audit-events`

Returns actions performed by the authenticated user plus administrator actions explicitly visible to that user. Administrator identity is redacted to `administrator` for subject-visible events.

Query params: `action`, `resource_type`, `outcome`, `limit`, and `cursor`.

### GET `/audit-events/:auditEventID`

Returns one event only when it is visible to the authenticated user.

## Admin APIs

User management, admin billing, and admin audit routes require at least the `admin` role, so both `admin` and `system` users are authorized. Provider credential, model, model price, model settings, and mail settings routes require the exact `system` role; an `admin` user is not authorized for them.

### GET `/admin/overview`

Returns aggregate user, active billing account, and visible audit counts plus the eight most recent visible audit events. System users also receive enabled model and active credential counts. This endpoint does not enumerate the underlying lists.

### GET `/users`

Query params:

- `limit`: default `50`, max `200`

Response:

```json
{
  "data": [{}],
  "page": {
    "next_cursor": "opaque-cursor",
    "has_more": true
  }
}
```

### POST `/users`

Request:

```json
{
  "email": "user@example.com",
  "username": "alice",
  "password": "secret",
  "role": "user",
  "status": "active"
}
```

Response: `201 Created`

```json
{
  "user": {}
}
```

### GET `/users/:userID`

Response:

```json
{
  "user": {}
}
```

### PATCH `/users/:userID`

Request:

```json
{
  "email": "new@example.com",
  "username": "new-name",
  "role": "user",
  "status": "disabled"
}
```

All fields are optional.

Response:

```json
{
  "user": {}
}
```

### POST `/users/:userID/reset-password`

Request:

```json
{
  "new_password": "new-secret"
}
```

Response: `200 OK`

```json
{
  "user": {}
}
```

## Admin Provider Credentials

All routes in this section require the exact `system` role.

Provider API keys are accepted only on create and rotate. Responses contain `masked_key`; plaintext keys are AES-256-GCM encrypted at rest and are never returned.

### GET `/admin/provider-credentials`

Query params: `limit` and `cursor`.

### POST `/admin/provider-credentials`

```json
{
  "provider": "openai",
  "name": "primary-openai",
  "base_url": "https://api.openai.com/v1",
  "api_key": "sk-secret"
}
```

Response: `201 Created` with `credential`.

### GET `/admin/provider-credentials/:credentialID`

Returns `credential` without the plaintext key.

### PATCH `/admin/provider-credentials/:credentialID`

Optional fields: `name`, `base_url`.

### POST `/admin/provider-credentials/:credentialID/rotate`

```json
{"api_key":"sk-new-secret"}
```

Rotating increments the encrypted key version, enables the credential, and clears prior validation state.

### POST `/admin/provider-credentials/:credentialID/validate`

Calls `GET {base_url}/models` with the decrypted key and records validation time/status. Provider response bodies are not exposed.

### POST `/admin/provider-credentials/:credentialID/enable`

Enables a non-revoked credential.

### POST `/admin/provider-credentials/:credentialID/disable`

Disables a credential. Models referencing it become unavailable for new turns.

### DELETE `/admin/provider-credentials/:credentialID`

Permanently marks the credential as `revoked`. Referenced credentials return `409 Conflict`.

## Admin Models and Prices

All routes in this section require the exact `system` role.

### GET `/admin/models`

Lists enabled and disabled models. Query params: `limit`, `cursor`.

### POST `/admin/models`

```json
{
  "provider": "openai",
  "credential_id": "018f-credential-id",
  "slug": "gpt-primary",
  "upstream_model": "gpt-5.4",
  "display_name": "GPT Primary",
  "description": "",
  "input_modalities": ["text"],
  "output_modalities": ["text"],
  "supports_tools": true,
  "supports_parallel_tools": true,
  "supported_reasoning_efforts": ["low", "medium", "high", "xhigh"],
  "context_window_tokens": 128000,
  "max_output_tokens": 8192,
  "default_parameters": {
    "reasoning_effort": "medium",
    "reasoning_summary": "auto",
    "text_verbosity": "medium"
  }
}
```

Response: `201 Created` with `model`.

`supported_reasoning_efforts` is the sole reasoning-capability field and is always present in model responses. It accepts `low`, `medium`, `high`, and `xhigh`; an empty list disables reasoning. When `default_parameters.reasoning_effort` is set, it must be included in the list.

### GET `/admin/models/:modelID`

Returns one model, including its credential ID.

### PATCH `/admin/models/:modelID`

Optional fields: `credential_id`, `display_name`, `description`, modalities, tool capability booleans, `supported_reasoning_efforts`, token limits, and `default_parameters`. Each update increments `revision`.

### POST `/admin/models/:modelID/enable`

### POST `/admin/models/:modelID/disable`

Enable or disable selection for new turns.

### GET `/admin/models/:modelID/prices`

Lists immutable draft, published, and archived price versions. Query params: `limit` (default `50`, max `200`) and the opaque `cursor` returned by the previous page. The response uses the standard `{ "data": [], "page": {} }` cursor envelope.

### POST `/admin/models/:modelID/prices`

```json
{
  "currency": "USD",
  "input_per_million_nanos": 1250000000,
  "output_per_million_nanos": 10000000000,
  "cache_read_input_per_million_nanos": 125000000,
  "cache_creation_input_per_million_nanos": 1500000000,
  "image_input_per_million_nanos": null,
  "image_output_per_image_nanos": null
}
```

All rates are currency nanos. Token rates are per one million tokens. Response: `201 Created` with a draft `price`.

### GET `/admin/models/:modelID/prices/:priceID`

### POST `/admin/models/:modelID/prices/:priceID/publish`

Optional request: `{"effective_from":"2026-07-11T20:00:00Z"}`. Omitted time means immediately.

### POST `/admin/models/:modelID/prices/:priceID/archive`

Published price snapshots remain attached to existing turns and usage events after archival.

### GET `/admin/model-settings`

Returns `default_chat_model_id` and `compaction_model_id`.

### PATCH `/admin/model-settings`

```json
{
  "default_chat_model_id": "018f-model-id",
  "compaction_model_id": "018f-model-id"
}
```

## Admin Mail Settings

All routes in this section require the exact `system` role. SMTP passwords are accepted on update but are never returned; responses expose `password_configured` instead.

### GET `/admin/mail-settings`

Response: `200 OK` with `settings`.

### PATCH `/admin/mail-settings`

All fields are optional.

```json
{
  "enabled": true,
  "host": "smtp.example.com",
  "port": 587,
  "security": "starttls",
  "username": "mailer@example.com",
  "password": "secret",
  "from_email": "mailer@example.com",
  "from_name": "Assistant"
}
```

Response: `200 OK` with `settings`.

### POST `/admin/mail-settings/test`

```json
{
  "recipient": "recipient@example.com"
}
```

Response: `200 OK`

```json
{
  "sent": true
}
```

## Admin Billing

### GET `/admin/billing/accounts`

Lists accounts with cursor pagination.

### GET `/admin/billing/accounts/:userID`

Gets or lazily creates the user's configured-currency account.

### PATCH `/admin/billing/accounts/:userID`

```json
{
  "status": "active"
}
```

Billing is prepaid only. `status` is `active | frozen`; frozen accounts cannot enqueue model turns or receive manual balance mutations.

### POST `/admin/billing/accounts/:userID/topups`

Requires an `Idempotency-Key` header.

```json
{
  "amount": "10.00",
  "currency": "USD",
  "reason": "support adjustment",
  "reference": "ticket-123"
}
```

Creates an immediate credit and returns `201 Created` with `transaction`. Replaying the same key and input returns the existing transaction; reusing the key with different input returns `409`.

### POST `/admin/billing/accounts/:userID/refunds`

Uses the same request and idempotency rules as topups, but creates an arbitrary manual debit. It returns `402` when the debit would make the balance negative.

### POST `/admin/billing/redemption-codes`

```json
{
  "amount": "10.00",
  "quantity": 20,
  "expires_at": "2026-12-31T23:59:59Z"
}
```

Atomically generates `quantity` single-use redemption codes with the same face value and expiry in the configured billing currency. `quantity` must be between `1` and `100`. New codes are 48-character lowercase hexadecimal strings generated from 24 cryptographically random bytes. `expires_at` is optional and must be in the future. Returns `201 Created` with a `redemption_codes` array containing metadata and plaintext codes. Plaintext is returned only by this response; only SHA-256 hashes and masked hints are stored.

### GET `/admin/billing/redemption-codes`

Lists redemption code metadata with cursor pagination. Status is computed as `active | disabled | expired | redeemed`; list responses never contain plaintext codes.

### POST `/admin/billing/redemption-codes/:codeID/disable`

Permanently disables an active redemption code. The amount and expiry remain immutable. Repeating the disable is safe; redeemed or expired codes return `409 Conflict`.

### GET `/admin/billing/tool-prices`

Returns the configured-currency flat prices for `sandbox.create`, `image_generation`, `tavily.search`, and `tavily.extract`. The local `internet.search` and `internet.extract` tools are billed under the corresponding Tavily keys.

### PUT `/admin/billing/tool-prices`

Replaces the complete supported tool pricing plan atomically. Every supported tool must appear exactly once. Enabled prices must be greater than zero; disabled tools may retain a non-negative price for later activation.

```json
{
  "tool_prices": [
    { "tool_key": "sandbox.create", "price_per_call_nanos": 250000000, "enabled": true, "version": 1 },
    { "tool_key": "image_generation", "price_per_call_nanos": 500000000, "enabled": true, "version": 1 },
    { "tool_key": "tavily.search", "price_per_call_nanos": 5000000, "enabled": true, "version": 1 },
    { "tool_key": "tavily.extract", "price_per_call_nanos": 10000000, "enabled": true, "version": 1 }
  ]
}
```

`version` is required for optimistic concurrency. A stale plan returns `409 Conflict` instead of overwriting a newer update.

Tool prices are resolved when a successful model run settles. Only completed durable tool calls and image-generation output items with non-empty results are counted. The selected price versions and counts are stored on the immutable usage event. Model and tool amounts are debited together, so a retry cannot charge the run twice. Admission remains token-based; insufficient balance at settlement fails the turn without writing a debit transaction.

### GET `/admin/billing/transactions`

Query params: `user_id`, `kind`, `limit`, `cursor`.

### GET `/admin/billing/transactions/:transactionID`

### GET `/admin/billing/usage-events`

Query params: `user_id`, `status`, `limit`, `cursor`.

### GET `/admin/billing/usage-events/:usageEventID`

Billing transactions and usage events are append-only database records.

## Admin Audit

### GET `/admin/audit-events`

Query params: `actor_user_id`, `subject_user_id`, `action`, `resource_type`, `outcome`, `limit`, `cursor`.

### GET `/admin/audit-events/:auditEventID`

Audit events are append-only. Mutation audits store route/status metadata but never request bodies or provider API keys.
