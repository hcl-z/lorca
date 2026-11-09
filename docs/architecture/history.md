# History

How a Device gets an account's chats and how much of each it keeps. The relay stores what
Devices upload; this doc is the client side. See [Relay](relay.md) for the storage and APIs.

## First screen

An identity Device uploads the account's history again after a relay reset, or for a Device
that pairs, in two passes. The first is the first screen: keys and keyrings, the roster with
each chat's latest message, the credentials, the `machine` blobs, and bot avatars, at outbox
priority 0. The second is each chat's latest `CHAT_WINDOW` (30) messages, oldest to newest,
with the files they reference, at priority 2. Live writes are priority 1 and jump the backfill.
No message older than the window is uploaded.

The relay pages a chat by slot `MIN(seq)`, so a window goes up in content order; the roster's
`previews` map carries each chat's latest message for the list in the meantime. `App::push_roster`
refreshes those previews from a five-second loop after a message moved.

## First sync

At seq 0 a Device pages the log without the `chat` kind (roster, machine, credentials, keys,
and the envelopes sealed to it), applies the roster and the previews, marks every chat as
having more on the relay (`history_before = i64::MAX`), and emits one snapshot. It fetches no
chat messages then.

## Paging

`chats.messages` answers from SQLite, then, while the chat is partial, fetches one page with
`wait_ms` (600 by default). From the relay it reads 100 messages back (`older_messages`); when
the relay's copy ends it asks an origin Device for a page of 100 (`history.page`, a signed and
sealed request/response envelope) capped at 256 KiB. The origin order is the chat's bot Runner
(the owner's first in a group), then the identity Devices, then any other Device. A candidate
that does not list the chat answers `not_here` and the next is tried. A page past `wait_ms`
arrives through `chat.messages.loaded`, with `pending: true` on the first answer.

`has_more` is true while the relay or an origin holds more. When the relay is exhausted and the
origin that holds more is offline, the answer carries `history_elsewhere { device_id,
device_name }` and `has_more` is false.

A message an edit reaches that this Device never loaded and that lies before its local copy is
ignored for the transcript; the origin serves the message as it stands when it is asked.

## Turn context

Before a turn reads its context, a Runner fills the chat's local page to `MAX_CONTEXT_MESSAGES`
(400) from the relay and then an origin, within about two seconds. A slow origin leaves the
turn to run with what is local.

## Revisions

Each message carries `rev`, a hash of its content. The apps skip an incoming message whose
revision matches the one they show, and the phone coalesces streamed updates into one store
pass every 100 ms.
