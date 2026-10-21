// Writes the phone's icon table, mobile/src/ui/lucideIcons.ts, from the desktop app's
// desktop/icons_lucide.go, so both apps draw the same Lucide icon for an SF Symbol name.
// `bun run phone-icons` after an icon is added there; `--check` fails when the table is stale.

import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir, "..");
const go = readFileSync(join(root, "desktop/icons_lucide.go"), "utf8");
const icons = readFileSync(join(root, "desktop/icons.go"), "utf8");

function goMap(name: string): [string, string][] {
  const start = go.indexOf(`var ${name} = map[string]string{`);
  const body = go.slice(start, go.indexOf("\n}\n", start));
  return [...body.matchAll(/^\t"([^"]+)":\s+"((?:[^"\\]|\\.)*)",/gm)].map((m) => [m[1], m[2].replace(/\\"/g, '"')]);
}

const symbols = goMap("symbolIcons");
const shapes = goMap("lucideShapes");
const filled = [...(/filledSymbols = map\[string\]bool\{([^}]*)\}/.exec(icons)?.[1] ?? "").matchAll(/"([^"]+)"/g)].map((m) => m[1]);
const version = /Lucide (\d+\.\d+\.\d+)/.exec(go)?.[1] ?? "";

const out = `// Lucide ${version} (ISC license), the icons the desktop app draws, by the SF Symbol name each
// stands for. Written by \`bun run phone-icons\` from desktop/icons_lucide.go; edit that file.

/** The Lucide icon of each SF Symbol name. */
export const SYMBOL_ICONS: Record<string, string> = ${JSON.stringify(Object.fromEntries(symbols), null, 2)};

/** The elements of each Lucide icon's 24-unit SVG. */
export const LUCIDE_SHAPES: Record<string, string> = ${JSON.stringify(Object.fromEntries(shapes), null, 2)};

/** Symbols SF draws solid, which the stroke icons fill to match. */
export const FILLED_SYMBOLS = new Set(${JSON.stringify(filled)});
`;

const target = join(root, "mobile/src/ui/lucideIcons.ts");
if (process.argv.includes("--check")) {
  if (readFileSync(target, "utf8") !== out) {
    console.error("mobile/src/ui/lucideIcons.ts is stale: run bun run phone-icons");
    process.exit(1);
  }
} else {
  writeFileSync(target, out);
  console.log(`${symbols.length} symbols, ${shapes.length} icons`);
}
