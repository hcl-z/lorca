// Who is in a chat and who `@` can address: the coding agents a bot started, the chat's bots,
// and the speaker key that keeps an agent's words apart from its bot's.

import { agentIsOpen, agentName, showsSpeakers, type AgentRun, type Bot, type Chat, type Message } from "./model";

export type MentionTarget =
  | { kind: "bot"; id: string; name: string; bot: Bot }
  | { kind: "agent"; id: string; name: string; agent: AgentRun };

export interface ChatAgent {
  message: Message;
  agent: AgentRun;
}

/// The chat's open coding agents, newest first. An agent Auto-review is still deciding on has no
/// conversation yet; a checking card is not offered. The answer is kept per chat object, which
/// the transcript, the composer's `@` list, and the header all ask for in one render.
const agentsByChat = new WeakMap<Chat, ChatAgent[]>();
export function chatAgents(chat: Chat): ChatAgent[] {
  const cached = agentsByChat.get(chat);
  if (cached) return cached;
  const seen = new Set<string>();
  const found: ChatAgent[] = [];
  for (const message of chat.messages) {
    const agent = message.body.kind === "tool" ? message.body.agent : undefined;
    if (!agent || !agent.id || !agentIsOpen(agent) || agent.state === "checking" || seen.has(agent.id)) continue;
    seen.add(agent.id);
    found.push({ message, agent });
  }
  const sorted = found.sort((a, b) => (b.agent.started_at ?? 0) - (a.agent.started_at ?? 0));
  agentsByChat.set(chat, sorted);
  return sorted;
}

/// The chat has shown a coding agent at all: one is open, or one has written here and ended.
/// A direct chat with an agent reads with speakers, as a group does.
export function chatHasAgents(chat: Chat): boolean {
  return chat.messages.some((message) => !!message.agent || (message.body.kind === "tool" && !!message.body.agent));
}

const speakersByChat = new WeakMap<Chat, boolean>();
export function showsSpeakersIn(chat: Chat): boolean {
  const cached = speakersByChat.get(chat);
  if (cached !== undefined) return cached;
  const shown = showsSpeakers(chat) || chatHasAgents(chat);
  speakersByChat.set(chat, shown);
  return shown;
}

/// The key a run of messages groups by: one per agent, bot, contact, or the user.
export function speakerKey(message: Message): string {
  if (message.agent) return `agent:${message.agent.id}`;
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

/// The `@` menu's targets: a multi-bot chat offers its bots, then the agents its Runner can take
/// messages for. A one-bot chat offers only its agents: its bot is implicit.
export function mentionTargets(chat: Chat, bots: Map<string, Bot>, runnerSupportsAgents: boolean): MentionTarget[] {
  const targets: MentionTarget[] = [];
  if (chat.bot_ids.length > 1) {
    for (const id of chat.bot_ids) {
      const bot = bots.get(id);
      if (bot) targets.push({ kind: "bot", id: bot.id, name: bot.name, bot });
    }
  }
  if (runnerSupportsAgents) {
    for (const { agent } of chatAgents(chat)) {
      targets.push({ kind: "agent", id: agent.id, name: agentName(agent), agent });
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
