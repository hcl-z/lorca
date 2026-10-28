// The agent transcript as the phone draws it, after the desktop's: steps in order, a run of reads
// and searches folded into one line, tools in the past or present tense, and a thought with how
// long it took.

import type { AgentEntry, AgentTool } from "../core/agentEntries";
import { toolFailed, toolRunning } from "../core/agentEntries";
import { t } from "../i18n";

export interface AgentStep {
  entry: AgentEntry;
  /** The reads and searches folded into this step, when it is one. */
  explored?: AgentEntry[];
}

export function agentSteps(entries: AgentEntry[]): AgentStep[] {
  const steps: AgentStep[] = [];
  for (const entry of entries) {
    if (entry.kind === "tool" && entry.tool && (entry.tool.kind === "read" || entry.tool.kind === "search")) {
      const last = steps[steps.length - 1];
      if (last?.explored) {
        last.explored.push(entry);
        continue;
      }
      steps.push({ entry, explored: [entry] });
      continue;
    }
    steps.push({ entry });
  }
  return steps;
}

/// A fold's line: "Explored 3 files, 2 searches", "Exploring 1 file" while one runs.
export function exploredText(items: AgentEntry[]): string {
  let files = 0;
  let searches = 0;
  let running = false;
  for (const item of items) {
    if (item.tool?.kind === "read") files++;
    else searches++;
    if (item.tool && toolRunning(item.tool)) running = true;
  }
  const fileWords = files === 1 ? t("1 file") : files > 1 ? t("{count} files", { count: files }) : "";
  const searchWords = searches === 1 ? t("1 search") : searches > 1 ? t("{count} searches", { count: searches }) : "";
  const what = [fileWords, searchWords].filter(Boolean).join(" ");
  if (!what) return t("Explored");
  return running ? t("Exploring {what}", { what }) : t("Explored {what}", { what });
}

/// What a tool acts on: its detail when the agent says, else its title with a leading verb
/// dropped, so "Edit src/a.rs" does not read "Edited Edit src/a.rs".
export function toolSubject(tool: AgentTool): string {
  let subject = tool.detail || tool.title;
  if (!tool.detail) {
    for (const verb of ["Read ", "Reading ", "Searched ", "Searching ", "Edit ", "Edited ", "Editing ", "Run ", "Ran ", "Running ", "Fetch ", "Fetched ", "Fetching ", "Used ", "Using "]) {
      if (subject.startsWith(verb)) {
        subject = subject.slice(verb.length).trim();
        break;
      }
    }
  }
  return subject;
}

/// A step's line, in the past once it is done and in the present while it runs.
export function agentToolText(tool: AgentTool): string {
  const running = toolRunning(tool);
  const subject = toolSubject(tool);
  if (toolFailed(tool) && !running) return t("Failed: {title}", { title: subject });
  const verb = (done: string, doing: string) => t(running ? doing : done, { title: subject });
  switch (tool.kind) {
    case "read":
      return verb("Read {title}", "Reading {title}");
    case "search":
      return verb("Searched {title}", "Searching {title}");
    case "edit":
      return verb("Edited {title}", "Editing {title}");
    case "execute":
      return verb("Ran {title}", "Running {title}");
    case "fetch":
      return verb("Fetched {title}", "Fetching {title}");
    default:
      return verb("Used {title}", "Using {title}");
  }
}

/// A thought's line: "Thought for 3s" when the agent says how long it took.
export function thoughtTitle(entry: AgentEntry): string {
  if (entry.seconds === undefined) return t("Thought");
  return t("Thought for {seconds}s", { seconds: String(Math.max(1, Math.round(entry.seconds))) });
}

/// What a step's edit changed, "+1 −1", for a step's line.
export function editCounts(tool: AgentTool): { added: number; removed: number } {
  let added = 0;
  let removed = 0;
  for (const diff of tool.diffs ?? []) {
    added += diff.added;
    removed += diff.removed;
  }
  return { added, removed };
}
