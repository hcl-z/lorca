# Bot and Coding-Agent Interaction

Research checked on 2026-10-26. This document compares official agent interfaces with Lorca's current coding-agent implementation and recommends how a bot should operate Claude Code, Codex, and other coding agents, exchange messages with them, receive questions, and handle interruption and failure.

## Recommendation

Keep ACP over local stdio as Lorca's default provider-neutral boundary. Put a small, structured lifecycle and event model above the provider drivers; do not make the bot infer success or failure from transcript text. Preserve distinct outcomes for a completed turn, a requested interruption, an unexpected turn failure, a question, a permission request, and a process exit.

Use Codex App Server as an optional specialized driver only if Lorca needs Codex's active-turn steering or richer Codex-specific lifecycle/error details that the Codex ACP adapter does not expose. Keep a direct Claude Agent SDK integration optional for SDK hooks or deferred input that ACP adapters cannot expose. Do not make provider-specific APIs or JetBrains AIR metadata part of the common contract.

Lorca already implements most of the user-facing workflow: agents run as local processes on the Runner; a bot can start, read, message, configure, answer, and stop them; ACP permission requests go through Access and Auto-review, with a user approval card only when needed, while form elicitation becomes a question card; transcripts stream to the apps; and completion, question, stall, process exit, and restart interruption can wake the bot. The most important gap is semantic: prompt-level failures are written into the transcript but still reported to the supervisor as idle/finished.

## Interface comparison

| Interface | Best fit | Control and events | Trade-off for Lorca |
| --- | --- | --- | --- |
| ACP v1 | One client for multiple coding agents | Initialize and negotiate capabilities, create/resume sessions, send prompts, receive message/tool/plan/notice updates, handle permission requests and structured elicitation, cancel a prompt | Best common denominator. The protocol's stop reason and process exit must be mapped explicitly; an adapter may omit provider-specific details. |
| Codex App Server | A rich Codex client | JSON-RPC over local stdio or Unix socket; threads and turns; streamed items; `turn/steer`, `turn/interrupt`, approvals, user-input requests, and explicit completed/interrupted/failed turn statuses | More Codex detail and true active-turn steering, but Codex-specific and larger to maintain. The documented WebSocket transport is experimental and unsupported for production. |
| Codex SDK | Headless automation and jobs | TypeScript/Python APIs start/resume threads and run tasks; the SDK documentation directs rich clients to App Server | Useful for jobs and staged automation, not the strongest surface for Lorca's live approval and event workflow. |
| Claude CLI `stream-json` | A small scripted Claude integration | `claude -p` can emit JSONL events and accept stream-JSON input; output includes streaming messages and a final result, plus permission-denial/retry status where applicable. A configured MCP permission-prompt tool can route some approvals to a host. | Avoids writing an ACP adapter, but has a provider-specific event schema and CLI process/session semantics. The caller must carefully implement the long-lived input loop, cancellation, questions, and process errors; structured `AskUserQuestion` handling is documented on the SDK host API. |
| Claude Agent SDK | Claude-specific host integration | `query()` streams typed SDK messages; `canUseTool` handles permission requests and `AskUserQuestion`; hooks, resume, and abort are available. | Rich Claude integration, but adds a provider-specific path and a TypeScript/Python runtime. The callback is not invoked for actions already auto-approved by Claude's permission evaluation. |
| Codex CLI `exec --json` | Scripted, non-interactive Codex runs | Emits JSONL thread/turn/item/error events, supports output schemas, and can resume a previous run in a later invocation. | Good for durable jobs with predetermined approval/sandbox settings. It is documented as non-interactive; use App Server for active-turn steer and richer host interactions. |
| A2A | Communication with a remote agent service | Agent Cards advertise agent capabilities; tasks have ids, context, artifacts and lifecycle states including input-required, failed and canceled. Messages can be streamed, tasks can be queried/canceled, and optional push notifications report significant changes. | Useful if an agent is a separately deployed network service. It does not standardize local Claude/Codex process control or ACP-style tool approvals/forms; a gateway must map A2A tasks onto the provider runtime. |
| MCP control facade | Exposing Lorca's agent controls as tools to another MCP client | A server can expose `start`, `read`, `send`, `answer`, and `stop` tools; MCP also has progress and server-to-client elicitation primitives. | Useful interoperability surface, not a coding-agent runtime. MCP does not standardize the delegated agent's sessions, transcript, turn outcome, process lifecycle, or how each MCP host renders a question. |
| Lorca-owned coding loop | A coding agent implemented in Lorca | Use Lorca's own agent runtime, model adapters, tools, approval checks, and event model instead of launching Claude Code/Codex. | Maximum control over events, policy, and questions, but Lorca owns the coding-agent behavior and loses provider CLI features unless it rebuilds them. |
| PTY/TUI automation | A fallback for agents with no usable API | Launch the interactive terminal UI in a pseudo-terminal and send keystrokes/read screen output. | Fragile across versions, terminal sizes, prompts, and rendering changes; hard to distinguish tool output from UI and reliably detect errors. Use only as a last-resort adapter. |

ACP's flow is initialize, create/load/resume a session, send `session/prompt`, consume `session/update` notifications, then interpret the prompt response. Updates include streamed agent text, tools, plans, settings, usage, notices, and compaction. Permission requests are request/response interactions; elicitation is a separate structured-information interaction. See the [ACP initialization](https://agentclientprotocol.com/protocol/v1/initialization.md), [session setup](https://agentclientprotocol.com/protocol/v1/session-setup.md), [prompt turns](https://agentclientprotocol.com/protocol/v1/prompt-turn.md), [tool calls and permissions](https://agentclientprotocol.com/protocol/v1/tool-calls.md), and [elicitation](https://agentclientprotocol.com/protocol/v1/elicitation.md) specifications.

Codex App Server is explicitly positioned for rich clients, while the Codex SDK is positioned for automation. App Server exposes streamed item events, active-turn steering and interruption, and `turn/completed` with completed/interrupted/failed status and error data. Its WebSocket transport is documented as experimental and unsupported; local stdio is the suitable default transport for a Runner-owned process. See [Codex App Server](https://developers.openai.com/codex/app-server.md) and [Codex SDK](https://developers.openai.com/codex/sdk.md).

Claude's SDK routes both tool approvals and `AskUserQuestion` through `canUseTool`; a pending callback can wait for a user, while a hook can defer input so a process may exit and the session resume later. Streaming input supports a long-lived interactive session, queued messages, interruption, and real-time output; single-message mode does not. Critically, `canUseTool` does not run for actions auto-approved earlier in Claude's permission evaluation. See [Claude Agent SDK: approvals and user input](https://code.claude.com/docs/en/agent-sdk/user-input.md) and [streaming vs. single-message input](https://code.claude.com/docs/en/agent-sdk/streaming-vs-single-mode.md).

A2A is an alternative at a different boundary: the caller treats another agent as a remote task service. A2A models long-running work and artifacts, has explicit `INPUT_REQUIRED`, `AUTH_REQUIRED`, `FAILED`, and `CANCELED` states, and supports streaming, task lookup/cancellation, and optional webhook notifications. It is a better fit when agents are independently deployed or owned by other services. For a local CLI that already runs beside Lorca, A2A would require an additional gateway that wraps the CLI and translates its session, question, permission, and process events; it does not replace the ACP driver directly. A2A's input-required state carries a message but does not define ACP's form schema or per-tool approval exchange. See the [A2A specification](https://a2a-protocol.org/latest/specification/) and its [streaming and asynchronous operations](https://a2a-protocol.org/latest/topics/streaming-and-async/) guidance.

### ACP alternatives in more detail

**Provider SDKs.** A native SDK gives a host typed events and callbacks without parsing a provider's CLI stream. Claude's TypeScript SDK exposes an async `query()` stream, `canUseTool`, session resume, and abort; its Python SDK has the corresponding host API. Codex's SDK provides start/run/resume for scripted work. This is the best alternative when a product is intentionally provider-specific or needs a provider-only feature. Lorca is a Rust CLI, so a TypeScript/Python SDK means adding a sidecar process or an RPC bridge rather than calling a native Rust library. Claude's [SDK reference](https://code.claude.com/docs/en/agent-sdk/typescript.md) and OpenAI's [Codex SDK guide](https://developers.openai.com/codex/sdk.md) describe these surfaces.

**Provider CLI JSON/JSONL.** Claude Code's print mode supports `--output-format json` and `stream-json`, partial output events, and `--input-format stream-json` for streaming user messages to a live process. Its output includes initialization, partial messages, retry status, permission denials where applicable, and a final result; the CLI also supports an MCP permission-prompt tool. This is more capable than one-shot stdin piping, but it remains a Claude-specific CLI contract: the rich `canUseTool` callback and its structured `AskUserQuestion` response are documented as Agent SDK host APIs, and process signals are not the same typed host control surface as SDK interruption. Codex `exec --json` emits thread, turn, item, and error events and can resume a later invocation, but is documented as non-interactive. CLI JSONL is a reasonable lower-cost adapter for a prototype or batch workflow; verify long-lived input, interruption, and question behavior for the exact supported CLI versions before treating it as a full live-session driver. See [Claude headless mode](https://code.claude.com/docs/en/headless.md), the [Claude CLI reference](https://code.claude.com/docs/en/cli-reference.md), and [Codex non-interactive mode](https://developers.openai.com/codex/noninteractive.md).

**A2A gateway.** Wrap a coding agent behind an A2A server when the caller and agent are separate services, potentially on different machines or owned by different teams. Map an A2A `Task` to the provider's session, A2A messages to prompts/follow-ups, artifacts to outputs, and terminal/input-required states to Lorca's durable job and question events. Keep a mapping table for provider session ids and A2A task/context ids, authenticate both directions, and make webhook delivery replay-safe. This provides a standard remote boundary but adds a network service, authorization, task persistence, and a provider adapter; for a local agent process controlled by the same Runner it is extra infrastructure rather than a simpler replacement for ACP.

**MCP facade.** A Lorca MCP server could let another MCP-capable agent call Lorca's existing coding-agent tools. The facade would expose stable control tools (`agent_start`, `agent_read`, `agent_send`, `agent_answer`, `agent_stop`) and return an opaque Lorca agent id. Long work would need a clear polling or task/progress strategy. MCP supports stdio and Streamable HTTP transports, and supports server-initiated elicitation in which an MCP server asks its client for user input; however, this does not standardize the downstream Claude/Codex session state or guarantee that a host can show Lorca's rich cards. Prefer stdio for a local facade, or authenticated localhost-only HTTP when a separate process requires it. See [MCP transports](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports.md), [MCP tools](https://modelcontextprotocol.io/specification/2025-11-25/server/tools.md), and [MCP elicitation](https://modelcontextprotocol.io/specification/2025-11-25/client/elicitation.md).

**Own the coding agent.** Lorca already has an agent runtime and coding tools in `crates/agent`. The bot can be given file, search, shell, and patch tools directly, with Lorca's own approvals, interruption, questions, durable jobs, and outcome events. This avoids controlling another agent process and gives the cleanest internal event model. It is a product choice, not an adapter choice: model quality, tools, prompt/harness behavior, compaction, retries, subagents, and interactive coding-agent features become Lorca's responsibility. It is appropriate when consistent policy and event semantics matter more than access to Claude Code/Codex's native harness.

**Terminal UI automation.** A PTY wrapper can operate an agent that has no machine-facing protocol, but it should be isolated as a low-confidence compatibility backend. It cannot provide the same reliability as structured callbacks/events for question identity, permission decisions, interruption acknowledgement, or typed errors.

For Lorca specifically, the practical choice is therefore: keep ACP for the supported multi-agent baseline; fix its common lifecycle mapping; consider Codex App Server for a concrete Codex-only requirement; and use a native SDK or provider CLI JSONL only when a measured provider-specific requirement justifies a second adapter. Consider A2A if Lorca later needs a remote agent-service boundary, and an MCP facade if external MCP hosts need to invoke Lorca. Neither is a replacement for the local driver by itself.

## What Lorca already has

The implementation and current behavior are described in [Coding agents](../architecture/coding-agents.md), [Questions](../architecture/questions.md), [Coding agent cards](../architecture/agent-cards.md), and [Bot permissions](../architecture/bot-permissions.md). In brief:

- `crates/cli/src/coding/acp.rs` drives Claude, Codex, and other ACP agents as local child processes. The adapter initializes sessions, sends prompts, handles streamed updates, resumes/loads sessions, cancels turns, changes offered settings, and translates permission and form requests.
- `crates/cli/src/coding/driver.rs` is the common driver/event boundary; `crates/cli/src/coding/mod.rs` supervises process and session state, persists agent records, creates cards, and wakes the bot.
- The bot's `coding_agent` tool supports `start`, `read`, `send` (with optional interruption), `answer`, `set`, `stop`, `list`, and `commands`.
- A user question is a separate durable card. The bot is also woken with the question, but is instructed to answer only when the user's intent is clear; the user and bot race to answer the same request. Permission to perform an action follows a different Access/Auto-review path and only reaches a user card when review asks.
- Agent transcript entries have sequence/revision values and are secret-redacted. Pending bot news is a persisted `News` value; an unanswered question, finished work, stall, process end, or Lorca restart interruption can trigger a bot turn.
- The adapter's `interrupt: true` cancels the active ACP prompt, then sends the replacement message first. An ordinary follow-up queues behind the active prompt. Stopping closes the session when supported, then stops the process.
- Lorca advertises ACP form elicitation but not URL elicitation. It declines URL requests, which is appropriate for sensitive or externally hosted flows unless Lorca adds an explicit, consented URL-handling surface.

## ACP capabilities in practice

ACP is the local agent-control protocol, not a bot API and not a webhook service. Lorca starts an agent process on the Runner and is the ACP client. The two processes exchange JSON-RPC messages over the agent's standard input and output. The bot uses Lorca's `coding_agent` tool; it does not speak ACP directly. This keeps the agent process, session id, file paths, and provider-specific details on the Runner, while Lorca sends the existing encrypted chat projection to paired Devices.

An ACP session has a small set of control operations: initialize and negotiate capabilities; create, load, or resume a session; send a prompt; change offered session options; cancel the active prompt; and close the session where supported. A session update is a progress projection, not necessarily a new bot turn. The prompt response and child-process exit are separate outcome sources.

| ACP surface | Typical information | What Lorca does with it |
| --- | --- | --- |
| `session/update` notification: agent message and thought chunks | Streaming text, and optional thought text | Streams the agent's answer to its transcript. Thought entries are shown to the apps, not included in the bot's transcript. |
| `session/update`: tool call and tool-call update | Tool name/title, input, running/completed/failed status, output, exit code, and file diffs when the agent supplies them | Updates a structured transcript entry. This is observability; it does not itself pause the tool or authorize it. |
| `session/update`: plan | Steps and their status | Updates the plan in the transcript/card. A request to leave plan mode is a separate permission request. |
| `session/update`: settings and available commands | Model, effort, mode/config options, title, slash commands | Updates the card and controls; the bot can list offered commands and send one as a message. |
| `session/update`: usage, notice, compaction | Reported token/context/cost fields, informational notices, and compaction status/summary | Updates the card/transcript. An error-severity notice is still only a notice; it does not mean the turn failed. |
| `session/request_permission` request | A tool/action, its input or locations, and the decision options the agent offers | Runs Lorca's Access and Auto-review path for this request. If review requires the user, Lorca waits on an approval card and returns the selected one-time option or a rejection. |
| `elicitation/create` request | A message and a JSON-schema form, or a URL-style request | A form becomes a question card. The bot and user may answer it; the first valid answer wins. Lorca does not advertise URL elicitation and declines it. |
| `session/prompt` response | A stop reason such as normal end, cancellation, refusal, or a turn limit | Settles the prompt. The adapter must map the result explicitly; transcript text alone must not decide success. |
| Child-process exit / stderr | Exit code, whether Lorca stopped it, and diagnostic output | Lorca's process supervisor reports an unexpected exit or failure. This is a local process event, not an ACP session update. |

The common standard update names include `agent_message_chunk`, `agent_thought_chunk`, `tool_call`, `tool_call_update`, `plan`, `available_commands_update`, `config_option_update`, `current_mode_update`, `session_info_update`, `usage_update`, `notice`, and compaction updates. Protocol versions and agents differ: an agent may omit a category, send extension metadata, or never issue a particular request. Lorca ignores updates it does not recognize. In particular, ACP defines no universal `process_crashed` notification: the Runner must monitor the child process itself. Likewise, ACP's `notice` does not replace a typed turn outcome.

### How the bot talks to the agent

The user talks to their bot in the normal chat. The bot translates a coding request into `coding_agent` actions; Lorca translates those into ACP session operations:

| Bot action | `coding_agent` operation | ACP-side effect |
| --- | --- | --- |
| Delegate a job | `start` with `agent`, `prompt`, `folder`/`worktree`, optional `plan`, `model`, `effort`, and named `secrets` | Start or resume the local agent, open a session, and send the initial `session/prompt`. |
| Inspect progress | `read` or the live card/transcript | Read the new transcript lines or current card state. Progress deltas update the card; they do not wake the bot one by one. |
| Continue the conversation | `send` with a message | Queue a follow-up prompt after the current turn. With `interrupt: true`, send `session/cancel`, then put the replacement prompt first in the queue. ACP cancellation is not in-place steering: it ends the current prompt before the replacement is sent. |
| Answer a question | `answer` with field values, or `decline: true` | Reply to the outstanding `elicitation/create` request. The question card is also available for the user to answer. |
| Change model/effort | `set` | Call `session/set_config_option` for options the agent advertised. Modes are not changed through this bot operation. |
| Stop or inspect controls | `stop`, `list`, or `commands` | Close the session if supported, stop the local process, or read session/agent metadata. `commands` can be followed by sending the selected slash command as a message. |

When the agent finishes, asks a question, stalls, or exits unexpectedly, Lorca records pending news and schedules a bot turn when the chat is available. That turn receives a short cue and can call `read`, `send`, `answer`, or `stop`. A running agent's ordinary progress is not an event stream delivered as separate model turns; use its transcript/card for that. This separation avoids spending a bot turn on every streamed token or tool-status change.

### Permission boundary

ACP standardizes a request/response point for an agent to ask the client to authorize a specific action. The agent supplies the action and the choices it supports; the client selects an offered choice. ACP does not define a complete access policy, an OS sandbox, or a guarantee that every shell command and file operation must generate a permission request. A `tool_call` update is only a report of a tool call. A permission callback is a gate only for actions the agent actually routes through `session/request_permission`.

Lorca handles permission requests in layers:

1. The starting bot's current Access must allow shell/coding-agent use. A request that reaches Lorca is refused if that grant has been removed.
2. Auto-review checks the action against the bot's task, user rules, and local facts. Clear, low-risk work may pass automatically; held actions go back to the agent with a reason; an action requiring user consent becomes a card.
3. When a card is required, the user may allow or deny it, and some reviews offer Always allow to save a Lorca Auto-review rule. Even then, the response to this ACP request selects only the agent-provided one-time allow/reject option; Lorca does not select ACP's persistent `allow_always` option.

This policy applies to the requests Lorca receives, not every capability of the child process. The process runs as the Runner's user and inherits the login shell's environment, files, processes, and network; Lorca may also inject named saved secrets for the job. Lorca does not advertise ACP filesystem or terminal client capabilities, so these agents use their own tools. An agent's configuration may let it act without asking, and an ACP adapter may not expose all provider-native hooks. Therefore a transcript, an agent permission callback, or Lorca's worktree is not a containment boundary. If the product requirement is that every read/write/command must be authorized, run the agent in an OS-enforced sandbox or route execution through client-owned tools that Lorca controls; verify that the chosen agent cannot escape that boundary.

Do not conflate three distinct interactions:

- **Progress** (`session/update`) tells the bot and user what is happening; it does not request a decision.
- **Question** (`elicitation/create`) asks for user intent or missing information; it does not grant tool access.
- **Permission** (`session/request_permission`) asks to authorize one proposed action; an answer does not answer unrelated questions or create a general grant.

## Event contract

Keep provider payloads and bot-facing lifecycle events separate. ACP updates are useful transcript material, but not every update should wake the bot. A small normalized event can carry only the information needed for state, routing, notification, and recovery:

```json
{
  "event_id": "evt_01J...",
  "agent_id": "agent-...",
  "session_id": "session-...",
  "turn_id": "turn-...",
  "seq": 42,
  "kind": "turn_failed",
  "occurred_at": "2026-10-26T07:00:00Z",
  "payload": {
    "category": "provider_limit",
    "message": "The agent reached its token limit.",
    "retryable": false
  }
}
```

Recommended event classes:

| Event | Meaning | Bot action |
| --- | --- | --- |
| `message_delta`, `tool_started`, `tool_updated`, `plan_updated`, `notice` | Live progress or presentation. These can update a transcript/card and be coalesced. An ACP notice is not a terminal outcome. | Do not wake on every delta; notify only when configured or actionable. |
| `question` | The agent requests structured information. Include request/card id, schema, deadline, and current resolution state. | Wake the bot once; answer only when user intent settles the choice, otherwise leave it for the user. |
| `permission` | An action needs authorization. Include the action, affected paths/command, policy result, and request id. | Run existing Access/Auto-review/card policy. Never treat it as a general question. |
| `turn_completed` | The prompt completed normally. | Publish outputs, then wake the bot to inspect and report results. |
| `turn_interrupted` | The client or user intentionally cancelled the active turn. | Do not classify as failure; send the queued replacement prompt if one exists. |
| `turn_failed` | The prompt returned a provider/protocol failure, refusal, limit, or invalid empty response. | Wake with a machine-readable category and recovery guidance; do not claim success. |
| `process_exited` | The agent process ended, with exit code, stderr tail, and whether it was expected. | Distinguish a clean idle exit, explicit stop, crash, and unexpected exit; resume only when the session supports it. |

Persist state-changing events or an equivalent durable pending-news record before waking the bot. Include a stable event/request id and monotonic sequence so app projections can suppress duplicates and reconcile after reconnect. Progress deltas may remain in the bounded transcript rather than a permanent event journal. The relay should continue to see only encrypted, signed device-to-device data; the agent process and its local control channel stay on the Runner.

## Correct outcome semantics

ACP v1 distinguishes prompt stop reasons including `end_turn`, `max_tokens`, `max_turn_requests`, `refusal`, and `cancelled`. A client-sent `session/cancel` should settle as cancelled. A `notice` with severity `error` is explicitly presentation-only: it does not fail a request, stop work, change session state, or imply a prompt stop reason. Use the prompt response, JSON-RPC error, and process lifecycle as outcome sources, not the notice severity. See [ACP prompt turns](https://agentclientprotocol.com/protocol/v1/prompt-turn.md#stop-reasons) and [session notices](https://agentclientprotocol.com/protocol/v1/prompt-turn.md#session-notices).

In Lorca, `turn_ended` detects refusal, limits, unexpected cancellation, JSON-RPC errors, and a turn that produces nothing. It writes `Error: ...` into the transcript but returns `Option<String>`. The caller emits `Event::Idle(said)`, and the supervisor unconditionally changes state to `idle` and writes `News::Finished`. The existing regression test explicitly expects the bot cue to say “is done” after “ended its turn without answering.” This conflicts with the current [Coding agents architecture doc](../architecture/coding-agents.md), which says those outcomes are errors for the bot. The implementation and architecture description therefore disagree: the transcript records the error, but the bot wake cue currently classifies it as done. See `crates/cli/src/coding/acp.rs` (`turn_ended` and the prompt loop), `crates/cli/src/coding/mod.rs` (`Event::Idle`, `News::Finished`, and `wake_cue`), and `crates/cli/src/coding/tests.rs` (`an_acp_agent_refused_a_command_goes_on_and_one_that_says_nothing_is_not_called_done`).

This is not a loss of the textual error: a person or bot that reads the transcript can see it. The gap is that status, card tone, pending news, and bot wake cue classify it as completed work, contrary to the architecture description. A bot cannot safely automate a retry or tell the user that the turn failed from structured state.

### Failure recovery: let the bot continue the same agent

The recovery target should be the existing `agent_id`, not a new agent. The handle identifies the worktree, transcript, saved settings, and ACP session record. A bot can then decide whether to continue, ask the user, or stop without losing the task identity.

| Failure situation | Process/session state | Safe continuation |
| --- | --- | --- |
| Prompt failed but ACP process is still alive (provider error, limit, empty answer) | The session remains open and the driver is available | Keep the same agent id and send a diagnostic follow-up. The agent should inspect its transcript and worktree before doing more work. |
| Agent process exited unexpectedly, session id is saved | No live driver; the session may be resumable | Restart the agent process and use `session/resume` when advertised, otherwise `session/load` when supported. Send the continuation only after the session is restored. |
| Resume/load is unsupported or fails | The old context cannot be trusted | Do not silently claim continuity. Mark the recovery as degraded, tell the bot that a fresh session would lose context, and require an explicit decision to start fresh with a generated summary. |
| Start/handshake failed before a session was opened | No usable session id | Retry the start only for a classified transient setup error; otherwise ask the bot/user to fix installation, sign-in, working directory, or provider configuration. |
| User cancellation, permission denial, agent refusal, invalid request | Intentional or non-retryable | Do not automatically retry. Let the bot explain the reason and ask for a changed instruction or explicit user decision. |

The continuation prompt should be an instruction to recover, not a blind replay of the original prompt:

```text
The previous turn failed: <category>: <message>.
Inspect the current transcript and worktree, identify what completed and what did not,
then continue the original task from the last verified state. Do not repeat completed
steps. First state the recovery plan briefly; stop and report if the required context
or permission is unavailable.
```

Default to a bot-directed continuation, not replaying the original prompt. A bounded automatic retry is safe only when Lorca knows the failure happened before the agent could execute any tool; give it a stable attempt id and backoff, and record it as a new `turn_id`. A transport reset or process crash after a tool started has an unknown outcome: first resume/read the session and inspect the worktree, then continue without repeating completed actions. Refusals, policy denials, malformed requests, user cancellation, and an agent asking a question are not automatic-retry cases. A token or step limit normally needs a shorter, staged continuation rather than an identical retry.

The bot-facing wake-up should be explicit:

```text
[Claude Code (agent-123) failed its turn: provider_limit. It is still available.
Read the transcript, then use coding_agent send with a recovery instruction, or tell
the user why it cannot continue. Do not report the job as completed.]
```

If the process exited, the cue should say that Lorca will try to resume the saved session when the bot sends `coding_agent send`. The existing Lorca path already does this: `send` uses the live driver when present, and when it is absent it launches the installed agent with the saved session id. At present, if the selected resume/load operation fails, the driver starts a fresh session and adds a note that earlier context could not be restored. The bot must treat that as degraded recovery and restate the task from the saved transcript/worktree, not claim the old conversation continued. The recovery work is therefore mostly about truthful failure state and bot guidance, not a second control API. The implementation should make a failed prompt distinguishable from a clean idle turn, and should surface when resume fell back to a new session instead of hiding that loss of context.

### Recommended ACP optimization plan

Keep the current ACP process/session driver and `coding_agent` tool. Put a typed turn lifecycle and recovery policy between `acp.rs` and the bot; do not make the bot parse ACP JSON or infer outcomes from transcript text. The primary behavior is: classify one turn, persist its result, wake the bot with that result, and let it continue the same `agent_id` after inspecting the current state.

#### 1. Separate agent state from turn outcome

`Agent.state` answers whether the coding-agent handle can currently receive work (`starting`, `working`, `idle`, `asking`, `stopped`, `exited`, or `failed`). A separate `last_turn` answers how the most recent prompt ended. A failed prompt with a live ACP process is therefore `Agent.state = idle` and `last_turn = failed`; the card can say “Turn failed; session available” rather than making a live session look dead. A process crash is `Agent.state = failed` with a process-exit outcome, while its last turn result remains available for recovery.

Replace `Event::Idle(Option<String>)` with a typed event. Keep raw protocol details for diagnostics, but give the supervisor and bot a normalized result:

```rust
enum TurnOutcome {
    Completed { last_message: Option<String> },
    Interrupted { requested: bool },
    Failed {
        category: FailureCategory,
        message: String,
        raw_stop_reason: Option<String>,
        retry: RetryHint,
        effects: EffectCertainty,
    },
}

enum RetryHint {
    Never,
    InspectThenContinue,
    SafeToRetry,
}

enum EffectCertainty {
    NoToolCallObserved,
    ToolCallObserved,
    Unknown,
}
```

`FailureCategory` should start small and stable: `provider_error`, `rate_limited`, `token_limit`, `step_limit`, `refusal`, `permission_denied`, `unexpected_cancel`, `rpc_error`, `empty_response`, and `process_exit`. Store an internal `turn_id`, attempt number, start/end time, raw ACP stop reason, and whether the process is still alive. The category is Lorca's contract; the raw ACP/provider data is diagnostic context and may be absent.

#### 2. Make recovery explicit and safe

Use the same `agent_id`, saved worktree, transcript, and session record for recovery. `coding_agent send` remains the normal continuation path:

1. If the driver is alive, send the recovery instruction on the existing ACP session.
2. If the process exited, first attempt the saved session's advertised `session/resume`, otherwise `session/load` if supported.
3. If restore fails, do not silently call `session/new`. Return a structured `session_restore_failed` result and let the bot tell the user that continuity was lost.
4. Only after an explicit fresh-context decision, start a new ACP session under the same Lorca agent handle and worktree. The bot must seed it with the original task, failure cause, last transcript, current git status/diff, completed work, and remaining work. Label this as a reconstructed continuation, not a resumed session.

On a `turn_failed` wake-up, instruct the bot to read the latest transcript and inspect the worktree before sending a targeted continuation. Do not blindly replay the original prompt. An interrupted turn with a replacement prompt is not a failure; an explicit user stop is not retried. A question or permission denial remains pending/user-directed and must not be transformed into an automatic retry.

Automatic retry is opt-in and narrow. “No tool call observed” is not proof that no side effect happened: ACP only reports what the agent/adapter exposes. Use `SafeToRetry` only when the driver has positive evidence that the prompt was rejected before execution began. Use `InspectThenContinue` after any observed tool activity, a process crash, a lost response, or any ambiguous result because an external side effect may already have happened. Use `Never` for refusal, policy/permission denial, malformed requests, user cancellation, and unresolved elicitation. Apply a small retry limit and backoff only to `SafeToRetry`; retries get a new `turn_id` and attempt id. Retry must re-enter the same Access and Auto-review checks, with no permission grant carried forward.

#### 3. Persist a useful, deduplicable failure wake-up

Add `News::TurnFailed { turn_id, category, message, retry, effects }` (or an equivalent typed `News::TurnEnded`) to the agent record. Save it and the card state before waking the bot. The wake cue should include only the facts needed to decide the next action:

```text
[Claude Code (agent-123) turn 4 failed: rate_limited. The session is still live;
no tool call was observed. Read the transcript and inspect the worktree, then continue
with coding_agent send.
Do not report the task as complete.]
```

For an exited process, name the exit code and say whether Lorca will try restoring the saved session. For a failed restore, say context is unavailable and offer a fresh-context continuation. Use `(agent_id, turn_id, outcome_revision)` as the idempotency key so duplicate supervisor notifications do not create duplicate bot turns. Reading the transcript may acknowledge the news as today, but it must not erase the durable failure outcome from the card.

#### 4. Preserve clear user-facing states

Show two independent facts on the agent card: current session availability (`working`, `idle/live`, `asking`, `exited`) and last turn outcome (`completed`, `interrupted`, `failed: reason`). Keep the transcript's error note, but make the structured status authoritative. An agent whose turn failed but whose process is alive should offer Continue and Stop; a dead process should offer Resume; a failed restore should offer Start fresh with context. A user-facing question remains a question card, not an error.

#### 5. Delivery sequence

| Phase | Change | Acceptance criteria |
| --- | --- | --- |
| A. Truthful outcomes | Add typed `TurnOutcome`; map ACP `StopReason`, RPC errors, empty output, and explicit cancellation; retain process exit as a separate event. | Refusal, token/step limit, empty answer, RPC error, requested cancellation, unrequested cancellation, and normal completion each produce distinct tested outcomes. Error notices never fail a turn by themselves. |
| B. Bot recovery | Persist `News::TurnFailed`; teach the wake cue not to claim completion; use existing `coding_agent send` for a live session or restore. | A failed-but-live agent wakes its bot, can receive a follow-up with the same id, and the bot inspects state before continuing. |
| C. Honest session restore | Return whether a run resumed, loaded, created new, or failed to restore. Remove silent fresh-session fallback from recovery; add an explicit fresh-context path. | Tests prove resume/load reuse the same session, failed restore does not silently create one, and explicit fresh continuation preserves the Lorca agent id/worktree while reporting lost provider context. |
| D. Retry safety and delivery | Add retry hints, effect certainty, attempt ids, event idempotency, and bounded retry policy for provably pre-execution failures only. | No duplicate tool execution after ambiguous transport/process failure; retries do not bypass Access/Auto-review; stale or duplicate wake-ups are consumed once. |
| E. Projection and recovery tests | Update agent card/API projections and restart behavior; close pending permission/question requests when their process/session is gone. | Desktop and phone distinguish failed turn from dead agent; restart tests cover pending questions, pending approvals, saved session recovery, and fresh-context fallback. |

Phase A and B deliver the user's main requirement. C is required before claiming reliable same-session continuation after a process crash. D should follow only after the outcomes and recovery path are correct; automatic retries are not required for the bot to continue the same agent safely.

### Direct user conversation and intermediate artifacts

Lorca should support two additional interaction paths without turning the coding agent into a second, uncontrolled chat participant:

- **User-to-agent:** the user can address a running coding agent directly, and the agent's answer appears in the chat with `Message.agent` identifying its author.
- **Agent-to-chat artifacts:** a screenshot, log, recording, or other intermediate file the agent produces can become a chat attachment, with the bot notified as its supervisor and the user told what it means.

These are separate from ACP client/server mechanics. ACP carries prompts, updates, permission requests, and elicitation; Lorca owns chat identity, durable turns, attachment transport, notification policy, and bot supervision.

#### Direct user conversation

Treat a direct message as a new turn on the same agent, not as a second owner of the session. The chat row needs a stable target: `agent_id` and `message_id`, the same handle that `coding_agent` and `coding.transcript` already use. A direct user message is a `Message` with `Author::You` and a reply target or a small typed routing reference to the agent; it is not `Author::Bot`. The agent's response uses the existing `agent` metadata on the bot's message rather than inventing a new author enum, so older clients still render it as its bot's message.

The route should be:

1. The user writes in the chat while naming an agent, or uses the agent card's composer. The message is persisted and gets a stable `chat_message_id`.
2. Lorca sends its text to the agent through the same `send` path as `coding_agent send`, including queued/interrupt behavior. If the agent is dead, the recovery rules above apply.
3. The ACP `session/update` text and entries continue to update the card. At turn completion, the agent's durable chat reply is a bot-authored `Body::Text` with `agent = Some(AgentAuthor { id, name, kind })`; it says, for example, "Claude Code: I need the API base URL before I can run the integration test."
4. The bot receives a delegated-message cue on its next turn, not a pretend user instruction. The cue must identify the author as the agent and include the parent `chat_message_id`, `agent_id`, and turn id. The bot should summarize, ask, or continue supervision; it must not treat the agent's words as the user's consent.

Do not create a public "agent can send arbitrary instructions to a bot" capability. The user talks to the agent; the bot observes and supervises. If the agent output needs a follow-up from the bot, that follow-up should use the existing `coding_agent send` tool and remain visible in the transcript. This preserves a single approval and ownership chain.

Concurrent input needs explicit ordering. Keep one prompt in flight per ACP session and use the existing queue for ordinary follow-ups. A direct user message and a bot message must not race to become "the next prompt" without a deterministic rule: FIFO by durable arrival time, with an explicit `interrupt: true` for deliberate preemption. A message accepted while the agent is asking a question should either answer that exact pending elicitation (when it is clearly an answer) or be queued as a new turn; it must not be sent as an unrelated prompt that leaves the form hanging.

The transcript tab and the chat must not become two competing histories. The card remains the detailed agent view; the chat gets concise agent-authored replies and selected artifacts. A direct-message thread should show the user's message, the agent's answer, and any question/permission cards in chronological order. The agent card and chat should share ids, not copy state independently.

Direct conversation also needs an ownership and visibility rule. Only a user in the bot's chat, or a bot with a valid delegation to that agent, can address it. Contacts in a channel cannot address a coding agent directly unless a bot relays the request under its own policy. The agent's output inherits the chat's audience; a screenshot or file from a private worktree must not be pushed to a channel merely because the agent produced it.

#### Intermediate artifacts

ACP provides content blocks for messages and tool results, but transcript content is not a reliable file-delivery API. An agent can provide a `resource_link`, a file path, a screenshot path, or image content depending on its adapter, and not every adapter emits the same form. Treat these as candidate artifacts, not as automatically published deliverables.

Use a staged pipeline:

1. **Detect:** during `session/update`, extract explicit file references, image content, `resource_link`s, and paths from tool results. Never upload a path merely because its name looks like `screenshot.png`; require that the agent named it, it is inside the agent's worktree or a declared scratch directory, it exists as a regular file, its media type is recognized, and it passes size limits.
2. **Register:** copy the bytes into the private attachment store with a fresh attachment id. Keep provenance on the artifact record: `agent_id`, `turn_id`, `tool_call_id` or message id when known, source relative path, creation time, MIME type, dimensions, size, hash, and a sensitivity flag. Upload its encrypted `file` blob and queue it before the chat row that references it.
3. **Classify:** distinguish a final output (a diff, test report, or a screenshot the agent explicitly presents as proof) from an intermediate artifact (a debug capture or temporary log). Final outputs use the existing `outputs.publish` path and can be cited as evidence. Intermediate artifacts use a normal message attachment, or an output with `unverified` evidence if they are worth keeping.
4. **Notify:** do not wake the bot for every file. An explicit `publish_artifact` request wakes the bot once; a completed turn with an artifact summary wakes it once at the end; manual "show me" from the user streams the selected artifact immediately. Debounce and coalesce artifacts by `agent_id` and turn.
5. **Tell:** the bot receives a machine-readable artifact summary (`name`, kind, dimensions, size, path provenance, tool/message id, whether it is final or intermediate, and the chat message id) plus a short extraction of nearby text. It can then tell the user "the mobile layout screenshot shows the nav overlap" rather than dumping an unexplained image.

For screenshots specifically, the preferred UX is:

- The agent saves a screenshot in its worktree or Lorca's declared agent scratch folder.
- Lorca copies it, validates it as PNG/JPEG/WebP/GIF, and creates a chat message with an image attachment.
- The bot gets a cue such as: `[Claude Code (agent-123) produced a screenshot after the mobile layout change: shot.png, 390x844 at msg-... . Explain the visual result to the user; publish it as output evidence only if it verifies the task.]`
- The card shows the artifact inline or as a thumbnail; the chat shows the image and a short agent/bot explanation. The user can open the original, ask for another, or tell the bot to publish it as evidence.

If the underlying adapter can send image content directly, use the same staging and provenance rules as a file path. Inline image bytes should not be trusted as already uploaded; convert them to an attachment, hash them, and queue their encrypted blob. If the platform limits a model's image input, keep the file and give the bot a textual description plus dimensions/name instead of silently dropping it.

A good artifact API would be small:

```text
coding_agent { action: "artifacts", id }
  -> [{ artifact_id, message_id?, name, mime, size, width?, height?, kind,
        final, source, redacted }]

coding_agent { action: "publish_artifact", id, artifact_id, name?, evidence? }
  -> output reference

coding_agent { action: "attach_artifact", id, artifact_id }
  -> chat message id
```

`artifacts` lists only artifacts Lorca has staged; it never walks the worktree on a whim. `publish_artifact` makes a durable output; `attach_artifact` puts a normal image/file in the conversation. Both re-check the bot's Access and redact saved secrets before upload.

#### Security and retention

- **No automatic screenshot ingestion from arbitrary paths.** Allow only a declared artifact directory, the agent's worktree, or an explicit tool-result path; canonicalize and verify the path remains inside the allowed root. Reject symlinks, special files, oversized files, and unknown MIME types.
- **Treat images and logs as potentially sensitive.** Apply existing secret redaction to text and metadata; do not OCR or describe an artifact automatically with a model unless the user opted in. A browser screenshot can contain credentials, tokens, customer data, or private messages. Apply the same attachment retention and chat deletion rules as user-uploaded files.
- **Separate "agent produced" from "user approved for sharing."** A file in the private chat may still need review before sending to a channel, teammate, or external service. The artifact's provenance and sensitivity must travel with it.
- **Bound resource use.** Cap files per turn, bytes per artifact, total bytes per agent, image dimensions, and text extraction size. Deduplicate by hash so retries and repeated screenshots do not create multiple blobs. Rate-limit notifications and keep a bounded artifact list per agent.
- **Make failures visible.** If extraction, validation, encryption, upload, or publishing fails, record a short reason on the agent card and tell the bot at the next wake. Never claim an artifact was delivered when only its path was seen.

#### Implementation sequence

| Phase | Change | Acceptance criteria |
| --- | --- | --- |
| F. Direct message routing | Add an agent-targeted user message and a `coding.message` path that uses the existing `send` queue; set `Message.agent` on agent-authored replies. | User can ask an agent directly; the answer appears once, links back to the same agent/turn, and FIFO/interrupt ordering is deterministic. |
| G. Bot visibility | Add a delegated-message cue with agent id, parent message id, turn id, and a clear "not user consent" marker. | Bot can summarize or act on the agent's reply without treating agent text as user authorization; no duplicate bot turn. |
| H. Artifact staging | Detect explicit file/image content, validate and copy it into the attachment store, record provenance, and expose `artifacts`. | Screenshot and log paths from a turn can be listed; traversal, symlinks, oversized files, and unknown types are rejected; bytes are encrypted and queued. |
| I. Chat delivery and notification | Add `attach_artifact`/`publish_artifact`, display intermediate images in the card/chat, and coalesce bot notifications per turn. | The user sees the screenshot in the chat; the bot gets one actionable summary, not one wake per artifact; deletion/retention matches other attachments. |

The direct-conversation path and artifact staging should share the same `agent_id` and `turn_id` model as failure recovery. That keeps a single audit trail: what the user asked, what the agent did, what it produced, what the bot observed, and whether the artifact was only intermediate or became a final deliverable.

Recommended change when implementation work is scheduled:

1. Implement phases A and B first so a failure reliably wakes the bot with a truthful outcome and the existing agent handle accepts a continuation.
2. Implement phase C before promising process-crash recovery; a resume failure must not quietly become a new ACP session.
3. Implement phases D and E after the lifecycle contract is stable, with no automatic retry for ambiguous side effects.
4. Keep `notice` as notice. If an error notice needs to wake the bot, introduce an explicit durable incident event from provider-specific structured data; do not infer turn failure from `severity: error`.

Codex App Server already exposes richer native turn status, including `failed` with error details, `interrupted`, and active-turn `turn/steer`; its `tool/requestUserInput` request is experimental. The Claude and Codex ACP adapters also document an opt-in JetBrains AIR `sessionFailure` extension for typed transcript failures. AIR is adapter-specific, experimental metadata and should be parsed only as an optional enhancement with a standard-ACP fallback, not used as Lorca's shared failure contract. See [Codex App Server events](https://developers.openai.com/codex/app-server.md#events), [Codex ACP AIR extensions](https://raw.githubusercontent.com/agentclientprotocol/codex-acp/main/docs/air-extensions.md#session-failure), and [Claude ACP AIR extensions](https://raw.githubusercontent.com/agentclientprotocol/claude-agent-acp/main/docs/air-extensions.md#session-failure).

## Authentication and product boundary

Anthropic's Agent SDK overview says third-party products may not offer claude.ai login or its rate limits without prior approval, including products built on the Agent SDK; it directs SDK integrations to API-key authentication. The SDK quickstart documents API keys and supported cloud-provider credentials. Lorca's current ACP adapter launches the user's installed Claude Code CLI and uses that CLI's existing sign-in, rather than embedding the Agent SDK. This research does not determine the terms that apply to Lorca's current distribution or user's CLI credentials; it does mean a move to the Agent SDK, or a product workflow that offers Claude subscription login/limits, needs an explicit terms review and likely a different credential path. See the [Agent SDK overview](https://code.claude.com/docs/en/agent-sdk/overview.md) and [quickstart authentication](https://code.claude.com/docs/en/agent-sdk/quickstart.md).

## Safety and recovery

- **Keep permissions separate from questions.** A question gathers user intent; a permission request authorizes an action. Keep action identity, request id, one-time decision, and expiry attached to the permission card. ACP says a client must answer pending permission requests as cancelled when cancelling a prompt.
- **Do not assume ACP is a sandbox.** Lorca starts agents locally with the user's environment and a working directory. ACP tool calls are observations unless the agent asks the client to gate them. Claude's own docs warn that auto-approved tools do not reach `canUseTool`; Lorca's coding-agent docs likewise note that a command already allowed by Claude's settings may not ask Lorca. If Lorca must authorize every filesystem or shell operation, the enforcement point has to be outside the provider's optional approval prompt (for example, a restrictive OS sandbox or client-owned tools).
- **Keep sensitive input out of forms and logs.** ACP reserves form elicitation for non-sensitive values and URL mode for sensitive or hosted workflows. Route credentials through Lorca's existing secret flow, redact all event/transcript/log projections, and make unsupported URL requests visibly declined.
- **Keep the control plane local.** Spawn the agent as a Runner-owned process using stdio (or a protected local socket). Do not expose its stdio or an experimental app-server WebSocket through the relay. Send only the existing encrypted device projection across devices.
- **Recover by session capability.** Persist provider/adapter version, session id, working folder, settings, pending question/permission, last event sequence, and stop reason. On restart, use ACP `session/resume` when offered, else `session/load` when supported; distinguish replayed history from new events. A pending question or permission must be explicitly closed or marked interrupted if its request channel cannot survive restart.
- **Resolve races by request identity.** User and bot answers should target the exact pending question id and be accepted atomically once. Late, duplicate, expired, or mismatched answers must not be sent to a new question in the same session.

ACP requires a client to tell users which agent is asking, allow form responses to be reviewed and changed, and offer decline/cancel. Sensitive information belongs in URL mode, not form mode. See the [elicitation requirements](https://agentclientprotocol.com/protocol/v1/elicitation.md#user-interaction-requirements). ACP cancellation is turn-scoped and requires the client to cancel pending permission requests; it is not the same as killing the agent process. See [ACP cancellation](https://agentclientprotocol.com/protocol/v1/prompt-turn.md#cancellation).

## Suggested delivery order

1. **Fix lifecycle semantics:** explicit turn result, structured failure news, truthful bot cue, and tests for refusal, limits, JSON-RPC failure, empty response, intentional interruption, and process crash.
2. **Harden event delivery:** stable ids, deduplication, pending-question expiry/closure on restart, and tests for a user/bot answer race and stale answer.
3. **Evaluate provider-specific depth:** prototype Codex App Server only for the concrete Codex features ACP loses (especially active-turn steer and error detail); prototype direct Claude SDK only if deferred question hooks are required. Keep parity tests against the ACP contract.
4. **Audit authorization guarantees:** document what runs inside the agent without an ACP permission callback, then decide whether provider-native permissions are sufficient or an OS/client-owned tool boundary is required.

The first item is the highest-value next engineering slice. It repairs bot-visible semantics without changing the current provider architecture.

## Sources

### Protocols and provider APIs

- [ACP overview](https://agentclientprotocol.com/protocol/v1/overview.md)
- [ACP initialization and capability negotiation](https://agentclientprotocol.com/protocol/v1/initialization.md)
- [ACP session setup and recovery](https://agentclientprotocol.com/protocol/v1/session-setup.md)
- [ACP prompt turns, updates, notices, stop reasons, and cancellation](https://agentclientprotocol.com/protocol/v1/prompt-turn.md)
- [ACP tool calls and permission requests](https://agentclientprotocol.com/protocol/v1/tool-calls.md)
- [ACP elicitation](https://agentclientprotocol.com/protocol/v1/elicitation.md)
- [Codex App Server](https://developers.openai.com/codex/app-server.md)
- [Codex SDK](https://developers.openai.com/codex/sdk.md)
- [Codex non-interactive mode (`codex exec --json`)](https://developers.openai.com/codex/noninteractive.md)
- [Claude Code headless mode and JSONL streaming](https://code.claude.com/docs/en/headless.md)
- [Claude Code CLI reference](https://code.claude.com/docs/en/cli-reference.md)
- [Claude Agent SDK streaming vs. single-message input](https://code.claude.com/docs/en/agent-sdk/streaming-vs-single-mode.md)
- [Claude Agent SDK: approvals and user input](https://code.claude.com/docs/en/agent-sdk/user-input.md)
- [Claude Agent SDK TypeScript reference](https://code.claude.com/docs/en/agent-sdk/typescript.md)
- [Claude Agent SDK overview and terms/authentication note](https://code.claude.com/docs/en/agent-sdk/overview.md)
- [Claude Agent SDK quickstart authentication](https://code.claude.com/docs/en/agent-sdk/quickstart.md)
- [A2A specification](https://a2a-protocol.org/latest/specification/)
- [A2A streaming and asynchronous operations](https://a2a-protocol.org/latest/topics/streaming-and-async/)
- [MCP transports](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports.md)
- [MCP tools](https://modelcontextprotocol.io/specification/2025-11-25/server/tools.md)
- [MCP elicitation](https://modelcontextprotocol.io/specification/2025-11-25/client/elicitation.md)
- [Claude ACP adapter AIR extensions](https://raw.githubusercontent.com/agentclientprotocol/claude-agent-acp/main/docs/air-extensions.md)
- [Codex ACP adapter AIR extensions](https://raw.githubusercontent.com/agentclientprotocol/codex-acp/main/docs/air-extensions.md)

### Lorca implementation and architecture

- [Coding agents](../architecture/coding-agents.md)
- [Coding agent questions](../architecture/questions.md#a-coding-agents-questions)
- `crates/cli/src/coding/acp.rs`
- `crates/cli/src/coding/driver.rs`
- `crates/cli/src/coding/mod.rs`
- `crates/cli/src/coding/tests.rs`
