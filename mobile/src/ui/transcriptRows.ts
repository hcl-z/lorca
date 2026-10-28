// The transcript's pure row builder: what a message is in the list, how a run of one speaker's
// words groups, and which reply is the latest. Kept apart from the views so it is plain to test.

import { assignedTo, isSentMessage, showsCard, type AgentRun, type Body, type Bot, type Chat, type CommandRun, type Message } from "../core/model";
import { canRegenerate, rerunnable } from "../core/rerun";
import { showsSpeakersIn, speakerKey } from "../core/participants";
import { t } from "../i18n";
import type { DraftBody } from "./drafts";

export const SEPARATOR_GAP_SECS = 15 * 60;
/// A message that hands nothing to anyone, shared so its row compares equal from build to build.
const NO_BOTS: Bot[] = [];

export type Row =
  | { key: string; type: "day"; at: number }
  | { key: string; type: "message"; message: Message; groupStart: boolean; groupEnd: boolean; showsName: boolean; copyable: boolean; regenerable: boolean; editable: boolean; stackAbove: boolean; stackBelow: boolean; assigned: Bot[] }
  | { key: string; type: "marker"; text: string; bot: Bot | undefined; tooltip?: string; groupStart: boolean }
  | { key: string; type: "notice"; text: string; groupStart: boolean }
  | { key: string; type: "permission"; message: Message; body: Extract<Body, { kind: "permission" }>; bot: Bot | undefined; groupStart: boolean }
  | { key: string; type: "command"; message: Message; run: CommandRun; bot: Bot | undefined; groupStart: boolean }
  | { key: string; type: "agent"; message: Message; agent: AgentRun; bot: Bot | undefined; groupStart: boolean }
  | { key: string; type: "draft"; message: Message; body: DraftBody; bot: Bot | undefined; groupStart: boolean }
  | { key: string; type: "working"; bots: Bot[] }
  | { key: string; type: "status"; text: string };

/// A bot's published output, which shows as a card of its own.
function isOutputCard(message: Message): boolean {
  return message.output !== undefined && message.author.kind !== "you";
}

/// Builds the rows for a chat, the way the Mac app's ChatViewController does.
export function buildRows(chat: Chat, bots: Map<string, Bot>, workingBotIds: string[], isWorking: boolean, status: string | null): Row[] {
  const rows: Row[] = [];
  const speakers = showsSpeakersIn(chat);
  let previous: Message | null = null;
  let previousAuthorKey: string | null = null;
  const shown = chat.messages.filter((m) => m.body.kind !== "tool" || !!m.body.agent || showsCard(m.body) || isSentMessage(m.body));
  // The latest exchange's actions, under its messages: Copy and Regenerate under the bot's last
  // reply, Edit under the user's message (`src/core/rerun.ts`).
  const run = rerunnable(chat, isWorking);
  const last = shown[shown.length - 1];
  const latestReply = !isWorking && last?.author.kind === "bot" && !last.agent && last.body.kind === "text" && last.state.kind === "complete" && !last.output ? last.id : undefined;
  for (let i = 0; i < shown.length; i++) {
    const message = shown[i];
    const separated = !previous || message.created_at - previous.created_at >= SEPARATOR_GAP_SECS;
    if (separated) {
      rows.push({ key: `day-${message.id}`, type: "day", at: message.created_at });
      previousAuthorKey = null;
    }
    const authorKey = speakerKey(message);
    const groupStart = separated || authorKey !== previousAuthorKey;
    switch (message.body.kind) {
      case "text": {
        const next = shown[i + 1];
        const nextKey = next ? speakerKey(next) : null;
        const nextSeparated = next ? next.created_at - message.created_at >= SEPARATOR_GAP_SECS : true;
        const groupEnd = nextSeparated || nextKey !== authorKey || next?.body.kind !== "text";
        // A run of one bot's outputs joins into one list.
        const output = isOutputCard(message);
        rows.push({
          key: message.id,
          type: "message",
          message,
          groupStart,
          groupEnd,
          showsName: speakers && (message.author.kind === "bot" || message.author.kind === "contact" || !!message.agent) && groupStart,
          copyable: message.id === latestReply && message.body.text.trim().length > 0,
          regenerable: message.id === latestReply && canRegenerate(chat, isWorking, message),
          editable: run?.asked.id === message.id,
          assigned: chat.kind === "group" ? assignedTo(message, chat.bot_ids, bots) : NO_BOTS,
          stackAbove: output && !groupStart && isOutputCard(shown[i - 1]),
          stackBelow: output && !groupEnd && isOutputCard(next),
        });
        previousAuthorKey = authorKey;
        break;
      }
      case "tool":
        if (message.body.agent) {
          const bot = message.author.kind === "bot" ? bots.get(message.author.bot_id) : undefined;
          rows.push({ key: message.id, type: "agent", message, agent: message.body.agent, bot, groupStart });
        } else if (message.body.run) {
          const bot = message.author.kind === "bot" ? bots.get(message.author.bot_id) : undefined;
          rows.push({ key: message.id, type: "command", message, run: message.body.run, bot, groupStart });
        } else {
          rows.push({ key: message.id, type: "marker", text: t("Messaged"), bot: bots.get(message.body.target_bot_id ?? ""), tooltip: message.body.detail, groupStart });
        }
        previousAuthorKey = null;
        break;
      case "handoff": {
        // From a bot outside the chat, as a DM's request or a handoff's report: a message.
        const incoming = chat.bot_ids.includes(message.body.to) && !chat.bot_ids.includes(message.body.from);
        // To a bot outside the chat, as the request its bot sent: a message it sent.
        const outgoing = chat.bot_ids.includes(message.body.from) && !chat.bot_ids.includes(message.body.to);
        rows.push({
          key: message.id,
          type: "marker",
          text: incoming ? t("Message from") : outgoing ? t("Messaged") : t("Handed off to"),
          bot: bots.get(incoming ? message.body.from : message.body.to),
          tooltip: message.body.reason,
          groupStart,
        });
        previousAuthorKey = null;
        break;
      }
      case "notice":
        rows.push({ key: message.id, type: "notice", text: message.body.text, groupStart });
        previousAuthorKey = null;
        break;
      case "permission":
        rows.push({ key: message.id, type: "permission", message, body: message.body, bot: message.author.kind === "bot" ? bots.get(message.author.bot_id) : undefined, groupStart });
        previousAuthorKey = null;
        break;
      case "draft":
        rows.push({ key: message.id, type: "draft", message, body: message.body, bot: message.author.kind === "bot" ? bots.get(message.author.bot_id) : undefined, groupStart });
        previousAuthorKey = null;
        break;
    }
    previous = message;
  }
  if (isWorking) rows.push({ key: "working", type: "working", bots: workingBotIds.map((id) => bots.get(id)).filter((b): b is Bot => !!b) });
  else if (status) rows.push({ key: "status", type: "status", text: status });
  return rows;
}
