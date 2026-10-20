/// What every coding agent surface reads off its card, as the desktop's agent kit does: how it
/// stands (its tone), how long it has been at it, its settings by name, how full its context is,
/// its model's short name, its pull request's name, what it does now, its plan's progress, the
/// agents at work in every chat, and a command's colored output as spans.

import type { AgentEntry } from "./agentEntries";
import { agentIsRunning, type AgentRun, type Chat, type Message } from "./model";

/// The tint of how it stands: it needs the user (`ask`), works (`busy`), went quiet (`ask` too),
/// is done (`done`), failed (`fail`), or ended otherwise (`quiet`).
export type AgentTone = "ask" | "busy" | "done" | "fail" | "quiet";

export function agentTone(agent: AgentRun): AgentTone {
  if (agent.question || agent.asking || agent.state === "asking") return "ask";
  if (agent.stalled && agent.state === "working") return "ask";
  if (agent.state === "starting" || agent.state === "checking" || agent.state === "working") return "busy";
  if (agent.state === "idle") return "done";
  if (agent.state === "failed") return "fail";
  return "quiet";
}

/// It is doing something now: its dot breathes, and its time ticks.
export function agentBusy(agent: AgentRun): boolean {
  return !agent.question && !agent.asking && (agent.state === "starting" || agent.state === "working") && !agent.stalled;
}

/// How long ago it started, "42s", "4m", or "1h 12m"; nothing without a start.
export function agentSince(agent: AgentRun, nowMs: number): string {
  if (!agent.started_at) return "";
  const gone = Math.max(0, Math.floor(nowMs / 1000 - agent.started_at));
  if (gone < 60) return `${gone}s`;
  if (gone < 3600) return `${Math.floor(gone / 60)}m`;
  return `${Math.floor(gone / 3600)}h ${Math.floor(gone / 60) % 60}m`;
}

/// One of its settings by the agent's name for it: "Model", "Context", "Cost".
export function agentSetting(agent: AgentRun, name: string): string {
  return agent.settings?.find((setting) => setting.name === name)?.value ?? "";
}

/// "34k", "1.2M", or "512" as a number; -1 when it is none of them.
export function tokenCount(text: string): number {
  let words = text.trim();
  let scale = 1;
  if (words.endsWith("k")) [scale, words] = [1e3, words.slice(0, -1)];
  else if (words.endsWith("M")) [scale, words] = [1e6, words.slice(0, -1)];
  const n = Number(words);
  return words === "" || Number.isNaN(n) ? -1 : n * scale;
}

/// How much of its context it has used, from "34k / 200k": the share, capped at 1, and the words.
export function agentContext(agent: AgentRun): { share: number; words: string } | null {
  const words = agentSetting(agent, "Context");
  const [used, size] = words.split("/");
  if (size === undefined) return null;
  const u = tokenCount(used);
  const s = tokenCount(size);
  if (s <= 0 || u < 0) return null;
  return { share: Math.min(u / s, 1), words: words.trim() };
}

/// A model's name as a cell has room for: without the provider a gateway puts before it, and
/// without the context hints after it ("…/claude-sonnet[1M]" reads "claude-sonnet").
export function modelShortName(name: string): string {
  let short = name.slice(name.lastIndexOf("/") + 1);
  while (short.endsWith("]")) {
    const at = short.lastIndexOf("[");
    if (at <= 0) break;
    short = short.slice(0, at);
  }
  return short;
}

/// "acme/shop #7" for a pull request's link, or nothing when it is not one.
export function pullRequestName(link: string): string {
  const parts = link.replace(/^https?:\/\//, "").replace(/\/$/, "").split("/");
  if (parts.length >= 5 && ["pull", "pulls", "merge_requests"].includes(parts[3])) return `${parts[1]}/${parts[2]} #${parts[4]}`;
  return "";
}

/// What it does now: its last tool's line, without the mark.
export function agentNow(agent: AgentRun): string {
  const lines = (agent.output ?? "").split("\n");
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = lines[i].trim();
    if (line.startsWith("● ")) return line.slice(2);
  }
  return "";
}

/// Its last `count` lines with something on them, while it works.
export function agentTail(agent: AgentRun, count: number): string[] {
  if (agent.question || (agent.state !== "starting" && agent.state !== "working")) return [];
  return (agent.output ?? "")
    .split("\n")
    .filter((line) => line.trim() !== "")
    .slice(-count);
}

/// The steps of the latest plan in a transcript, and how many are done.
export function latestPlan(entries: AgentEntry[]): { steps: { text: string; done: boolean }[]; done: number } | null {
  for (let i = entries.length - 1; i >= 0; i--) {
    const steps = entries[i].kind === "plan" ? entries[i].steps : undefined;
    if (steps && steps.length > 0) return { steps, done: steps.filter((step) => step.done).length };
  }
  return null;
}

/// The coding agents at work in every chat, the latest started first, each with its card's message.
export function runningAgents(chats: Chat[]): { chat: Chat; message: Message; agent: AgentRun }[] {
  const found: { chat: Chat; message: Message; agent: AgentRun }[] = [];
  for (const chat of chats) {
    for (const message of chat.messages) {
      const agent = message.body.kind === "tool" ? message.body.agent : undefined;
      if (agent && agentIsRunning(agent)) found.push({ chat, message, agent });
    }
  }
  return found.sort((a, b) => (b.agent.started_at ?? 0) - (a.agent.started_at ?? 0));
}

/// A span of a command's output in one color: an ANSI color by its number (0–15), a 256-color
/// index (16–255), or a hex `rgb`; bold or dim.
export interface OutputSpan {
  text: string;
  color?: number;
  rgb?: string;
  bold?: boolean;
  dim?: boolean;
}

/// A command's output as lines of colored spans, after its colors (`ESC [ … m`) only: every other
/// control sequence is dropped, and a line a carriage return rewrote reads as what it ended as.
export function outputSpans(output: string): OutputSpan[][] {
  let style: Omit<OutputSpan, "text"> = {};
  const lines = output
    .replace(/\r\n/g, "\n")
    .replace(/\n+$/, "")
    .split("\n");
  return lines.map((raw) => {
    const line = raw
      .slice(raw.lastIndexOf("\r") + 1)
      // eslint-disable-next-line no-control-regex
      .replace(/\x1b\][^\x07\x1b]*(\x07|\x1b\\)?/g, "");
    const spans: OutputSpan[] = [];
    // eslint-disable-next-line no-control-regex
    const pattern = /\x1b\[([0-9;:]*)([@-~])/g;
    let at = 0;
    const push = (text: string) => {
      // eslint-disable-next-line no-control-regex
      const clean = text.replace(/[\x00-\x08\x0b-\x1f\x7f\x80-\x9f]/g, "");
      if (!clean) return;
      const last = spans[spans.length - 1];
      if (last && last.color === style.color && last.rgb === style.rgb && last.bold === style.bold && last.dim === style.dim) last.text += clean;
      else spans.push({ text: clean, ...style });
    };
    for (let match = pattern.exec(line); match; match = pattern.exec(line)) {
      push(line.slice(at, match.index));
      at = pattern.lastIndex;
      if (match[2] === "m") style = applySgr(style, match[1]);
    }
    push(line.slice(at));
    return spans.map((span) => Object.fromEntries(Object.entries(span).filter(([, value]) => value !== undefined)) as OutputSpan);
  });
}

function applySgr(style: Omit<OutputSpan, "text">, params: string): Omit<OutputSpan, "text"> {
  const codes = params === "" ? [0] : params.split(/[;:]/).map((code) => Number(code) || 0);
  let next = { ...style };
  for (let i = 0; i < codes.length; i++) {
    const code = codes[i];
    if (code === 0) next = {};
    else if (code === 1) next.bold = true;
    else if (code === 2) next.dim = true;
    else if (code === 22) next.bold = next.dim = undefined;
    else if (code >= 30 && code <= 37) [next.color, next.rgb] = [code - 30, undefined];
    else if (code >= 90 && code <= 97) [next.color, next.rgb] = [code - 90 + 8, undefined];
    else if (code === 39) next.color = next.rgb = undefined;
    else if (code === 38 && codes[i + 1] === 5) {
      [next.color, next.rgb] = [codes[i + 2], undefined];
      i += 2;
    } else if (code === 38 && codes[i + 1] === 2) {
      const hex = codes.slice(i + 2, i + 5).map((n) => Math.max(0, Math.min(255, n)).toString(16).padStart(2, "0"));
      [next.color, next.rgb] = [undefined, `#${hex.join("")}`];
      i += 4;
    } else if (code === 48 && (codes[i + 1] === 5 || codes[i + 1] === 2)) i += codes[i + 1] === 5 ? 2 : 4;
  }
  return next;
}
