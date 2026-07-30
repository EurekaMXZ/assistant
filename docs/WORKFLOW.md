# Workflow and Execution Architecture

Status: Proposed

This document describes the target workflow architecture for model requests and tool execution. It complements [STORAGE-PLAN.md](./STORAGE-PLAN.md), which defines the durable storage, artifact, context, and Kafka replay model.

## 1. Scope

The design covers:

- The boundary between the OpenAI adapter, workflow orchestration, and tool execution.
- The lifecycle of a Turn containing multiple model requests and tool steps.
- Separate LLM client and tool execution workers.
- Kafka and Outbox events used for scheduling.
- Tool execution groups, parallelism, and `ask_user` ordering.
- Actor concurrency, leases, idempotency, and Worker crash recovery.

The design does not change the frontend presentation contract. The existing presentation stream remains the projection from internal events to filtered SSE events.

## 2. Current Implementation

The current system already separates the OpenAI adapter from the tool handlers at the code level:

- `internal/openai` sends the model request, parses provider SSE, and returns `llm.ModelResult`/`llm.ModelEvent` values.
- `internal/tool` contains local tool handlers and the `ToolExecutor` interface.
- `internal/mcpconfig.CompositeRuntime` dispatches local and user-configured MCP tools.
- `internal/workflow.ToolOrchestrator` builds model requests, interprets model output, persists tool calls, invokes `ToolExecutor`, and schedules the next model step.

The main coupling is in `workflow.ToolOrchestrator`. `PostprocessScheduledRun` currently performs all of these responsibilities in one Worker call:

1. Interpret the completed model response.
2. Normalize function tool names.
3. Select and order local tool calls.
4. Acquire and persist tool-call records.
5. Invoke the tool executor directly.
6. Append tool outputs to the next model request.
7. Prepare the next `turn_run` state.

`executeLocalToolCalls` currently executes calls in a synchronous `for` loop. The existing `ask_user` handling moves `ask_user` to the end of the list, but all other calls are still serialized.

The existing `WORKER_CONCURRENCY` setting controls in-memory workflow slots. A slot is a long-lived Go goroutine that processes one Kafka workflow event at a time. It is not a durable `turn_run`, and it is not an execution actor.

Relevant current implementation:

- Workflow consumer slots: `internal/worker/consumer.go`
- Workflow service configuration: `internal/worker/settings.go`
- Model request and tool postprocessing: `internal/workflow/tool_run_step.go`
- Direct local tool execution: `internal/workflow/tool_orchestrator_steps.go`
- Tool dispatch: `internal/tool/executor.go` and `internal/mcpconfig/runtime.go`

## 3. Target Responsibilities

### 3.1 OpenAI adapter

The OpenAI adapter is a model transport adapter only. It may:

- Serialize an `llm.ModelRequest`.
- Send one upstream request.
- Read and parse provider SSE events.
- Build `llm.ModelEvent` values for streaming.
- Build `llm.ModelResult` values from the completed response.
- Parse usage, output items, text items, response IDs, and provider errors.

It must not:

- Resolve a local tool handler.
- Execute a local or MCP tool.
- Decide tool execution order.
- Create tool execution tasks.
- Persist tool execution state.

The adapter does not own the agent loop. The workflow system owns the sequence of model requests.

### 3.2 LLM client worker

The LLM client worker consumes a durable `turn_run.requested` task. It owns one model request and its provider stream:

1. Claim the `turn_run` lease.
2. Load the immutable request/state artifacts.
3. Call the model client once.
4. Publish model stream events for live presentation and replay.
5. Persist the raw response and normalized output artifacts.
6. Interpret the completed output into a tool execution plan or a terminal response.
7. Persist the plan and publish tool execution tasks when tools are required.

The LLM client worker must not invoke `ToolExecutor` directly. After it publishes a tool plan, it releases the model-request execution responsibility and the run enters a durable waiting-for-tools state.

### 3.3 Execution worker

The execution worker consumes durable `tool_call.requested` tasks. It is the only layer allowed to invoke `ToolExecutor`.

It owns:

- Tool handler lookup through the configured executor/runtime.
- Tool execution leases and execution attempts.
- Tool arguments and output artifacts.
- Tool success, failure, cancellation, and awaiting-input transitions.
- `tool.started`, `tool.completed`, and `tool.failed` semantic events.
- Completion of an execution group.
- Reporting execution-group completion after all required calls are settled.

The execution worker must not call the OpenAI client. It returns model-visible `function_call_output` data through durable artifacts and workflow state; the next LLM client worker consumes that state in a new `turn_run`.

### 3.4 Workflow coordinator

The workflow coordinator owns state transitions and Outbox writes. It does not perform long-running provider or tool work.

It is responsible for:

- Creating and advancing `turn_run` records.
- Persisting the tool execution plan.
- Publishing ready tool tasks.
- Detecting execution-group completion.
- Creating the next `turn_run.requested` task.
- Handling cancellation, retry, lease expiry, and terminal Turn transitions.

## 4. Event Topology

The system has three distinct event categories. They must not be conflated.

### 4.1 Workflow events

Workflow events are durable scheduling commands carried through the Outbox and workflow Kafka topic:

```text
turn.accepted
turn.context_ready
turn_run.requested
tool_call.requested
tool_group.completed
turn.cancel_requested
```

They trigger work and are consumed by the appropriate worker role.

### 4.2 Stream events

Stream events describe provider and tool activity for live delivery, replay, and complete-event accumulation:

```text
response.started
response.created
response.output_text.delta
response.output_text.done
reasoning.summary
tool.started
tool.completed
tool.failed
interaction.awaiting_input
interaction.completed
response.completed
response.failed
turn.done
```

They are not execution commands. A stream event must never be the only durable trigger for running a tool.

### 4.3 Presentation SSE events

The server projects internal stream and complete events into the existing frontend contract:

```text
turn.snapshot
item.upsert
item.delta
item.done
turn.done
conversation.updated
```

Raw provider events, tool arguments, raw tool output, and execution commands remain outside this API. See [API.md](./API.md#stream-api).

### 4.4 Transactional Outbox

Workflow events are published through a transactional Outbox. The Outbox is the reliable source of scheduling commands; Kafka is the transport and consumer fan-out layer.

The business state transition and Outbox insert must commit in the same PostgreSQL transaction:

```text
BEGIN
  update turn_run / tool_call / execution-group state
  insert outbox_events(event_type, conversation_id, turn_id, turn_run_id, ...)
COMMIT
```

The transaction must not publish to Kafka directly. A database commit means that the workflow command is durably queued, even if the Worker or Kafka is unavailable immediately afterward.

The current Outbox stores the stable event ID, event type, conversation/Turn/run references, claim lease, publish timestamp, and error information. Workflow consumers reload authoritative state by ID. Event-specific data that must remain immutable across retries must either be stored in durable state/artifacts referenced by the event or added to a versioned event payload; it must not depend on transient Worker memory.

The relay follows this protocol:

```text
1. Claim unpublished, unleased or expired events in created_at order.
2. Use FOR UPDATE SKIP LOCKED so multiple relay actors do not claim the same row.
3. Publish each event to the workflow Kafka topic using its stable event ID.
4. Mark published_at only after Kafka accepts the message.
5. Release or record an error for failed publishes and retry after the lease expires.
```

Publishing and marking the row cannot be one atomic transaction. A relay crash after Kafka accepts a message but before `published_at` is committed can publish a duplicate. The contract is therefore at-least-once delivery:

- Consumers must treat the Outbox event ID as an idempotency key.
- Reprocessing a completed `turn_run` or `tool_call` must be a no-op.
- Tool execution uses a stable operation identity in addition to the Worker attempt ID.
- A stale claim must not be allowed to overwrite a newer lease owner.

The relay should publish events outside the claim transaction and keep claim transactions short. It should process a batch independently so one failed event does not unnecessarily delay unrelated events in the same batch. Published rows are retained long enough for diagnosis and replay, then archived or deleted according to the workflow retention policy.

### 4.5 Outbox latency and wake-up strategy

The baseline reliability mechanism is periodic polling. The current Worker scans immediately and then at `WORKER_POLL_INTERVAL`, which defaults to two seconds. A newly committed event can therefore wait up to one polling interval before publication, in addition to Kafka and consumer scheduling latency.

The target relay uses a hybrid wake-up strategy:

```text
transactional Outbox insert
        |
        +--> PostgreSQL NOTIFY: wake the relay quickly
        |
        +--> periodic polling: reliable fallback when notifications are lost
```

`NOTIFY` is only a wake-up hint and never the source of truth. The relay still claims rows from PostgreSQL. This keeps the interactive path responsive without making correctness depend on a best-effort notification channel. Under high event volume, the relay may coalesce notifications and drain multiple rows in one batch.

The relay exposes at least:

```text
outbox_pending_count
outbox_oldest_event_age
outbox_claim_latency
outbox_publish_latency
outbox_publish_failures
outbox_duplicate_delivery_count
```

## 5. Turn and Run Lifecycle

A Turn may contain multiple model requests. Each model request is exactly one `turn_run` step.

```text
User message
  -> turn.accepted
  -> turn.context_ready
  -> turn_run.requested(step=1)
  -> LLM client worker: one upstream request
  -> tool execution plan, if required
  -> execution worker: one or more tool groups
  -> turn_run.requested(step=2)
  -> LLM client worker: next upstream request
  -> ...
  -> turn.completed / turn.failed / turn.cancelled
```

There is no cross-step `while` loop in a Worker. The next model request is a new durable Outbox/Kafka task. A Worker may use local loops for parsing a response or processing a batch, but those loops do not replace durable workflow boundaries.

The `turn_run` identity remains `(turn_id, step_index, attempt)`. A stale attempt is superseded by a new attempt for the same step. A new step is created only after the current model response and its required tool effects are durably settled.

## 6. Model Output Postprocessing

After a single upstream response completes, the LLM client worker performs model-output postprocessing without executing tools.

The postprocessing steps are:

1. Extract reasoning summaries and remove encrypted content before publishing reasoning events.
2. Extract function calls, remote MCP calls, unsupported approval requests, and continuation items.
3. Normalize provider-visible tool names against the current tool catalog.
4. Preserve the complete model output items for the next request context.
5. Build a `ToolExecutionPlan` for local function calls.
6. Persist the plan, tool-call records, and required artifacts.
7. Publish the first ready execution group through Outbox/Kafka.

When no tool execution is required, the worker persists the successful response/context state and completes the Turn through the normal workflow coordinator.

Remote MCP results returned by the upstream provider are recorded as observed tool results. They are not executed again by the local execution worker unless the tool contract explicitly identifies them as client-executed calls.

## 7. Tool Execution Plan

The plan is the durable boundary between model output and execution. A plan contains one node per model function call and enough information to resume execution without rereading an in-memory model response.

Each node must have:

```text
tool_call_id
tool_call_record_id
turn_id
turn_run_id
call_id
tool name and namespace
arguments artifact reference
execution group
dependency/barrier information
stable operation identity
```

The plan must distinguish the following concepts:

- Model output order: the order in which the provider returned call items.
- Execution order: the groups/barriers selected by the workflow policy.
- Completion order: the actual order in which tools finish.

Provider output order must not implicitly serialize calls when the request allows parallel tool calls. A call is serialized only when the plan gives it a dependency, a group barrier, or an execution capability requires mutual exclusion.

## 8. Ordering and Parallelism

### 8.1 Default policy

For `parallel_tool_calls=true`:

```text
Group 0: all ordinary independent tool calls, concurrently executable
Group 1: ask_user, if present
```

For `parallel_tool_calls=false`:

```text
Group 0: first ordinary call
Group 1: second ordinary call
Group 2: third ordinary call
...
Final group: ask_user, if present
```

The model parameter controls whether the model may emit multiple calls in one response. It does not execute client-side tools and does not replace the backend execution plan.

### 8.2 `ask_user` barrier

`ask_user` is always placed after all ordinary calls from the same response. It must not be started while an earlier ordinary group is still running.

If the ordinary group fails, the policy must explicitly define whether `ask_user` is skipped or still presented. The initial implementation should skip `ask_user` when a prerequisite group fails unless the plan explicitly marks it as failure-independent.

There may be at most one `ask_user` call in a model response. The existing validation rule remains.

### 8.3 Resource constraints

Parallelism is valid only for calls that the execution layer can safely run concurrently. Tool definitions or execution capabilities may declare a resource key or exclusive class, for example:

- A shared conversation mutation resource.
- A conversation sandbox lifecycle resource.
- A persistent shell session resource.
- An external account or document lock.

Calls with the same exclusive resource key must be placed in ordered groups even when `parallel_tool_calls=true`. Independent read-only tools may remain in the same group.

## 9. Actors and Concurrency

The current `WORKER_CONCURRENCY` is a generic workflow goroutine count. The target design gives the two worker roles separate actor pools.

In this document, one actor is one long-lived execution slot backed by one Go goroutine. There is no separate coroutine-count setting in the initial design:

```text
one actor = one worker goroutine = one in-flight Kafka task
```

The process has a default total actor budget of four. The two worker roles have separate actor settings, and their configured counts must fit within that process budget:

```env
WORKER_ACTOR_BUDGET=4
LLM_CLIENT_ACTORS=<configured allocation>
EXECUTION_ACTORS=<configured allocation>
LLM_CLIENT_POLL_INTERVAL=2s
EXECUTION_POLL_INTERVAL=2s
LLM_CLIENT_LEASE_TIMEOUT=2m
EXECUTION_LEASE_TIMEOUT=2m
```

For example, an even allocation is two LLM client actors plus two execution actors. The allocation is a deployment decision; the initial design does not assume that both pools default to four actors. The exact environment variable names may follow the existing configuration naming convention. The important rules are that LLM and execution capacity are independently configurable and that the process budget is enforced.

An execution actor consumes one `tool_call.requested` task at a time. Parallel tools are achieved by publishing multiple ready tasks and allowing different execution actors to claim them. An actor must not create unbounded child goroutines for tool calls.

If a future implementation needs one actor to multiplex multiple asynchronous operations, it may add an explicit in-flight limit. That is a separate optimization and is not required for the initial actor model.

The process uses a shared actor budget with separate pools. This preserves isolation: an upstream model stream must not consume all execution actors, and a slow tool must not prevent the system from issuing unrelated model requests. The deployment chooses the split while respecting `LLM_CLIENT_ACTORS + EXECUTION_ACTORS <= WORKER_ACTOR_BUDGET`.

## 10. Durable Execution State

The current `turn_runs` and `tool_calls` statuses are not sufficient for an asynchronous execution queue. The target schema needs explicit waiting and queued states.

At minimum:

- `turn_runs.waiting_tools` or an equivalent durable state while the model response waits for tool groups.
- `tool_calls.queued` before an execution actor claims a call.
- `tool_calls.running` while an execution actor owns the call lease.
- An execution group/batch identity.
- A stable operation identity independent of Worker attempt IDs.
- Dependency or group metadata sufficient to determine the next ready tasks.

The exact schema may use columns, a normalized dependency table, or a durable plan artifact plus indexed state. The choice must preserve transactional state transitions and efficient queries for ready calls.

## 11. Execution Transaction Boundaries

### 11.1 Plan creation

The LLM client worker commits, in one transaction where possible:

1. The normalized response/result references.
2. Tool-call records in `queued` state.
3. Execution group and dependency metadata.
4. The `waiting_tools` run state.
5. Outbox events for the first ready group.

The model request must not be reissued merely because tool execution is pending.

### 11.2 Tool claim

An execution actor claims one queued tool call using a lease/fencing token. The claim must be idempotent and must reject a stale actor.

### 11.3 Tool completion

The execution layer writes the output artifact before storing its durable reference. It then commits:

1. Tool status and output reference.
2. Complete semantic tool event or an Outbox event that will produce it.
3. Group progress.
4. The next ready tool task, or a group-completed event.

The system must not rely on a best-effort live stream publication as the only record of tool completion.

### 11.4 Next model request

Only after all required groups are complete does the coordinator commit:

1. The resumable context checkpoint containing tool outputs.
2. The next `turn_run` request/state artifacts.
3. The next `turn_run.requested` Outbox event.

## 12. Crash Recovery

### 12.1 LLM client worker crash

The model run lease expires and a requeue process creates a new attempt for the same `turn_run` step. Recovery checks deterministic request, response, result, and checkpoint artifacts before issuing another upstream request.

The provider request should use a stable idempotency identity when the provider supports one. Without provider-side idempotency, a crash after provider acceptance but before response artifact persistence remains an at-least-once request window.

### 12.2 Execution worker crash before tool completion

The execution lease expires. A new execution actor may claim the queued/running tool operation. The stable operation identity must be passed to the tool runtime where the external system supports idempotency.

For an uncertain external side effect, the system must not claim exactly-once execution without a provider/runtime idempotency or reconciliation API. It should mark the operation as uncertain or use a tool-specific reconciliation policy.

### 12.3 Execution worker crash after tool completion

If the output artifact and durable tool status exist, the next actor must reuse the stored output and not invoke the handler again. Completion event publication must be recoverable from the durable Outbox or complete-event store.

### 12.4 Lease fencing

Every run and tool state transition that can be performed by a Worker must validate the current lease/fencing token. A stale actor must not be able to complete a tool call after a replacement actor has taken ownership.

## 13. Stream and Presentation Behavior

The worker split does not change the frontend event contract.

The expected live sequence for a tool group is:

```text
tool.started       internal stream event
  -> item.upsert   presentation event

tool.completed or tool.failed
  -> item.upsert   same stable item ID with terminal status
```

Raw `tool_call.requested` execution commands never enter the presentation stream. The backend continues to construct sanitized tool presentations through the presentation registry.

The LLM client worker continues publishing provider output deltas and response lifecycle events. The execution worker publishes tool lifecycle events. Both are observed by the same presentation projection, but neither worker sends raw internal payloads directly to the frontend.

## 14. Migration Plan

### Phase 1: Make the plan explicit

- Extract model-output interpretation from direct execution.
- Introduce a durable plan representation and execution groups.
- Preserve current synchronous behavior behind the new plan interface.
- Add tests for ordinary parallel groups, serial mode, and `ask_user` as the final group.

### Phase 2: Add execution workflow events

- Add `tool_call.requested` and group completion workflow events.
- Add queued/waiting states and Outbox writes.
- Ensure a model run is not held open while tools execute.

### Phase 3: Split Worker pools

- Route `turn_run.requested` to the LLM client worker pool.
- Route `tool_call.requested` to the execution worker pool.
- Add independent actor and lease configuration.
- Keep per-conversation ordering in the coordinator rather than relying on process affinity.

### Phase 4: Harden recovery

- Add tool execution fencing.
- Add stable operation identities to tool runtimes.
- Make tool completion events recoverable through Outbox/complete-event transactions.
- Add crash and uncertain-outcome tests.

### Phase 5: Remove the old path

- Remove direct `ToolExecutor` calls from `ToolOrchestrator`.
- Remove the old synchronous tool loop.
- Remove any workflow slot configuration that accidentally controls execution capacity.

## 15. Testing Requirements

The implementation must include:

- The OpenAI adapter never invokes a tool executor.
- LLM client worker handles exactly one upstream request per `turn_run`.
- Execution worker is the only component invoking `ToolExecutor`.
- Multiple ordinary tools in a parallel group can execute concurrently up to the actor limit.
- `parallel_tool_calls=false` serializes ordinary calls according to the plan.
- `ask_user` never starts before ordinary prerequisite groups complete.
- Shared-resource tools are serialized by their resource policy.
- Group completion schedules exactly one next model run.
- Duplicate Outbox/Kafka events do not execute a completed tool again.
- Worker crash before, during, and after a tool call is recoverable.
- A stale execution actor cannot commit after lease replacement.
- An uncertain external side effect is not reported as exactly-once.
- Tool completion remains reconstructable when live delivery is lost.
- Existing presentation filtering and SSE replay tests continue to pass.

## 16. Acceptance Criteria

The target architecture is accepted when:

- The OpenAI adapter contains no tool execution dependency.
- The LLM client worker and execution worker have independent actor capacity and leases.
- A Turn with multiple model requests is represented by multiple durable `turn_run` steps.
- A model response with independent tools executes them concurrently when policy allows.
- `ask_user` is always a final barrier unless an explicit plan marks it otherwise.
- A model request is not repeated merely because its tools are still executing.
- Durable tool results can resume the next model request after an execution-worker crash.
- All frontend presentation events remain sanitized and backward compatible.
- The system documents at-least-once behavior for external side effects unless a tool provides a stable idempotency/reconciliation contract.
