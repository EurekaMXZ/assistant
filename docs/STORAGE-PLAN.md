# Storage and Context Refactoring Plan

Status: Proposed

This document defines the target storage, event, context reconstruction, and cache architecture for conversations, Turns, provider requests, tool execution, and image inputs.

## 1. Decision summary

The target architecture assigns a distinct responsibility to each storage system:

| System | Responsibility |
| --- | --- |
| PostgreSQL | Complete semantic events, conversation and Turn metadata, direct frontend queries, S3 object indexes, and context-head versioning |
| S3-compatible object storage | Immutable files grouped by provider request/run, exact request and response archives, tool artifacts, context checkpoints, and original image bytes |
| Kafka | Transport and short-term replay of high-frequency streaming deltas before they are merged into complete events |
| Redis | One complete serialized model-context cache entry per conversation version; no segmentation in this phase |
| Worker-local memory | Decoded, ready-to-use model context for the conversations currently executing on that Worker |

The following decisions are explicit:

- PostgreSQL does not store text deltas.
- Multiple deltas for one output item are merged into one complete semantic event.
- PostgreSQL does not use a separate frontend projection database or projection table family.
- Frontend conversation views query complete events directly from PostgreSQL.
- S3 objects are grouped by provider request/run rather than by the whole Turn.
- Workers never read and rewrite a whole-Turn stream object when a new request completes.
- S3 objects are immutable. "Append" means creating the next request/run prefix, not appending bytes to an existing object.
- Redis stores the entire serialized model context in one key for a context version. Redis big-key segmentation is explicitly deferred.
- Images remain in S3. Redis and local context snapshots contain image references, not base64 data.
- Before each provider request, the Worker downloads referenced images from S3, verifies them, converts them to base64 data URLs, and releases the bytes after the request.
- Provider-managed state, provider Files APIs, external image URLs, and `previous_response_id` optimizations are outside this phase.

## 2. Goals

The design must satisfy the following requirements:

1. Opening a conversation that has not been used for a long time must remain fast.
2. Starting work on an old conversation must not require replaying the full conversation history.
3. A Turn may contain multiple provider requests and tool steps.
4. Every provider request must have an independently recoverable archive.
5. If a Turn fails, a Worker crashes, or a user terminates generation, the most recent successful request and resumable context must remain available.
6. Streaming delivery must not be blocked by repeated S3 read-modify-write operations.
7. Redis loss, eviction, or unavailability must not cause data loss or make recovery impossible.
8. The storage model must support multiple Workers without relying on process affinity.

## 3. Non-goals

The following work is deferred:

- Redis context segmentation and Redis big-key mitigation.
- A dedicated CQRS frontend read database.
- Permanent storage of raw token or text delta events in PostgreSQL.
- Provider-side file reuse or provider-managed conversation state.
- Cross-provider translation of historical provider-specific request payloads.
- Immediate deletion of legacy Turn-level objects before migration verification is complete.

## 4. Target architecture

```text
Provider stream
  |
  +--> Kafka delta events --------------------------+
  |                                                |
  +--> Redis/SSE live delivery                     |
                                                   v
                                            Worker accumulator
                                                   |
                                                   v
                                      PostgreSQL complete events

Provider request lifecycle
  |
  +--> S3 run request manifest
  +--> S3 raw provider response
  +--> S3 normalized output items
  +--> S3 tool results
  +--> S3 context checkpoint
  |
  +--> PostgreSQL turn_runs and context_heads

Context load
  |
  +--> PostgreSQL context_heads
  +--> Worker-local cache
  +--> Redis full-context cache
  +--> S3 checkpoint + PostgreSQL tail events on cache miss
```

## 5. PostgreSQL complete-event model

### 5.1 Event granularity

PostgreSQL stores complete semantic events, not transport deltas. For example, a provider may emit:

```text
"Hel"
"lo"
" world"
```

The Worker accumulates those deltas by `(run_id, item_id)` and writes one event:

```json
{
  "event_type": "output_text.completed",
  "payload": {
    "item_id": "output-1",
    "content_text": "Hello world"
  }
}
```

If the request fails or is terminated before the item completes, the Worker flushes the accumulated text as one interrupted event:

```json
{
  "event_type": "output_text.interrupted",
  "payload": {
    "item_id": "output-1",
    "content_text": "Hello wor",
    "reason": "cancelled"
  }
}
```

### 5.2 Event types

The initial complete-event vocabulary should include:

- `user_message.created`
- `reasoning_summary.completed`
- `tool_call.started`
- `tool_call.completed`
- `tool_call.failed`
- `output_text.completed`
- `output_text.interrupted`
- `run.started`
- `run.completed`
- `run.failed`
- `run.cancelled`
- `turn.completed`
- `turn.failed`
- `turn.cancelled`

Events used as frontend content must contain all data required for display without reading S3. Large diagnostic payloads remain in S3 and are represented by object keys and summaries in the event.

### 5.3 Proposed table

```sql
CREATE TABLE conversation_events (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    turn_id uuid REFERENCES turns (id) ON DELETE CASCADE,
    turn_run_id uuid REFERENCES turn_runs (id) ON DELETE CASCADE,
    event_seq bigint NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    context_included boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, event_seq)
);

CREATE INDEX conversation_events_turn_seq_idx
    ON conversation_events (turn_id, event_seq);
```

`event_seq` is a conversation-level presentation order. Transport-level Kafka sequence numbers remain separate and may be more granular.

### 5.4 Frontend reads

The frontend queries complete events directly:

```http
GET /conversations/:conversationID/events?limit=100&before_seq=...
```

The endpoint returns a cursor page ordered by `event_seq`. Opening an old conversation only reads PostgreSQL. It does not load Redis, replay Kafka, or reconstruct S3 request objects.

While a Turn is active, the frontend combines:

```text
PostgreSQL complete events
+ Redis/SSE live deltas after the last durable presentation event
```

After an output item completes, the complete DB event replaces the temporary live delta state.

### 5.5 No frontend projection database

No additional message, timeline, or presentation projection database is introduced in this phase. Existing entity tables may remain for workflow integrity and migration compatibility, but complete event rows are the frontend content source.

This means event payload schemas must be versioned and stable enough for direct frontend consumption.

## 6. Kafka and live delta handling

Kafka carries high-frequency stream deltas and provides short-term replay. PostgreSQL does not receive those deltas individually.

Each transport event must include:

```text
conversation_id
turn_id
run_id
item_id
transport_seq
event_type
delta or payload
created_at
```

Kafka partitions use `turn_id` as the partition key so one Turn is ordered. Consumers are at-least-once; transport events therefore require stable identifiers.

The complete-event accumulator must:

1. Merge text deltas by `(run_id, item_id)`.
2. Emit a complete event when `item.done` arrives.
3. Flush accumulated data as interrupted when the run fails or is cancelled.
4. Reconstruct an interrupted event from Kafka after a hard Worker crash when possible.
5. Never make PostgreSQL event correctness depend on Redis Pub/Sub retention.

Kafka retention must exceed the maximum expected recovery window. Redis remains a live-delivery mechanism rather than a durable event source.

## 7. S3 request/run storage

### 7.1 Object layout

Every provider request receives an immutable run prefix:

```text
conversations/{conversationID}/
  turns/{turnID}/
    runs/{stepIndex}-{runID}/
      request.json.zst
      response.json.zst
      output-items.json.zst
      tool-results.json.zst
      presentation-events.json.zst
      context-checkpoint.json.zst
      failure.json.zst
```

Only objects applicable to a run need to exist.

### 7.2 Object meanings

| Object | Meaning |
| --- | --- |
| `request.json.zst` | Canonical request manifest sent for this run, using S3 image references instead of cached base64 |
| `response.json.zst` | Complete raw provider response after provider success |
| `output-items.json.zst` | Normalized provider output items required by later context construction |
| `tool-results.json.zst` | Durable tool outputs associated with the request step |
| `presentation-events.json.zst` | Complete DB-level semantic events produced by this run, used for archive recovery |
| `context-checkpoint.json.zst` | Complete resumable `[]ModelItem` state after the successful request and required tool processing |
| `failure.json.zst` | Provider or workflow failure details that should not be placed in frontend event payloads |

### 7.3 Immutable writes

S3 does not support appending to an existing object. A Worker handling the next request creates the next run prefix:

```text
runs/000001-run-a/*
runs/000002-run-b/*
runs/000003-run-c/*
```

It does not read and rewrite a whole-Turn archive.

Object keys are deterministic for a run. Each PostgreSQL object reference stores:

```text
object_key
content_type
uncompressed_size
compressed_size
sha256
schema_version
```

Readers use exact object keys stored in PostgreSQL. They do not discover run state by listing an S3 prefix.

## 8. Turn-run and context-head metadata

### 8.1 Turn-run metadata

`turn_runs` remains the authoritative request index and should expose at least:

```text
id
turn_id
step_index
attempt
status
request_blob_key
response_blob_key
output_items_blob_key
tool_results_blob_key
presentation_events_blob_key
checkpoint_blob_key
provider_response_id
request_checksum
response_checksum
started_at
completed_at
failed_at
cancelled_at
```

The unique request identity is `(turn_id, step_index, attempt)`.

### 8.2 Context-head metadata

The durable context pointer is stored in PostgreSQL:

```text
conversation_id
version
latest_request_run_id
latest_successful_run_id
latest_checkpoint_key
checkpoint_covered_event_seq
last_context_event_seq
active_context_tokens
updated_at
```

The distinctions are intentional:

- `latest_request_run_id` is the most recent request manifest durably written, even if the provider response later fails.
- `latest_successful_run_id` is the most recent provider request whose successful provider response and response artifacts were committed. It may advance before a resumable post-tool checkpoint exists.
- `latest_checkpoint_key` is the most recent context state that can be used to continue execution safely.
- `checkpoint_covered_event_seq` identifies the last complete DB event included in the checkpoint.
- `last_context_event_seq` identifies the latest DB event that should be applied to model context.

## 9. Redis context cache

### 9.1 Cache content

Redis stores the complete serialized model context for one immutable context version:

```text
key: context:{conversationID}:{version}
value: zstd(msgpack(ContextSnapshot))
```

The cached snapshot contains:

```text
conversation_id
version
schema_version
covered_event_seq
latest_checkpoint_key
latest_successful_run_id
model_items
token_count
checksum
created_at
```

This phase deliberately stores the whole context in one Redis key. No segmentation or special big-key handling is implemented yet.

The Redis value does not contain image base64 or original attachment bytes. Model items contain immutable image references.

### 9.2 Cache role

Redis is a shared L2 cache, not a source of truth. Its loss must not affect correctness.

Redis entries use immutable versioned keys. Updating context creates a new key rather than mutating an old value. Old versions expire by TTL.

Recommended initial behavior:

- TTL between 30 minutes and 6 hours with random jitter.
- Compression before network transfer.
- Checksum validation after decode.
- Cache writes only after the PostgreSQL context-head transaction commits.
- Redis read or write failures fall back to durable reconstruction.

## 10. Worker-local context cache

The Worker-local cache is L1 and stores the decoded, ready-to-use form of the same logical Redis snapshot:

```text
key: (conversation_id, context_version)
value: immutable ContextSnapshot with decoded []ModelItem
```

It avoids:

- Redis network access during consecutive tool steps.
- Zstandard decompression.
- MessagePack or JSON decoding.
- Repeated context reduction.

The local cache is process-scoped, bounded, and disposable. It must not contain unique state that is absent from PostgreSQL, S3, or Kafka.

The current process-local cache implementation should be evolved from anchor/tail-oriented entries to versioned complete context snapshots. The existing DB context-head validation remains the basis for stale-cache rejection.

## 11. Context load and reconstruction

### 11.1 Cache-hit path

Before every request, the Worker reads the PostgreSQL context head to obtain the authoritative version:

```text
1. SELECT context_heads by conversation_id.
2. Look up local cache by (conversation_id, version).
3. On local miss, GET Redis context:{conversation_id}:{version}.
4. Validate schema version, context version, and checksum.
5. Decode and populate local cache.
6. Append the current request input and apply the provider token budget.
```

### 11.2 Cold path for an old conversation

When both local and Redis caches have expired:

```text
1. Read PostgreSQL context_heads.
2. Download latest_checkpoint_key from S3, if present.
3. Query complete PostgreSQL events where:
     event_seq > checkpoint_covered_event_seq
     event_seq <= last_context_event_seq
     context_included = true
4. Reduce the complete events onto the checkpoint.
5. Validate ordering and recalculate token count.
6. Build a new complete ContextSnapshot for the existing context version.
7. Write the serialized snapshot to Redis.
8. Put the decoded snapshot in the local cache.
9. Continue request construction.
```

The expected cold-path I/O is therefore:

```text
one PostgreSQL context-head query
+ one S3 checkpoint GET
+ one PostgreSQL tail-event query
```

It does not scan all prior requests or reconstruct the entire conversation from the beginning.

### 11.3 No-checkpoint fallback

New or legacy conversations may not have a checkpoint. The fallback is:

```text
1. Query all context-included complete events for the conversation.
2. Resolve any required S3 run artifacts referenced by those events.
3. Build the complete context.
4. Populate Redis and local cache.
5. Schedule an asynchronous checkpoint backfill.
```

This path is correct but not expected to be the steady-state path.

### 11.4 Context event reduction

Initial reduction rules:

| Event | Context behavior |
| --- | --- |
| `user_message.created` | Append user message and attachment references |
| `output_text.completed` | Append completed assistant output |
| `tool_call.completed` | Append provider function call and durable tool result |
| `reasoning_summary.completed` | Include only when required by provider continuation semantics |
| `output_text.interrupted` | Exclude by default |
| `tool_call.failed` | Include a normalized tool failure only when it was part of the next attempted request |
| `turn.failed` / `turn.cancelled` | Update execution metadata; do not append model content |

The reducer must be deterministic and versioned. A snapshot records the reducer schema version used to build it.

## 12. Image references and request hydration

### 12.1 Cached representation

Redis, local context snapshots, complete DB events, and S3 context checkpoints store images as references:

```json
{
  "type": "input_image_ref",
  "attachment_id": "attachment-123",
  "object_key": "attachments/conv-1/image.png",
  "content_type": "image/png",
  "size_bytes": 482311,
  "sha256": "..."
}
```

They do not store a base64 data URL.

### 12.2 Provider request hydration

For every provider request that contains historical image references, the Worker performs:

```text
1. Collect input_image_ref items from the assembled context.
2. Download the referenced image objects from S3.
3. Enforce per-image and total-image byte limits.
4. Verify size and SHA-256.
5. Convert each image to a base64 data URL.
6. Replace the internal reference with the provider input_image representation.
7. Send the provider request.
8. Release image bytes and generated base64 strings after the request.
```

This repeated S3 download and base64 conversion is an accepted cost in this phase.

The request manifest saved to S3 uses image references rather than duplicating base64. Exact request reproduction is defined as:

```text
request manifest
+ immutable image objects
+ serializer schema version
```

### 12.3 Image object lifetime

An image object referenced by an active context checkpoint or archived request manifest must remain available. User-visible attachment deletion and physical context-asset deletion therefore require an explicit retention policy.

Missing or checksum-invalid image objects are hard context errors. The Worker must not silently omit an image and send a semantically different request.

## 13. Request lifecycle

### 13.1 Request preparation

```text
1. Load the authoritative context version.
2. Resolve local/Redis cache or reconstruct from S3 + DB events.
3. Append the current user or tool input.
4. Apply compaction and token limits.
5. Build the canonical request manifest with image references.
6. Write request.json.zst to the deterministic run prefix.
7. Persist request key, checksum, and run status in PostgreSQL.
8. Hydrate image references into base64.
9. Call the provider.
```

### 13.2 Successful request

Provider response completion and resumable checkpoint completion are separate commit boundaries.

After the provider reports completion:

```text
1. Write raw response.json.zst.
2. Write output-items.json.zst.
3. Write the provider-response presentation events available at this boundary.
4. Commit one PostgreSQL transaction that:
     marks the run completed,
     inserts the complete provider-response semantic events,
     updates latest_successful_run_id,
     records response object keys and checksums,
     writes the tool-execution outbox event when tools are required.
5. Execute required tools and write tool-results.json.zst.
6. Build and write context-checkpoint.json.zst after all required tool effects and outputs are durable.
7. Write the final presentation-events.json.zst for the run.
8. Commit a second PostgreSQL transaction that:
     inserts completed tool semantic events,
     updates latest_checkpoint_key,
     marks eligible events context_included,
     advances context version and covered event sequence,
     writes any next-request outbox event.
9. After the checkpoint transaction commits, write the new Redis cache version.
10. Update the local cache.
```

S3 objects are written before PostgreSQL references them. A database transaction never points at an object that has not been successfully written and verified.

When no tools are required, the two boundaries may be executed back-to-back, but `latest_successful_run_id` and `latest_checkpoint_key` retain their distinct meanings.

### 13.3 Failed request

```text
1. Flush current accumulators into complete interrupted events.
2. Write failure.json.zst and any available presentation-events.json.zst.
3. Mark the current run failed.
4. Insert failure and interrupted complete events.
5. Do not advance latest_successful_run_id.
6. Do not replace latest_checkpoint_key with incomplete context.
7. Keep the failed run request manifest for diagnosis or exact retry.
```

### 13.4 User termination

Cancellation is a protocol rather than an immediate destructive update:

```text
running -> cancel_requested -> cancelled
```

The API records `cancel_requested_at` and signals the Worker. The Worker then:

1. Stops provider streaming when possible.
2. Flushes pending Kafka and in-memory deltas.
3. Commits a provider response and advances `latest_successful_run_id` first if `response.completed` was already durably observed.
4. Writes interrupted complete events for unfinished items.
5. Marks the current run and Turn cancelled.
6. Preserves the latest successful request and checkpoint pointers.

If a completion and cancellation race, a compare-and-swap state transition decides the winner. A completed run cannot be overwritten as cancelled.

## 14. Recovery semantics

### 14.1 Latest successful request

For a Turn with three requests:

```text
run-1 completed -> checkpoint-v1
run-2 completed -> checkpoint-v2
run-3 failed    -> request manifest exists, no successful checkpoint
```

The durable state is:

```text
latest_request_run_id    = run-3
latest_successful_run_id = run-2
latest_checkpoint_key    = checkpoint-v2
```

Retrying the failed provider call may reuse `run-3/request.json.zst`. Continuing from the last safe model state uses `checkpoint-v2`.

### 14.2 Hard Worker crash

Recovery uses:

```text
PostgreSQL run lease and status
+ deterministic S3 run keys
+ Kafka replay within retention
```

The recovery Worker determines which artifacts exist, verifies checksums, reconstructs interrupted complete events from Kafka when available, and applies idempotent state transitions.

### 14.3 Redis failure

Redis is never part of the commit boundary. On Redis timeout, eviction, corruption, or outage, the Worker reconstructs from S3 and PostgreSQL and continues. Failure to refill Redis does not fail the Turn.

### 14.4 Orphan S3 objects

An S3 write may succeed before the PostgreSQL transaction fails. A periodic reaper lists objects by lifecycle metadata or a durable upload journal and removes objects that have no committed `turn_runs` reference after a safety interval.

## 15. Context compaction

Context compaction produces a new S3 checkpoint and advances context-head metadata. It does not rewrite historical request objects.

The compaction flow is:

```text
1. Load the current complete context snapshot.
2. Select the covered event range.
3. Produce a compressed historical anchor.
4. Build the post-compaction complete ModelItems.
5. Write a new immutable checkpoint object.
6. Commit context-head version, checkpoint key, covered event sequence, and token count.
7. Populate Redis and local cache for the new version.
```

Old checkpoints remain immutable and can be expired according to archive retention after no request manifest references them.

## 16. Consistency and idempotency

The following rules are mandatory:

- Every complete event has a stable conversation event sequence and unique identity.
- Conversation event sequences are allocated atomically under the conversation write boundary; concurrent writers cannot independently guess the next value.
- Every S3 object has a deterministic run key and checksum.
- Every state update uses an expected current status or version.
- S3 is written before PostgreSQL stores a committed pointer.
- Redis is written only after PostgreSQL commits.
- Redis values are versioned and immutable.
- Kafka consumers tolerate duplicate transport events.
- Complete event insertion is idempotent.
- Retrying a run never mutates artifacts belonging to a different run attempt.

## 17. Retention

| Data | Initial retention |
| --- | --- |
| PostgreSQL complete semantic events | Long-term; they are the frontend source |
| PostgreSQL run and context metadata | Long-term |
| Kafka transport deltas | 3-7 days, subject to recovery requirements |
| Redis complete context | TTL cache, initially 30 minutes to 6 hours |
| Worker-local context | Process lifetime and local eviction |
| S3 request/run artifacts | Long-term according to account retention and deletion policy |
| Legacy Turn-level stream objects | Retain until migration verification completes |

## 18. Migration plan

### Phase 1: Schema and formats

- Add `conversation_events` and required indexes.
- Add context-head version and latest-run/checkpoint fields.
- Add missing run-level object keys and checksums to `turn_runs`.
- Define versioned complete-event, request-manifest, checkpoint, and Redis snapshot schemas.

### Phase 2: Request-level S3 dual write

- Write new immutable run-prefix objects while preserving current objects.
- Verify new objects against existing Turn results.
- Stop adding new data to the whole-Turn stream archive only after parity checks pass.

### Phase 3: Complete-event accumulation

- Keep live deltas in Kafka and Redis/SSE.
- Add Worker accumulators and complete-event writes.
- Dual-read frontend data in verification tooling before changing production reads.

### Phase 4: Redis shared context cache

- Introduce a cache interface with local L1 and Redis L2 implementations.
- Add immutable context version keys and checksums.
- Preserve S3 + PostgreSQL reconstruction as the fallback.

### Phase 5: Context loader switch

- Read the context head first.
- Prefer local cache, then Redis, then S3 checkpoint plus DB tail events.
- Replace per-Turn S3 context scans with latest-checkpoint reconstruction.

### Phase 6: Frontend event reads

- Add cursor-based complete-event API endpoints.
- Switch conversation rendering to complete events.
- Keep legacy message APIs during migration and compare results.

### Phase 7: Legacy backfill and cleanup

- Convert legacy messages and timeline records into complete events where required.
- Treat existing Turn request/response objects as the first run archive when possible.
- Backfill context checkpoints lazily for old conversations.
- Remove legacy read paths and Turn-level stream rewriting after measured parity.

## 19. Testing requirements

The implementation must include:

- Delta accumulator tests for ordering, duplication, item completion, failure, and cancellation.
- Complete-event schema compatibility tests.
- Frontend event pagination and active-stream merge tests.
- Request-level S3 key and immutability tests.
- Context snapshot serialization, checksum, and version tests.
- Local hit, Redis hit, Redis miss, Redis outage, and corrupt-cache fallback tests.
- Cold reconstruction tests using one checkpoint plus DB tail events.
- No-checkpoint legacy fallback tests.
- Multi-request Turn success and failed-latest-run recovery tests.
- Cancellation/completion race tests.
- S3-success/DB-failure orphan cleanup tests.
- Image-reference hydration, size validation, checksum validation, and base64 request tests.
- Worker crash recovery tests using Kafka replay.
- Load tests for long conversations, large complete events, context rebuild latency, Redis serialization, and repeated historical image downloads.

## 20. Observability and acceptance criteria

Required metrics include:

```text
complete_event_insert_latency
complete_event_payload_bytes
stream_delta_accumulator_bytes
stream_delta_flush_count
context_l1_hit_rate
context_redis_hit_rate
context_s3_rebuild_count
context_rebuild_latency
context_snapshot_bytes
context_snapshot_decode_latency
image_s3_download_bytes
image_hydration_latency
s3_run_artifact_write_latency
s3_orphan_count
latest_checkpoint_age
```

Initial acceptance criteria:

- No provider stream event performs a whole-Turn S3 read-modify-write.
- A frontend old-conversation load does not require S3 or Redis.
- A Worker cold start for a checkpointed conversation uses one checkpoint GET plus one bounded DB tail query.
- A failed or cancelled latest run leaves the previous successful checkpoint usable.
- Redis unavailability does not prevent context construction.
- Images are absent from Redis snapshots and are hydrated from S3 before provider calls.
- Complete DB events reproduce the same settled frontend content as the live stream.
- Every committed S3 pointer passes checksum verification.

## 21. Deferred decisions

The following decisions must be revisited after production measurements:

- Redis maximum context-key size and segmentation strategy.
- Whether contexts above a threshold should bypass Redis.
- Whether provider file IDs or provider-managed conversation state should replace repeated historical image downloads.
- Whether complete DB events require time or hash partitioning.
- Whether old complete events should be archived out of PostgreSQL after a separate frontend read model exists.
- Whether exact provider wire requests, including expanded base64 images, require separate S3 retention.
