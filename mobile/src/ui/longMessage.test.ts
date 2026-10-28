import { expect, test } from "bun:test";
import { collapsedPrefix, COLLAPSED_HEIGHT, estimatedLines, isLongMessage, PREFIX_CHARS } from "./longMessage";

test("a short message is not folded, and a long one at a phone's width is", () => {
  expect(isLongMessage("Hi there.", 300, 16.5)).toBe(false);
  expect(isLongMessage("A line.\n\nAnother line.", 300, 16.5)).toBe(false);
  // About sixty lines of Chinese in a 300 pt bubble, well past the folded height.
  const cjk = "这是一段很长的消息，用来测试折叠。".repeat(40);
  expect(isLongMessage(cjk, 300, 16.5)).toBe(true);
  const latin = "This is a long message that keeps going. ".repeat(80);
  expect(isLongMessage(latin, 300, 16.5)).toBe(true);
  expect(estimatedLines("a\nb\nc", 300, 16.5)).toBe(3);
});

test("a folded message keeps a prefix, cut at a line, with an open code fence closed", () => {
  const paragraphs = Array.from({ length: 120 }, (_, at) => `Paragraph ${at} of a long message.`).join("\n\n");
  const prefix = collapsedPrefix(paragraphs);
  expect(prefix.length).toBeLessThanOrEqual(PREFIX_CHARS);
  expect(prefix.endsWith("Paragraph")).toBe(false);
  expect(prefix.split("\n").at(-1)).toMatch(/^Paragraph \d+ of a long message\.$/);
  // A cut inside a fence closes it, so the rest reads as code rather than as prose.
  const code = "Before.\n\n```ts\n" + "const line = 1;\n".repeat(300);
  const cut = collapsedPrefix(code);
  expect(cut.match(/^\s*```/gm)?.length).toBe(2);
  expect(cut.endsWith("```")).toBe(true);
  // A text shorter than the prefix is its own prefix.
  expect(collapsedPrefix("short")).toBe("short");
});

test("a prefix never splits a surrogate pair", () => {
  const text = "😀".repeat(PREFIX_CHARS);
  const prefix = collapsedPrefix(text);
  expect(prefix.isWellFormed()).toBe(true);
  expect(prefix.length).toBeLessThanOrEqual(PREFIX_CHARS);
  expect(COLLAPSED_HEIGHT).toBe(320);
});
