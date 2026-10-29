// The account as the phone last showed it, kept in the app's caches folder so a launch draws the
// chat list before the core has opened the account: the core's start and its first `bootstrap`
// take the better part of a second, and the cached copy is replaced by that snapshot as soon as
// it lands. It holds each chat's last few messages; a build of its own keeps its own file, and
// unpairing removes it. It is written when the app leaves the foreground and after a snapshot.

import * as Application from "expo-application";
import { File, Paths } from "expo-file-system";
import type { replaceSnapshot, StoreState } from "./store";

type Snapshot = Parameters<typeof replaceSnapshot>[0];

/// The messages kept of each chat: what the first screen of a transcript shows.
export const CACHED_MESSAGES = 30;
const SAVE_DELAY_MS = 1000;
let saveTimer: ReturnType<typeof setTimeout> | null = null;

function cacheFile(): File {
  return new File(Paths.cache, `snapshot-${Application.nativeApplicationVersion ?? "0"}-${Application.nativeBuildVersion ?? "0"}.json`);
}

/// What the store shows, as a snapshot to start from. Its relay is not connected, no turn is in
/// flight, and tasks are left out: a snapshot keeps the tasks the phone holds that it does not
/// list, so a cached one could bring back a task deleted since.
export function cachedSnapshotOf(s: StoreState): Snapshot | null {
  if (!s.paired || !s.identityId) return null;
  return {
    has_identity: true,
    identity_id: s.identityId,
    this_device_id: s.deviceId,
    relay_url: s.relayUrl,
    relay_connected: false,
    devices: s.devices,
    bots: s.bots,
    chats: s.chats.map((chat) => {
      const messages = chat.messages.slice(-CACHED_MESSAGES);
      return { ...chat, messages, has_more: !!chat.has_more || messages.length < chat.messages.length };
    }),
    routines: s.routines,
    reviews: s.reviews,
    budgets: s.budgets,
    shared_links: s.shared_links,
    sections: s.sections,
    auto_review: s.auto_review,
    attention: s.attention,
    providers: s.providers,
    models: s.models,
    running_turns: [],
  };
}

/// Reads the cached account off the JS thread: the file can hold hundreds of messages, and a
/// launch should not wait for the parse before it draws.
export async function loadCachedSnapshot(): Promise<Snapshot | null> {
  try {
    const file = cacheFile();
    if (!file.exists) return null;
    const snapshot = JSON.parse(await file.text()) as Snapshot;
    return snapshot.has_identity && snapshot.identity_id && Array.isArray(snapshot.chats) ? snapshot : null;
  } catch {
    // A file from a run that ended mid-write: the launch waits for the core.
    return null;
  }
}

/// Writes what the store shows, now or after the next pause.
export function saveCachedSnapshot(read: () => StoreState, { now = false } = {}) {
  if (saveTimer) clearTimeout(saveTimer);
  saveTimer = null;
  const write = () => {
    const snapshot = cachedSnapshotOf(read());
    if (!snapshot) return;
    try {
      cacheFile().write(JSON.stringify(snapshot));
    } catch (error) {
      console.warn("saving the startup snapshot", error);
    }
  };
  if (now) write();
  else saveTimer = setTimeout(() => {
    saveTimer = null;
    write();
  }, SAVE_DELAY_MS);
}

export function clearCachedSnapshot() {
  if (saveTimer) clearTimeout(saveTimer);
  saveTimer = null;
  try {
    const file = cacheFile();
    if (file.exists) file.delete();
  } catch {
    // Nothing to remove.
  }
}
