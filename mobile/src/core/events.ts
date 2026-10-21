// One event frame from the core, decoded once. `crates/cli/src/events.rs` is the contract; this
// file is the phone's side of it, so the engine applies typed events and reads no JSON field name.

import type { AttentionView } from "./attention";
import type { BudgetState, ChatUsage, DurableTask, Message, RelayProblem, ReviewItem } from "./model";
import type { applyRoster, replaceSnapshot } from "./store";

type Snapshot = Parameters<typeof replaceSnapshot>[0];
type Roster = Parameters<typeof applyRoster>[0];

export interface JobFacts {
  jobId: string;
  chatId: string;
  /// Empty while a group exchange is between member turns.
  botId: string;
  routineId?: string;
}

export type CoreEvent =
  | { kind: "snapshot"; snapshot: Snapshot }
  | { kind: "roster.changed"; roster: Roster }
  | { kind: "message.added" | "message.updated"; message: Message }
  | { kind: "message.removed"; chatId: string; messageId: string }
  | { kind: "chat.removed"; chatId: string }
  | { kind: "job.started" | "job.finished"; job: JobFacts }
  | { kind: "job.thinking"; chatId: string; botId: string }
  | { kind: "job.retry"; chatId: string; attempt: number; maxAttempts: number; delayMs: number }
  | { kind: "chat.usage"; chatId: string; usage: ChatUsage }
  | { kind: "relay.status"; connected: boolean; updateRequired: boolean; error: RelayProblem | null; url: string | null }
  | { kind: "provider.auth"; url: string }
  | { kind: "plugin.auth"; url: string; signIn: string }
  | { kind: "plugin.auth.done"; signIn: string }
  | { kind: "identity.changed"; hasIdentity: boolean }
  | { kind: "attention.changed"; view: AttentionView }
  | { kind: "tasks.changed"; task: DurableTask }
  | { kind: "reviews.changed"; item: ReviewItem }
  | { kind: "budgets.changed"; budgets: BudgetState[] }
  | { kind: "projects.changed"; chatId: string }
  // The phone keeps no state for these: feedback has its own screen, pairing listens for
  // `pair.posted` itself, and the push key lives in the native extension.
  | { kind: "feedback.changed" | "pair.posted" | "pair.completed" | "account.key_changed" };

class Malformed extends Error {}

type Fields = Record<string, unknown>;

function object(value: unknown, what: string): Fields {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Malformed(`${what} is not an object`);
  return value as Fields;
}

function text(data: Fields, key: string): string {
  const value = data[key];
  if (typeof value !== "string") throw new Malformed(`${key} is not a string`);
  return value;
}

/// A string the core leaves out or sends as null for "none".
function optionalText(data: Fields, key: string): string | undefined {
  const value = data[key];
  if (value === undefined || value === null) return undefined;
  if (typeof value !== "string") throw new Malformed(`${key} is not a string`);
  return value;
}

function count(data: Fields, key: string): number {
  const value = data[key];
  if (typeof value !== "number") throw new Malformed(`${key} is not a number`);
  return value;
}

function list<T>(data: Fields, key: string): T[] {
  const value = data[key];
  if (!Array.isArray(value)) throw new Malformed(`${key} is not a list`);
  return value as T[];
}

function job(data: Fields): JobFacts {
  return { jobId: text(data, "job_id"), chatId: text(data, "chat_id"), botId: optionalText(data, "bot_id") ?? "", routineId: optionalText(data, "routine_id") };
}

function decode(name: string, data: unknown): CoreEvent | null {
  switch (name) {
    case "snapshot":
      return { kind: name, snapshot: object(data, "the snapshot") as unknown as Snapshot };
    case "roster.changed": {
      const roster = object(data, "the roster");
      list(roster, "chats");
      return { kind: name, roster: roster as unknown as Roster };
    }
    case "message.added":
    case "message.updated": {
      const message = object(object(data, name).message, "the message");
      text(message, "id");
      text(message, "chat_id");
      return { kind: name, message: message as unknown as Message };
    }
    case "message.removed": {
      const fields = object(data, name);
      return { kind: name, chatId: text(fields, "chat_id"), messageId: text(fields, "message_id") };
    }
    case "chat.removed":
      return { kind: name, chatId: text(object(data, name), "chat_id") };
    case "projects.changed":
      return { kind: name, chatId: text(object(data, name), "chat_id") };
    case "job.started":
    case "job.finished":
      return { kind: name, job: job(object(data, name)) };
    case "job.thinking": {
      const fields = object(data, name);
      return { kind: name, chatId: text(fields, "chat_id"), botId: text(fields, "bot_id") };
    }
    case "job.retry": {
      const fields = object(data, name);
      return { kind: name, chatId: text(fields, "chat_id"), attempt: count(fields, "attempt"), maxAttempts: count(fields, "max_attempts"), delayMs: count(fields, "delay_ms") };
    }
    case "chat.usage": {
      const fields = object(data, name);
      return { kind: name, chatId: text(fields, "chat_id"), usage: object(fields.usage, "the usage") as unknown as ChatUsage };
    }
    case "relay.status": {
      const fields = object(data, name);
      return { kind: name, connected: !!fields.connected, updateRequired: !!fields.update_required, error: (fields.error ?? null) as RelayProblem | null, url: optionalText(fields, "url") ?? null };
    }
    case "provider.auth":
      return { kind: name, url: text(object(data, name), "url") };
    case "plugin.auth": {
      const fields = object(data, name);
      return { kind: name, url: text(fields, "url"), signIn: text(fields, "sign_in") };
    }
    case "plugin.auth.done":
      return { kind: name, signIn: text(object(data, name), "sign_in") };
    case "identity.changed":
      return { kind: name, hasIdentity: !!object(data, name).has_identity };
    case "attention.changed":
      return { kind: name, view: object(data, name) as unknown as AttentionView };
    case "tasks.changed":
      return { kind: name, task: object(object(data, name).task, "the task") as unknown as DurableTask };
    case "reviews.changed":
      return { kind: name, item: object(object(data, name).item, "the item") as unknown as ReviewItem };
    case "budgets.changed":
      return { kind: name, budgets: list<BudgetState>(object(data, name), "budgets") };
    case "feedback.changed":
    case "pair.posted":
    case "pair.completed":
    case "account.key_changed":
      return { kind: name };
  }
  return null;
}

/// The frame `name` carries `data` for, typed; `null`, with a warning, for an event this build
/// does not know or a payload that does not read, so the engine applies nothing of it.
export function decodeEvent(name: string, data: unknown): CoreEvent | null {
  try {
    const event = decode(name, data);
    if (!event) console.warn(`event ${name}: unknown`);
    return event;
  } catch (error) {
    if (!(error instanceof Malformed)) throw error;
    console.warn(`event ${name}: ${error.message}`);
    return null;
  }
}
