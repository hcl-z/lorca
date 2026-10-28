import { describe, expect, test } from "bun:test";
import type { AgentRun, Bot, Chat, Message } from "../core/model";
import { messageAuthorName, preview, quoteAuthorName } from "./format";
import { buildRows } from "./transcriptRows";

const bot = (id: string, name: string): Bot =>
  ({ id, name, runner_id: "runner", provider: "deepseek", created_at: 0 }) as Bot;

const agent = (id: string, name: string, state: AgentRun["state"] = "working"): AgentRun =>
  ({ id, kind: "claude", name, state, task: "Fix it", started_at: 1 }) as AgentRun;

const chat = (messages: Message[], botIds = ["b1"], kind = "dm"): Chat =>
  ({ id: "chat", kind, bot_ids: botIds, messages, unread_count: 0, created_at: 0 }) as Chat;

const text = (id: string, author: Message["author"], body: string, at = 1, patch: Partial<Message> = {}): Message => ({
  id,
  chat_id: "chat",
  author,
  body: { kind: "text", text: body },
  state: { kind: "complete" },
  created_at: at,
  ...patch,
});

const agentSaid = (id: string, run: AgentRun, body: string, at = 2): Message =>
  text(id, { kind: "bot", bot_id: "b1" }, body, at, { agent: { id: run.id, name: run.name ?? "Claude Code", kind: run.kind } });

const card = (id: string, run: AgentRun): Message => ({
  id,
  chat_id: "chat",
  author: { kind: "bot", bot_id: "b1" },
  body: { kind: "tool", name: "coding_agent", summary: run.name ?? "", detail: "", is_running: false, agent: run },
  state: { kind: "complete" },
  created_at: 1,
});

describe("transcript rows", () => {
  test("agent messages group by their speaker, not the bot", () => {
    const run = agent("a1", "Claude Code");
    const messages = [text("u1", { kind: "you" }, "hi", 1), agentSaid("m1", run, "one", 2), agentSaid("m2", run, "two", 3), text("m3", { kind: "bot", bot_id: "b1" }, "three", 4)];
    const rows = buildRows(chat(messages), new Map([["b1", bot("b1", "Chef")]]), [], false, null).filter((row) => row.type === "message");
    expect(rows.map((row) => row.groupStart)).toEqual([true, true, false, true]);
  });

  test("a DM with an agent shows the bot's and the agent's names", () => {
    const run = agent("a1", "Claude Code");
    const messages = [agentSaid("m1", run, "one", 2), text("m2", { kind: "bot", bot_id: "b1" }, "two", 3)];
    const rows = buildRows(chat(messages), new Map([["b1", bot("b1", "Chef")]]), [], false, null).filter((row) => row.type === "message");
    expect(rows.map((row) => row.showsName)).toEqual([true, true]);
  });

  test("an agent reply breaks the bot's bubble run", () => {
    const run = agent("a1", "Claude Code");
    const messages = [text("m1", { kind: "bot", bot_id: "b1" }, "one", 2), agentSaid("m2", run, "two", 3), text("m3", { kind: "bot", bot_id: "b1" }, "three", 4)];
    const rows = buildRows(chat(messages), new Map([["b1", bot("b1", "Chef")]]), [], false, null).filter((row) => row.type === "message");
    expect(rows.map((row) => row.groupStart)).toEqual([true, true, true]);
  });

  test("the latest reply is the bot's, not an agent's", () => {
    const run = agent("a1", "Claude Code");
    const messages = [text("u1", { kind: "you" }, "hi", 1), text("m1", { kind: "bot", bot_id: "b1" }, "one", 2), agentSaid("m2", run, "two", 3)];
    const rows = buildRows(chat(messages), new Map([["b1", bot("b1", "Chef")]]), [], false, null).filter((row) => row.type === "message");
    expect(rows.every((row) => !row.copyable && !row.regenerable)).toBe(true);
  });

  test("preview names the agent that spoke", () => {
    const c = chat([agentSaid("m1", agent("a1", "Claude Code"), "status is green", 2)]);
    expect(preview(c, new Map())).toBe("Claude Code: status is green");
  });

  test("preview keeps the group's bot name", () => {
    const c = chat([text("m1", { kind: "bot", bot_id: "b1" }, "all done", 2)], ["b1"], "group");
    expect(preview(c, new Map([["b1", bot("b1", "Chef")]]))).toBe("Chef: all done");
  });

  test("quotes and reply targets name the agent behind a bot message", () => {
    const run = agent("a1", "Claude Code");
    const said = agentSaid("m1", run, "one", 2);
    const quote = { message_id: "m1", author: { kind: "bot" as const, bot_id: "b1" }, text: "one", agent: said.agent };
    expect(quoteAuthorName(quote, new Map())).toBe("Claude Code");
    expect(messageAuthorName(said, new Map())).toBe("Claude Code");
    expect(messageAuthorName(text("m2", { kind: "bot", bot_id: "b1" }, "two"), new Map([["b1", bot("b1", "Chef")]]))).toBe("Chef");
  });

  test("an agent card itself is not a message bubble", () => {
    const rows = buildRows(chat([card("c1", agent("a1", "Claude Code"))]), new Map([["b1", bot("b1", "Chef")]]), [], false, null);
    expect(rows.some((row) => row.type === "message")).toBe(false);
    expect(rows.some((row) => row.type === "agent")).toBe(true);
  });
});
