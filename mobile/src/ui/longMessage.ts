// Whether a message is longer than a folded bubble shows, and how much of its Markdown to lay
// out while it is folded. The answer comes from the text's characters, not a full layout: laying
// the whole of a long message out is what folding exists to avoid.

/// The height a folded message's text stands before its fade and Show more.
export const COLLAPSED_HEIGHT = 320;
/// The most Markdown, in UTF-16 units, a folded message lays out.
export const PREFIX_CHARS = 1400;
/// The line height the native Markdown view gives its text, over its font size.
const LINE_HEIGHT = 1.42;

/// A message's frame in the window, which the reader grows from.
export interface MessageFrame {
  x: number;
  y: number;
  w: number;
  h: number;
}

/// A character's room in a line: CJK, full-width, and emoji take about two Latin letters.
function room(code: number): number {
  if (
    code >= 0x1100 &&
    (code <= 0x115f ||
      code === 0x2329 ||
      code === 0x232a ||
      (code >= 0x2e80 && code <= 0xa4cf && code !== 0x303f) ||
      (code >= 0xac00 && code <= 0xd7a3) ||
      (code >= 0xf900 && code <= 0xfaff) ||
      (code >= 0xfe30 && code <= 0xfe6f) ||
      (code >= 0xff00 && code <= 0xff60) ||
      (code >= 0xffe0 && code <= 0xffe6) ||
      (code >= 0x1f300 && code <= 0x1faff) ||
      (code >= 0x20000 && code <= 0x3fffd))
  ) {
    return 2;
  }
  return 1;
}

/// The lines `text` wraps to at `width`, estimated from its characters and their room. Paragraph
/// breaks and code lines count as the lines they are.
export function estimatedLines(text: string, width: number, size: number): number {
  const perLine = Math.max(6, Math.floor(width / (size * 0.55)));
  let lines = 0;
  for (const line of text.split("\n")) {
    if (line === "") {
      lines += 1;
      continue;
    }
    let units = 0;
    for (const char of line) units += room(char.codePointAt(0)!);
    lines += Math.max(1, Math.ceil(units / perLine));
  }
  return lines;
}

/// Whether `text` stands taller than `height` at `width` and `size`.
export function isLongMessage(text: string, width: number, size: number, height = COLLAPSED_HEIGHT): boolean {
  return estimatedLines(text, width, size) * size * LINE_HEIGHT > height;
}

/// The head of `text` a folded message lays out: cut at a line where it can be, and a code fence
/// left open closed, so the tail reads as the code it is.
export function collapsedPrefix(text: string, limit = PREFIX_CHARS): string {
  if (text.length <= limit) return text;
  let cut = limit;
  const line = text.lastIndexOf("\n", limit);
  if (line > limit * 0.6) cut = line;
  // Do not split a surrogate pair.
  const code = text.charCodeAt(cut);
  if (code >= 0xdc00 && code <= 0xdfff) cut -= 1;
  let prefix = text.slice(0, cut).replace(/\s+$/, "");
  const fences = prefix.match(/^\s*(?:```|~~~)/gm)?.length ?? 0;
  if (fences % 2 === 1) prefix += "\n```";
  return prefix;
}
