import { expect, mock, test } from "bun:test";

mock.module("../../modules/lorca-core", () => ({ request: async () => ({}) }));
const { byteCount, gauge, isConflict, lineCount } = await import("./memory");

test("lines and bytes are counted the way the Runner loads the file", () => {
  expect(lineCount("")).toBe(0);
  expect(lineCount("a\nb")).toBe(2);
  expect(byteCount("ab")).toBe(2);
  expect(byteCount("记忆")).toBe(6);
  expect(byteCount("🙂")).toBe(4);
});

test("a draft turns orange near its budget and red past it", () => {
  const file = { max_lines: 10, max_bytes: 1000 };
  expect(gauge("a\nb", file).tone).toBe("secondary");
  expect(gauge(Array(8).fill("x").join("\n"), file).tone).toBe("orange");
  const over = gauge(Array(12).fill("x").join("\n"), file);
  expect(over).toMatchObject({ tone: "red", hiddenLines: 2 });
  expect(gauge("x".repeat(1001), file)).toMatchObject({ tone: "red", hiddenLines: 0 });
});

test("a write the Runner refused because the file changed reads as a conflict", () => {
  expect(isConflict(new Error("The file changed since you opened it. Reload it and apply only your change."))).toBe(true);
  expect(isConflict(new Error("The Runner is offline"))).toBe(false);
});
