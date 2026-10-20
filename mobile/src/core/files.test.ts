import { describe, expect, test } from "bun:test";
import { decodeHead, fileIconName, layOutJSON, lineNumbers, parseTable, peekIsEmpty, peekOf, previewKind } from "./files";

describe("previewKind", () => {
  test("by extension, then media type", () => {
    expect(previewKind("Guide.md", "application/octet-stream")).toBe("markdown");
    expect(previewKind("page.HTM", "")).toBe("html");
    expect(previewKind("Sales.csv", "")).toBe("table");
    expect(previewKind("logo.svg", "")).toBe("svg");
    expect(previewKind("clip.mov", "")).toBe("media");
    expect(previewKind("main.go", "")).toBe("text");
    expect(previewKind("blob", "image/png")).toBe("image");
    expect(previewKind("blob", "audio/mpeg")).toBe("media");
    expect(previewKind("blob", "application/json")).toBe("text");
    expect(previewKind("archive.zip", "application/zip")).toBe("none");
  });
});

describe("fileIconName", () => {
  const icons = new Set(["html", "pdf", "xlsx", "json", "txt", "zip", "code", "document", "empty"]);
  test("its own, an alias's, its kind's, or the empty page", () => {
    expect(fileIconName("calculator-v1.html", "text/html", icons)).toBe("html");
    expect(fileIconName("Report.PDF", "", icons)).toBe("pdf");
    expect(fileIconName("main.go", "", icons)).toBe("code");
    expect(fileIconName("notes.md", "", icons)).toBe("document");
    expect(fileIconName("run.log", "", icons)).toBe("txt");
    expect(fileIconName("backup.tar.gz", "", icons)).toBe("zip");
    expect(fileIconName("Dockerfile", "", icons)).toBe("code");
    expect(fileIconName("photo", "image/avif", icons)).toBe("image");
    expect(fileIconName("blob", "application/octet-stream", icons)).toBe("empty");
  });
});

describe("peekOf", () => {
  test("a Markdown file's heading and paragraph", () => {
    const peek = peekOf("```\nskip\n```\n# Welcome to **Lorca**\n\nA [guide](https://x.dev) to `pairing`\nyour phone.\n\n## Next\n", "markdown");
    expect(peek).toEqual({ title: "Welcome to Lorca", body: "A guide to pairing your phone." });
  });
  test("an HTML page's title and heading", () => {
    expect(peekOf("<title>Calc &amp; Co</title><h1>A <b>calculator</b></h1>", "html")).toEqual({ title: "Calc & Co", body: "A calculator" });
    expect(peekOf("<h1>Only</h1>", "html")).toEqual({ title: "Only", body: undefined });
  });
  test("a table's first rows and columns", () => {
    expect(peekOf("a,b,c,d,e\n1,2,3,4,5\n", "table", "x.csv").rows).toEqual([["a", "b", "c", "d"], ["1", "2", "3", "4"]]);
  });
  test("text's first lines", () => {
    expect(peekOf("\n\nfunc main() {\n\tprintln(1)\n}\n", "text").lines).toEqual(["func main() {", "    println(1)", "}", ""]);
    expect(peekIsEmpty(peekOf("", "text"))).toBe(true);
  });
});

describe("parseTable", () => {
  test("quoted fields hold separators, quotes, and line breaks", () => {
    expect(parseTable('Step,Note\n"Connect a provider, then a bot","say ""hi""\nthen go"\n', false, false)).toEqual([
      ["Step", "Note"],
      ["Connect a provider, then a bot", 'say "hi"\nthen go'],
    ]);
  });
  test("tabs, a limit, and a cut file", () => {
    expect(parseTable("a\tb\n1\t2\n", true, false)).toEqual([["a", "b"], ["1", "2"]]);
    expect(parseTable("a,b\n1,2\n3,4\n", false, false, 2)).toEqual([["a", "b"], ["1", "2"]]);
    expect(parseTable("a,b\n1,2\n3,", false, true)).toEqual([["a", "b"], ["1", "2"]]);
  });
});

test("lineNumbers and layOutJSON", () => {
  expect(lineNumbers("a\nb\nc\n")).toBe("1\n2\n3");
  expect(layOutJSON('{"a":[1,2]}', "x.json")).toBe('{\n  "a": [\n    1,\n    2\n  ]\n}');
  expect(layOutJSON("{\n}", "x.json")).toBe("{\n}");
  expect(layOutJSON("not json", "x.json")).toBe("not json");
});

test("decodeHead drops a split character and refuses bytes that are not text", () => {
  const bytes = new TextEncoder().encode("héllo");
  expect(decodeHead(bytes.subarray(0, 2))).toBe("h");
  expect(decodeHead(new Uint8Array([0xff, 0xfe, 0x00, 0x80, 0xff]))).toBeUndefined();
});
