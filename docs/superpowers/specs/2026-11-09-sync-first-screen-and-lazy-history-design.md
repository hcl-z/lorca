# Fast first screen, history on demand

## Intent

A newly paired Device, or a Device moved to a fresh relay, waits for the whole
account history to be uploaded and then for up to 400 messages per chat to be
pulled before its first screen is usable. The user wants the opposite: after a
short wait the bot and chat list is complete, with each chat's latest message
and time; opening a chat shows the latest ~30; scrolling up fetches one older
page at a time from the Device that holds it.

The reported account has 14,505 messages in 29 chats, created 2026-10-12 to
2026-11-09. At the measured sequential drain rate of about 2.5 blobs per second,
a full history upload takes roughly 1.6 hours. The first screen needs almost
none of it.

## Current behavior

Upload (`App::push_history`): for each chat, every message oldest to newest,
with the attachments each references. Blobs go into the local SQLite `outbox`;
`drain_outbox` takes `first_outbox` (`ORDER BY position`) and PUTs them one at a
time, so live messages queue behind the entire backfill.

Pull (`sync::first_sync`): pages the log without `chat` blobs (roster, machine,
credentials, keys, envelopes), then fetches up to `FIRST_SYNC_MESSAGES = 400`
messages per chat, one chat at a time, and emits a single full `snapshot`
(event) only after every chat is done.

Pagination already exists: `chats.messages` answers from local SQLite, then
`sync::older_messages` reads one page of `OLDER_PAGE = 100` back from the relay
when the local copy runs out. The app scrolls into it already.

A bot's turn reads up to `MAX_CONTEXT_MESSAGES = 400` from the local store
(`turns.rs`), which is why `FIRST_SYNC_MESSAGES` is 400 rather than a screen.

### Why the latest message cannot be uploaded first on its own

`relay/db::group_page` groups a chat's blobs by slot and orders them by
`MIN(seq) DESC` (`crates/relay/src/db/sqlite.rs:635`). The client reads the
first slot as the newest message and its place as `history_before`
(`crates/cli/src/sync.rs:311`, `352`). A message written ahead of the rest of
its chat therefore has the smallest `MIN(seq)` and reads as the oldest. The
latest-message preview must arrive without reordering a chat's log.

`LocalStore::upsert` also appends a message it does not hold at `MAX(position)+1`
(`crates/cli/src/local_store.rs:318`), so a chat's window must be uploaded in
content order.

## Goals

- G1. After pairing on a fresh relay, the first screen — bots, chats,
  sections, devices, and each chat's latest message and time — appears in about
  10-15 seconds.
- G2. A live message, turn, or request never waits behind backfill.
- G3. Opening a chat shows its latest messages from the relay; scrolling past
  them fetches one page (100) at a time from an origin Device.
- G4. The relay holds only the first screen, live writes, and the latest 30
  messages per chat — not the full history.
- G5. A bot's turn sees up to 400 messages of context when they can be fetched
  in time; a slow origin degrades the turn to what is local.

## Non-goals

- No relay protocol or storage change. The relay keeps serving what it holds;
  a relay that already holds full history keeps working.
- No encryption change. Origin pages use the existing signed, sealed
  `request`/`response` envelopes.
- No new navigation or transcript UI. Only the existing "loading older",
  "history is elsewhere", and "beginning of chat" states gain wording.

## Anti-goals

- First paint blocked on full history.
- Uploading history nobody scrolled to.
- Repeating a multi-megabyte full snapshot during backfill.

## Design

### 1. Outbox priority

- Add `priority INTEGER NOT NULL DEFAULT 1` to the local `outbox` table (SQLite
  migration on open).
- `LocalStore::first_outbox` becomes `ORDER BY priority, position`.
- Tiers:
  - `priority = 0` — first screen: keys and keyrings, roster (with the previews
    below), credentials, this Device's `machine` blob, bot avatars.
  - `priority = 1` — live writes: new messages, edits, removals, read marks,
    turns (`job`, `job_result`), and `request`/`response`. Default for ordinary
    enqueues.
  - `priority = 2` — each chat's latest 30 messages, oldest to newest, plus the
    attachments they reference. Content order is preserved: no message is
    written ahead of an older one in the same chat.
- No third backfill tier. Messages older than 30 per chat are never bulk
  uploaded.
- `push_history` becomes two passes: P0 metadata (including a roster with
  previews), then P2 chat windows.
- A live enqueue is always `priority = 1`, so it preempts the P2 pass. A row
  already queued live when `push_history` runs is kept live, not demoted; a
  backfill pass tags only the rows it owns.

### 2. The roster carries the previews

- Extend `RosterBlob` (and `ChatMeta`, or a parallel `previews` map) with a
  structured `last_message` preview per chat: `{ message_id, author, sort_at,
  body_kind, text or detail, attachment summary }`. The app renders it in its own
  language; the roster never carries a pre-rendered string.
- It is refreshed on a debounce (about 5 s) after message writes and at most once
  per interval, so a burst of streaming updates writes one roster, not one per
  token. The roster is a latest-only slot; the relay keeps the newest.
- The receiving app uses `last_message` for the list while the chat's messages
  are absent or older. When the real message arrives, the message wins.
- This is the only way to show the true latest message within the first-screen
  budget without breaking the relay's place ordering.

### 3. First sync (pull)

- Keep the metadata pass unchanged: page the log without `chat` blobs until
  empty, applying the roster (with previews) and the rest.
- Do not fetch any chat messages on first sync. Emit one light `snapshot` after
  the metadata pass; it carries the list and the previews, and is small.
- Opening a chat triggers `chats.messages`, which fetches the chat's messages on
  demand (relay window, then origin). This removes the 400-per-chat loop and the
  long first snapshot entirely.
- Nothing is pre-filled in the background.

### 4. Opening a chat and paginating

- `chats.messages` keeps its signature: local page first; if short and more
  exists, a remote page; insert with `LocalStore::insert_older`; answer again.
- Source chain: local → relay (up to the stored 30) → origin Device.
- Remote work never blocks the call: `chats.messages` returns the local page
  immediately with `pending: true` when it has to go remote, and the core emits
  `chat.messages.loaded` (or the existing message events) when the page lands.
  A caller may pass `wait_ms` (for example 600) to get a short synchronous
  attempt; the core never waits beyond it.
- Per-chat locks replace the global `READING_BACK` mutex around network work;
  a slow chat never blocks another.
- The app prefetches when the list nears the top (about two screens away), so
  the loading row is rarely seen.
- Cancellation: leaving the chat, backgrounding the app, or a new fetch cancels
  the old one.

### 5. Origin pages

- Add a client-to-client verb `history.page` over `requests.rs`:
  - request: `{ chat_id, before: { sort_at, message_id }, limit, max_bytes }`
  - response: `{ messages: [...], has_more: bool, not_here?: true }`
- Candidate order:
  1. The chat's bot Runner (direct chat: the bot's `runner_id`; group: the owner
     bot's runner, then other members' runners).
  2. The identity Device.
  3. Any online Device that lists the chat.
- A candidate that lacks the chat, or the cursor message, answers `not_here` and
  the requester tries the next; a candidate that is offline is skipped.
- The origin answers from its local SQLite by `(sort_at, message_id)`, capped by
  count (100) and 256 KiB, and does not write to the relay. Attachments stay
  metadata; files are fetched by id as today.
- Add `identity_device: bool` to the `machine` blob (or the roster's device
  entries), so a peer can identify the identity Device without guessing.

### 6. `has_more` and the per-chat history state

Persist a state per chat (in `chat_history` or a sibling table):

- `relay_more` — `history_before` is set; the relay has more.
- `origin_more` — the relay is exhausted and the last origin page had more.
- `origin_offline` — the relay is exhausted and no origin answered; store the
  candidate and a `checked_at`.
- `exhausted` — the relay is exhausted and the last origin page said no more.

`chats.messages` answers:

- `has_more = true` when `relay_more` or `origin_more`.
- `has_more = false` when `exhausted`, or when `origin_offline` (with
  `history_elsewhere` naming the Device).
- `history_elsewhere: { device_id, device_name }` when a known Device holds
  more but is offline.

Retries: `origin_offline` is retried when presence changes, on pull-to-refresh,
and after a TTL (for example 60 s). Never retry in a tight loop.

### 7. Runner turn context

- `turns.rs` keeps `MAX_CONTEXT_MESSAGES = 400`.
- Only when the local page is shorter than 400: fill on demand, relay pages
  first, then origin pages, capped at 400 messages or about 2 s. The 2 s budget
  is soft: a slow origin leaves the turn to run with what is local. G5 is
  best-effort.
- This is the only path that pulls more than a screen.

### 8. Events

- One light snapshot on the first-screen path.
- Live and backfill arrivals use incremental events (`message.added`), batched
  per page. No repeated full snapshot.
- Keep the existing backlog behavior for pages unrelated to the first screen.

### 9. Migration

- Existing `outbox` rows get `priority = 2`, and rows whose messages fall outside
  each chat's latest 30 are deleted rather than uploaded. The in-progress 1.6 h
  backfill stops at the upgrade.
- `register`/`push_history` re-queues the P0 roster (with previews) and the P2
  windows.
- Old relays keep their full history; `history_before` still pages them 100 at a
  time. Both behaviors coexist; no relay data migration.

## Edge cases

- **Edits, removals, read marks.** A message the Device holds is updated in
  place. An upsert for a message the Device does not hold and that lies before
  its local window is ignored for the transcript (the origin serves the current
  state when asked); it must not append to the end. Read marks from old pages do
  not clear current unread state.
- **Cursor missing on the origin.** Fall back to `sort_at` alone.
- **Attachments.** P2 uploads the files referenced by the 30. Older pages
  reference files that may not be on the relay; `GET /v1/files/{id}` returns 404
  and the existing missing-file state shows. Uploading a file when its message
  is served from an origin is left open below.
- **Multiple origins, divergent copies.** Prefer the bot's Runner; a `not_here`
  moves to the next; messages converge by id and position.
- **Origin gone.** If no Device holds messages past the relay's 30, that history
  is unavailable. Say so; do not present the chat as complete.
- **The requester is the origin.** The identity Device has the history locally;
  no request is made.
- **Security.** `history.page` is answered only for an envelope sealed by a
  machine of the identity, for a chat the origin lists. The relay sees ciphertext
  only.
- **Rate limits.** Origin pages are ordinary envelopes and count against the
  identity rate limit.
- **Quota.** The relay stores a bounded window per chat instead of all history.

## Success criteria

- Pairing on a fresh relay reaches a first snapshot with every chat and its
  latest message in about 10-15 s (about 30 small blobs at the measured rate).
- The list is usable before the 30-message windows finish uploading.
- Opening a chat fetches its first page on demand; the call returns within the
  `wait_ms` bound, then fills in.
- Scrolling past 30 fetches one origin page (about 100) in about 2 s when the
  origin is online, with a clear state when it is offline.
- No run uploads more than 30 messages per chat.
- A turn reaches 400 messages of context when the fill fits in about 2 s.

## Testing

- Local store: outbox migration, default priority, pruning, `first_outbox`
  ordering, live rows kept live.
- Roster previews: debounce, structured fields, newest wins, real message wins.
- `push_history`: P0 set, P2 set (oldest to newest), nothing older; attachments
  only for the window.
- `first_sync`: metadata only, one light snapshot, no chat page loop, no 400
  loop.
- `chats.messages`: local → relay → origin; `pending`/`wait_ms`; cancellation;
  `has_more`/`history_elsewhere` in each state; retry on presence and TTL.
- `history.page`: cursor including a deleted message, byte cap, `has_more`,
  `not_here`, requester not a paired machine, chat not on the origin, identity
  Device as requester, candidate fallback.
- Turn fill: reaches 400, stops at the cap, falls back on a slow origin.
- Mobile and desktop: list previews from `last_message`; the
  `history_elsewhere` state; existing chat-list and transcript tests.
- `bun run check:docs` after the doc changes.

## Docs

`docs/architecture/relay.md` is at 23.1 KiB of its 24 KiB budget, and its client
sync paragraphs describe the behavior this changes. Split the client-side sync
and history behavior into a new subject doc (for example
`docs/architecture/history.md` or `docs/architecture/sync.md`), add it to the
Subjects table in ARCHITECTURE.md, and update:

- `relay.md` — upload/pull summary and the origin-page protocol.
- `phone-app.md` — the "history is on <device>" transcript state and the
  preview from the roster.

## Calls made here

- The latest-message preview lives in the roster, debounced; chat windows upload
  oldest to newest. No chat blob is written ahead of its chat's order.
- First sync pulls metadata only; chat messages are fetched when a chat opens.
- Origin pages are sealed responses to the requester, not uploaded to the relay.
- Origin order: bot's Runner, identity Device, any online Device; `not_here`
  falls through; presence and a TTL drive retries.
- Turn context is filled on demand, up to 400 or about 2 s, best effort.
- Existing relays keep both behaviors; no relay data migration.
- Existing outbox backfill rows become P2 and are pruned to 30 per chat.
- The minimal UI lands in both apps (list preview, offline-origin state).

## Mobile performance (separate follow-up)

The list libraries are already the right ones: the transcript uses LegendList
with `maintainVisibleContentPosition`, `maintainScrollAtEnd`, shared row
identity, and memoized rows; the chat list uses FlashList v2. The perceived
jank is in state and ordering, not the libraries:

- `ChatsScreen` subscribes to the whole `chats` array; every message rebuilds
  `items`, rows, and data. Use per-row selectors or a normalized `chatIds` +
  `chatsById` store.
- `maintainVisibleContentPosition={{ disabled: true }}` deliberately lets the
  list shift when the first row changes; new messages reorder by
  `lastActivity`. Freeze the order during a drag, apply reorders at rest or on a
  short interval, and show a new-activity pill at the top.
- `upsertMessage` compares with two `JSON.stringify` calls per update; compare a
  revision or `updated_at`, and coalesce streaming updates (about 10 Hz).
- `replaceSnapshot` rebuilds every chat object; preserve unchanged ones so row
  memos hold.
- Use `expo-image` with fixed dimensions, `cachePolicy`, a placeholder, and no
  transition for avatars; layout shifts are a common flicker source.
- The `last_message` preview makes `preview()` O(1) instead of scanning messages
  per row.
- Consider LegendList for the chat list too if FlashList reordering stays
  janky; measure before switching.

## Open questions

- Should a file referenced by a page served from an origin be uploaded to the
  relay so later readers do not also need the origin?
- The exact wording of the offline-origin state on the phone and the desktop.
