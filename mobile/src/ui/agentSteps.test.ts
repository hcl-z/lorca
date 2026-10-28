import { describe, expect, test } from "bun:test";
import type { AgentEntry, AgentTool } from "../core/agentEntries";
import { agentSteps, agentToolText, exploredText, thoughtTitle } from "./agentSteps";

let seq = 0;
const tool = (kind: string, title: string, detail: string, status: AgentTool["status"]): AgentEntry => ({
  seq: ++seq,
  rev: 1,
  kind: "tool",
  tool: { kind, title, detail, status },
});

describe("agent steps", () => {
  test("a run of reads and searches folds into one step", () => {
    const steps = agentSteps([tool("read", "src/a.rs", "", "completed"), tool("search", "login", "", "completed"), tool("edit", "src/a.rs", "", "completed")]);
    expect(steps).toHaveLength(2);
    expect(steps[0].explored).toHaveLength(2);
    expect(steps[1].explored).toBeUndefined();
  });

  test("an explored line counts files and searches, and says while one runs", () => {
    expect(exploredText([tool("read", "src/a.rs", "", "completed")])).toBe("Explored 1 file");
    expect(exploredText([tool("read", "src/a.rs", "", "completed"), tool("search", "login", "", "completed")])).toBe("Explored 1 file 1 search");
    expect(exploredText([tool("read", "src/a.rs", "", "in_progress")])).toBe("Exploring 1 file");
  });

  test("a tool reads in the past or present tense, and never repeats its verb", () => {
    expect(agentToolText(tool("execute", "bun test", "", "completed").tool!)).toBe("Ran bun test");
    expect(agentToolText(tool("execute", "bun test", "", "in_progress").tool!)).toBe("Running bun test");
    expect(agentToolText(tool("edit", "Edit src/a.rs", "", "completed").tool!)).toBe("Edited src/a.rs");
    expect(agentToolText(tool("edit", "Edit", "src/a.rs", "completed").tool!)).toBe("Edited src/a.rs");
    expect(agentToolText(tool("execute", "bun test", "", "failed").tool!)).toBe("Failed: bun test");
  });

  test("a thought says how long it took when the agent does", () => {
    const thought: AgentEntry = { seq: ++seq, rev: 1, kind: "thought", text: "hm", seconds: 3.4 };
    expect(thoughtTitle(thought)).toBe("Thought for 3s");
    expect(thoughtTitle({ ...thought, seconds: undefined })).toBe("Thought");
  });
});
