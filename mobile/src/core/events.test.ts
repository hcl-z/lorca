import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { decodeEvent } from "./events";

// Every event the core sends decodes to a typed event: `events.rs` is the contract.
test("every event the core sends decodes", () => {
  const source = readFileSync(new URL("../../../crates/cli/src/events.rs", import.meta.url), "utf8");
  const names = [...source.matchAll(/#\[serde\(rename = "([a-z_.]+)"\)\]/g)].map((match) => match[1]);
  expect(names.length).toBeGreaterThan(20);
  const sample: Record<string, unknown> = {
    "message.added": { chat_id: "c", message: { id: "m", chat_id: "c" } },
    "message.updated": { chat_id: "c", message: { id: "m", chat_id: "c" } },
    "message.removed": { chat_id: "c", message_id: "m" },
    "chat.removed": { chat_id: "c" },
    "projects.changed": { chat_id: "c", entry_id: "e" },
    "job.started": { chat_id: "c", bot_id: "b", job_id: "j" },
    "job.finished": { chat_id: "c", bot_id: "b", job_id: "j" },
    "job.thinking": { chat_id: "c", bot_id: "b" },
    "job.retry": { chat_id: "c", bot_id: "b", attempt: 1, max_attempts: 3, delay_ms: 500, error: "busy" },
    "chat.usage": { chat_id: "c", usage: {} },
    "provider.auth": { kind: "openai", url: "https://example.com" },
    "plugin.auth": { plugin_id: "p", sign_in: "s", url: "https://example.com" },
    "plugin.auth.done": { plugin_id: "p", sign_in: "s" },
    "roster.changed": { devices: [], bots: [], chats: [] },
    "tasks.changed": { task: {} },
    "reviews.changed": { item: {}, change: {} },
    "budgets.changed": { budgets: [] },
  };
  for (const name of names) expect(decodeEvent(name, sample[name] ?? {})?.kind).toBe(name as never);
});

test("an event's fields read as the phone names them", () => {
  expect(decodeEvent("job.finished", { chat_id: "c", bot_id: "", job_id: "j", routine_id: "r" })).toEqual({ kind: "job.finished", job: { jobId: "j", chatId: "c", botId: "", routineId: "r" } });
  expect(decodeEvent("job.started", { chat_id: "c", job_id: "j" })).toEqual({ kind: "job.started", job: { jobId: "j", chatId: "c", botId: "", routineId: undefined } });
  expect(decodeEvent("relay.status", { connected: true, url: null, update_required: false, error: null })).toEqual({ kind: "relay.status", connected: true, updateRequired: false, error: null, url: null });
});

test("an unknown or malformed event decodes to nothing", () => {
  expect(decodeEvent("chat.renamed", { chat_id: "c" })).toBeNull();
  expect(decodeEvent("message.added", { chat_id: "c" })).toBeNull();
  expect(decodeEvent("job.retry", { chat_id: "c", attempt: "1" })).toBeNull();
});
