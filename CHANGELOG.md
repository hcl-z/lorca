# Changelog

The release notes of the command line, the desktop app, and the phone app, which share one version.
A release attaches its version's section to its GitHub release, and the apps show it in their
update windows. `bun run release <version>` needs the section. Versions before 1.0.17 were numbered
apart: 1.0.16 and below are the Mac app's, 0.1.11 and below the desktop app's, 1.0.5 and below the
phone app's.

## [Unreleased]

## [1.0.32]

- The phone now takes part in a coding agent's chat as the desktop does: `@` it from the composer, and what you write reaches it as written, interrupting what it is doing. Its words are its own messages under its mark, its questions wait for you, and the bot no longer answers them for you. A reply is one message per turn, growing as it streams, even across tool calls and plans.
- The phone's quick menus, pickers, short forms, and details open as native bottom sheets: New, attach, participants, an agent's actions and question, model and effort, a message, a command, the running tasks, and chat info's model. Full agent transcripts and file viewers stay full screen.
- A message to an agent no longer steers or stops the bot's turn, and an in-flight bot turn no longer sees or answers it.
- A macOS island at the top of every display shows what the bots and coding agents are doing while the main window is out of sight: idle, working, the last result, and a question you can answer there. View > Island (Cmd-Shift-I) opens the first waiting question.
- What a bot remembers now goes to the right place: lasting facts about you to your profile, a bot's work to its own memory, and a sensitive fact needs your words in the same message. The memory trace on the desktop and phone shows each capture's facts, where they went, and Undo; older captures still undo to the right file.

## [1.0.31]

- A coding agent a bot starts now takes part in the chat: `@` it from the composer, and what you write reaches it as written, interrupting what it is doing. Its words appear as its own messages under its mark, and the bot reads them as its delegate's.
- An attended agent's question waits on its card for you; the bot answers only when you made it your proxy, or when nobody is watching a routine's or an event's agent.
- A turn that a stop or a restart left behind now recovers: it starts once the login shell is ready, answers the message it left unanswered, and checks its whole transcript before calling a claim stale.
- The desktop `@` menu lists only the bots and agents in that chat, and a pick no longer repeats a name. A chat with one bot drops the "Group" subtitle and stacks the bot's and its agents' avatars; Gemini, Copilot, Cursor, OpenCode, and Kimi have their marks.
- The agent participant is desktop-only for now: the phone still draws an agent's messages as its bot's and does not list agents in `@`.

## [1.0.30]

- When a bot saves something about you, the chat says so as it happens: the fact, and the one it replaced if it replaced one. The line from the capture, after a chat goes quiet, still covers the rest.
- A table in a message no longer opens a blank band above and below itself; it sits as close to the text around it as any other block, at any message width.


## [1.0.29]

- A bot that remembers something from a chat says so there, in one quiet line: the facts it learned, a changed one with the fact it replaced, and where to undo it.
- A fact with a deadline, such as leave or a trip, is remembered until it ends and then set aside, where you can put it back.
- A message's paragraphs sit closer together in the desktop app, instead of a blank line apart.


## [1.0.28]

- A bot remembers what bears on the message you just sent: each turn carries the lines from its memory, its topic notes, and what its computer knows about you that match your words, found by search rather than by the whole file, in English and in Chinese, Japanese, or Korean.
- What you say about yourself reaches every bot on every computer you run Lorca on, and a fact that changed replaces the old one instead of sitting beside it: "I moved to Hangzhou" strikes "lives in Shanghai". A fact you removed, or one a newer fact replaced, stays gone on every computer. No relay update is needed.
- A fact with a deadline, such as leave or a trip, is remembered until it ends and then put aside.
- What a bot remembered from a chat now shows in the chat as a quiet line naming it, with where to undo it.
- Each bot tidies its own memory once a day: what changed over time moves to a history note, and notes no message has needed for two months move to an archive once memory is nearly full. Everything moved is still searchable, and any tidy can be undone from the bot's memory.
- A way of working you keep correcting can become a skill: the bot suggests it from two examples, and you accept or reject it like any other suggested change.
- Lorca Dev shows what a bot's memory did: the lines a turn was reminded of, what was saved, and what was refused.
- The memory line and the deadlines above arrived in 1.0.29: this build does not have them.


## [1.0.27]

- The phone opens faster: a launch shows your chats as you last saw them while the account loads, the first chat you open measures only messages it has not drawn before, and a message that arrives changes only its own chat on screen.
- The phone's conversation opens at its newest message, stays with the newest while a reply comes in, keeps your place when older messages load above, and settles a message you send just under the header.
- The phone's Attention sheet and a coding agent's timeline stay smooth with hundreds of rows.
- On the phone you can edit a bot's memory and what its Runner knows about you, undo what a bot remembered from a chat, and see the prompt its next turn sends.
- A Runner's screen on the phone lists its MCP servers, turns each on or off, and reconnects one.
- A coding agent's card names its pull request for GitLab merge requests in nested groups too, and the desktop and the phone read its status and model from the CLI.
- Coming back after time away no longer reloads the whole account when only other Devices' turns moved, and a phone no longer re-sends its own details on every launch.

## [1.0.26]

- Coding agents on the phone look and work as they do on the desktop. The agent's card has its mark, a state pill that ticks while it works, its console, and its model and effort as menus beside how full its context is and what it has cost. The transcript opens as a full sheet with the task, workspace, plan, and pull request, a timeline, and command output in color. Agents at work in any chat show as small cards over the composer, each with Stop.
- The phone shows HTML pages in the app, and on iPhone a PDF, with no access to the app, cookies, or files. A link you follow opens in the browser. Audio and video play in place.
- The phone draws the desktop's icons on iPhone and Android, and its cards share one button style.
- How a coding agent stands (needs you, working, done, failed), how full its context is, and what it does now are worked out once by the CLI, so the desktop and the phone always agree.
- The phone keeps a task it just heard about when an older snapshot arrives, and the desktop keeps a message that arrives before its chat does.
- The preview of what a bot sends (the inspector's Prompt row and `lorca prompt`) now shows the task and attention notes too.
- A bot that asks you to choose without listing the choices is told to list them or to ask in its message instead, rather than trying the same card over and over. A new bot setting itself up asks its open questions in its messages.

## [1.0.25]

- Coding agents run over the Agent Client Protocol: Claude Code and Codex through adapters pinned to your own CLI, pi, Gemini, and any other ACP agent, all handled the same way. Terminal panes and `lorca coding hook` are gone. A question the agent asks shows on a card of its own in the chat, and you or the bot answer it there. On the desktop, the agent's card shows its model, effort, mode, how full its context is, and what it has cost, with its pull request one click away. Its transcript reads as a timeline, and each running agent has a floating window that fills the app when clicked.
- Files open over the whole desktop window. The bar is the title bar, with the file's icon, its name, Preview and Source, Copy, Save As…, and Open. Below it the file runs edge to edge:
  - a picture pinches and slides
  - Markdown is a page to read
  - a CSV or TSV file is a table whose header stays in view
  - code shows with line numbers
  - an SVG, a PDF, or an HTML page draws in place, and a recording plays
  An output's other versions and the file's details are in a pane at the side.
- Files open full screen on the phone too: pictures and SVGs zoom, Markdown reads as a page, tables keep their header, and code has line numbers. A PDF, a recording, or a web page opens in Quick Look.
- Files wear their format's icon everywhere, from Untitled UI's file icons. A lone card of a document, a page, a table, or a text file shows the start of the file.
- Cards and viewers show the file only. What a bot says it checked when it published a file stays with the task and is no longer drawn.

## [1.0.24]

- About you: one profile per computer that every bot there reads, with your name, your timezone, the language you want, and how you like replies. A bot saves what you tell it about yourself there, and you edit it from a bot's inspector or with `lorca profile`, so a second bot no longer has to learn the same things again.
- A bot learns from a short chat too. Once its direct chat has gone quiet, it notes the facts you stated there, each one quoting your own words; the inspector's Learned row lists them, and Undo… forgets them so they are not learned again. Group chats and channel conversations are never read this way. `LORCA_MEMORY_CAPTURE=0` turns it off.
- A bot's memory lives in `~/.lorca/memory/`, outside its working folder, and moves there on the first start. Only the memory tools write it: a file edit or shell command that would change it is refused. Each new fact is checked first, and one that holds a secret, reads like an instruction, hides characters, or brings back a fact you removed is refused.
- See what a bot sends: the inspector's Prompt row and `lorca prompt <bot>` show its instructions section by section with their sizes, then the notes after the conversation, with Copy.
- Bots' instructions are shorter. They say once whom a bot follows and that web pages, files, and tool results are data, keep progress and stale details out of memory, use the language your profile names, and leave tool details to the tools themselves. Memory, your profile, and the time come after the conversation, so a fact saved mid-conversation keeps the rest cached.
- The desktop inspector is regrouped: a Bot tab with the profile, what it runs with, and this chat's context, spending, limits, and prompt; and Memory and About you in Skills. A direct chat's Runner opens from its header.

## [1.0.23]

- A bot's browser is ego (lite)'s where it is installed, and a built-in one elsewhere. Lorca now carries ego's `ego-browser` skill, and every computer has the Browser plugin without installing it; Settings › Plugins picks Automatic, ego (lite), or Built-in. A bot's Access has one Browser switch for either. Browser profiles, taking the browser over, and recording a workflow are gone, and the Browser page on the phone with them.
- Only the chief, the first bot made, makes new bots and group chats. The other bots are told to ask it, and Chef's description now says so. Chef's description changes for new accounts only; edit it in an existing account to match.
- The browser instructions a bot reads are shorter, name only the browser in use, and end with closing it.

## [1.0.22]

- A bot that says what it is about to do and then stops is asked once more, so a morning routine no longer ends on "Fetching all sources now." with nothing fetched: the bot does the work, or stays quiet when it is done. Bots are also told to keep messages short but not the work, to say they did something only when it shows, and to answer in your language.
- A bot's new name or description, and a new bot's finished setup, apply from its next step, even in a conversation under way, and its chat notes each change, so the bot follows what you wrote instead of repeating what it said about itself before.
- Every bot turn leaves a trace on the computer that runs it: each request, reply, and tool call, what held a call back, and why the turn ended. `lorca traces list`, `show`, `export`, and `stats` read them. Traces stay on that computer, leave out the secrets you saved, and are kept for two weeks; `LORCA_TRACES=0` turns them off.
- Bots' instructions are steadier and shorter. What started a turn (a routine, an event, a teammate's message) comes with that turn instead of changing the instructions every time, so more of each conversation is read from the provider's cache, and a scheduled run leaves out what only your requests need.

## [1.0.21]

- A file viewer on the desktop, and an image lightbox. A file in a chat, or an output in the inspector, opens in a sheet that draws it as it is: an image fitted or at its own size, Markdown as a page, an HTML page or a PDF in a web view, and other text as it is, with Preview and Source for Markdown and HTML and a Copy button for text. A published picture shows in the transcript as the picture itself and opens in a lightbox: the picture alone on a dark ground, pinch to zoom, a drag to move it, and Save As… on its bar.
- A bot keeps its skills as files. A skill is now a folder with a `SKILL.md`, the format Claude Code, Codex, and Cursor read, in one of three places: the skills every bot on a computer uses, a bot's own, or a group's. A bot reads them from the next turn, writes them with the file tools, and the chat says which ones it saved, changed, or removed. Lorca ships three: one pointing at its docs, one on writing a skill, and one that brings in the skills other tools saved.
- Skills travel to your other Devices, and a Skill Store to find them: search ClawHub and skills.sh, or paste a link to a skill on GitHub, and add it to a bot or a group. A skill saved elsewhere since the sheet opened offers the latest version or saving over it.

## [1.0.20]

- A Skill Store. Search ClawHub and skills.sh, or paste a link to a skill on GitHub, and add it to a bot or a group; it works as soon as it is added. Open it from Skills with Add from Skills….
- A bot's handoff shows on both sides: the chat that sent the request gets its own "Messaged" marker beside the recipient's "Message from", even for a follow-up or a call that never finished.
- Bots make and manage groups with the new `create_group` and `edit_group` tools. New Group Chat asks what the group is for, and the first bot picked owns the work. An `@Name` of another member reads in that member's color on the desktop, and as a "→ Name" line under the bubble on the phone.
- The marketplace has 39 more connectors, with their marks, among them Tavily, Perplexity, Asana, HubSpot, MongoDB, PagerDuty, and China-market services such as 百度地图, 语雀, 钉钉, Gitee, and 腾讯地图. Zhipu's web search now also has the page reader and Zread.
- The marketplace lists by category or everything at once: a switch in its title row shows every plugin and bot in one grid, featured ones first.
- Development builds keep `target/debug` small, with line tables only and a cleaner for the build cache (`bun run clean:rust`).

## [1.0.19]

- Lorca catches up with upstream. Everything egoist/lorca shipped since the last sync is in: the Attention, Telegram, output, and Auto-review work below, plus a Runner that stays online while busy, a custom model's own thinking levels, and the phone's typing indicator.
- Right-click a brief in Attention and choose Dismiss to take it off the list on every Device; the coordinator's next brief shows again. A brief also leaves once every item it is about is resolved.
- A bot resolves an Attention item it alone reported as soon as it sees the work is done, such as a run it approved or a pull request that merged, so you no longer clear them by hand.
- A bot listening on Telegram sees the photos people send it, with a caption or without one, and an album's photos together as one message. The photo shows in the conversation on every Device.
- Several outputs in a row from one bot, such as the proof files a coding agent saves, show as one list with a line each.
- Auto-review allows an action by default, and holds only one it can name a rule for. A held action goes back to the bot before it reaches you, and push to a branch of the working repository is no longer held.
- A custom provider's model offers the thinking levels its own list declares, `xhigh` and `max` included, and sends them as written.
- While a bot's turn runs for a message a Telegram channel took, that chat shows the bot typing, and the reply stops it.
- Every bot has a built-in `lorca` skill that points at the docs on lorca.app, so it looks up how Lorca works instead of guessing.
- The phone app shows Telegram's and Slack's real marks on a chat's avatar badge, keeps the transcript scrollable while a reply arrives, and no longer crashes from an unbounded composer inset.

## [1.0.18]

- Bots delegate. Each bot knows its teammates and their jobs, and sends the part of a request that falls in a teammate's job to that teammate; in a group, the owner splits the work and assigns each part with `@Name`.
- A new bot sets itself up. New Bot creates a bot at once, with no form; in its own chat it works out its job with you, proposes a name and description for you to confirm, then suggests plugins as Install and Sign in cards you can answer whenever you like.
- A bot that passes part of its work on reports back only once that work is done, and says why when it couldn't finish.
- On Android, rows no longer go blank while you scroll a chat, and menus are drawn by the app: the composer's +, the chat list's New, and a long press on a chat or section. Button shadows are tidier on iPhone and Android.
- A source chip at the end of a line keeps its padding, and the menu bar icon on the Mac is in color.

## [1.0.17]

- Lorca on the Mac is now the desktop app that also runs on Windows and Linux, and the separate Mac-only app is gone. Download `Lorca <version>.arm64.dmg` from the latest `desktop-v` release; it replaces the old app (both are `app.lorca`) and keeps your account, which lives in `~/.lorca`. Allow it once under System Settings › Privacy & Security › Open Anyway. The old app's update feed ends with 1.0.16, so install this one by hand.
- The command line, the desktop app, and the phone app share one version number from here on, and one set of notes.
- A web link in a bot's message shows as a chip with the site's icon and name, on the desktop and on the phone.
- The desktop app has a new look: a chat's bots and name sit in a pill at the top, which opens and closes the inspector; the inspector shows the bot or group large over tabs for Activity, Profile or Members, Skills, and Routines; the sidebar has Search and Create buttons, larger rows with an unread dot, and a Marketplace button. Messages are larger and roomier, and panes, tabs, and new messages move into place, or only fade with Reduce Motion. The time inside message bubbles and its setting are gone; day rows still show it.
- The desktop app puts an icon in the menu bar or system tray, and a coding agent a bot runs can be watched in a terminal view.
- On the phone, Stop ends a running turn, a bot's question has options you tap, with Other… and Skip, and the latest exchange of a direct chat has Copy, Regenerate, and Edit. The phone app has a new monochrome look on iPhone and Android.

## [1.0.16]

- A bot can ask you to choose. The question shows in the chat with its options numbered, a mark on the one the bot suggests, and Skip to leave the choice to the bot; reply with a number or your own words, and the card keeps what you picked.
- The marketplace adds Email (IMAP/SMTP): recent mail and sending from QQ Mail, 163, Outlook, Tencent Exmail, Aliyun, and other mailboxes.
- The Mac app is no longer notarized or signed with a Developer ID, and its updates now come from this project's GitHub releases. Drag Lorca to Applications and allow it once under System Settings › Privacy & Security › Open Anyway (or run `xattr -dr com.apple.quarantine /Applications/Lorca.app`); updates the app installs for itself do not ask. Install this version by hand: apps from before it look for updates elsewhere.

## [1.0.15]

- A chat that asks for a channel gets one reply, "Pending approval", and once you allow it, the bot answers what people asked while it waited. A chat that never asks, such as a group where the bot only hears replies to it, can be added by its id with Add… on the channel's Chats row.
- Conversations a Telegram or Slack channel keeps show the service's mark on their avatar, so they read apart from the bot's own DM.

## [1.0.14]

- A channel answers only in the chats you allow. Anyone can message a Telegram bot or add it to their group, so a chat you haven't allowed now asks first: the channel's row says A chat is waiting, and its sheet lists it under Asking to Chat with Allow and Ignore. Until you allow it, the bot does nothing there. A channel that listened in every chat its bot was in now listens nowhere until you allow its chats, and each chat asks once someone writes there.
- Clicking an item in the Attention list that waits on a review opens its chat with the review on top, so you can approve or reject it right away.

## [1.0.13]

- A Telegram channel that listens for mentions or hashtags tells you when it can't hear them. While the Telegram bot's privacy mode is on, Telegram sends it only replies and commands in groups. The channel's row in the bot's Details now says Privacy mode on, its sheet says what it misses and how to turn privacy mode off, and the bot tells you when it sets up or changes such a channel.

## [1.0.12]

- Update Lorca on every Device: the Mac app, the phone app, the Windows and Linux app, and each computer that runs the command line. Devices now sign what they send each other through the relay, so a relay can no longer send work to your Runners in another Device's name, and unpairing a Device gives your account a new key, so the Device you unpaired cannot read what your account writes after that. An older Lorca cannot read what an updated Device sends, and once your Devices have updated, Lorca's relay asks older versions to update before they sync.
- Each bot has Access, which only you change: how far it may use each plugin (Read only, Read and draft, Read and write, or No access), which of its tools it may call, and whether it may read or change files and run commands on its Runner. When a bot reaches a limit, the chat offers Edit Access… to change it. A bot you never set keeps full access.
- A bot writes an email or a Slack message as a draft card in the chat, with its recipients, subject, text, and attachments editable in place, and nothing goes out until you press Send. Access › Messages turns drafts off for a bot, as Always Send does on a Slack card.
- Routines no longer refuse what Auto-review would ask you about: they leave it for your review. The chat says so, the inspector lists what waits, and Approve runs exactly what the sheet shows, edits included.
- Bots publish what they make, such as a report, a screenshot, or a chart, with what they checked: Passed, Failed, or Not verified, and the command they ran. An output shows as a card of its own in the chat, and the inspector lists a chat's outputs.
- Tasks: work that outlasts one turn, with an owning bot, what done looks like, the next step, and the evidence once it's done. A chat with tasks has a Tasks section in the inspector, and File › New Task… adds one.
- A bot that hands work to a teammate gets the result back in its chat when the teammate finishes, gets stuck, or is stopped, also from a Runner that was offline when it asked.
- One list of what needs you: the coordinating bot keeps the reviews, blockers, and promises your bots report, with a short brief, behind a toolbar button that appears while something waits.
- A group chat keeps a shared project context beside its transcript: a brief, goals, constraints, decisions, facts, links, and files, which every bot in the group reads. It's the Project section of the group's inspector.
- Skills: Save as Skill… on a bot's reply, or Save as Standing Instruction… on your own message, writes a reusable instruction from the messages you pick, and it is used once you save it. Give Feedback… on a bot's message teaches the bot, which suggests changes to its routines and skills for you to accept.
- Workflows set up an outcome in one place, such as meeting preparation, inbox triage, or watching a repository: the bot, the accounts it needs, and a sample run before it runs on a schedule. They're first in the marketplace, and on onboarding's last page.
- Share a bot as a link: File › Share as Template… shows exactly what the template holds, keys and passwords removed, and the link opens a page that shows the bot and opens it in Lorca. Neither the relay nor lorca.app can read it.
- Limits on a bot's turns, a routine's runs, and a task: spending, tokens, run time, retries, and plugin calls, with Resume when work stopped at one. Each plugin account has a call limit all bots share.
- Browser profiles keep a bot's sign-ins on its Runner. Take Over gives you the bot's browser and Return to Bot hands it back, and Record teaches the bot a browser workflow by doing it once.
- Gmail, Google Calendar, Google Drive, and Slack are in the marketplace, each with as many named accounts as you want, such as a Work Gmail beside a Personal one.
- A bot asks for a password, a key, or a code on a card, and the value goes sealed to its Runner. It never enters the chat or the model. Settings › Secrets lists them, without their values.
- Routines keep their timezone, run once when their Runner comes back from missed times (or skip them), and say when they need you. A bot can also set a one-time reminder, watch a pull request, run around your calendar's events, or run on a webhook, hosted on Lorca's relay.
- A bot can listen on a Telegram or Slack account and answer people in their thread. The Feedback collector workflow files community feedback as GitHub issues.
- A bot can run Claude Code or Codex on its Runner and supervise it, and the chat shows the run as a card with its transcript and its questions.
- Sidebar sections, and Hide and Mute in a chat's menu. A muted chat sends no notification and stays out of the Dock badge, and the phone gets no push for the chat you have in front on a computer.
- Stop also stops the work a turn handed off, and ends a turn within about 15 seconds whatever its tools do.
- Any Device renames any Device: Rename… in its Settings › Devices pane.
- A message you send while a bot waits on a background command, or on a browser you took over, reaches the bot at once.
- On a Windows Runner, a bot's commands run in a terminal, so they can go to the background, take an answer, and show in Running tasks, and Stop ends one that held the turn.
- Claude Haiku 5.5, and Sonnet 5.5's lower cache read price.
- Only an app on this Mac that holds Lorca's token can connect to its command line.
- Switching the language in Settings no longer quits the app.
- Pick the model Auto-review runs: Settings › Providers › Review Models has each provider's, a small, fast one unless you pick another, including a decision model such as OpenCode Zen's Jev 1.13, which only allows an action or names what it could harm. Settings › Auto-review › Reviews with picks another provider to review for every bot.
- Add Provider… offers decision APIs for Auto-review: OpenRouter Decisions, OpenAI Decisions, and TypeSafe, and a custom provider can speak System One or OpenAI Decisions. Bots never run on them.

## [1.0.11]

- A computer that runs the Lorca command line without the app keeps it up to date by itself: it installs each new release, signed by Lorca, and restarts into it once no bot is working there. Settings › Devices shows the version it runs, and Update when a newer one is out, so you can update it from this Mac. `lorca service install` keeps `lorca serve` running on such a computer, from login on.

## [0.1.10]

- The marketplace comes from lorca.app, so new plugins and bots show up without an update, and plugins you installed from it get their fixes the same way. Lorca checks when it starts, when the app connects, and when you or a bot look through the marketplace, at most once an hour, and a bot that can't find a plugin you asked for checks again first. `lorca marketplace reload` checks right away.
- A bot's scripts can run commands, edit files, and use its memory, as pi's do, not just read and write files and call plugins. A script can run a command for each project or file it finds, keep going when one fails, and hand the bot only what matters. Auto-review checks each command with the script that runs it, and one that needs your permission asks in the chat.
- While a bot's script runs a command, the working row says which, as it does for a command the bot runs itself, and bots can read a plugin's whole instructions from its server, not just their start.

## [0.1.9]

- Your own MCP servers: Settings › Plugins has an MCP Servers section, where you add a server by the command that runs it or its URL, or paste its JSON from a README or another app's settings, and edit, turn off, or remove it. Each server shows how it stands and the tools it offers, with Sign in when it asks for one, and every bot on that computer can use it. Lorca keeps them in `mcp.json` in its folder, in the format Claude Desktop, Cursor, and Claude Code use; after editing the file by hand, click Reload. Servers that offer resources, such as files or records, give bots tools to list and read them. Pick another Runner in Settings to manage its servers.
- MCP servers take a `timeout` for slow tools; can keep tools from bots, with a switch beside each tool on the server's sheet or `toolExposure` in mcp.json, as pi writes it; and can sign in where a server wants an app registered with it, with the redirect port or URL it was registered with, a name to register under, and the authorization server's address when the server names the wrong one. A server that needs more access asks you to sign in again for it.
- `lorca mcp` lists, adds, removes, turns on or off, hides tools of, reloads, and signs in to or out of MCP servers from a terminal, and `lorca mcp import` adds the servers Claude Desktop, Claude Code, Cursor, Windsurf, VS Code, or Gemini CLI have on the computer.
- Bots can add an MCP server the marketplace lacks, as its README gives it: the `lorca` command is in their shell, pointed at their Mac, and Auto-review checks each change it makes, asking you when you didn't ask for it.
- Plugin servers are sturdier. One that fails to start says why, in the words it printed, and one that stopped starts again on the next call instead of failing until Lorca restarts. Stopping a server also stops what it started, such as npx's node; a call you stop is called off at the server; servers start side by side, so a slow one holds up no other; a server's new tools reach bots without a reconnect; and a remote server that is busy for a moment is tried again. A server whose tool list never ends no longer hangs while it connects, and an image a tool returns that no model takes, such as an SVG, is named instead of failing every later turn of the chat. A bot that calls a tool by a wrong name hears the closest right ones. A sign-in is never sent to another host when a server's address changes, and a plugin's sheet now has Sign Out beside Sign in again.
- Bots on ChatGPT, Grok, and OpenCode's GPT, Grok, and Muse Spark models see the images their tools return, such as a browser plugin's screenshot or an image file they read. They used to get only the text beside the image.
- Bots see the images you attach and their tools return in a form every model takes. A HEIC photo, a BMP, a GIF, or a TIFF is converted, a photo taken sideways is turned upright, and a large photo or screenshot is scaled down to fit 2,000 pixels, with a note telling the bot its original size. An image that still cannot go, such as an SVG, is named with the reason. Before, such an image could make every later message in its chat fail. Each message sends a chat's latest 20 images, so a long chat full of screenshots keeps working, and a text file that starts with "BM" now reads as text.
- Pairing a Mac or restoring your identity no longer asks you to connect a provider your account already has. Onboarding waits until the account's providers arrive from the relay, which a slow connection or a long list of chats used to outlast.
- Onboarding's last step says the Mac is paired, or that your identity is restored, instead of calling it your first Device.
- Pair a Device works on every paired Mac. On a Mac that had joined by pairing it showed an error, since only the Mac that created or restored your identity could pair others. If you run your own relay, update it first.
- Settings › Devices shows a machine that is paired to your account but never sent its name or system as Unknown Device, with a note to unpair it if you don't recognize it.
- While onboarding pairs or restores, the Pair or Restore button and the field are disabled beside a spinner, and Back stops a pairing that is still waiting on the other computer.
- The marketplace adds plugins for 飞书, 飞书项目, 滴答清单, 腾讯文档, 秘塔 AI 搜索, 知乎, 高德地图, and 可灵. 飞书, 飞书项目, 滴答清单, 腾讯文档, and 可灵 sign in with your account in the browser; 秘塔 AI 搜索 and 知乎 take an API key from their sites, and 高德地图 a Web Service key from the Amap console.
- Settings › Devices can unpair this Mac too: Lorca forgets the account's keys, credentials, and chats here and goes back to onboarding. When this Mac holds your identity, the confirmation says that your backup phrase becomes the only way to restore it.

## [0.1.8]

- A routine can watch for something without spending a turn each time: the bot gives it a check, a short script that looks at an inbox, a repository, or a feed at each due time and starts the bot only when it finds something new. Checks only read and never change anything. A check that finds nothing runs no turn, so it spends nothing on the bot's model; one that asks the small model to sort or screen what it read pays for those calls, which count in the chat's Spent figure. A routine's sheet shows its check, and the inspector its next check.
- When a long chat fills a bot's context, the summary that replaces the older part, and the memory save before it, reuse the prompt cache of the bot's own turn instead of sending the whole chat again, so compacting while a bot works costs a fraction of what it did.
- A bot's command shows in the chat as a card only while it needs you: Auto-review's question, with Allow once, Always allow, and Deny, or a command the bot left running for you, with its latest output in a block that scrolls.
- Running tasks: while a chat's bots run commands, a terminal button sits in the chat's toolbar, with a badge counting them when there are two or more. It lists each command with what it does, who runs it and for how long, its latest output, and Stop, which ends that command alone while the bot carries on.
- A bot's command that asks for input (a `sudo` password, an `ssh` passphrase, a `[Y/n]`) no longer hangs its turn. You answer or stop it from its card on any of your Devices. What you type goes straight to the command and is never saved in the chat, and once the command finishes, the bot hears how it went and carries on. A command that prints nothing for 20 seconds waits the same way, and one still waiting stops after 30 minutes without output, when Lorca quits, or when its chat is deleted.
- Command output reaches the bot without colors and other terminal codes, which stay in the full-output file.
- A bot's read-only command that redirects between output streams (`2>&1`, `>&2`, `&>/dev/null`) runs at once, as other read-only commands do, instead of waiting on Auto-review, so a routine runs it with nobody watching. One that writes a file through `>&`, or from inside a quoted `$(…)`, goes to Auto-review.
- Chats with ChatGPT, Grok, and OpenCode's GPT and Grok models keep reaching the provider server that holds their prompt cache, so long chats answer sooner and cost less.
- A chat's Spent figure no longer counts cached input twice on ChatGPT, Grok, and most OpenCode models.
- Claude chats reuse their prompt cache from one turn to the next in groups and after turns with many tool calls.
- A bot speaking in one of its chats no longer costs its other chats their prompt cache.
- Auto-review weighs what a bot's action could break against what you asked for, reading the chat around your request. A step your request calls for runs without asking, destructive ones included: deleting build output, stopping a process the bot started, pushing the branch when you asked for a PR, or posting a comment after you answered "yes" to the bot's question. It still asks before harm you did not ask for, such as deleting your files, discarding uncommitted work, or deploying. The rule Always allow adds names the kind of work ("deploy Railway services to production") rather than one folder or file, so it covers the next time too.
- Bots use plugins by writing a short script that calls the plugin's tools, pages through the results, and keeps only what matters, so a long list or a big search no longer fills the chat's context. A script can also have a small, fast model rate or sort many items one by one, and that cost counts in the chat's Spent figure. A change a script wants to make still asks first on a card, and saying no stops the script.
- A plugin's tools are ready from the first chat after you install it, and chats that use plugins keep their prompt cache for the whole turn. Claude bots keep their thinking when they use a plugin.
- Lorca opens on your last chat, even when you quit it with Settings open.
- Your phone still gets the notification for a reply that finishes while the relay restarts or is briefly out of reach.

## [0.1.0]

- The first release.
