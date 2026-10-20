# Lorca Architecture

Source of truth for how Lorca is built. Read this before writing code, then the docs in [Subjects](#subjects) for the parts you change.

Lorca is a Grok Bot alternative: persistent named bots, 1:1 chats, group chats, handoff, and orchestration. Work runs on **Devices you own**. The Rust CLI runs on macOS, Linux, and Windows. The desktop app (macOS, Windows, and Linux) is the UI for the local CLI, which it bundles and launches.

Identity is a **key pair**. Devices pair. The relay stores public keys and ciphertext, after [Happy’s security model](https://happy.engineering/docs/security/).

## Constraints

1. **The app speaks only to the local CLI** over localhost websocket. The app ships the CLI binary inside its bundle and starts `lorca serve` itself, unless one already answers on the port. The CLI holds keys, talks to the relay, and talks to models.
2. **The desktop UI is a MyGo app in Go**, drawn with MyGo's native UI toolkit, one codebase for macOS, Windows, and Linux.
3. **The CLI owns the agent loop:** inference, tools, streaming, cancellation, orchestration.
4. **The relay is zero-knowledge:** opaque blobs and public keys. Auth is a signature challenge. Devices check what it delivers: the account DEK encrypts what they share, and what one Device sends another is signed ([Relay trust](docs/architecture/relay-trust.md)). Its [hosted receivers](docs/architecture/receivers.md) read a service's webhook while they seal it to a Runner, and keep only the sealed copy.
5. **Every Device records its `os`.** A Device with a desktop `os` (`macos`, `linux`, `windows`) is a **Runner**. Phones and tablets (`ios`, `ipados`, `android`) are Devices, never Runners.
6. **Provider credentials belong to the account.** API keys, ChatGPT and Grok tokens, and custom providers (any server that speaks OpenAI's or Anthropic's API, or a decision API) are connected once, on any Device, and reach every paired Device as a `credentials` blob encrypted with the account DEK. A bot runs with them on whichever Runner it is assigned to.
7. **A bot runs on one Runner:** that Device’s CLI.

## Three processes

```
┌─────────────────────┐     local websocket      ┌──────────────────────────┐
│  Lorca (desktop)  │ ◄──────────────────────► │  lorca CLI (Rust)      │
│  MyGo (Go)          │     127.0.0.1           │  keys + agent loop       │
└─────────────────────┘                          └────────────┬─────────────┘
                                                              │ HTTPS
                                                              │ ciphertext + signed requests
                                                              ▼
                                                 ┌──────────────────────────┐
                                                 │  Relay (Rust, axum +     │
                                                 │  SQLite or Postgres)     │
                                                 │  public keys + blobs     │
                                                 └──────────────────────────┘
```

- **App:** native chat UI. Create or restore identity, pair, through the CLI. Launches the bundled CLI as a child process and restarts it if it exits.
- **CLI:** identity and machine keys, local websocket, encrypt/decrypt, agent loop, the account’s provider credentials, sync with the relay.
- **Relay:** store-and-forward API. Rust, axum, SQLite or Postgres (`crates/relay`). Self-host it anywhere; clients point `LORCA_RELAY_URL` at it.

Production installs are **Lorca** (`app.lorca`) and development installs **Lorca Dev** (`app.lorca.dev`): separate applications on every platform, each with its own data, port, and keychain group ([Production and development installs](docs/architecture/development.md#production-and-development-installs)).

If the CLI is down, the app shows a native empty state with the launcher’s status and the manual `lorca serve` command.

## Domain model

Plaintext lives **on Devices**:

```
Identity 1──* Device
Device   1──* Bot          (only a Runner: os is macos, linux, or windows)
Identity 1──* Chat
Chat     *──* Bot          (kind dm: exactly 1 bot, fixed · kind group: 1–6 bots, members change)
Chat     1──* Message
Bot      1──* Routine      (a scheduled task, run in the bot's DM on its Runner)
Bot      1──* Channel      (a Telegram or Slack account it listens on; each thread is a Chat)
Device   1──* Plugin       (an MCP server installed on a Runner or in its mcp.json, per bot Access)
Bot      1──* Job          (a turn on the bot's Runner)
Bot      1──* Handoff      (a durable delegated request with attempts and result reports)
Identity 1──* Task         (durable work; an owning Bot, assigned Runner, and linked Chats)
```

Where each entity is kept on a Device, and what the relay holds of it: [Relay surface](docs/architecture/identity.md#relay-surface).

Bots carry a name, description, provider/model settings, and an SF Symbol or encrypted avatar attachment. The apps edit these profiles through the local CLI ([Bot profiles](docs/architecture/bots.md#bot-profiles)).

A bot made from one Device runs on its Runner, which can be offline: [A bot on another Runner](docs/architecture/bots.md#a-bot-on-another-runner).

## Subjects

One doc per subject under `docs/architecture/`, each read in one pass: `bun run check:docs` holds this file to 16 KiB and each subject to 24 KiB, and checks that every subject is listed and every link and anchor resolves.

| Doc | Read it for |
| --- | --- |
| [Identity](docs/architecture/identity.md) | Key pairs and the identity device, pairing and unpairing, Devices and Runners, what the relay sees, the account's provider credentials |
| [Relay](docs/architecture/relay.md) | `crates/relay`: storage on SQLite or Postgres, files, housekeeping, quotas, metrics, rate limits, auth, tables and migrations, the blob, sync socket, and push APIs, deploys |
| [Relay trust](docs/architecture/relay-trust.md) | What a Device takes from the relay and how it checks it: Device records, unpaired keys, signed envelopes, the challenge nonce, the pairing reply key, account key generations and rotation on unpair |
| [Protocols](docs/architecture/protocols.md) | The app ↔ CLI websocket and the CLI ↔ relay requests and blobs |
| [CLI (runtime)](docs/architecture/runtime.md) | The `lorca` binary and its data directory, installing it, its signed self-updates and `lorca service`, the agent loop and a turn on a Runner |
| [Turn context](docs/architecture/turn-context.md) | A turn's system prompt, brief and notes, and the follow-up on announced work |
| [Turn traces](docs/architecture/traces.md) | A turn's steps and end reason, `lorca traces` |
| [Notifications](docs/architecture/notifications.md) | Phone pushes for replies, failures, and questions, the desktop app's own notifications, reads on another Device, the chat on screen |
| [Bot permissions](docs/architecture/bot-permissions.md) | A bot's Access to plugins, tools, files, and shell; where the CLI checks it; access requests |
| [Tools](docs/architecture/tools.md) | Team, memory, and coding tools |
| [Auto-review](docs/architecture/auto-review.md) | Fast paths, what it reads, its rules and two passes, holds back to the bot, asks |
| [Handoffs](docs/architecture/handoffs.md) | Durable delegated contracts, expected outputs, evidence and return routing, offline delivery and restart recovery |
| [Review queue](docs/architecture/review-queue.md) | Encrypted editable proposals, version-bound approval, Runner execution and outcomes |
| [Message drafts](docs/architecture/drafts.md) | Emails and Slack messages a bot writes in a chat as drafts the user edits, sends, or discards; sending directly; edits as feedback |
| [Outputs and evidence](docs/architecture/outputs.md) | Bot-generated files and document links, immutable versions, task evidence references, encrypted transport and native previews |
| [Stop](docs/architecture/stopping.md) | What Stop ends: the turn's model call and tool calls, a call that never returns, commands and browser calls, the work it handed off on any Runner; what keeps running |
| [Web links in messages](docs/architecture/links.md) | Source chips for http(s) links in the desktop app, and the CLI's `links.favicon` that fetches a site's icon |
| [Terminal sessions](docs/architecture/terminal-sessions.md) | A bot's commands in terminals of their own: when a call returns, background commands, the command's card, answering and stopping, Running tasks |
| [Coding agents](docs/architecture/coding-agents.md) | Claude Code, Codex, or another ACP agent a bot supervises on its Runner: worktrees, secrets, review of its commands, transcripts, follow-ups, outputs, wake-ups, the card and its window |
| [Questions](docs/architecture/questions.md) | A bot or its coding agent asking the user: `ask_user`, an agent's form, the card and its answer, each app |
| [Secret requests](docs/architecture/secrets.md) | A password, key, or code a bot asks for on a card: sealed to and kept on its Runner, filled by name into Browser on its site or a command's environment, kept out of results and chats; managing them |
| [Browser](docs/architecture/browser.md) | A bot's browser: ego (lite) or the Browser plugin, the Runner's setting, the Access switch, Stop |
| [Codemode and Plugins](docs/architecture/plugins.md) | Scripts that call plugin tools, MCP plugins and their installs, sign-in, plugin calls at turn time |
| [Integrations and named accounts](docs/architecture/integrations.md) | Slack, Gmail, Calendar, and Drive; stable account instances, OAuth on the Runner, account selection, access recovery, and source links |
| [Marketplace](docs/architecture/marketplace.md) | The index of plugins and bot templates and how lorca.app keeps it current on every Device, bots added from a template, the marketplace sheet |
| [Workflow onboarding](docs/architecture/workflows.md) | Workflows from the marketplace: packs, a setup on a Runner and its accounts, the sample before the schedule, how setups sync, the workflow page in each app |
| [Bot templates](docs/architecture/templates.md) | A bot shared as a link or a file and a new bot made from one: what a template holds, redaction and flags, encrypted links and their keys, the Runner's own connections, the apps' sheets |
| [MCP servers](docs/architecture/mcp-servers.md) | The user's own MCP servers in a Runner's `mcp.json`: the file and other apps' spellings, sign-in when a server asks, `mcp.*` and `lorca mcp`, the apps' MCP Servers section and sheet |
| [Bots](docs/architecture/bots.md) | The lead bot, DMs and groups, who answers, handoffs between bots |
| [Memory](docs/architecture/memory.md) | Memory outside the working directory, its gate, capture, the profile |
| [Routines](docs/architecture/routines.md) | A bot's scheduled tasks: schedules and their timezones, runs and read-only checks, missed runs, health and recovery, and the apps' routine sheet and service row |
| [One-time routines, routines on events, and calendar events](docs/architecture/routine-triggers.md) | Routines run once at a time, on each event about one subject (a pull request until it merges or closes, a webhook's requests), or around a Calendar's events |
| [Event triggers](docs/architecture/event-triggers.md) | Runner event subscriptions, the gateways users run to sign and seal a service's events, the encrypted inbox, ordering and recovery |
| [Hosted receivers](docs/architecture/receivers.md) | The relay's receivers for routines on events: the Lorca GitHub App and its install and bind, routine webhooks with their URL and key, what the relay sees, setup |
| [Channels](docs/architecture/channels.md) | A bot listening on Telegram and Slack: accounts and their builtin servers, filters, chats the user allows and ones asking, conversations and what people write, replies, the feedback collector |
| [Channel readers](docs/architecture/channel-readers.md) | How the Runner reads Telegram and Slack, what it takes, Telegram's photos and albums |
| [Durable tasks](docs/architecture/tasks.md) | Work that spans turns: owner, revisions, runs and recovery, evidence, the apps' Tasks section |
| [Coordinator attention](docs/architecture/attention.md) | Consolidated reviews, blockers, commitments and changes, coordinator briefs, deduplication, encrypted records and notification preferences |
| [Shared project context](docs/architecture/project-context.md) | Group briefs, goals, constraints, decisions, source freshness and corrections, encrypted reference assets, bounded bot discovery |
| [Skills](docs/architecture/skills.md) | Skill folders for all bots, a bot, or a group; the shipped ones; a turn's list; sync; imports |
| [Workflow feedback](docs/architecture/feedback.md) | Feedback on bots' work, suggested changes to routines and skills, versions and undo, exclusions |
| [Providers](docs/architecture/providers.md) | Each model provider and its sign-in, custom providers, thinking levels, the model catalog and cost, compaction, retries |
| [Budgets and connector limits](docs/architecture/budgets.md) | Limits on turns, tasks, and routines, stopping and resuming, price labels, shared plugin call limits |
| [Sidebar sections and alerts](docs/architecture/sidebar.md) | Sections, hidden and muted chats in every app, and the chat a computer shows in front, which no phone is alerted about |
| [Desktop app](docs/architecture/desktop-app.md) | The MyGo app for macOS, Windows, and Linux: its model, host, and native views, launching the CLI, windows and onboarding, settings, title bar, commands, motion |
| [Desktop updates and builds](docs/architecture/desktop-builds.md) | The desktop app's updater and its releases, the development loop and its tests, builds for each platform and their signing |
| [Phone app](docs/architecture/phone-app.md) | The Expo app over the Rust core: the native module, pairing, relay status, attachments, dictation, notifications, turns |
| [Phone app look and layout](docs/architecture/phone-ui.md) | The phone's monochrome palette, round-button bars, chat list and search, composer and Stop, forms and sheets, transcript, cards and message actions, a tablet's two panes |
| [Phone releases and updates](docs/architecture/phone-updates.md) | TestFlight builds, Android APK releases, and the Android app's self-update: the daily check and offer, the progress dialog, the package installer |
| [Website](docs/architecture/website.md) | `web/`: the site, its docs, and the install scripts it serves |
| [Languages](docs/architecture/languages.md) | English and Simplified Chinese in each app, and what the CLI words |
| [Repo layout and development](docs/architecture/development.md) | Where each crate and app lives, the Mac, phone, and Android development loops, release bundles and their SDK restamp, a local relay, a change from branch to release |

## Status

Done: crypto and blob protocol, relay, CLI (identity, pairing, restore, local WS, API-key and subscription providers, server-side web search, agent loop, encrypt-before-upload, group chats, cross-Runner jobs and handoffs, steering and stop, routines, limits on turns and routines with resuming, shared plugin call limits, plugins over MCP with a marketplace, channels on Telegram and Slack, the user's own servers in `mcp.json`, and permission cards, encrypted pushes for replies, failures, and pending confirmations, signed self-updates of a CLI without an app, updated from any Device, and `lorca service`), app wiring and the bundled CLI launcher.

Next: keychain storage.

The phone app (`mobile/`) pairs as a Device with `os` `ios`, `ipados`, or `android`; it is never a Runner and does not hold the master secret. The Device that creates or restores the identity holds the master secret.

## Open points

- Keychain instead of 0600 files for the master secret and credentials

When those are chosen, update this file.
