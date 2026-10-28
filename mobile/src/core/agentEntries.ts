/// A coding agent's transcript as the Runner keeps it for the apps (`coding.transcript`): what it
/// was sent, what it said, each tool it used with its command's output and its edits, its plan,
/// and Lorca's notes. `seq` is an entry's place, kept when it changes; `rev` the change it was last
/// part of, so an app asks only for what changed since the latest it has.

export interface AgentEntry {
  seq: number;
  rev: number;
  /** `log` is the bot's text of an agent from before entries. */
  kind: "sent" | "said" | "thought" | "tool" | "plan" | "note" | "notice" | "compaction" | "log";
  text?: string;
  /** A notice's severity: `info`, `warning`, or `error`. */
  level?: string;
  /** How a compaction stands: `in_progress`, `completed`, `failed`, or `cancelled`. */
  status?: string;
  /** How long a thought took, in seconds, while it thinks. */
  seconds?: number;
  /** `user` on what the user sent the agent, absent on what the bot sent. */
  from?: string;
  tool?: AgentTool;
  steps?: { text: string; done: boolean }[];
}

export interface AgentTool {
  title: string;
  /** The tool's own name, when the agent gives it: `Bash`, an MCP tool's. */
  name?: string;
  detail?: string;
  kind?: string;
  status: "pending" | "in_progress" | "completed" | "failed";
  /** What its command's terminal wrote, with only its colors (`ESC [ … m`). */
  output?: string;
  exit_code?: number;
  result?: string;
  diffs?: { path: string; lines: string[]; added: number; removed: number }[];
}

export interface AgentTranscriptPage {
  /** The transcript as the bot reads it, which a Runner that keeps no entries answers alone. */
  text: string;
  entries?: AgentEntry[];
  rev?: number;
  first?: number;
}

export function toolRunning(tool: AgentTool): boolean {
  return tool.status === "pending" || tool.status === "in_progress";
}

export function toolFailed(tool: AgentTool): boolean {
  return tool.status === "failed" || (tool.exit_code !== undefined && tool.exit_code !== null && tool.exit_code !== 0);
}

/// A page's entries put into what the app has: each in place of the one with its seq, or in order
/// after the rest, and none from before the first the Runner keeps.
export function mergeAgentEntries(have: AgentEntry[], page: AgentTranscriptPage): AgentEntry[] {
  const bySeq = new Map(have.map((entry) => [entry.seq, entry]));
  // An entry at the revision held keeps its object, so its row is not drawn again.
  for (const entry of page.entries ?? []) if (bySeq.get(entry.seq)?.rev !== entry.rev) bySeq.set(entry.seq, entry);
  const first = page.first ?? 0;
  const next = [...bySeq.values()].filter((entry) => entry.seq >= first).sort((a, b) => a.seq - b.seq);
  // A refresh that brought nothing new keeps the list itself.
  return next.length === have.length && next.every((entry, at) => entry === have[at]) ? have : next;
}

/// A command's output as plain text: its colors and every other control sequence dropped, and a
/// line a carriage return rewrote read as what it ended as.
export function outputText(output: string): string {
  const plain = output
    // eslint-disable-next-line no-control-regex
    .replace(/\x1b\[[0-9;:]*[@-~]/g, "")
    // eslint-disable-next-line no-control-regex
    .replace(/\x1b\][^\x07\x1b]*(\x07|\x1b\\)?/g, "")
    // eslint-disable-next-line no-control-regex
    .replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f\x80-\x9f]/g, "");
  return plain
    .replace(/\r\n/g, "\n")
    .split("\n")
    .map((line) => line.slice(line.lastIndexOf("\r") + 1))
    .join("\n")
    .replace(/\n+$/, "");
}
