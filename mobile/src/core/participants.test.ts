import { describe, expect, test } from "bun:test";
import { agentAskCard, chatAgents, mentionTargets, participantNames, speakerKey } from "./participants";
import { agentKindName, type AgentRun, type Bot, type Chat, type Message } from "./model";

const bot = (id: string, name: string): Bot =>
  ({ id, name, runner_id: "runner", provider: "deepseek", created_at: 0 }) as Bot;

const agent = (id: string, state: AgentRun["state"], started = 0, name = "Claude Code"): AgentRun =>
  ({ id, kind: "claude", name, state, started_at: started, task: "Fix it" }) as AgentRun;

const tool = (id: string, botId: string, run: AgentRun): Message => ({
  id,
  chat_id: "chat",
  author: { kind: "bot", bot_id: botId },
  body: { kind: "tool", name: "coding_agent", summary: run.name ?? "", detail: "", is_running: false, agent: run },
  state: { kind: "complete" },
  created_at: 1,
});

const said = (id: string): Message => ({
  id,
  chat_id: "chat",
  author: { kind: "you" },
  body: { kind: "text", text: "hi" },
  state: { kind: "complete" },
  created_at: 1,
});

const chat = (messages: Message[], botIds = ["b1"], kind = "dm"): Chat =>
  ({
    id: "chat",
    kind,
    bot_ids: botIds,
    messages,
    unread_count: 0,
  }) as Chat;

describe("chat agents", () => {
  test("chatAgents lists open agents newest first and skips checking", () => {
    const idle = agent("a1", "idle", 1);
    const working = agent("a2", "working", 2);
    const checking = agent("a3", "checking", 3);
    const found = chatAgents(chat([tool("m1", "b1", idle), tool("m2", "b1", working), tool("m3", "b1", checking)]));
    expect(found.map(({ agent }) => agent.id)).toEqual(["a2", "a1"]);
  });

  test("an ended agent leaves the header", () => {
    const ended = agent("a1", "stopped", 1);
    expect(chatAgents(chat([tool("m1", "b1", ended)]))).toEqual([]);
  });
});

describe("mention targets", () => {
  const bots = new Map([["b1", bot("b1", "Chef")], ["b2", bot("b2", "Scout")]]);

  test("a one-bot chat offers no one, its agents included: they are written to on their page", () => {
    const c = chat([tool("m1", "b1", agent("a1", "idle"))], ["b1"]);
    expect(mentionTargets(c, bots)).toEqual([]);
  });

  test("a multi-bot group offers its bots only", () => {
    const c = chat([tool("m1", "b1", agent("a1", "idle"))], ["b1", "b2"], "group");
    expect(mentionTargets(c, bots).map((target) => `${target.kind}:${target.name}`)).toEqual(["bot:Chef", "bot:Scout"]);
  });
});

describe("speakers and names", () => {
  test("speaker keys follow who wrote the message", () => {
    expect(speakerKey(said("m1"))).toBe("you");
    expect(speakerKey(tool("m2", "b1", agent("a1", "idle")))).toBe("bot:b1");
  });

  test("participantNames stacks the bot then its active agents", () => {
    expect(participantNames([bot("b1", "Chef")], [agent("a1", "working", 1)])).toEqual(["Chef", "Claude Code"]);
  });
});


test("each agent kind has a name without its card", () => {
  const names: Record<string, string> = {
    claude: "Claude Code",
    codex: "Codex",
    gemini: "Gemini CLI",
    copilot: "GitHub Copilot",
    cursor: "Cursor",
    opencode: "OpenCode",
    kimi: "Kimi Code",
    pi: "pi",
    kiro: "Kiro",
    unknown: "Coding agent",
  };
  for (const [kind, name] of Object.entries(names)) expect(agentKindName(kind)).toBe(name);
});


test("the agent's ask row is the one its card points to", () => {
  const ask = { agent_id: "a1", kind: "claude", name: "Claude Code", row_id: "ask-1", message: "Which one?", fields: [] };
  const card = {
    id: "ask-1",
    chat_id: "chat",
    author: { kind: "bot", bot_id: "b1" },
    body: { kind: "permission", plugin_id: "agent", plugin_name: "Claude Code", tool: "agent_ask", summary: "Claude Code asks", arguments: {}, decision: "pending", agent_ask: ask },
    state: { kind: "complete" },
    created_at: 1,
  } as Message;
  const run = agent("a1", "asking", 1);
  run.asking = "ask-1";
  expect(agentAskCard(chat([card]), run)?.id).toBe("ask-1");
  expect(agentAskCard(chat([card]), agent("a2", "working", 2))).toBeUndefined();
});
