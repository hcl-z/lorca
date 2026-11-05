# The island

A window at the top of every display says what the bots and coding agents are doing when the main window is out of sight. Its look, motion, and rules are Cindy's Agent Island (`makecindy/cindy`, Apache-2.0), ported one to one from its SwiftUI helper (`apps/desktop/native/agent-island/macos-agent-island-helper.swift`) and its state machine (`apps/desktop/src/main/agent-island/state.ts`). The numbers below are Cindy's.

## Files

| File | What it holds |
| --- | --- |
| `island.go` | The windows, the pointer, and the views |
| `island_state.go` | The sessions, their timers, what the island shows, and the sounds |
| `island_store.go` | The store read into one observation per session |
| `island_metrics.go` | Sizes, the badge, text limits |
| `island_darwin.go` | AppKit: window level and panel, each display's notch, the mouse button, system sounds |
| `island_styles.go` | A failed session's ringed avatar and red card |
| `island_gallery.go` | Debug › Island Gallery |

## Sessions

A session is each bot with a turn in a chat (keyed `chat#bot`) and each coding agent's card (keyed `chat/message`). Each store change reads every session into an `islandObservation`: whether it runs, the user's last words, the reply as it streams, the tool it runs, a failure, and the question it asks. The machine turns the change since the last read into Cindy's transitions:

- **Running.** A run starts or the user writes: a new card, the user's words as the first activity line. A tool sets the detail line, which lingers 2 s after the tool ends. A streamed reply updates one line in place. The last 3 lines are kept.
- **Needs you.** A question arrives. Its kinds are an `ask_user` with options (answered on the island), an approval (Deny, Always allow, Allow once), or a form the chat answers (several picks, the user's own words, a coding agent's form, access, secrets, sign-ins). It pushes the reveal showing now back to the front of the queue and reveals for 5 s. A question outlives the turn that asked it.
- **Completed.** A turn ends with no question. The result is held 5 s and revealed for at least 8 s, or for 5 s past its last queued preview line. A completed card swipes right to be marked read: past 24 points it rests open on its button, and past the larger of 140 points and 72% of its width a release commits.
- **Failed.** A failed reply, or a coding agent's error. It is held and revealed 12 s and is unread until the user opens or marks the chat read; seeing the chat does not clear it.

What the island shows, first match wins: the list, when the pointer opened it or a click did; nothing, when no session is left; a question card, opened at once; the one result or failure revealed now, with the rest queued in order; else the compact island naming the first session in line. Two sessions at work do not swap names within 1.2 s. The order is waiting on the user, then unread results, then at work; within a rank, the latest user prompt first, then the latest start. While the list is open its order holds.

The chat the user is looking at, with the app in front (`notifier.watchedChat`), reveals nothing. A question in it leaves the island after 5 s for the chat's own card. Approvals are never held back. Muted chats are tracked and never shown. Results stay up to 24 h and unread errors 4 h.

The machine schedules one wake-up for its next deadline instead of ticking.

## Look

The island is a black path (`islandNotchPath`). Its top edge reaches past the sides by `ext` into the menu bar, concave shoulders run down into the sides, and the bottom corners have continuous curvature. Compact, `ext` is 3 and the bottom radius 12; expanded, 14 and 24. Width, height, `ext`, radius, and shadow follow one spring (Cindy's response 0.42, damping 0.82). Each display's window is the expanded island's widest size plus an 80-point inset, so the island grows inside it without resizing the window.

`island_darwin.go` reads each display's notch from its `NSScreen` (`auxiliaryTopLeftArea` and `auxiliaryTopRightArea`, with the top from `safeAreaInsets`, else the menu bar's height). Compact, a notched display shows two wings: the status dot and the title on the left, the detail and badge on the right. The words fade in as a wing widens past 44 points. A display without a notch gets a simulated one 14% of its width, between 160 and 240 points, with one line: the status dot, the title, the detail from 190 points wide, and the badge from 152. Compact width is the notch plus at least 64 points (88 when simulated with a session), never under 210.

With no session to show, the island is gone. When its first session arrives it grows out of the notch: on a notched display it starts as the notch itself, black on black, and its wings spring out; without a notch it starts as a sliver at the top edge and springs down and out. Its content fades in 90 ms after the shape starts. When its last session goes, the content fades first, the shape goes back in drawing the session it last showed, and the windows hide once it is gone. A gone island takes no pointer.

- **Badge.** The waiting count (blue while a question is pending, else green), a dot, and the running count in orange, or the number of sessions. It is sized from its characters, 7 points each.
- **Status dot.** At work, a coding agent's mark or a bot's SF symbol in orange, breathing 0.3 to 1 over 0.75 s each way. Asking, a question glyph on blue; done, a check on white. Failed, the bot's avatar or the agent's tile inside a red ring, a gap of black between them.
- **Running text.** The title and detail shimmer: a 72-point band sweeps across every 2.45 s. The line takes the width the badge leaves.
- **Expanded.** A top bar (the session's mark, then sound, Settings, and New Session buttons around the notch; the buttons stand in place at once and fade in 0.14 s after the island opens), then one card or a list of up to 5 rows (scrolling past that). Each row is 64 points with a 6-point gap, rounded 12, and shows a 32-point mark, the title, the state's word (Input, Done, Running) or a Review button, and a summary line. A failed card is washed red, outlined red, and says Failed in a red pill. An approval's buttons sit under its card; a bot's options are rows answered in place (`Store.AnswerAsk`).

Debug › Island Gallery (⌘⌥I, or `LORCA_ISLAND_GALLERY=1` at launch) draws every state from made-up sessions on each kind of display, live, and plays the island's coming and going (`island_gallery.go`).

## Pointer and keys

A 33 ms poll reads `Screen.CursorScreenPoint` against each display's island and its menu-bar strip, toggling `SetIgnoreMouseEvents` so a click elsewhere reaches the app below. 500 ms over the strip opens the list; leaving closes it 150 ms later, but never within 1 s of opening, and hover cannot open it again for 300 ms. A question or a reveal holding the island keeps hover from opening the list. A click on the compact island opens the list. A press anywhere else (`NSEvent.pressedMouseButtons`) closes an open card: a result goes back to wait, and a question stays in the list without opening again. A press within 300 ms of the island opening on its own is ignored. Only the display under the pointer opens its list.

A click on a card opens its chat. View › Island (⌘⇧I, registered with `GlobalShortcut`) opens the first question's chat, else Lorca.

## Sounds

When the shown sessions change, the island plays a macOS system sound at volume 0.58, in Cindy's priority order: a new question (Glass), a new failure (Basso), a new result (Hero), a new run (Pop). Each sound waits 1.5 s before playing again, or 0.8 s for a run.

## Window

MyGo's window stops at the floating level under the menu bar, so `island_darwin.go` uses objc to raise it two levels above `NSStatusWindowLevel`, drop its title bar, and keep it through a Space switch and a full-screen app. It also swaps the window's class to `NSPanel` with the nonactivating style, so a click answers without taking the keyboard from what the user is typing in. The other platforms build the same code without the window. `Screen.OnDisplaysChanged` rebuilds a display's window.

## Settings

General › Island, all on at first: the island itself (`ShowsIsland`); one on every display or the main one alone (`IslandAllDisplays`); opening under the pointer (`IslandHoverGrow`); and sounds (`IslandSounds`, also toggled by the island's speaker button). The section is not shown off macOS.

## Differences from Cindy

MyGo draws no text masks, so the shimmer paints the text again in 12 slices under a moving clip, each in the band's color at its place. Lorca has no mascots, so the status dot uses the bot's symbol, avatar, or the agent's mark. Cindy keeps an idle island with its sleeping mascot and a New Session row; Lorca's goes away. Cindy breathes only a coding agent's mark and marks a failure with a faint triangle on an otherwise plain card; Lorca breathes every session at work alike and gives a failure its avatar in a red ring on a red card. Lorca chats are not archived, so only a right swipe exists. Lorca answers a bot's `ask_user` in place where Cindy only opens the chat. Cindy's dragging and resizing of the island, its remote-device sessions, companions, and group avatars have no counterpart.
