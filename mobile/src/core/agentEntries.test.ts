import { expect, test } from "bun:test";
import { type AgentEntry, mergeAgentEntries, outputText, toolFailed } from "./agentEntries";

const said = (seq: number, text: string): AgentEntry => ({ seq, rev: seq, kind: "said", text });

test("a page of entries lands in order, in place of what it changes, and drops what the Runner no longer keeps", () => {
  let have = mergeAgentEntries([], { text: "", entries: [said(0, "a"), said(1, "b")] });
  have = mergeAgentEntries(have, { text: "", entries: [said(3, "d"), { ...said(1, "b2"), rev: 4 }] });
  expect(have.map((entry) => entry.text)).toEqual(["a", "b2", "d"]);
  have = mergeAgentEntries(have, { text: "", entries: [said(2, "c")], first: 1 });
  expect(have.map((entry) => entry.seq)).toEqual([1, 2, 3]);
  // The same entries again keep the list and each entry as they were.
  const again = mergeAgentEntries(have, { text: "", entries: [{ ...said(2, "c") }] });
  expect(again).toBe(have);
  const changed = mergeAgentEntries(have, { text: "", entries: [{ ...said(3, "d2"), rev: 9 }] });
  expect(changed[0]).toBe(have[0]);
  expect(changed[2].text).toBe("d2");
});

test("a command's output reads as plain text, without its colors or anything else its terminal got", () => {
  expect(outputText("\x1b[32m50%\r100%\x1b[0m\r\n\x1b]52;c;ZXZpbA==\x07next\x07\r\n")).toBe("100%\nnext");
  expect(toolFailed({ title: "x", status: "completed", exit_code: 1 })).toBe(true);
  expect(toolFailed({ title: "x", status: "completed", exit_code: 0 })).toBe(false);
});
