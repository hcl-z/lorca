// A bot's question with options (`ask_user`), answered on the phone's card: the options picked,
// by their place, and the user's own words, checked as the Runner's `ask::answer` checks them
// (desktop: `model/ask.go`), so the card only sends what the Runner will take.

import type { AskQuestion } from "./model";

export interface AskAnswer {
  choices: number[];
  other?: string;
}

/// One pick sends on its tap; several, or the user's own words, send with Submit.
export function sendsOnPick(ask: AskQuestion): boolean {
  return !ask.multiple;
}

/// The picks after a tap on option `index`: one pick replaces the other, several toggle.
export function togglePick(ask: AskQuestion, picked: number[], index: number): number[] {
  if (!ask.multiple) return [index];
  return picked.includes(index) ? picked.filter((each) => each !== index) : [...picked, index].sort((a, b) => a - b);
}

/// The answer to send, or why there is none yet: picks within the options, one unless several are
/// allowed, the user's words only where the question takes them and, for one pick, instead of an
/// option.
export function askAnswer(ask: AskQuestion, picked: number[], other: string): { answer: AskAnswer } | { error: "nothing" | "invalid" } {
  const words = ask.allow_other ? other.trim() : "";
  const choices = [...new Set(picked)].filter((index) => Number.isInteger(index) && index >= 0 && index < ask.options.length).sort((a, b) => a - b);
  if (choices.length !== picked.length) return { error: "invalid" };
  if (!ask.multiple && choices.length + (words ? 1 : 0) > 1) return { error: "invalid" };
  if (!choices.length && !words) return { error: "nothing" };
  return { answer: words ? { choices, other: words } : { choices } };
}

/// What was picked, in words: the options' labels, then the user's own in quotes.
export function answerWords(ask: AskQuestion, separator: string): string {
  const answer = ask.answer;
  if (!answer) return "";
  const labels = (answer.choices ?? []).map((index) => ask.options[index]?.label).filter((label): label is string => !!label);
  return [...labels, ...(answer.other ? [`“${answer.other}”`] : [])].join(separator);
}
