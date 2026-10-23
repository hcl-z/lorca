import { expect, mock, test } from "bun:test";

// The sizes Markdown measured, kept across launches: what a stored key holds and when the native
// side is asked again.
let measures = 0;
const written: string[] = [];
mock.module("expo-modules-core", () => ({
  requireNativeModule: () => ({ measure: () => (measures++, { width: 10, height: 20 }), sourceHosts: () => [], setFavicon: () => {} }),
  requireNativeViewManager: () => () => null,
}));
mock.module("react-native", () => ({ PixelRatio: { getFontScale: () => 1 } }));
mock.module("expo-application", () => ({ nativeApplicationVersion: "1.0.0", nativeBuildVersion: "1" }));
mock.module("expo-file-system", () => ({
  Paths: { cache: "/cache" },
  File: class {
    exists = false;
    textSync() { return "[]"; }
    write(text: string) { written.push(text); }
  },
}));
const { measureMarkdown, sizeKey } = await import("../../modules/lorca-core/MarkdownView");

test("a key names the text without holding it, and tells texts apart", () => {
  expect(sizeKey("hello")).toBe(sizeKey("hello"));
  expect(sizeKey("hello")).not.toBe(sizeKey("hellp"));
  expect(sizeKey("ab")).not.toBe(sizeKey("ba"));
  expect(sizeKey("a secret".repeat(50))).not.toContain("secret");
});

test("a text measured once is not measured again at the same width", () => {
  expect(measureMarkdown("**hi**", 300, 16, 14, false)).toEqual({ width: 10, height: 20 });
  measureMarkdown("**hi**", 300, 16, 14, false);
  expect(measures).toBe(1);
  measureMarkdown("**hi**", 280, 16, 14, false);
  expect(measures).toBe(2);
});
