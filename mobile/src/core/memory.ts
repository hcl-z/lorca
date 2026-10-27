// A bot's memory, the account's profile, and the bot's prompt, from the bot's Runner. They live
// there (`docs/architecture/memory.md`): `bots.memory`, `profile`, `bots.prompt`, and
// `memory_capture.*` reach it as sealed requests through the core when it is another Device.
// A write carries the hash of the text it was opened at, and the Runner refuses it when the
// file changed since; the screen then offers the file as it is now, or to write over it.

import * as core from "../../modules/lorca-core";

/// A memory file as its Runner holds it, against the budget a turn loads of it.
export interface MemoryFile {
  text: string;
  hash: string;
  lines: number;
  bytes: number;
  truncated: boolean;
  max_lines: number;
  max_bytes: number;
}

/// A saved fact and the file that holds it.
export interface CaptureFact {
  text: string;
  target: "profile" | "memory";
}

/// One memory capture batch. The client never receives source chat identifiers or quotes.
export interface CaptureRun {
  id: string;
  /// Unix seconds.
  at: number;
  facts: CaptureFact[];
  undone: boolean;
}

/// The newest pass of the memory housekeeping that moved lines out of MEMORY.md, while it is not
/// undone: struck-through history to memory/history.md, unused entries to memory/archive.md.
export interface TidyRun {
  id: string;
  /// Unix seconds.
  at: number;
  history: string[];
  archived: string[];
  undone: boolean;
}

export interface BotMemory {
  bot_id: string;
  /// The bot runs on this Device.
  here: boolean;
  /// Its Runner's name.
  runner: string;
  index: MemoryFile;
  topics: string[];
  profile: MemoryFile;
  capture: CaptureRun | null;
  captures?: CaptureRun[];
  tidy?: TidyRun | null;
}

export interface Profile extends MemoryFile {
  runner_id: string;
  runner: string | null;
}

export interface PromptPreview {
  bot_id: string;
  chat_id: string;
  sections: { id: string; title: string; text: string; bytes: number }[];
  total_bytes: number;
  approx_tokens: number;
  notes: string[];
  notes_bytes: number;
}

/// The Runner refused a write: the file changed since it was opened.
export function isConflict(error: unknown): boolean {
  return error instanceof Error && error.message.includes("changed since");
}

export function botMemory(botId: string): Promise<BotMemory> {
  return core.request<BotMemory>("bots.memory", { bot_id: botId });
}

/// Writes MEMORY.md; without `expectedHash` it writes over whatever is there.
export function writeBotMemory(botId: string, text: string, expectedHash: string | null): Promise<{ hash: string }> {
  return core.request("bots.memory.write", { bot_id: botId, text, expected_hash: expectedHash });
}

export function profile(runnerId: string): Promise<Profile> {
  return core.request<Profile>("profile", { runner_id: runnerId });
}

export function writeProfile(runnerId: string, text: string, expectedHash: string | null): Promise<{ hash: string }> {
  return core.request("profile.write", { runner_id: runnerId, text, expected_hash: expectedHash });
}

/// Forgets what a capture run added.
export function undoCapture(botId: string, runId: string): Promise<{ removed: number }> {
  return core.request("memory_capture.undo", { bot_id: botId, run_id: runId });
}

export function captureHistory(botId: string): Promise<{ runs: CaptureRun[] }> {
  return core.request("memory_capture.list", { bot_id: botId });
}

/// Puts back what a housekeeping pass moved out of MEMORY.md.
export function undoTidy(botId: string, runId: string): Promise<{ restored: number }> {
  return core.request("memory_tidy.undo", { bot_id: botId, run_id: runId });
}

export function promptPreview(botId: string, chatId: string): Promise<PromptPreview> {
  return core.request<PromptPreview>("bots.prompt", { bot_id: botId, chat_id: chatId });
}

/// A text's lines as a turn counts them: none when it is empty.
export function lineCount(text: string): number {
  return text ? text.split("\n").length : 0;
}

/// UTF-8 bytes, as the Runner measures the budget.
export function byteCount(text: string): number {
  let bytes = 0;
  for (const char of text) {
    const code = char.codePointAt(0)!;
    bytes += code < 0x80 ? 1 : code < 0x800 ? 2 : code < 0x10000 ? 3 : 4;
  }
  return bytes;
}

export type Gauge = { lines: number; bytes: number; tone: "secondary" | "orange" | "red"; hiddenLines: number };

/// A draft against its budget: orange from 80%, red once anything stops loading, and how many
/// lines past the budget a turn leaves out.
export function gauge(text: string, file: Pick<MemoryFile, "max_lines" | "max_bytes">): Gauge {
  const lines = lineCount(text);
  const bytes = byteCount(text);
  const hiddenLines = Math.max(0, lines - file.max_lines);
  const over = lines > file.max_lines || bytes > file.max_bytes;
  const near = lines >= file.max_lines * 0.8 || bytes >= file.max_bytes * 0.8;
  return { lines, bytes, hiddenLines, tone: over ? "red" : near ? "orange" : "secondary" };
}
