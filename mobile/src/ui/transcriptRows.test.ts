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

const card = (id: string, run: AgentRun): Message => ({
  id,
  chat_id: "chat",
  author: { kind: "bot", bot_id: "b1" },
  body: { kind: "tool", name: "coding_agent", summary: run.name ?? "", detail: "", is_running: false, agent: run },
  state: { kind: "complete" },
  created_at: 1,
});

describe("transcript rows", () => {
  test("a DM reads without speakers, even with an agent at work in it", () => {
    const messages = [card("c1", agent("a1", "Claude Code")), text("m1", { kind: "bot", bot_id: "b1" }, "one", 2), text("m2", { kind: "bot", bot_id: "b1" }, "two", 3)];
    const rows = buildRows(chat(messages), new Map([["b1", bot("b1", "Chef")]]), [], false, null).filter((row) => row.type === "message");
    expect(rows.map((row) => row.showsName)).toEqual([false, false]);
    expect(rows.map((row) => row.groupStart)).toEqual([true, false]);
  });

  test("a streamed last message rebuilds only the last rows", () => {
    const messages = [text("m1", { kind: "you" }, "one", 1), text("m2", { kind: "you" }, "two", 2)];
    const first = buildRows(chat(messages), new Map(), [], false, null);
    const changed = [...messages.slice(0, -1), { ...messages[1], body: { kind: "text", text: "two!" }, rev: 2 } as Message];
    const next = buildRows(chat(changed), new Map(), [], false, null, { messages, rows: first, isWorking: false, status: null, bots: new Map(), workingBotIds: [], historyElsewhere: undefined });
    expect(next[0]).toBe(first[0]);
    expect(next.at(-1)).not.toBe(first.at(-1));
  });

  test("a change before the last message rebuilds it too", () => {
    const messages = [text("m1", { kind: "you" }, "one", 1), text("m2", { kind: "you" }, "two", 2)];
    const first = buildRows(chat(messages), new Map(), [], false, null);
    const changed = [{ ...messages[0], body: { kind: "text", text: "one!" }, rev: 5 } as Message, messages[1]];
    const next = buildRows(chat(changed), new Map(), [], false, null, { messages, rows: first, isWorking: false, status: null, bots: new Map(), workingBotIds: [], historyElsewhere: undefined });
    expect(next[0]).not.toBe(first[0]);
  });

  test("a message an agent wrote into the chat before reads as its bot's", () => {    // From when an agent took part in the chat: its words carried `agent`, which no longer counts.
    const old = { ...text("m1", { kind: "bot", bot_id: "b1" }, "one", 2), agent: { id: "a1", name: "Claude Code", kind: "claude" } } as Message;
    const messages = [text("u1", { kind: "you" }, "hi", 1), old];
    const rows = buildRows(chat(messages), new Map([["b1", bot("b1", "Chef")]]), [], false, null).filter((row) => row.type === "message");
    expect(rows[1].type === "message" && rows[1].copyable).toBe(true);
    expect(messageAuthorName(old, new Map([["b1", bot("b1", "Chef")]]))).toBe("Chef");
    expect(preview(chat([old]), new Map())).toBe("one");
  });

  test("preview keeps the group's bot name", () => {
    const c = chat([text("m1", { kind: "bot", bot_id: "b1" }, "all done", 2)], ["b1"], "group");
    expect(preview(c, new Map([["b1", bot("b1", "Chef")]]))).toBe("Chef: all done");
  });

  test("a quote names the bot that wrote it", () => {
    const quote = { message_id: "m1", author: { kind: "bot" as const, bot_id: "b1" }, text: "one" };
    expect(quoteAuthorName(quote, new Map([["b1", bot("b1", "Chef")]]))).toBe("Chef");
  });

  test("an agent card itself is not a message bubble", () => {
    const rows = buildRows(chat([card("c1", agent("a1", "Claude Code"))]), new Map([["b1", bot("b1", "Chef")]]), [], false, null);
    expect(rows.some((row) => row.type === "message")).toBe(false);
    expect(rows.some((row) => row.type === "agent")).toBe(true);
  });
});
