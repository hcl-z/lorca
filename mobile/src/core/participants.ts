// Who is in a chat and who `@` can address: the chat's bots, and the coding agents its bots run,
// which its header shows beside them.

import { agentIsOpen, agentName, type AgentRun, type Bot, type Chat, type Message } from "./model";

export type MentionTarget = { kind: "bot"; id: string; name: string; bot: Bot };

export interface ChatAgent {
  message: Message;
  agent: AgentRun;
}

/// The chat's open coding agents, newest first. An agent Auto-review is still deciding on has no
/// conversation yet; a checking card is not offered.
export function chatAgents(chat: Chat): ChatAgent[] {
  const seen = new Set<string>();
  const found: ChatAgent[] = [];
  for (const message of chat.messages) {
    const agent = message.body.kind === "tool" ? message.body.agent : undefined;
    if (!agent || !agent.id || !agentIsOpen(agent) || agent.state === "checking" || seen.has(agent.id)) continue;
    seen.add(agent.id);
    found.push({ message, agent });
  }
  return found.sort((a, b) => (b.agent.started_at ?? 0) - (a.agent.started_at ?? 0));
}

/// The key a run of messages groups by: one per bot, contact, or the user.
export function speakerKey(message: Message): string {
  switch (message.author.kind) {
    case "bot":
      return `bot:${message.author.bot_id}`;
    case "contact":
      return `contact:${message.author.name}`;
    case "system":
      return "system";
    default:
      return "you";
  }
}

/// The `@` menu's targets: a multi-bot chat offers its bots. A one-bot chat offers none: its bot
/// is implicit, and the user writes to a coding agent on its own page.
export function mentionTargets(chat: Chat, bots: Map<string, Bot>): MentionTarget[] {
  const targets: MentionTarget[] = [];
  if (chat.bot_ids.length > 1) {
    for (const id of chat.bot_ids) {
      const bot = bots.get(id);
      if (bot) targets.push({ kind: "bot", id: bot.id, name: bot.name, bot });
    }
  }
  return targets;
}

/// The names a header cluster shows: the bots, then the active agents, at most `max`.
export function participantNames(bots: Bot[], agents: AgentRun[], max = 2): string[] {
  return [...bots.map((bot) => bot.name), ...agents.map(agentName)].filter(Boolean).slice(0, max);
}

/// The permission card the agent's `asking` points at, when it holds the agent's form.
export function agentAskCard(chat: Chat, agent: AgentRun | undefined): Message | undefined {
  if (!agent?.asking) return undefined;
  const message = chat.messages.find((each) => each.id === agent.asking);
  return message?.body.kind === "permission" && message.body.agent_ask ? message : undefined;
}
