// Running the latest exchange of a direct chat again (`chats.rerun`), as the desktop app's
// `model/rerun.go`: Regenerate under the bot's reply, and Edit under the user's message, which
// sends it again with new words. The bot's Runner takes what came after the message out of the
// chat on every Device and answers it anew; the core checks the same rules.

import type { Chat, Message } from "./model";

/// The user's message whose exchange can run again, with the rows after it: the user's latest
/// message in a direct chat with no turn at work, whose reply left nothing the user still has to
/// act on or that lives on elsewhere (a draft, a published output, work handed to another bot).
export function rerunnable(chat: Chat | undefined, responding: boolean): { asked: Message; after: Message[] } | null {
  if (!chat || chat.kind !== "dm" || chat.channel || responding) return null;
  let at = -1;
  for (let i = chat.messages.length - 1; i >= 0; i--) {
    if (chat.messages[i].author.kind === "you") {
      at = i;
      break;
    }
  }
  if (at < 0) return null;
  const asked = chat.messages[at];
  if (asked.queued || asked.state.kind !== "complete" || asked.body.kind !== "text") return null;
  // A message addressed to a coding agent is the agent's; a bot reply cannot run it again.
  if (asked.body.agents?.length) return null;
  const after = chat.messages.slice(at + 1);
  for (const later of after) {
    if (later.body.kind === "draft" || later.output) return null;
    if (later.body.kind === "tool" && later.body.target_bot_id) return null;
    if (later.body.kind === "handoff" && later.id.startsWith("sent-")) return null;
  }
  return { asked, after };
}

/// Whether `message` is the bot's reply to the rerunnable message: its last text, so Regenerate
/// shows once, under the end of the answer.
export function canRegenerate(chat: Chat | undefined, responding: boolean, message: Message): boolean {
  const run = rerunnable(chat, responding);
  if (!run || message.author.kind !== "bot") return false;
  for (let i = run.after.length - 1; i >= 0; i--) {
    const later = run.after[i];
    if (later.author.kind === "bot" && later.body.kind === "text") return later.id === message.id;
  }
  return false;
}

/// Whether `message` is the user's rerunnable message.
export function canEdit(chat: Chat | undefined, responding: boolean, message: Message): boolean {
  return rerunnable(chat, responding)?.asked.id === message.id;
}

/// The words a rerun sends: the edit when it changed them, else none, which keeps the message.
export function editedText(asked: Message, text: string | undefined): string | undefined {
  const edited = text?.trim();
  if (!edited || asked.body.kind !== "text" || edited === asked.body.text) return undefined;
  return edited;
}
