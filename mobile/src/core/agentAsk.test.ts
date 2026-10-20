import { expect, test } from "bun:test";
import { type AgentAsk, emptyDraft, formValues, sendsOnPick, togglePick } from "./agentAsk";

const ask: AgentAsk = {
  agent_id: "agent-1",
  kind: "claude",
  name: "Claude Code",
  row_id: "row",
  message: "How should I set up the release?",
  fields: [
    { key: "targets", label: "Targets", kind: "many", required: true, other: true, choices: [{ value: "mac", title: "macOS" }, { value: "win", title: "Windows" }] },
    { key: "notes", label: "Notes", kind: "text" },
    { key: "draft", label: "Draft first", kind: "flag", required: true },
  ],
};

test("a form is ready once each required field has an answer, and says it in words", () => {
  const draft = emptyDraft();
  expect(formValues(ask, draft, "Yes", "No", ", ").ready).toBe(false);
  draft.picks.targets = togglePick(ask.fields[0], togglePick(ask.fields[0], [], 1), 0);
  draft.other.targets = " Linux ";
  draft.picks.draft = togglePick(ask.fields[2], [], 1);
  const { values, answers, ready } = formValues(ask, draft, "Yes", "No", ", ");
  expect(ready).toBe(true);
  expect(values).toEqual({ targets: ["mac", "win", "Linux"], draft: false });
  expect(answers).toEqual([{ name: "Targets", value: "macOS, Windows, Linux" }, { name: "Draft first", value: "No" }]);
});

test("only a form that asks for one pick sends on its tap", () => {
  const one: AgentAsk = { ...ask, fields: [{ key: "q", label: "Database", kind: "one", other: true, choices: [{ value: "pg", title: "Postgres" }] }] };
  const draft = emptyDraft();
  expect(sendsOnPick(one, draft)).toBe(true);
  draft.other.q = "";
  expect(sendsOnPick(one, draft)).toBe(false);
  expect(sendsOnPick(ask, emptyDraft())).toBe(false);
});
