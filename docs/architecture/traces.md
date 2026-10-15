# Turn traces

Every bot turn on a Runner leaves a trace (`crates/cli/src/traces.rs`): each model request as it went out, each reply, tool call, hold, steering message, and follow-up, and how the turn ended, so a turn that stopped can be read afterwards step by step. Traces stay on the Runner; nothing of them goes to the relay. `LORCA_TRACES=0` turns them off.

## Files

A turn's trace is `traces/<bot id>/<UTC day>/<job id>.jsonl` in Lorca's folder, one JSON object per line, readable only by the user (0600). A request names its system prompt and its tool list by a hash of their text (the first 16 bytes of its SHA-256); each is written once to the day's `blobs/<hash>.txt` beside that bot's traces, so a turn's twenty requests keep one copy and a pruned day takes its blobs with it. A day past fourteen days goes, and past 500 MB the oldest days go first; the Runner prunes once a day, when the first trace of the day opens.

Before a line is written, every string in it has this Runner's saved [secrets](secrets.md) replaced by their placeholders, as a tool result does, and only then is a string longer than 64 KB clipped to its first 32 KB and last 16 KB, at character boundaries, with how many bytes were cut, so a cut never leaves part of a secret. An image keeps its size, not its bytes.

## Records

Every record has `v` (the format, 1), `type`, `job_id`, `seq`, `step` (the loop's step, counted from its `turn_start` events; 0 before the first), and `ts`.

| `type` | What it holds |
| --- | --- |
| `job_start` | the bot, the chat and its kind, the job's kind, trigger, routine, requesting bot, and task, whether it is unattended, the provider, model, and thinking, Lorca's version |
| `request` | its `phase` (`loop`, or `compaction` for the turn's housekeeping: a compaction and its memory flush), the system prompt's and tool list's hashes and sizes, the size of its messages, how many messages it sends and how many are the loop's previous request's, the messages it adds, its cache points, and its reply cap |
| `call_end` | how long the call took and when its first event came, its stop reason, usage, and error |
| `response` | the reply as the loop keeps it: text, thinking, tool calls, stop reason, error, usage |
| `tool` | a call's id, name, arguments, result text, the type and base64 size of each image it returned, details, whether it failed or asked the turn to end, and how long it ran |
| `hold` | a call `before_tool_call` refused, the codemode call whose script made it (`parent`, empty for the model's own), and which check refused it: `limits`, `feedback`, `access`, `browser_review`, `coding_review`, `plugin_review`, or `auto_review` |
| `steering` | a message the user sent while the turn ran, as the loop read it |
| `follow_up` | the reply the turn asked about once more ([Turn context](turn-context.md#asking-once-more-for-announced-work)) |
| `prompt_refresh` | the bot's profile changed mid-turn (`changed`: name, description, setup), the new system prompt's size, and whether a finished setup brought the usual tools |
| `retry`, `compaction` | a model call tried again, a compaction before the turn or mid-turn and the size before it |
| `job_end` | the outcome (`sent`, `pass`, `error`, as in a `job_result`), the steps, whether the turn followed up, the last reply's text, and `end_reason` |

`end_reason` says why the loop stopped, from its last reply and the step's tool results: `no_tool_call` (a reply without a call), `follow_up_pass` (the bot answered PASS to the follow-up), `tool_terminate` (every result of the last step ended the turn: a dismissed question, a refused install), `budget` (a limit refused the last calls, or the model call itself before it was sent), `length`, `error`, `stopped`, or `no_reply`.

The turn's provider is wrapped (`traces::TracedProvider`) so the request record is what the adapter received, after `transform_context`, with the turn's notes. The loop's events reach the trace through the turn's sink, and the hooks record holds, follow-ups, and compactions. Auto-review's own model calls are not traced; its verdict is the hold.

## Reading traces

`lorca traces` reads the files on this Runner and needs no running `lorca serve`:

- `lorca traces list [--bot <id>] [--days N] [--end-reason <reason>]`: one line per turn, newest first, with its kind, bot, end reason, requests, tools, and tokens.
- `lorca traces show <job id> [--prompts]`: the turn step by step as Markdown, with each request's added messages, the replies, tool results, and holds; `--prompts` adds the system prompt where it changes.
- `lorca traces export <job id> [--out file.md]`: the same with the system prompts, as a file.
- `lorca traces stats [--bot <id>] [--days N]`: how turns ended, follow-ups and whether the bot went on, requests per turn, tokens with the share read from the cache, and how much of what the loop's requests sent was system prompt, tools, and messages.
