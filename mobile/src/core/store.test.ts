import { expect, mock, test } from "bun:test";
import type { Bot, Chat, DurableTask } from "./model";

// The roster keeps what did not change: the screens memoize on these identities.
mock.module("./prefs", () => ({ loadPrefs: () => ({}), savePrefs: () => {}, coreHome: () => "/unused", pathOf: (p: string) => p, wipePrefs: () => {} }));
mock.module("expo-localization", () => ({ getLocales: () => [{ languageCode: "en", languageTag: "en-US" }] }));
const { applyRoster, replaceSnapshot, useStore } = await import("./store");

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
