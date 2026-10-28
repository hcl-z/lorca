import { describe, expect, test } from "bun:test";
import { AGENT_MARK_KINDS, agentBrandColor, agentMark } from "./agentMarks";

describe("agent marks", () => {
  test("every ACP agent has a mark", () => {
    for (const kind of AGENT_MARK_KINDS) expect(agentMark(kind).length).toBeGreaterThan(20);
  });

  test("agent brands follow the desktop's colors", () => {
    expect(agentBrandColor("claude", false, "#0000ff")).toBe("#D97757");
    expect(agentBrandColor("gemini", false, "#0000ff")).toBe("#4C8DF6");
    expect(agentBrandColor("codex", false, "#0000ff")).toBe("#1D1D1F");
    expect(agentBrandColor("codex", true, "#0000ff")).toBe("#F5F5F7");
    expect(agentBrandColor("copilot", false, "#0000ff")).toBe("#1D1D1F");
    expect(agentBrandColor("cursor", false, "#0000ff")).toBe("#1D1D1F");
    expect(agentBrandColor("opencode", false, "#0000ff")).toBe("#0000ff");
    expect(agentBrandColor("kimi", false, "#0000ff")).toBe("#0000ff");
  });
});
