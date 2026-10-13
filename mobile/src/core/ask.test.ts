import { expect, test } from "bun:test";
import type { AskQuestion } from "./model";
import { answerWords, askAnswer, sendsOnPick, togglePick } from "./ask";

const options = [{ label: "Quick" }, { label: "Thorough", description: "Reads every file" }, { label: "Skip tests" }];

test("one pick sends at once and replaces the last; several toggle in order", () => {
  const one: AskQuestion = { question: "How?", options };
  const several: AskQuestion = { question: "Which?", options, multiple: true };
  expect(sendsOnPick(one)).toBe(true);
  expect(sendsOnPick(several)).toBe(false);
  expect(togglePick(one, [0], 2)).toEqual([2]);
  expect(togglePick(several, [2], 0)).toEqual([0, 2]);
  expect(togglePick(several, [0, 2], 2)).toEqual([0]);
});

test("an answer holds picks within the options and the user's words only where allowed", () => {
  const one: AskQuestion = { question: "How?", options, allow_other: true };
  expect(askAnswer(one, [1], "")).toEqual({ answer: { choices: [1] } });
  expect(askAnswer(one, [], "  my own way ")).toEqual({ answer: { choices: [], other: "my own way" } });
  expect(askAnswer(one, [1], "my own way")).toEqual({ error: "invalid" });
  expect(askAnswer(one, [], "  ")).toEqual({ error: "nothing" });
  expect(askAnswer(one, [7], "")).toEqual({ error: "invalid" });
  const closed: AskQuestion = { question: "How?", options };
  expect(askAnswer(closed, [], "words")).toEqual({ error: "nothing" });
  const several: AskQuestion = { question: "Which?", options, multiple: true, allow_other: true };
  expect(askAnswer(several, [2, 0], "and docs")).toEqual({ answer: { choices: [0, 2], other: "and docs" } });
});

test("what was picked reads as the labels, then the user's words", () => {
  const ask: AskQuestion = { question: "Which?", options, multiple: true, answer: { choices: [0, 2], other: "and docs" } };
  expect(answerWords(ask, ", ")).toBe("Quick, Skip tests, “and docs”");
  expect(answerWords({ question: "?", options }, ", ")).toBe("");
});
