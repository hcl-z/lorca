import { expect, mock, test } from "bun:test";
import type { Bot, Chat, DurableTask, Message } from "./model";

// The roster keeps what did not change: the screens memoize on these identities.
mock.module("./prefs", () => ({ loadPrefs: () => ({}), savePrefs: () => {}, coreHome: () => "/unused", pathOf: (p: string) => p, wipePrefs: () => {} }));
mock.module("expo-localization", () => ({ getLocales: () => [{ languageCode: "en", languageTag: "en-US" }] }));
const { applyRoster, replaceSnapshot, sameMessage, useStore } = await import("./store");

const bot: Bot = { id: "bot", name: "Chef", symbol_name: "sparkles", accent: "indigo" } as Bot;

function meta(id: string, extra: Partial<Chat> = {}) {
  return { id, kind: "dm" as const, title: id, bot_ids: ["bot"], is_pinned: false, created_at: 0, unread_count: 0, usage: { input_tokens: 1 } as any, ...extra };
}

test("a roster that repeats the store keeps its chats, bots and lists", () => {
  applyRoster({ devices: [], bots: [bot], chats: [meta("a"), meta("b")] });
  const before = useStore.getState();
  applyRoster({ devices: [], bots: [{ ...bot }], chats: [meta("a"), meta("b")] });
  const after = useStore.getState();
  expect(after.chats).toBe(before.chats);
  expect(after.bots).toBe(before.bots);
  expect(after.devices).toBe(before.devices);
});

test("a changed chat is new, the others keep their identity", () => {
  applyRoster({ devices: [], bots: [bot], chats: [meta("a"), meta("b")] });
  const before = useStore.getState().chats;
  applyRoster({ devices: [], bots: [bot], chats: [meta("a"), meta("b", { unread_count: 2 })] });
  const after = useStore.getState().chats;
  expect(after).not.toBe(before);
  expect(after[0]).toBe(before[0]);
  expect(after[1]).not.toBe(before[1]);
  expect(after[1].unread_count).toBe(2);
});

// A snapshot can be older than a task a reply or an event delivered; tasks are retained records.
test("a snapshot keeps a newer task, and one it does not list, for the same identity", () => {
  const snapshot = (identity: string | null, tasks: DurableTask[]) => ({ has_identity: identity !== null, identity_id: identity, this_device_id: "phone", relay_url: null, relay_connected: false, devices: [], bots: [], chats: [], running_turns: [], tasks });
  const task = (id: string, revision: number) => ({ id, revision, goal: `${id} r${revision}` }) as DurableTask;
  replaceSnapshot(snapshot("me", [task("a", 1)]));
  useStore.setState({ tasks: [task("a", 3), task("b", 1)] });
  replaceSnapshot(snapshot("me", [task("a", 2)]));
  expect(useStore.getState().tasks.map((t) => t.goal)).toEqual(["a r3", "b r1"]);
  replaceSnapshot(snapshot("me", [task("a", 4)]));
  expect(useStore.getState().tasks.map((t) => t.goal)).toEqual(["a r4", "b r1"]);
  replaceSnapshot(snapshot("other", [task("c", 1)]));
  expect(useStore.getState().tasks.map((t) => t.goal)).toEqual(["c r1"]);
});

// A message changes its own chat alone: the others keep their identity, so the chat list's rows
// and the agents' cards built from them are not drawn again.
test("a message keeps every other chat as it was", async () => {
  const { upsertMessage } = await import("./store");
  applyRoster({ devices: [], bots: [bot], chats: [meta("a"), meta("b")] });
  const message = (id: string, chat: string, text: string) => ({ id, chat_id: chat, author: { kind: "you" }, body: { kind: "text", text }, state: { kind: "complete" }, created_at: 1 }) as never;
  const before = useStore.getState().chats;
  expect(upsertMessage(message("m1", "a", "hi")).added).toBe(true);
  const after = useStore.getState().chats;
  expect(after[0].messages.map((m) => m.id)).toEqual(["m1"]);
  expect(after[1]).toBe(before[1]);

  // The same message again changes nothing; an edit changes that chat only.
  upsertMessage(message("m1", "a", "hi"), false);
  expect(useStore.getState().chats).toBe(after);
  upsertMessage(message("m1", "a", "hello"), false);
  expect(useStore.getState().chats[0].messages[0].body).toEqual({ kind: "text", text: "hello" } as never);
  expect(useStore.getState().chats[1]).toBe(before[1]);

  // A message ahead of its roster waits under a placeholder chat.
  upsertMessage(message("m2", "c", "early"));
  expect(useStore.getState().chats.map((c) => c.id)).toEqual(["a", "b", "c"]);
});

test("a snapshot that repeats a chat keeps it, messages and all", () => {
  const message = (text: string) => ({ id: "m1", chat_id: "a", author: { kind: "bot", bot_id: "bot" }, body: { kind: "text", text }, state: { kind: "complete" }, created_at: 1 }) as Message;
  const snapshot = (text: string) => ({ has_identity: true, identity_id: "me", this_device_id: "phone", relay_url: null, relay_connected: false, devices: [], bots: [bot], chats: [{ ...meta("a"), messages: [message(text)] }], running_turns: [] });
  replaceSnapshot(snapshot("hi"));
  const before = useStore.getState().chats;
  replaceSnapshot(snapshot("hi"));
  expect(useStore.getState().chats).toBe(before);
  expect(useStore.getState().chats[0].messages[0]).toBe(before[0].messages[0]);
  replaceSnapshot(snapshot("hello"));
  const after = useStore.getState().chats;
  expect(after).not.toBe(before);
  expect(after[0]).not.toBe(before[0]);
  expect(after[0].messages[0]).not.toBe(before[0].messages[0]);
  expect(after[0].messages[0].body).toEqual({ kind: "text", text: "hello" } as never);
});

test("sameMessage compares the fields that changed", () => {
  const message = (extra: Record<string, unknown> = {}) => ({ id: "m", chat_id: "a", author: { kind: "bot", bot_id: "bot" }, body: { kind: "text", text: "hi" }, state: { kind: "complete" }, created_at: 1, ...extra }) as Message;
  expect(sameMessage(message(), message())).toBe(true);
  expect(sameMessage(message(), message({ edited_at: 3 }))).toBe(false);
  expect(sameMessage(message(), message({ state: { kind: "streaming" } }))).toBe(false);
  expect(sameMessage(message(), message({ body: { kind: "text", text: "ho" } }))).toBe(false);
  expect(sameMessage(message({ output: { id: "o", name: "a", mime: "x", bot_id: "bot", version: 1 } }), message({ output: { id: "o", name: "a", mime: "x", bot_id: "bot", version: 2 } }))).toBe(false);
  expect(sameMessage(message({ output: { id: "o", name: "a", mime: "x", bot_id: "bot", version: 1 } }), message({ output: { id: "o", name: "a", mime: "x", bot_id: "bot", version: 1 } }))).toBe(true);
});
