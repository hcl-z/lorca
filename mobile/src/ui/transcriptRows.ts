// The transcript's pure row builder: what a message is in the list, how a run of one speaker's
// words groups, and which reply is the latest. Kept apart from the views so it is plain to test.

import { assignedTo, isSentMessage, showsCard, showsSpeakers, type AgentRun, type Body, type Bot, type Chat, type CommandRun, type Message } from "../core/model";
import { canRegenerate, rerunnable } from "../core/rerun";
import { speakerKey } from "../core/participants";
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
/// What the previous pass built, so a streamed update rebuilds only the rows it can change.
export interface PreviousRows {
  messages: Message[];
  rows: Row[];
  isWorking: boolean;
  status: string | null;
  bots: Map<string, Bot>;
  workingBotIds: string[];
  historyElsewhere: Chat["history_elsewhere"];
}

/// Builds the rows for a chat, the way the Mac app's ChatViewController does. With `previous`,
/// the rows before the first changed message keep their identity; the rest are rebuilt.
export function buildRows(
  chat: Chat,
  bots: Map<string, Bot>,
  workingBotIds: string[],
  isWorking: boolean,
  status: string | null,
  previousRows?: PreviousRows,
): Row[] {
  const speakers = showsSpeakers(chat);
  const shown = chat.messages
    .map((message, index) => ({ message, index }))
    .filter(({ message }) => message.body.kind !== "tool" || !!message.body.agent || showsCard(message.body) || isSentMessage(message.body));
  // The latest exchange's actions, under its messages: Copy and Regenerate under the bot's last
  // reply, Edit under the user's message (`src/core/rerun.ts`).
  const run = rerunnable(chat, isWorking);
  const last = shown[shown.length - 1]?.message;
  const latestReply = !isWorking && last?.author.kind === "bot" && last.body.kind === "text" && last.state.kind === "complete" && !last.output ? last.id : undefined;

  let from = 0;
  let kept: Row[] = [];
  if (previousRows) {
    const old = previousRows.messages;
    const next = chat.messages;
    const sameVersion = (a: Message, b: Message) =>
      a.id === b.id && (a.rev !== undefined && b.rev !== undefined ? a.rev === b.rev : a.created_at === b.created_at && JSON.stringify(a.body) === JSON.stringify(b.body));
    const limit = Math.min(old.length, next.length);
    // The first message that may have moved. A batch can change one in the middle, so this is
    // found from the start; comparisons are a revision number, not a message body.
    let common = 0;
    while (common < limit && sameVersion(old[common], next[common])) common++;
    const sameBots = previousRows.bots.size === bots.size && [...bots].every(([id, bot]) => previousRows.bots.get(id) === bot);
    const sameWorking =
      previousRows.workingBotIds.length === workingBotIds.length && previousRows.workingBotIds.every((id, index) => id === workingBotIds[index]);
    const sameElsewhere =
      previousRows.historyElsewhere?.device_id === chat.history_elsewhere?.device_id &&
      previousRows.historyElsewhere?.device_name === chat.history_elsewhere?.device_name;
    if (!sameBots || !sameWorking || !sameElsewhere || previousRows.isWorking !== isWorking || previousRows.status !== status) {
      common = 0;
    }
    if (old.length === next.length && common === old.length) {
      return previousRows.rows;
    }
    let start = shown.findIndex(({ index }) => index >= common);
    if (start < 0) return previousRows.rows;
    const before = old[common];
    const after = next[common];
    if (before && after && (before.created_at !== after.created_at || JSON.stringify(before.author) !== JSON.stringify(after.author))) {
      start = Math.max(0, start - 1);
    }
    from = start;
    const keepTo = shown[from - 1]?.message.id;
    const rowIndex = keepTo ? previousRows.rows.findIndex((row) => row.key === keepTo) : -1;
    if (!keepTo) {
      from = 0;
    } else if (rowIndex >= 0) {
      kept = previousRows.rows.slice(0, rowIndex + 1);
    } else {
      from = 0;
      kept = [];
    }
  }

  const rows: Row[] = [];
  let previous: Message | null = from > 0 ? shown[from - 1].message : null;
  let previousAuthorKey: string | null = previous ? speakerKey(previous) : null;
  for (let i = from; i < shown.length; i++) {
    const message = shown[i].message;
    const separated = !previous || message.created_at - previous.created_at >= SEPARATOR_GAP_SECS;
    if (separated) {
      rows.push({ key: `day-${message.id}`, type: "day", at: message.created_at });
      previousAuthorKey = null;
    }
    const authorKey = speakerKey(message);
    const groupStart = separated || authorKey !== previousAuthorKey;
    switch (message.body.kind) {
      case "text": {
        const next = shown[i + 1]?.message;
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
          showsName: speakers && (message.author.kind === "bot" || message.author.kind === "contact") && groupStart,
          copyable: message.id === latestReply && message.body.text.trim().length > 0,
          regenerable: message.id === latestReply && canRegenerate(chat, isWorking, message),
          editable: run?.asked.id === message.id,
          assigned: chat.kind === "group" ? assignedTo(message, chat.bot_ids, bots) : NO_BOTS,
          stackAbove: output && !groupStart && isOutputCard(shown[i - 1]?.message),
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
  const header: Row[] = [];
  if (chat.history_elsewhere) {
    header.push({ key: "history-elsewhere", type: "status", text: t("History is on {name}", { name: chat.history_elsewhere.device_name }) });
  }
  if (isWorking) rows.push({ key: "working", type: "working", bots: workingBotIds.map((id) => bots.get(id)).filter((b): b is Bot => !!b) });
  else if (status) rows.push({ key: "status", type: "status", text: status });
  return [...kept, ...header, ...rows];
}
