// What the phone makes of a file before it shows one: how the viewer draws it, which file icon it
// wears, the start a card shows, and a table's rows. The desktop app makes the same calls
// (`model.PreviewKindOf`, `fileIconName`, `peekOf` in `desktop/`).

export type PreviewKind = "none" | "image" | "markdown" | "html" | "pdf" | "text" | "table" | "media" | "svg";

const PREVIEW_EXTENSIONS: Record<string, PreviewKind> = {
  png: "image", jpg: "image", jpeg: "image", gif: "image", webp: "image", bmp: "image", heic: "image", heif: "image", avif: "image",
  md: "markdown", markdown: "markdown", mdx: "markdown",
  html: "html", htm: "html", xhtml: "html",
  pdf: "pdf",
  csv: "table", tsv: "table",
  svg: "svg",
  mp3: "media", wav: "media", m4a: "media", aac: "media", flac: "media", ogg: "media", oga: "media", opus: "media",
  mp4: "media", m4v: "media", mov: "media", webm: "media",
};

const TEXT_EXTENSIONS = new Set(
  ("txt log json jsonl yaml yml toml xml css scss less js mjs cjs ts tsx jsx go rs py rb swift java kt c h cc cpp hpp cs php " +
    "lua sh zsh bash ps1 sql diff patch ini conf env properties vue svelte graphql proto dockerfile makefile gradle lock srt vtt tex rst adoc").split(" "),
);

/// Code and data, which the viewer shows unwrapped beside line numbers; other text wraps.
const CODE_EXTENSIONS = new Set(
  ("json jsonl yaml yml toml xml css scss less js mjs cjs ts tsx jsx go rs py rb swift java kt c h cc cpp hpp cs php lua sh zsh " +
    "bash ps1 sql diff patch ini conf env properties vue svelte graphql proto dockerfile makefile gradle lock html htm xhtml svg csv tsv").split(" "),
);

export function extensionOf(name: string): string {
  const dot = name.lastIndexOf(".");
  return (dot > 0 ? name.slice(dot + 1) : name).toLowerCase();
}

/// How a file of this name and media type is shown: its extension decides, then its media type.
export function previewKind(name: string, mime: string): PreviewKind {
  const ext = extensionOf(name);
  if (PREVIEW_EXTENSIONS[ext]) return PREVIEW_EXTENSIONS[ext];
  if (TEXT_EXTENSIONS.has(ext)) return "text";
  if (mime === "image/svg+xml") return "svg";
  if (mime === "text/csv" || mime === "text/tab-separated-values") return "table";
  if (mime.startsWith("audio/") || mime.startsWith("video/")) return "media";
  if (mime.startsWith("image/")) return "image";
  if (mime === "application/pdf") return "pdf";
  if (mime === "text/html") return "html";
  if (mime === "text/markdown") return "markdown";
  if (mime.startsWith("text/") || mime === "application/json" || mime === "application/xml") return "text";
  return "none";
}

/// Whether a file's words are code or data rather than prose.
export function isCode(name: string): boolean {
  return CODE_EXTENSIONS.has(extensionOf(name));
}

const ICON_ALIASES: Record<string, string> = {
  htm: "html", xhtml: "html", tif: "tiff", jsx: "js", mjs: "js", cjs: "js", scss: "css", less: "css",
  jsonl: "json", tsv: "spreadsheets", numbers: "spreadsheets", ods: "spreadsheets",
  md: "document", markdown: "document", mdx: "document", rtf: "document", pages: "document", odt: "document",
  tex: "document", rst: "document", adoc: "document", epub: "document",
  log: "txt", srt: "txt", vtt: "txt", key: "pptx", odp: "pptx",
  bmp: "image", heic: "image", heif: "image", avif: "image", ico: "image", raw: "image",
  m4a: "audio", aac: "audio", flac: "audio", ogg: "audio", oga: "audio", opus: "audio", aiff: "audio",
  mov: "video", m4v: "video", webm: "video", wmv: "video",
  gz: "zip", tgz: "zip", tar: "zip", "7z": "zip", bz2: "zip", xz: "zip",
  pkg: "dmg", msi: "exe", app: "dmg", deb: "exe", rpm: "exe", apk: "exe", ipa: "exe",
  sketch: "fig", xd: "fig", atom: "rss",
};

const ICON_CODE = new Set(
  ("ts tsx go rs py rb swift kt c h cc cpp hpp cs php lua sh zsh bash ps1 diff patch yaml yml toml ini conf env properties vue " +
    "svelte graphql proto dockerfile makefile gradle lock ipynb wasm").split(" "),
);

/// The icon of a file of this name and media type, among `icons`: its extension's own, an
/// alias's, its kind's, or the empty page.
export function fileIconName(name: string, mime: string, icons: ReadonlySet<string>): string {
  const ext = extensionOf(name);
  if (ICON_ALIASES[ext]) return ICON_ALIASES[ext];
  if (ICON_CODE.has(ext)) return "code";
  if (ext && icons.has(ext)) return ext;
  if (mime.startsWith("image/")) return "image";
  if (mime.startsWith("video/")) return "video";
  if (mime.startsWith("audio/")) return "audio";
  if (mime === "application/pdf") return "pdf";
  if (mime === "application/json") return "json";
  if (mime.startsWith("text/")) return "txt";
  return "empty";
}

/// What a card shows of a file: a title and a paragraph for a document, lines for text, rows for a
/// table.
export interface FilePeek {
  title?: string;
  body?: string;
  lines?: string[];
  rows?: string[][];
}

export function peekIsEmpty(peek: FilePeek): boolean {
  return !peek.title && !peek.body && !peek.lines?.length && !peek.rows?.length;
}

const PEEK_LINES = 6;
const PEEK_COLUMNS = 80;

const strip = (s: string) =>
  s
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\*\*|__|`|~~/g, "")
    .trim();

const plainHTML = (s: string) =>
  s
    .replace(/<[^>]*>/g, " ")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&nbsp;/g, " ")
    .replace(/\s+/g, " ")
    .trim();

/// What the card shows of a file that starts with `text`.
export function peekOf(text: string, kind: PreviewKind, name = ""): FilePeek {
  text = text.replace(/\r\n/g, "\n").replace(/\t/g, "    ");
  if (kind === "html") {
    let title = plainHTML(/<title[^>]*>([\s\S]*?)<\/title>/i.exec(text)?.[1] ?? "");
    let body = plainHTML(/<h1[^>]*>([\s\S]*?)<\/h1>/i.exec(text)?.[1] ?? "");
    if (!title) [title, body] = [body, ""];
    if (body === title) body = "";
    return { title: title || undefined, body: body || undefined };
  }
  if (kind === "markdown") {
    let title = "";
    const paragraph: string[] = [];
    let fence = false;
    for (const line of text.split("\n")) {
      const trimmed = line.trim();
      if (trimmed.startsWith("```")) {
        fence = !fence;
        continue;
      }
      if (fence || !trimmed || trimmed === "---") {
        if (paragraph.length && title) break;
        continue;
      }
      const clean = strip(trimmed);
      if (clean.startsWith("#")) {
        if (!title && !paragraph.length) {
          title = clean.replace(/^#+/, "").trim();
          continue;
        }
        break;
      }
      paragraph.push(clean.replace(/^[>\-*+ ]+/, "").trim());
    }
    return { title: title || undefined, body: paragraph.join(" ") || undefined };
  }
  if (kind === "table") {
    const rows = parseTable(text, extensionOf(name) === "tsv", true)
      .slice(0, 5)
      .map((row) => row.slice(0, 4));
    return { rows };
  }
  const lines: string[] = [];
  for (const line of text.split("\n")) {
    if (!lines.length && !line.trim()) continue;
    lines.push([...line.trimEnd()].slice(0, PEEK_COLUMNS).join(""));
    if (lines.length === PEEK_LINES) break;
  }
  return { lines };
}

/// A CSV or TSV file's rows, up to `limit`: quoted fields may hold the separator, quotes, and line
/// breaks. A file cut short loses its last row, which may be cut too.
export function parseTable(text: string, tabs: boolean, truncated: boolean, limit = 1000): string[][] {
  const separator = tabs ? "\t" : ",";
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  let i = 0;
  const end = () => {
    row.push(field);
    field = "";
  };
  while (i < text.length && rows.length < limit) {
    const ch = text[i];
    if (quoted) {
      if (ch === '"' && text[i + 1] === '"') {
        field += '"';
        i += 2;
        continue;
      }
      if (ch === '"') quoted = false;
      else field += ch;
      i++;
      continue;
    }
    if (ch === '"' && field === "") quoted = true;
    else if (ch === separator) end();
    else if (ch === "\n" || ch === "\r") {
      if (ch === "\r" && text[i + 1] === "\n") i++;
      end();
      rows.push(row);
      row = [];
    } else field += ch;
    i++;
  }
  if (rows.length < limit && (field !== "" || row.length)) {
    end();
    if (!truncated) rows.push(row);
  }
  return rows.filter((r) => r.length > 1 || r[0] !== "");
}

/// The numbers of the text's lines, one to a line.
export function lineNumbers(text: string): string {
  const count = text.replace(/\n$/, "").split("\n").length;
  return Array.from({ length: count }, (_, i) => String(i + 1)).join("\n");
}

/// A JSON file that is all on one line, laid out with indents; any other text as it is.
export function layOutJSON(text: string, name: string): string {
  if (extensionOf(name) !== "json" || text.trim().includes("\n")) return text;
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}

/// The start of some bytes as UTF-8 text, a character split by the cut dropped; undefined when
/// they are not text.
export function decodeHead(bytes: Uint8Array): string | undefined {
  for (let cut = 0; cut < 4 && cut <= bytes.length; cut++) {
    try {
      return new TextDecoder("utf-8", { fatal: true }).decode(bytes.subarray(0, bytes.length - cut));
    } catch {}
  }
  return undefined;
}
