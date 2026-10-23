import { expect, mock, test } from "bun:test";
import type { Chat, Message } from "./model";

// The account a launch draws before the core has opened it: what the copy keeps and leaves out,
// and a file that does not read.
const files = new Map<string, string>();
mock.module("expo-application", () => ({ nativeApplicationVersion: "1.0.0", nativeBuildVersion: "7" }));
mock.module("expo-file-system", () => ({
  Paths: { cache: "/cache" },
  File: class {
    path: string;
    constructor(dir: string, name: string) {
      this.path = `${dir}/${name}`;
    }
    get exists() {
      return files.has(this.path);
    }
    textSync() {
      return files.get(this.path)!;
    }
    write(text: string) {
      files.set(this.path, text);
    }
    delete() {
      files.delete(this.path);
    }
  },
}));
const { CACHED_MESSAGES, cachedSnapshotOf, clearCachedSnapshot, loadCachedSnapshot, saveCachedSnapshot } = await import("./startupCache");

const message = (i: number) => ({ id: `m${i}`, chat_id: "a", created_at: i }) as unknown as Message;
const chat = (count: number, extra: Partial<Chat> = {}) =>
  ({ id: "a", kind: "dm", title: "a", bot_ids: [], is_pinned: false, created_at: 0, unread_count: 0, messages: Array.from({ length: count }, (_, i) => message(i)), ...extra }) as Chat;

function state(extra: Record<string, unknown> = {}) {
  return {
    paired: true,
    identityId: "id",
    deviceId: "phone",
    relayUrl: "wss://relay",
    relayConnected: true,
    devices: [],
    bots: [],
    chats: [chat(CACHED_MESSAGES + 5), chat(3, { id: "b" })],
    routines: [],
    reviews: [],
    budgets: [],
    shared_links: [],
    sections: [],
    auto_review: { is_enabled: true, rules: [] },
    attention: { items: [] },
    providers: [],
    models: [],
    tasks: [{ id: "t" }],
    ...extra,
  } as never;
}

test("a copy keeps each chat's last messages and says there are older ones", () => {
  const snapshot = cachedSnapshotOf(state())!;
  const [long, short] = snapshot.chats;
  expect(long.messages).toHaveLength(CACHED_MESSAGES);
  expect(long.messages.at(-1)!.id).toBe(`m${CACHED_MESSAGES + 4}`);
  expect(long.has_more).toBe(true);
  expect(short.messages).toHaveLength(3);
  expect(short.has_more).toBe(false);
});

test("a copy is offline, runs nothing, and holds no tasks", () => {
  const snapshot = cachedSnapshotOf(state())!;
  expect(snapshot.relay_connected).toBe(false);
  expect(snapshot.running_turns).toEqual([]);
  expect(snapshot.tasks).toBeUndefined();
});

test("an unpaired phone keeps no copy", () => {
  expect(cachedSnapshotOf(state({ paired: false }))).toBeNull();
  saveCachedSnapshot(() => state({ paired: false }), { now: true });
  expect(loadCachedSnapshot()).toBeNull();
});

test("a copy written is read back, and unpairing removes it", () => {
  saveCachedSnapshot(() => state(), { now: true });
  expect([...files.keys()]).toEqual(["/cache/snapshot-1.0.0-7.json"]);
  expect(loadCachedSnapshot()?.identity_id).toBe("id");
  clearCachedSnapshot();
  expect(loadCachedSnapshot()).toBeNull();
});

test("a file cut off mid-write is no copy", () => {
  files.set("/cache/snapshot-1.0.0-7.json", '{"has_identity":true,"chats":[');
  expect(loadCachedSnapshot()).toBeNull();
  files.clear();
});
