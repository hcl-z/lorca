import { expect, test } from "bun:test";
import { agentContext, agentNow, agentSince, agentTail, agentTone, latestPlan, outputSpans, runningAgents } from "./agentKit";
import type { AgentRun, Chat } from "./model";

const agent = (more: Partial<AgentRun>): AgentRun => ({ id: "agent-1", kind: "claude", task: "Fix the link", state: "working", ...more });

// The core decides the tone (`AgentRun::summarize`); a card without one reads as quiet.
test("an agent's tone is the core's", () => {
  expect(agentTone(agent({ tone: "ask" }))).toBe("ask");
  expect(agentTone(agent({}))).toBe("quiet");
});

test("how long it has worked reads in seconds, minutes, then hours", () => {
  const started = agent({ started_at: 1000 });
  expect(agentSince(started, 1042_000)).toBe("42s");
  expect(agentSince(started, (1000 + 11 * 60 + 5) * 1000)).toBe("11m");
  expect(agentSince(started, (1000 + 72 * 60) * 1000)).toBe("1h 12m");
  expect(agentSince(agent({}), 5_000)).toBe("");
});

test("its context reads as a share of the window", () => {
  const used = agentContext(agent({ settings: [{ name: "Context", value: "34k / 200k" }], context: { used: 34_000, size: 200_000 } }));
  expect(used?.share).toBeCloseTo(0.17);
  expect(used?.words).toBe("34k / 200k");
  expect(agentContext(agent({ context: { used: 3, size: 2 } }))?.share).toBe(1);
  expect(agentContext(agent({ settings: [{ name: "Context", value: "lots" }] }))).toBeNull();
});

// The core reads what it does now and its last lines off its output (`AgentRun::summarize`).
test("what it does now and its tail are the core's", () => {
  const working = agent({ now: "Bash npm test", tail: ["  ⎿ 40 lines", "● Bash npm test"] });
  expect(agentNow(working)).toBe("Bash npm test");
  expect(agentTail(working)).toEqual(["  ⎿ 40 lines", "● Bash npm test"]);
  expect(agentTail(agent({ output: "● Bash npm test" }))).toEqual([]);
});

test("the latest plan counts its steps done, and only open agents are at work", () => {
  expect(latestPlan([{ seq: 1, rev: 1, kind: "plan", steps: [{ text: "a", done: true }] }, { seq: 2, rev: 2, kind: "plan", steps: [{ text: "a", done: true }, { text: "b", done: false }] }])).toEqual({ steps: [{ text: "a", done: true }, { text: "b", done: false }], done: 1 });
  const message = (id: string, run: AgentRun) => ({ id, chat_id: "c", body: { kind: "tool", agent: run } }) as never;
  const chats = [{ id: "c", messages: [message("m1", agent({ started_at: 1 })), message("m2", agent({ state: "idle" })), message("m3", agent({ started_at: 9 }))] }] as unknown as Chat[];
  expect(runningAgents(chats).map((found) => found.message.id)).toEqual(["m3", "m1"]);
});

test("a command's output keeps its colors as spans, and a rewritten line what it ended as", () => {
  const lines = outputSpans("\x1b[32mok\x1b[0m 3 tests\n\x1b[1;31mFAIL\x1b[22m x\x1b[39m\nload 10%\rload 100%\n\x1b]0;title\x07\x1b[38;5;208mwarm\x1b[38;2;1;2;3mrgb\n");
  expect(lines).toEqual([
    [{ text: "ok", color: 2 }, { text: " 3 tests" }],
    [{ text: "FAIL", color: 1, bold: true }, { text: " x", color: 1 }],
    [{ text: "load 100%" }],
    [{ text: "warm", color: 208 }, { text: "rgb", rgb: "#010203" }],
  ]);
  // A color holds until it is reset, from one line to the next, as on a terminal.
  expect(outputSpans("\x1b[33mone\ntwo\x1b[m three")).toEqual([[{ text: "one", color: 3 }], [{ text: "two", color: 3 }, { text: " three" }]]);
});
